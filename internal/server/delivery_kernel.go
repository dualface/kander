package server

import (
	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/statemachine/serverdelivery"
)

// The delivery decisions are made by the generated kernel in
// internal/statemachine/serverdelivery
// (formal/KanderFormal/Machines/ServerDelivery.lean). This file projects the
// record of one commit into a kernel state and maps the facts of an operation to
// kernel arguments; deliveries.go selects the error of a refused operation. See
// docs/formal-verification.md.

// deliveryFrame fixes how the uint64 generations and revisions of one operation
// map into the kernel's range: the kernel only compares them with the holder's
// generation and the card's revision, so a value maps to 0 for zero, 1 at the
// operation's value, and 2 elsewhere.
type deliveryFrame struct {
	gen, card uint64
}

func (f deliveryFrame) generation(g uint64) uint64 { return bucket(g, f.gen) }

func (f deliveryFrame) revision(r uint64) uint64 { return bucket(r, f.card) }

func bucket(v, ref uint64) uint64 {
	switch v {
	case 0:
		return 0
	case ref:
		return 1
	}
	return 2
}

// deliveryParams are the kernel parameters of every operation.
var deliveryParams = serverdelivery.Params{MaxGen: 2, MaxRev: 2}

// project is the kernel state of the record of a commit, or of a commit
// without one when record is nil. The latest index does not decide anything and
// stays empty; the refinement tests project it from the stored index.
func (f deliveryFrame) project(record *api.DeliveryEvidence) serverdelivery.State {
	if record == nil {
		return serverdelivery.State{}
	}
	state := serverdelivery.State{Rec: serverdelivery.RecDelivery, Rgen: f.generation(record.Generation), Rrev: f.revision(record.CardRevision)}
	if record.Purpose == "baseline" {
		state.Rec = serverdelivery.RecBaseline
	}
	return state
}

// publishEvent is the publication of evidence by the holder: exec says that the
// evidence names the holder's execution.
func (f deliveryFrame) publishEvent(input api.DeliveryEvidence, claim claimRecord, exec bool, card uint64) serverdelivery.Publish {
	return serverdelivery.Publish{
		Holder: true, Exec: exec, Baseline: input.Purpose == "baseline", Gen: f.generation(input.Generation),
		Cgen: f.generation(claim.Generation), Rev: f.revision(input.CardRevision), Card: f.revision(card),
	}
}

// guardEvent is the delivery guard of a move to review.
func (f deliveryFrame) guardEvent(record api.DeliveryEvidence, claim claimRecord, card uint64) serverdelivery.Guard {
	return serverdelivery.Guard{
		Holder: true, Exec: record.ExecutionID == claim.ExecutionID, Cgen: f.generation(claim.Generation), Card: f.revision(card),
	}
}

// kernelAccepts asks the delivery kernel whether it accepts e in state.
func kernelAccepts(state serverdelivery.State, e serverdelivery.Event) bool {
	_, ok := serverdelivery.Step(state, e, deliveryParams)
	return ok
}
