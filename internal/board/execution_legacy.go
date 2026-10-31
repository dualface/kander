package board

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"

	"github.com/dualface/kander/internal/fs"
)

// legacyRecordID is a stable identity for a record that exists only in memory.
func legacyRecordID(task string, seq int) string {
	return "legacy-" + ReviewDigest([]byte(task + "\n" + strconv.Itoa(seq)))[:16]
}

// legacyAttempts returns the schema 1 start attempts of a task, oldest first. Each
// retry must name a rolled-back predecessor, as the old lineage check required.
func legacyAttempts(tx *Transaction, task string) ([]ExecutionRecord, error) {
	entries, err := fs.ListDirectory(tx.root, control(tx.root, "groups", taskStartGroup, task))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byPrevious := map[string]ExecutionRecord{}
	for _, entry := range entries {
		if entry.Name == "current.json" || entry.Name == "transitions" {
			continue
		}
		pending, ok, e := readRecordJSON(tx, executionPath(task, entry.Name, "pending"))
		if e != nil {
			return nil, e
		}
		if !ok || pending.Schema != 0 {
			continue
		}
		if _, dup := byPrevious[pending.PreviousID]; dup {
			return nil, coordinatorError("start lineage cycle")
		}
		byPrevious[pending.PreviousID] = pending
	}
	var ordered []ExecutionRecord
	for next, ok := byPrevious[""]; ok; next, ok = byPrevious[next.ID] {
		result, exists, e := readRecordJSON(tx, executionPath(task, next.ID, "result"))
		if e != nil {
			return nil, e
		}
		// A result of this binary has its own stored link: an adoption or a
		// rollback of a legacy attempt continues the chain, it does not extend
		// the prefix that the stored links were numbered after.
		if exists && result.Schema == 0 {
			next = result
		}
		ordered = append(ordered, next)
	}
	if len(ordered) != len(byPrevious) {
		return nil, coordinatorError("start retry lacks rollback original")
	}
	for i := 1; i < len(ordered); i++ {
		if p := ordered[i-1]; p.Status != ExecutionRolledBack || p.FinishedRevision >= ordered[i].Revision {
			return nil, coordinatorError("start retry lacks rollback original")
		}
	}
	return ordered, nil
}

// legacyHandoffs returns the protected reclaim records no stored transition
// carries: a reclaim of the ledger era has its own link.
func legacyHandoffs(s Snapshot, stored []ExecutionTransition) []lifecycleHandoff {
	var result []lifecycleHandoff
	for _, line := range lifecycleJSONLines(s.Text) {
		var h lifecycleHandoff
		if json.Unmarshal([]byte(line), &h) != nil || h.Kind != "reclaim" {
			continue
		}
		if !slices.ContainsFunc(stored, func(t ExecutionTransition) bool { return t.Kind == TransitionReclaim && t.ToCycle == h.NewCycle }) {
			result = append(result, h)
		}
	}
	return result
}

// synthesizeChain derives the history of a card that predates the ledger from
// start originals and protected handoffs. The result is never written back.
// stored holds the transitions on disk: the oldest one fixes where the prefix
// ends, because the writer that created it read this same prefix.
func synthesizeChain(tx *Transaction, s Snapshot, stored []ExecutionTransition) ([]ExecutionTransition, error) {
	task := s.Entry.TaskID
	attempts, err := legacyAttempts(tx, task)
	if err != nil {
		return nil, err
	}
	handoffs := legacyHandoffs(s, stored)
	var chain []ExecutionTransition
	cycle, record := emptyCycle(task), ""
	add := func(kind, to, toCycle, owner, session string, revision uint64) {
		chain = append(chain, ExecutionTransition{Schema: transitionSchema, Seq: uint64(len(chain)) + 1, TaskID: task, Kind: kind, FromRecord: record, ToRecord: to, FromCycle: cycle, ToCycle: toCycle, NewOwner: owner, NewSession: session, CardRevision: revision})
		cycle, record = toCycle, to
	}
	active := false
	for _, a := range attempts {
		add(TransitionStart, a.ID, a.Cycle, a.Owner, a.Session, a.Revision)
		active = a.Status != ExecutionRolledBack
		if a.Status == ExecutionRunning {
			add(TransitionConfirm, a.ID, a.Cycle, a.Owner, a.Session, a.FinishedRevision)
		} else if a.Status == ExecutionRolledBack {
			add(TransitionRollback, a.ID, emptyCycle(task), "", "", a.FinishedRevision)
		}
	}
	// The first stored link froze the card facts these links read: a later change
	// of the card must not rewrite the history the stored links continue.
	owner, session, _ := cardExecutionFacts(s)
	revision := s.Revision
	if len(stored) > 0 {
		owner, session, revision = stored[0].OldOwner, stored[0].OldSession, stored[0].CardRevision
	}
	for i, h := range handoffs {
		if i == 0 && cycle != h.OldCycle {
			add(TransitionClaim, legacyRecordID(task, len(chain)), h.OldCycle, owner, session, h.CardRevision)
		}
		if h.OldCycle != cycle {
			return nil, coordinatorError("execution cycle changed without lifecycle handoff")
		}
		add(TransitionReclaim, legacyRecordID(task, len(chain)), h.NewCycle, h.Owner, session, h.CardRevision)
	}
	if active || len(handoffs) > 0 {
		return chain, nil
	}
	// A manual claim left no record but the claim line. It sits between the last
	// rollback and the first stored transition, or at the end of a started card.
	end, started := planCycle(s), MetadataFrom(s.Text, FieldStartedAt) != "" || s.Entry.State == "working" || s.Entry.State == "review"
	if len(stored) > 0 {
		end, started = stored[0].FromCycle, stored[0].FromRecord != record
	}
	if started {
		add(TransitionClaim, legacyRecordID(task, len(chain)), end, owner, session, revision)
	}
	return chain, nil
}

