package board

import (
	"context"
	"slices"
)

// coordinatorWalk is what one member's chain says since its mirror was written.
type coordinatorWalk struct {
	cycle    string
	awaiting bool
	attempt  string
	seq      uint64
	segment  []ExecutionTransition
}

// startAttempt names the newest launcher record of the chain. A reclaim is not
// one: it neither starts nor replaces a launcher.
func (l executionLedger) startAttempt() string {
	for _, t := range slices.Backward(l.chain) {
		if t.Kind == TransitionStart || t.Kind == TransitionHandoff {
			return t.ToRecord
		}
	}
	return ""
}

// coordinatorChainPosition finds where a mirror sits on the chain. A schema 1
// checkpoint has no position, so its facts select one. No match is the same
// refusal the snapshot comparison gave.
func coordinatorChainPosition(l executionLedger, old CoordinatorMember) (uint64, error) {
	if old.TransitionSeq != 0 {
		if old.TransitionSeq > l.end() {
			return 0, coordinatorError("execution chain regressed: " + l.task)
		}
		if !old.AwaitingStart && l.chain[old.TransitionSeq-1].ToCycle != old.Cycle {
			return 0, coordinatorError("execution cycle changed without lifecycle handoff")
		}
		return old.TransitionSeq, nil
	}
	if old.StartAttempt != "" && !slices.ContainsFunc(l.chain, func(t ExecutionTransition) bool { return t.ToRecord == old.StartAttempt }) {
		return 0, coordinatorError("observed start missing from retry lineage")
	}
	if old.AwaitingStart || old.Cycle == emptyCycle(l.task) {
		for i, t := range l.chain {
			if t.ToRecord == old.StartAttempt {
				return uint64(i), nil
			}
		}
		return 0, nil
	}
	for i, t := range slices.Backward(l.chain) {
		if t.ToCycle == old.Cycle {
			return uint64(i) + 1, nil
		}
	}
	return 0, coordinatorError("execution cycle changed without lifecycle handoff")
}

// verifyChainOriginals requires the immutable original each stored link
// published. A link without its original is damage, not a shorter history.
func verifyChainOriginals(tx *Transaction, l executionLedger) error {
	for _, t := range l.chain[l.synthesized:] {
		record, name := t.ToRecord, "result"
		switch t.Kind {
		case TransitionStart:
			name = "pending"
		case TransitionHandoff:
			name = "pending"
			if t.FromRecord == "" {
				return coordinatorError("start attempt original")
			}
			if _, ok, err := readRecordJSON(tx, executionPath(l.task, t.FromRecord, statusOriginal(ExecutionHandedOff))); err != nil {
				return err
			} else if !ok {
				return coordinatorError("start attempt original")
			}
		case TransitionRollback:
			record = t.FromRecord
		case TransitionUnresolve:
			name = statusOriginal(ExecutionUnresolved)
		case TransitionConfirm, TransitionAdopt, TransitionClaim, TransitionReclaim:
		default:
			continue
		}
		if _, ok, err := readRecordJSON(tx, executionPath(l.task, record, name)); err != nil {
			return err
		} else if !ok {
			return coordinatorError("start attempt original")
		}
	}
	return nil
}

// coordinatorWalkChain replaces the rules that guessed transitions from
// snapshots. Producers record every change, so any change the chain carries is
// legal however many reconciles it skipped, and the card must end where the chain
// ends. A missed reconcile can no longer strand a member.
func coordinatorWalkChain(ctx context.Context, tx *Transaction, s Snapshot, old CoordinatorMember) (l executionLedger, w coordinatorWalk, err error) {
	if err = ctx.Err(); err != nil {
		return l, w, err
	}
	if l, err = readExecutionLedger(tx, s); err != nil {
		return l, w, err
	}
	if err = l.assertCard(s); err != nil {
		return l, w, err
	}
	if err = verifyChainOriginals(tx, l); err != nil {
		return l, w, err
	}
	position, err := coordinatorChainPosition(l, old)
	if err != nil {
		return l, w, err
	}
	w = coordinatorWalk{cycle: planCycle(s), attempt: l.startAttempt(), seq: l.end(), segment: l.chain[position:]}
	for _, t := range w.segment {
		// A checkpoint from before the ledger can hold the cycle of a start that
		// was only ever pending. Only the exact rollback revokes such a cursor.
		if t.Kind == TransitionRollback && t.FromCycle == old.Cycle && t.ToCycle != old.Cycle && !old.AwaitingStart && (old.Review != nil || old.Dispatch != nil || old.DeliveryCommit != "") {
			return l, w, coordinatorError("rollback cannot replace confirmed cycle")
		}
	}
	empty := emptyCycle(l.task)
	cur := l.current
	switch {
	case cur == nil:
		w.cycle, w.awaiting = empty, true
	case cur.ExecutionTerminal():
		w.cycle, w.awaiting = empty, true
	case cur.Status == ExecutionPending || cur.Status == ExecutionUnresolved:
		// Metadata is only an attempt: the mirror keeps the cycle it confirmed,
		// and a first start keeps the unbound cycle until a durable result exists.
		w.awaiting = true
		if old.Cycle != cur.Cycle || old.AwaitingStart {
			w.cycle = empty
		}
	}
	return l, w, nil
}

