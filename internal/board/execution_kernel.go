package board

import (
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
	// revBase shifts card revisions into the kernel's uint32 parameters; 0
	// below 2^31, where the projection is the revision itself.
	revBase uint64
}

// exactBelow is the bound below which the projection keeps counters and
// revisions as they are. Above it, the kernel's uint32 parameters need a shift.
const exactBelow = 1 << 31

// kernelCounter keeps a record position, a transition count, or a claim count
// within the kernel's parameters. A decision only compares a fence with the
// record position and needs a claim count of at least one, so every value from
// 2^31 on projects to 1 without changing a decision.
func kernelCounter(c uint64) uint64 {
	if c < exactBelow {
		return c
	}
	return 1
}

// at maps a card revision with the shift of the projection. Revisions within
// 2^30 below the card's keep their order and equality; an older one maps to 0,
// below every revision of that window.
func (k *ledgerKernel) at(rev uint64) uint64 {
	if k.revBase == 0 {
		return rev
	}
	if rev < k.revBase {
		return 0
	}
	return rev - k.revBase + 1
}

// projectLedger is the kernel state of a card and its ledger. A record
// position counts the records the chain created; the claim count is the number
// of claim lines of LIFECYCLE_DECISION.
func projectLedger(s Snapshot, l executionLedger) (*ledgerKernel, error) {
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
	if s.Revision >= exactBelow {
		k.revBase = s.Revision - 1<<30
	}
	st := executionledger.State{
		Col: ledgerColumn(s.Entry.State), Seq: kernelCounter(seq), Rev: k.at(s.Revision), Cycle: kernelCounter(uint64(claimLines(s.Text))),
	}
	if cur := l.current; cur != nil {
		if !slices.Contains(k.ids, cur.ID) {
			k.ids = append(k.ids, cur.ID)
		}
		var ok bool
		if st.Kind, ok = kernelKinds[cur.Kind]; !ok {
			return nil, coordinatorError("execution record kind")
		}
		if st.Status, ok = kernelStatuses[cur.Status]; !ok {
			return nil, coordinatorError("execution record status")
		}
		st.Record, st.RecRev = kernelCounter(uint64(slices.Index(k.ids, cur.ID)+1)), k.at(cur.Revision)
		// History before the first stored link is not a record that holds the
		// card: a new claim of a legacy card in backlog or todo never waits for it.
		if len(l.chain) == l.synthesized && st.Col == executionledger.ColTodo {
			st.Status = executionledger.StatusRolledBack
		}
	}
	k.state = st
	k.params = ledgerParams(st)
	return k, nil
}

// ledgerParams bounds the counters of one operation: two above the projected
// values, so an entrance can add a record, a link, and a revision. Projected
// values stay below 2^31, so the bounds fit the kernel's parameters.
func ledgerParams(st executionledger.State) executionledger.Params {
	return executionledger.Params{MaxSeq: uint32(max(st.Record, st.Seq, st.Cycle) + 2), MaxRev: uint32(max(st.Rev, st.RecRev) + 2)}
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
