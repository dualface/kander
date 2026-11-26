package server

import (
	"math"

	sk "github.com/dualface/kander/v2/internal/statemachine/issueresultsave"
)

// The shell of the issue result resource kernel
// (formal/KanderFormal/Machines/IssueResultSave.lean). The kernel decides the
// revision compare-and-swap of the central result record: the request's work
// digest is current, it expects the stored revision, the revision has room, the
// new store passes the update and work relations, and the transaction guard
// holds. The kernel parameter is the largest `uint32`, so a revision from 2^31
// on is shifted: the kernel only compares a revision with the one the request
// expects and adds one, and it sees values that are equal exactly when the real
// revisions are.
var issueSaveParams = sk.Params{MaxRev: math.MaxUint32}

// issueRevision is the kernel view of the stored revision of one result record.
type issueRevision struct {
	actual uint64
	state  sk.State
}

func newIssueRevision(actual uint64) issueRevision {
	shift := uint64(0)
	if actual >= 1<<31 {
		shift = actual - 1<<31
	}
	return issueRevision{actual: actual, state: sk.State{Rev: actual - shift}}
}

// read asks whether a stored record may be used: a record that exists has a
// resource revision.
func (r issueRevision) read(found bool) bool {
	_, ok := sk.Step(r.state, sk.Read{Found: found}, issueSaveParams)
	return ok
}

// save asks whether a save of the revision `requested` is accepted, with the
// facts the shell computed, and returns the revision it writes.
func (r issueRevision) save(requested uint64, work, upd, tx bool) (uint64, bool) {
	expected := r.state.Rev
	if requested != r.actual {
		expected++
	}
	next, ok := sk.Step(r.state, sk.Save{Work: work, Expected: expected, Room: r.actual != ^uint64(0), Upd: upd, Tx: tx}, issueSaveParams)
	return r.actual + (next.Rev - r.state.Rev), ok
}
