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
	st := executionledger.State{
		Col: ledgerColumn(s.Entry.State), Seq: uint64(len(l.chain)), Rev: s.Revision, Cycle: uint64(claimLines(s.Text)),
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
		st.Record, st.RecRev = uint64(slices.Index(k.ids, cur.ID)+1), cur.Revision
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
// values, so an entrance can add a record, a link, and a revision.
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

// ledgerStep projects the card and runs one kernel event on it.
func ledgerStep(s Snapshot, l executionLedger, e executionledger.Event) (bool, error) {
	k, err := projectLedger(s, l)
	if err != nil {
		return false, err
	}
	return k.step(e), nil
}
