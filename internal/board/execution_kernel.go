package board

import (
	"math"
	"slices"

	"github.com/dualface/kander/internal/statemachine/executionledger"
)

// The execution ledger decisions of a card are made by the generated kernel in
// internal/statemachine/executionledger
// (formal/KanderFormal/Machines/ExecutionLedger.lean). This file projects a
// card and its ledger into a kernel state and runs kernel events. See
// docs/formal-verification.md.

var kernelStatuses = map[string]executionledger.Status{
	"": executionledger.StatusNone, ExecutionPending: executionledger.StatusPending,
	ExecutionRunning: executionledger.StatusRunning, ExecutionUnresolved: executionledger.StatusUnresolved,
	ExecutionRolledBack: executionledger.StatusRolledBack, ExecutionHandedOff: executionledger.StatusHandedOff,
}

var kernelKinds = map[string]executionledger.Kind{
	"": executionledger.KindNone, ExecutionStart: executionledger.KindStart, ExecutionClaim: executionledger.KindClaim,
	ExecutionTakeover: executionledger.KindTakeover, ExecutionReclaim: executionledger.KindReclaim,
	ExecutionRecover: executionledger.KindRecover,
}

// ledgerColumn maps a board state to the ledger's view of the column.
func ledgerColumn(state string) executionledger.Col {
	switch state {
	case "working":
		return executionledger.ColWorking
	case "review":
		return executionledger.ColReview
	case "done", "archived", "trash":
		return executionledger.ColClosed
	}
	return executionledger.ColTodo
}

// ledgerKernel is the projected ledger of one card in one operation. ids lists
// the records of the chain in the order they were created; a record's position
// in it, from 1, is its kernel identity.
type ledgerKernel struct {
	state  executionledger.State
	params executionledger.Params
	ids    []string
	base   ledgerBase
}

// exactBelow is the bound below which the projection keeps counters and
// revisions as they are. From it on, the kernel's uint32 parameters need a
// shift.
const exactBelow = 1 << 31

// ledgerBase holds the shifts of one operation: one base per counter and one
// for card revisions, 0 where the projected value is the value itself. The
// kernel decides only by equality, order, and steps of one, which a common
// shift keeps, so an operation fixes its bases from the state before it and
// maps the state, the event arguments, and the result with them.
type ledgerBase struct {
	record, seq, cycle, rev uint64
}

// counterBase shifts a counter from 2^31 on so that it projects to 1.
func counterBase(c uint64) uint64 {
	if c < exactBelow {
		return 0
	}
	return c
}

// revisionBase shifts a card revision from 2^31 on so that the 2^30 revisions
// below it keep their order and equality.
func revisionBase(rev uint64) uint64 {
	if rev < exactBelow {
		return 0
	}
	return rev - 1<<30
}

// shift maps v with base: unchanged without a base, else its distance from the
// base plus one, and 0 for a value below the base.
func shift(v, base uint64) uint64 {
	switch {
	case base == 0:
		return v
	case v < base:
		return 0
	}
	return v - base + 1
}

// at maps a card revision of an event argument with the operation's base.
func (k *ledgerKernel) at(rev uint64) uint64 { return shift(rev, k.base.rev) }

// projectLedger is the kernel state of a card and its ledger, with the bases
// the state itself fixes.
func projectLedger(s Snapshot, l executionLedger) (*ledgerKernel, error) {
	return projectLedgerAt(s, l, nil)
}

// projectLedgerAt is the kernel state of a card and its ledger. A record
// position counts the records the chain created; the claim count is the number
// of claim lines of LIFECYCLE_DECISION. base, when given, is the operation's
// shift; otherwise the state fixes it.
func projectLedgerAt(s Snapshot, l executionLedger, base *ledgerBase) (*ledgerKernel, error) {
	k := &ledgerKernel{}
	for _, t := range l.chain {
		if t.FromRecord != "" && !slices.Contains(k.ids, t.FromRecord) {
			k.ids = append(k.ids, t.FromRecord)
		}
		if t.ToRecord != "" && !slices.Contains(k.ids, t.ToRecord) {
			k.ids = append(k.ids, t.ToRecord)
		}
	}
	seq := uint64(len(l.chain))
	// An old takeover's handoff link is stored by the first write after it; it
	// already happened, so the projection counts it.
	if l.legacyTakeover() {
		seq++
	}
	var record, recRev uint64
	cur := l.current
	if cur != nil {
		if !slices.Contains(k.ids, cur.ID) {
			k.ids = append(k.ids, cur.ID)
		}
		record, recRev = uint64(slices.Index(k.ids, cur.ID)+1), cur.Revision
	}
	cycle := uint64(claimLines(s.Text))
	if base != nil {
		k.base = *base
	} else {
		k.base = ledgerBase{record: counterBase(record), seq: counterBase(seq), cycle: counterBase(cycle), rev: revisionBase(s.Revision)}
	}
	st := executionledger.State{
		Col: ledgerColumn(s.Entry.State), Seq: shift(seq, k.base.seq), Rev: k.at(s.Revision), Cycle: shift(cycle, k.base.cycle),
	}
	if cur != nil {
		var ok bool
		if st.Kind, ok = kernelKinds[cur.Kind]; !ok {
			return nil, coordinatorError("execution record kind")
		}
		if st.Status, ok = kernelStatuses[cur.Status]; !ok {
			return nil, coordinatorError("execution record status")
		}
		st.Record, st.RecRev = shift(record, k.base.record), k.at(recRev)
		// History before the first stored link is not a record that holds the
		// card: a new claim of a legacy card in backlog or todo never waits for it.
		if len(l.chain) == l.synthesized && st.Col == executionledger.ColTodo {
			st.Status = executionledger.StatusRolledBack
		}
	}
	k.state = st
	var ok bool
	if k.params, ok = ledgerParams(st); !ok {
		return nil, coordinatorError("execution ledger out of range")
	}
	return k, nil
}

// ledgerParams bounds the counters of one operation: two above the projected
// values, so an entrance can add a record, a link, and a revision. A state
// projected with its own bases always fits.
func ledgerParams(st executionledger.State) (executionledger.Params, bool) {
	top := max(st.Record, st.Seq, st.Cycle) + 2
	rev := max(st.Rev, st.RecRev) + 2
	if top > math.MaxUint32 || rev > math.MaxUint32 {
		return executionledger.Params{}, false
	}
	return executionledger.Params{MaxSeq: uint32(top), MaxRev: uint32(rev)}, true
}

// fence is the kernel identity of the record a cursor is bound to, or 0 when
// that record is not the card's current record of the card's cycle.
func (k *ledgerKernel) fence(l executionLedger, s Snapshot, f ExecutionFence) uint64 {
	cur := l.current
	if cur == nil || f.RecordID == "" || cur.ID != f.RecordID || cur.Cycle != f.Cycle || cur.Cycle != planCycle(s) {
		return 0
	}
	return k.state.Record
}

// step runs one kernel event and keeps the accepted state.
func (k *ledgerKernel) step(e executionledger.Event) bool {
	next, ok := executionledger.Step(k.state, e, k.params)
	if ok {
		k.state = next
	}
	return ok
}

// ledgerStep projects the card and runs the kernel event event builds from the
// projection.
func ledgerStep(s Snapshot, l executionLedger, event func(k *ledgerKernel) executionledger.Event) (bool, error) {
	k, err := projectLedger(s, l)
	if err != nil {
		return false, err
	}
	return k.step(event(k)), nil
}