// synthesizeCurrent derives the current record of a card with no stored
// transition. A running execution reads its facts from the card: the old
// takeover and session writes left no other record of them. An old takeover that
// changed OWNER reads as a record of its own, because OWNER is part of the identity
// the immutable originals of the attempt fix. Its ID derives from the chain, so
// every read and the first write name the same record.
func synthesizeCurrent(s Snapshot, stored *ExecutionRecord, chain []ExecutionTransition) *ExecutionRecord {
	if len(chain) == 0 {
		return stored
	}
	last := chain[len(chain)-1]
	owner, session, window := cardExecutionFacts(s)
	if stored != nil && last.ToRecord == stored.ID && (stored.Status == ExecutionPending || stored.Status == ExecutionRolledBack) {
		r := *stored
		r.Window = window
		return &r
	}
	r := ExecutionRecord{Schema: executionSchema, ID: last.ToRecord, TaskID: s.Entry.TaskID, Cycle: last.ToCycle, Kind: ExecutionClaim, Status: ExecutionRunning, Owner: owner, Session: session, Window: window, Revision: max(last.CardRevision, 1)}
	switch {
	case stored != nil && last.ToRecord == stored.ID && stored.Owner != owner:
		r.ID, r.Kind, r.FinishedRevision = legacyRecordID(s.Entry.TaskID, len(chain)), ExecutionTakeover, r.Revision
	case stored != nil && last.ToRecord == stored.ID:
		r.Kind, r.Revision, r.PreviousID, r.FinishedRevision = ExecutionStart, stored.Revision, stored.PreviousID, stored.FinishedRevision
	case last.Kind == TransitionReclaim:
		r.Kind = ExecutionReclaim
	case session != "":
		// A card without any start record but with a session comes from the
		// time before start attempts existed; only a launcher writes SESSION.
		r.Kind = ExecutionStart
	}
	if stored != nil && r.ID != stored.ID {
		r.PreviousID = stored.ID
	}
	return &r
}

// storeLegacyTakeover stores the originals of an old takeover before the first
// write on its card, with the handoff link the old binary never wrote. The caller
// moves the pointer. The originals of the attempt stay as they are. The link is a
// stored one on purpose: a synthesized link would read the card, so the prefix
// would change length with OWNER, and the stored links are numbered after the
// prefix.
func storeLegacyTakeover(tx *Transaction, s Snapshot, l *executionLedger) error {
	if !l.legacyTakeover() {
		return nil
	}
	cur := l.current
	attempt, ok, err := readRecordJSON(tx, executionPath(l.task, cur.PreviousID, statusOriginal(cur.Status)))
	if err != nil {
		return err
	}
	if !ok {
		return coordinatorError("start attempt original")
	}
	retired := attempt
	retired.Schema, retired.Status, retired.FinishedRevision = executionSchema, ExecutionHandedOff, s.Revision
	if err = putRetiredOriginal(tx, retired, statusOriginal(ExecutionHandedOff)); err != nil {
		return err
	}
	if _, err = putRecordOriginals(tx, *cur); err != nil {
		return err
	}
	t := transitionFrom(TransitionHandoff, s, cur).onChain(*l).withCard(s, s.Text, s.Revision)
	t.OldOwner, t.OldSession, t.OldWindow, t.FromStatus = attempt.Owner, attempt.Session, "", attempt.Status
	return putTransition(tx, l, t)
}
