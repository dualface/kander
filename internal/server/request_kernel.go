package server

import (
	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/statemachine/serverrequest"
)

// The collaboration request decisions are made by the generated kernel in
// internal/statemachine/serverrequest
// (formal/KanderFormal/Machines/ServerRequest.lean). This file projects a
// request record into a kernel state, maps the facts of an operation to kernel
// arguments, and holds the kernel call of an operation. requests.go and
// dispatch_request.go select the error of a refused operation. See
// docs/formal-verification.md.

var requestStatuses = map[string]serverrequest.Status{
	api.RequestOpen: serverrequest.StatusOpen, api.RequestAccepted: serverrequest.StatusAccepted,
	api.RequestDone: serverrequest.StatusDone, api.RequestRejected: serverrequest.StatusRejected,
	api.RequestSuperseded: serverrequest.StatusSuperseded,
}

// requestFrame fixes how the uint64 generations and revisions of one operation
// map into the kernel's range. The kernel only compares them, orders a request
// generation with a new one, and adds one to the request revision, so a frame
// keeps exactly those relations:
//   - a generation maps to 0 below the operation's generation, 1 at it, and 2
//     above it;
//   - a card revision maps to 0 for zero, 1 at the operation's card revision, and
//     2 elsewhere;
//   - the request revision shifts by a base, as the claim kernel shifts
//     generations from 2^31 on, and a revision the request names (an expected
//     one) maps to the request's own or to the next one.
type requestFrame struct {
	gen, card, base uint64
}

// newRequestFrame is the frame of an operation on request, a request that
// exists or nil for a request to create. gen is the generation the operation
// compares requests with, and card the card revision it compares proposals with.
func newRequestFrame(gen, card uint64, request *api.CollabRequest) requestFrame {
	f := requestFrame{gen: gen, card: card}
	if request != nil && request.Revision >= 1<<31 {
		f.base = request.Revision
	}
	return f
}

func (f requestFrame) generation(g uint64) uint64 {
	switch {
	case g < f.gen:
		return 0
	case g == f.gen:
		return 1
	}
	return 2
}

func (f requestFrame) cardRevision(r uint64) uint64 {
	switch r {
	case 0:
		return 0
	case f.card:
		return 1
	}
	return 2
}

func (f requestFrame) revision(r uint64) uint64 { return shiftGeneration(f.base, r) }

// expected maps the revision a request names: zero stays zero, the request's own
// revision maps like it, and any other maps to the revision after it.
func (f requestFrame) expected(named, record uint64) uint64 {
	switch named {
	case 0:
		return 0
	case record:
		return f.revision(record)
	}
	return f.revision(record) + 1
}

// project is the kernel state of a request record. A request carries a card
// revision only as a dispatch proposal. A status the server never writes counts
// as superseded: nobody can answer or prepare such a request.
func (f requestFrame) project(r api.CollabRequest) serverrequest.State {
	status, known := requestStatuses[r.Status]
	if !known {
		status = serverrequest.StatusSuperseded
	}
	state := serverrequest.State{Status: status, Rev: f.revision(r.Revision), Tgen: f.generation(r.TargetGeneration), Prop: r.Dispatch != nil}
	if state.Prop {
		state.Prev = f.cardRevision(r.CardRevision)
	}
	return state
}

// params are the kernel parameters of an operation on a request of revision rev:
// one above the revision, so a write can take the next one.
func (f requestFrame) params(rev uint64) serverrequest.Params {
	return serverrequest.Params{MaxGen: 2, MaxRev: uint32(max(f.revision(rev)+1, 2))}
}

// requestKernel is the projected request of one operation.
type requestKernel struct {
	frame  requestFrame
	state  serverrequest.State
	params serverrequest.Params
}

// newRequestKernel projects request, or an empty slot when it is nil.
func newRequestKernel(frame requestFrame, request *api.CollabRequest) *requestKernel {
	k := &requestKernel{frame: frame}
	var rev uint64
	if request != nil {
		k.state, rev = frame.project(*request), request.Revision
	}
	k.params = frame.params(rev)
	return k
}

func (k *requestKernel) step(e serverrequest.Event) bool {
	next, ok := serverrequest.Step(k.state, e, k.params)
	if ok {
		k.state = next
	}
	return ok
}

// accepts asks the kernel whether it would accept e, and keeps the state.
func (k *requestKernel) accepts(e serverrequest.Event) bool {
	_, ok := serverrequest.Step(k.state, e, k.params)
	return ok
}

// claimFacts are the facts of a claim record that decide a request: the card is
// in working or review, the record is active, its lease has not expired, and its
// generation.
type claimFacts struct {
	held, active, live bool
	generation         uint64
}

// bindEvent is the check of a proposal's card version: the proposal names the
// inspected task and a card revision that is the card's.
func (f requestFrame) bindEvent(taskOK bool, named uint64) serverrequest.Bind {
	return serverrequest.Bind{TaskOk: taskOK, InRev: f.cardRevision(named), Card: f.cardRevision(f.card)}
}

// createEvent is the creation of a request that targets target.
func (f requestFrame) createEvent(c claimFacts, target uint64, proposal bool, named uint64) serverrequest.Create {
	create := serverrequest.Create{Held: c.held, Active: c.active, Live: c.live, Cgen: f.generation(c.generation), Target: f.generation(target), Prop: proposal}
	if proposal {
		create.InRev = f.cardRevision(named)
	}
	return create
}

// recreateEvent is a second creation of the dispatch of a proposal; same says
// that its client, acceptance window, and dispatch material are the proposal's.
func (f requestFrame) recreateEvent(c claimFacts, target uint64, same bool) serverrequest.Recreate {
	return serverrequest.Recreate{Held: c.held, Active: c.active, Live: c.live, Cgen: f.generation(c.generation), Target: f.generation(target), Card: f.cardRevision(f.card), Same: same}
}

// answerFacts are the facts of an answer to request: the status and note it
// names, the revision it expects, and whether the acceptance window of a
// proposal is open.
type answerFacts struct {
	answer       api.CollabAnswer
	request      api.CollabRequest
	window       bool
	claim        uint64
	cardRevision uint64
}

// events are the repeated and the changing answer of a request.
func (f requestFrame) answerEvents(a answerFacts) (serverrequest.AnswerSame, serverrequest.Answer) {
	status, known := requestStatuses[a.answer.Status]
	if !known {
		status = serverrequest.StatusNone
	}
	same := serverrequest.AnswerSame{Holder: true, Cgen: f.generation(a.claim), Ans: status, Expected: f.expected(a.answer.ExpectedRevision, a.request.Revision),
		SameNote: a.request.Note == a.answer.Note, Window: a.window, Card: f.cardRevision(a.cardRevision)}
	return same, serverrequest.Answer(same)
}

// supersedeEvent is the handoff to the generation the frame is built for.
func (f requestFrame) supersedeEvent() serverrequest.Supersede {
	return serverrequest.Supersede{Ngen: f.generation(f.gen)}
}

// completeEvent is the settlement of the request of one dispatch, when the
// claim record exists.
func (f requestFrame) completeEvent(found, match bool) serverrequest.Complete {
	return serverrequest.Complete{Completed: true, Found: found, Match: match, Cgen: f.generation(f.gen)}
}