// reclaimHandoffs collects the protected records of the reclaims a walk crossed.
func reclaimHandoffs(tx *Transaction, s Snapshot, w coordinatorWalk) ([]lifecycleHandoff, error) {
	at := slices.IndexFunc(w.segment, func(t ExecutionTransition) bool { return t.Kind == TransitionReclaim })
	if at < 0 {
		return nil, nil
	}
	return lifecycleHandoffs(tx, s, w.segment[at].FromCycle)
}

// reconcileTerminalMember is the branch of a member that ended in archived or
// trash. No later round can start there, so it checks only the result field and
// the chain. Start facts and dispatch rounds no longer matter. Archive cleanup may
// have removed the records of a card; its receipt stands in for them.
func reconcileTerminalMember(ctx context.Context, tx *Transaction, s Snapshot, old CoordinatorMember, o CoordinatorObservation) (m CoordinatorMember, err error) {
	id := s.Entry.TaskID
	if MetadataFrom(s.Text, FieldResult) == "" {
		return m, coordinatorError("terminal member has no result: " + id)
	}
	if authFrom(s.Text) != (ExecutionAuthorization{DispatchID: o.DispatchID, Epoch: o.Epoch}) {
		return m, coordinatorError("wrong dispatch round: " + id)
	}
	if o.DeliveryCommit != "" && o.DeliveryCommit != old.DeliveryCommit {
		return m, coordinatorError("delivery has no completed receipt: " + id)
	}
	m = old
	m.State, m.Revision, m.AwaitingStart = s.Entry.State, s.Revision, false
	if s.Entry.State == "archived" {
		receipt, e := readPruneReceipt(tx.root, id)
		if e != nil || receipt.TaskID == id {
			return m, e
		}
	}
	l, w, err := coordinatorWalkChain(ctx, tx, s, old)
	if err != nil {
		return m, err
	}
	handoffs, err := reclaimHandoffs(tx, s, w)
	if err != nil {
		return m, err
	}
	m.Cycle, m.StartAttempt, m.TransitionSeq = planCycle(s), w.attempt, l.end()
	if m.Cycle != old.Cycle {
		m.DeliveryCommit = ""
	}
	m.Handoffs = append(slices.Clone(old.Handoffs), handoffs...)
	// The mirror keeps the last round as a record. Nothing here validates it.
	rounds, err := dispatchRounds(tx, s)
	if err != nil || len(rounds) == 0 {
		return m, err
	}
	last := rounds[len(rounds)-1]
	m.Dispatch, m.DispatchSeq = mirrorDispatch(id, last), uint64(len(rounds))
	if last.Completed != nil && (last.Cycle == "" || last.Cycle == m.Cycle) {
		m.DeliveryCommit = last.Completed.DeliveryCommit
	}
	return m, nil
}

// coordinatorAdvanceRounds walks the card's rounds from the mirror up to, but not
// including, the bound round. Every walked round must be over: released, settled,
// or replaced by the round after it. A round no reconcile saw counts the same.
func coordinatorAdvanceRounds(tx *Transaction, s Snapshot, m *CoordinatorMember, old CoordinatorMember, rounds []Dispatch, bound string) error {
	id := s.Entry.TaskID
	position := int(old.DispatchSeq)
	if old.Dispatch != nil {
		at := slices.IndexFunc(rounds, func(d Dispatch) bool { return d.Input.ID == old.Dispatch.ID })
		if at < 0 {
			return coordinatorError("unbound member lacks dispatch termination and release originals")
		}
		if d := rounds[at]; d.Input.Base != old.Dispatch.Base || d.Input.Kind != old.Dispatch.Kind {
			return coordinatorError("released dispatch facts changed")
		}
		if err := coordinatorDispatchAdvance(tx, id, *old.Dispatch, rounds[at]); err != nil {
			return err
		}
		position = at
	}
	if position > len(rounds) {
		return coordinatorError("dispatch chain regressed: " + id)
	}
	for i := position; i < len(rounds); i++ {
		d := rounds[i]
		if d.Input.ID == bound {
			if i != len(rounds)-1 {
				return coordinatorError("unfinished previous dispatch: " + id)
			}
			break
		}
		switch {
		case d.Release != nil:
			if err := verifyCardRelease(tx, s, *d.Release); err != nil {
				return err
			}
			m.ReleasedDispatches = appendReference(m.ReleasedDispatches, ArtifactReference{id, dispatchPath(d.Input.ID, "release")})
		case d.Settlement != nil:
			m.ReleasedDispatches = appendReference(m.ReleasedDispatches, ArtifactReference{id, dispatchPath(d.Input.ID, "settlement")})
		case i+1 < len(rounds) && coordinatorDispatchTerminal(d.State):
		default:
			return coordinatorError("unbound member lacks dispatch termination and release originals")
		}
		if d.Completed != nil && d.Cycle == m.Cycle {
			m.DeliveryCommit = d.Completed.DeliveryCommit
		}
	}
	// An open round is not passed yet, so the next walk starts at it.
	m.DispatchSeq = uint64(len(rounds))
	if bound != "" {
		m.DispatchSeq--
	}
	return nil
}

func appendReference(refs []ArtifactReference, ref ArtifactReference) []ArtifactReference {
	if slices.Contains(refs, ref) {
		return refs
	}
	return append(refs, ref)
}
