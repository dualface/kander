package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/statemachine/serverrequest"
)

// requestProbe projects the requests of one task before and after an entrance
// and compares them with the kernel step.
type requestProbe struct {
	t    *testing.T
	f    *fixture
	p    *client.Project
	task string
}

// revision is the revision of request id, or 0 when it does not exist.
func (r requestProbe) revision(id string) uint64 {
	r.t.Helper()
	requests, err := r.p.Requests(r.t.Context(), r.task)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, request := range requests {
		if request.ID == id {
			return request.Revision
		}
	}
	return 0
}

// state projects request id with the frame of an operation that compares
// requests with generation gen and proposals with card revision card.
func (r requestProbe) state(id string, gen, card, rev uint64) (serverrequest.State, serverrequest.Params) {
	r.t.Helper()
	st, params, err := r.f.srv.RequestState(r.p.ID(), r.task, id, server.NewRequestFrame(gen, card, rev))
	if err != nil {
		r.t.Fatal(err)
	}
	return st, params
}

// refines runs fn and asserts projection(after) = Step(projection(before), e),
// with the generation and the card revision of the operation, when code is
// empty, or the error code, a kernel refusal, and an unchanged projection.
func (r requestProbe) refines(id string, gen, card uint64, e serverrequest.Event, code string, fn func() error) {
	r.t.Helper()
	rev := r.revision(id)
	before, params := r.state(id, gen, card, rev)
	err := fn()
	after, _ := r.state(id, gen, card, rev)
	want, ok := serverrequest.Step(before, e, params)
	switch {
	case ok != (code == ""):
		r.t.Fatalf("kernel acceptance of %T on %+v: %v, want %v", e, before, ok, code == "")
	case code == "" && err != nil:
		r.t.Fatalf("%T: %v", e, err)
	case code == "" && after != want:
		r.t.Fatalf("%T: projection %+v, kernel %+v", e, after, want)
	case code != "" && !api.IsCode(err, code):
		r.t.Fatalf("%T on %+v: error %v, want %s", e, before, err, code)
	case code != "" && after != before:
		r.t.Fatalf("refused %T changed the projection: %+v -> %+v", e, before, after)
	}
}

// unchanged runs fn, which the entrance accepts although the kernel refuses e
// (the replay of a completion whose request is settled), and asserts the
// projection is kept.
func (r requestProbe) unchanged(id string, gen, card uint64, e serverrequest.Event, fn func() error) {
	r.t.Helper()
	rev := r.revision(id)
	before, params := r.state(id, gen, card, rev)
	err := fn()
	after, _ := r.state(id, gen, card, rev)
	if _, ok := serverrequest.Step(before, e, params); ok || err != nil || after != before {
		r.t.Fatalf("%T on %+v: accepted %v, after %+v, error %v", e, before, ok, after, err)
	}
}

// shellRefuses runs fn, which the kernel accepts as e but the shell refuses
// before it writes: the error has the given code, the projection is kept, and no
// request exists.
func (r requestProbe) shellRefuses(id string, gen, card uint64, e serverrequest.Event, code string, fn func() error) {
	r.t.Helper()
	before, params := r.state(id, gen, card, 0)
	err := fn()
	after, _ := r.state(id, gen, card, 0)
	if _, ok := serverrequest.Step(before, e, params); !ok || !api.IsCode(err, code) || after != before || r.revision(id) != 0 {
		r.t.Fatalf("%T on %+v: kernel accepted %v, error %v, after %+v, request revision %d", e, before, ok, err, after, r.revision(id))
	}
}

// returns runs fn, which the kernel accepts as e without changing the request
// (a repeated answer, a second creation), and asserts the projection is kept
// and the entrance returned the request.
func (r requestProbe) returns(id string, gen, card uint64, e serverrequest.Event, fn func() (api.CollabRequest, error)) {
	r.t.Helper()
	rev := r.revision(id)
	before, params := r.state(id, gen, card, rev)
	got, err := fn()
	after, _ := r.state(id, gen, card, rev)
	want, ok := serverrequest.Step(before, e, params)
	if err != nil || !ok || got.ID != id || after != want || after != before {
		r.t.Fatalf("%T on %+v: accepted %v, returned %q, after %+v, error %v", e, before, ok, got.ID, after, err)
	}
}

// kernelFixture is a claimed task with a sender and a holder.
type kernelFixture struct {
	f              *fixture
	holder, sender *client.Project
	grant          api.ClaimGrant
	proof          client.Claim
	task           string
	probe          requestProbe
}

func newKernelFixture(t *testing.T, slug string) *kernelFixture {
	return newKernelFixtureWith(t, slug, nil)
}

func newKernelFixtureWith(t *testing.T, slug string, start *api.StartInput) *kernelFixture {
	t.Helper()
	f := newFixture(t)
	k := &kernelFixture{f: f, holder: f.project("host-a"), sender: f.project("host-b")}
	card := todoTask(t, k.holder, slug)
	var err error
	if k.grant, err = k.holder.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-host-a", Owner: "claude", Start: start}); err != nil {
		t.Fatal(err)
	}
	k.proof = client.Claim{Token: k.grant.Token, Generation: k.grant.Claim.Generation}
	k.task = card.TaskID
	k.probe = requestProbe{t: t, f: f, p: k.holder, task: k.task}
	return k
}

func (k *kernelFixture) create(t *testing.T, key string, input api.NewCollabRequest) api.CollabRequest {
	t.Helper()
	request, err := k.sender.CreateRequest(t.Context(), key, k.task, input)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func (k *kernelFixture) answer(key, id string, answer api.CollabAnswer) (api.CollabRequest, error) {
	return k.holder.AnswerRequest(k.f.t.Context(), key, k.task, id, answer, k.proof)
}

// Creating a request refines the kernel: a plain request and a proposal are
// created open at revision 1 for the holder's generation, and every missing
// claim fact, another generation, an expired lease, and a proposal that does not
// bind the inspected card version is refused and leaves no request. A second
// creation of a proposal returns it; only the client, the window, or the
// material makes it a mismatch.
func TestRequestCreationRefinesTheKernel(t *testing.T) {
	k := newKernelFixture(t, "request-kernel-create")
	gen, card := k.grant.Claim.Generation, k.grant.Task.Revision
	claimed := serverrequest.Create{Held: true, Standing: true, Target: 1}

	plain := api.CollaborationRequestID("plain")
	k.probe.refines(plain, gen, 0, claimed, "", func() error {
		_, err := k.sender.CreateRequest(t.Context(), "plain", k.task, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: gen})
		return err
	})
	wrong := claimed
	wrong.Standing, wrong.Target = false, 2
	k.probe.refines(api.CollaborationRequestID("other-generation"), gen, 0, wrong, api.CodeLeaseInvalid, func() error {
		_, err := k.sender.CreateRequest(t.Context(), "other-generation", k.task, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: gen + 1})
		return err
	})

	// A card nobody holds has no claim record at all.
	idle := todoTask(t, k.holder, "request-kernel-idle")
	idleProbe := requestProbe{t: t, f: k.f, p: k.holder, task: idle.TaskID}
	idleProbe.refines(api.CollaborationRequestID("idle"), 0, 0, serverrequest.Create{Target: 2}, api.CodeClaimRequired, func() error {
		_, err := k.sender.CreateRequest(t.Context(), "idle", idle.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: 1})
		return err
	})

	intent := board.DispatchInput{ID: "kernel-proposal", TaskID: k.task, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Inspect the synchronized commit"}
	proposal := api.NewCollabRequest{Kind: api.RequestSync, Message: intent.Message, Dispatch: &intent, CardRevision: card, TargetGeneration: gen, AcceptWithin: api.DurationOf(120 * time.Second)}
	proposalID := api.CollaborationRequestID("proposal")
	for name, tc := range map[string]struct {
		bind  serverrequest.Bind
		patch func(*api.NewCollabRequest, *board.DispatchInput)
	}{
		"bind-moved":      {serverrequest.Bind{TaskOk: true, InRev: 2, Card: 1}, func(in *api.NewCollabRequest, _ *board.DispatchInput) { in.CardRevision = card + 1 }},
		"bind-missing":    {serverrequest.Bind{TaskOk: true, InRev: 0, Card: 1}, func(in *api.NewCollabRequest, _ *board.DispatchInput) { in.CardRevision = 0 }},
		"bind-other-task": {serverrequest.Bind{TaskOk: false, InRev: 1, Card: 1}, func(_ *api.NewCollabRequest, d *board.DispatchInput) { d.TaskID = "20260101-other-task" }},
	} {
		input, material := proposal, intent
		input.Dispatch = &material
		tc.patch(&input, &material)
		k.probe.refines(api.CollaborationRequestID(name), gen, card, tc.bind, api.CodeRevisionConflict, func() error {
			_, err := k.sender.CreateRequest(t.Context(), name, k.task, input)
			return err
		})
	}
	k.probe.refines(proposalID, gen, card, serverrequest.Create{Held: true, Standing: true, Target: 1, Prop: true, InRev: 1}, "", func() error {
		_, err := k.sender.CreateRequest(t.Context(), "proposal", k.task, proposal)
		return err
	})

	same := serverrequest.Recreate{Held: true, Standing: true, Target: 1, Card: 1, Same: true}
	k.probe.returns(proposalID, gen, card, same, func() (api.CollabRequest, error) {
		return k.sender.CreateRequest(t.Context(), "proposal-retry", k.task, proposal)
	})
	// Only the kind of the visible request differs; the proposal is the same.
	otherKind := proposal
	otherKind.Kind = api.RequestFix
	k.probe.returns(proposalID, gen, card, same, func() (api.CollabRequest, error) {
		return k.sender.CreateRequest(t.Context(), "proposal-kind", k.task, otherKind)
	})
	mismatch := same
	mismatch.Same = false
	changed := intent
	changed.Message = "Different proposed work"
	material := proposal
	material.Dispatch, material.Message = &changed, changed.Message
	window := proposal
	window.AcceptWithin = api.DurationOf(60 * time.Second)
	for name, input := range map[string]api.NewCollabRequest{"material": material, "window": window} {
		k.probe.refines(proposalID, gen, card, mismatch, api.CodeIdempotencyMismatch, func() error {
			_, err := k.sender.CreateRequest(t.Context(), "proposal-"+name, k.task, input)
			return err
		})
	}

	// A lapsed lease keeps the claim's standing: a creation for its generation
	// is accepted, and one for another generation is still refused.
	k.f.clock.advance(91 * time.Second)
	k.probe.refines(api.CollaborationRequestID("other"), gen, 0, serverrequest.Create{Held: true, Target: 2}, api.CodeLeaseInvalid, func() error {
		_, err := k.sender.CreateRequest(t.Context(), "other", k.task, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: gen + 1})
		return err
	})
	k.probe.refines(api.CollaborationRequestID("lapsed"), gen, 0, serverrequest.Create{Held: true, Standing: true, Target: 1}, "", func() error {
		_, err := k.sender.CreateRequest(t.Context(), "lapsed", k.task, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: gen})
		return err
	})
	if requests, err := k.holder.Requests(t.Context(), k.task); err != nil || len(requests) != 3 {
		t.Fatalf("lapsed creation was not kept beside the earlier requests: %+v %v", requests, err)
	}
}

// Answering a request refines the kernel: the answers advance a request one
// revision at a time, a repeated answer returns the request unchanged, a
// settled request cannot change its answer, an expected revision must be the
// request's, and a superseded request, or one of another generation, cannot be
// answered.
func TestRequestAnswersRefineTheKernel(t *testing.T) {
	k := newKernelFixture(t, "request-kernel-answer")
	gen := k.grant.Claim.Generation
	first := k.create(t, "first", api.NewCollabRequest{Kind: api.RequestFix, Message: "fix the failing case", TargetGeneration: gen})
	id := first.ID
	holder := serverrequest.Answer{Holder: true, Cgen: 1, Ans: serverrequest.StatusAccepted, SameNote: false, Window: true}
	on := func(id, key string, status, note string, expected uint64) func() error {
		return func() error {
			_, err := k.answer(key, id, api.CollabAnswer{Status: status, Note: note, ExpectedRevision: expected})
			return err
		}
	}
	answer := func(key string, status, note string, expected uint64) func() error {
		return on(id, key, status, note, expected)
	}
	// An expected revision other than the request's is a conflict.
	stale := holder
	stale.Ans, stale.Expected = serverrequest.StatusDone, 2
	k.probe.refines(id, gen, 0, stale, api.CodeRevisionConflict, answer("stale", api.RequestDone, "", 7))
	accepted := holder
	accepted.Expected = 1
	k.probe.refines(id, gen, 0, accepted, "", answer("accept", api.RequestAccepted, "first", first.Revision))
	repeated := serverrequest.AnswerSame{Holder: true, Cgen: 1, Ans: serverrequest.StatusAccepted, SameNote: true, Window: true}
	k.probe.returns(id, gen, 0, repeated, func() (api.CollabRequest, error) {
		return k.answer("repeat", id, api.CollabAnswer{Status: api.RequestAccepted, Note: "first"})
	})
	renote := holder
	k.probe.refines(id, gen, 0, renote, "", answer("renote", api.RequestAccepted, "second", 0))
	done := holder
	done.Ans = serverrequest.StatusDone
	k.probe.refines(id, gen, 0, done, "", answer("done", api.RequestDone, "done", 0))
	// A settled request keeps its answer and its note; only the same answer
	// returns it.
	reopen := holder
	reopen.Ans, reopen.SameNote = serverrequest.StatusRejected, true
	k.probe.refines(id, gen, 0, reopen, api.CodeRevisionConflict, answer("reject", api.RequestRejected, "done", 0))
	again := done
	again.SameNote = true
	k.probe.returns(id, gen, 0, serverrequest.AnswerSame(again), func() (api.CollabRequest, error) {
		return k.answer("repeat-done", id, api.CollabAnswer{Status: api.RequestDone, Note: "done"})
	})
	rename := done
	k.probe.refines(id, gen, 0, rename, api.CodeRevisionConflict, answer("rename", api.RequestDone, "other note", 0))

	// Without the holder nothing is answered, and nothing changes.
	intruder := holder
	intruder.Holder = false
	fresh := k.create(t, "intruder", api.NewCollabRequest{Kind: api.RequestFix, Message: "not for strangers", TargetGeneration: gen})
	k.probe.refines(fresh.ID, gen, 0, intruder, api.CodeHeldByOther, func() error {
		_, err := k.holder.AnswerRequest(t.Context(), "forged", k.task, fresh.ID, api.CollabAnswer{Status: api.RequestAccepted}, client.Claim{Token: "forged", Generation: gen})
		return err
	})

	// The request revision is shifted into the kernel's range like a generation:
	// large revisions answer like small ones, one revision at a time.
	for i, rev := range []uint64{1 << 31, 1 << 40, ^uint64(0) - 1} {
		big := api.CollabRequest{Revision: rev, ID: "req-big-" + string(rune('a'+i)), TaskID: k.task, Kind: api.RequestFix, Message: "big", FromClient: "host-b", TargetGeneration: gen, Status: api.RequestOpen, CreatedAt: k.f.clock.now(), UpdatedAt: k.f.clock.now()}
		if err := k.f.srv.SeedRequest(k.holder.ID(), big); err != nil {
			t.Fatal(err)
		}
		expected := serverrequest.Answer{Holder: true, Cgen: 1, Ans: serverrequest.StatusDone, Expected: 1, Window: true}
		k.probe.refines(big.ID, gen, 0, expected, "", on(big.ID, "answer-"+big.ID, api.RequestDone, "", rev))
		wrong := expected
		wrong.Expected = 2
		k.probe.refines(big.ID, gen, 0, wrong, api.CodeRevisionConflict, on(big.ID, "wrong-"+big.ID, api.RequestRejected, "", rev-1))
	}

	// A request that targets another generation without being superseded (the
	// claim moved without a handoff of requests) cannot be answered.
	old := api.CollabRequest{Revision: 1, ID: "req-old-generation", TaskID: k.task, Kind: api.RequestFix, Message: "old", FromClient: "host-b", TargetGeneration: 0, Status: api.RequestOpen, CreatedAt: k.f.clock.now(), UpdatedAt: k.f.clock.now()}
	if err := k.f.srv.SeedRequest(k.holder.ID(), old); err != nil {
		t.Fatal(err)
	}
	k.probe.refines(old.ID, gen, 0, holder, api.CodeLeaseInvalid, func() error {
		_, err := k.answer("old-generation", old.ID, api.CollabAnswer{Status: api.RequestAccepted})
		return err
	})
}

// A handoff refines the kernel: the takeover supersedes the open and accepted
// requests of the older generation in its own transaction and leaves the
// settled ones; the successor cannot answer a superseded request.
func TestHandoffSupersedesRequestsThroughTheKernel(t *testing.T) {
	k := newKernelFixture(t, "request-kernel-handoff")
	gen := k.grant.Claim.Generation
	open := k.create(t, "open", api.NewCollabRequest{Kind: api.RequestFix, Message: "open one", TargetGeneration: gen})
	accepted := k.create(t, "accepted", api.NewCollabRequest{Kind: api.RequestSync, Message: "accepted one", TargetGeneration: gen})
	settled := k.create(t, "settled", api.NewCollabRequest{Kind: api.RequestReview, Message: "settled one", TargetGeneration: gen})
	for id, status := range map[string]string{accepted.ID: api.RequestAccepted, settled.ID: api.RequestRejected} {
		if _, err := k.answer("answer-"+id, id, api.CollabAnswer{Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	next := gen + 1
	type frozen struct {
		id     string
		rev    uint64
		state  serverrequest.State
		params serverrequest.Params
	}
	var before []frozen
	for _, id := range []string{open.ID, accepted.ID, settled.ID} {
		rev := k.probe.revision(id)
		state, params := k.probe.state(id, next, 0, rev)
		before = append(before, frozen{id, rev, state, params})
	}
	k.f.clock.advance(91 * time.Second)
	successor, err := k.sender.Takeover(t.Context(), "", k.task, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "test explicit handoff", ExpectedGeneration: gen})
	if err != nil {
		t.Fatal(err)
	}
	if successor.Claim.Generation != next {
		t.Fatalf("successor generation %d, want %d", successor.Claim.Generation, next)
	}
	for i, want := range []bool{true, true, false} {
		b := before[i]
		after, _ := k.probe.state(b.id, next, 0, b.rev)
		stepped, ok := serverrequest.Step(b.state, serverrequest.Supersede{Ngen: 1}, b.params)
		switch {
		case ok != want:
			t.Fatalf("request %d: kernel accepted the handoff %v, want %v", i, ok, want)
		case after != stepped:
			t.Fatalf("request %d: projection %+v, kernel %+v", i, after, stepped)
		}
	}
	// The successor answers neither the superseded requests nor a request of the
	// settled generation that the handoff left alone.
	successorProof := client.Claim{Token: successor.Token, Generation: next}
	successorAnswer := serverrequest.Answer{Holder: true, Cgen: 1, Ans: serverrequest.StatusAccepted}
	for _, id := range []string{open.ID, accepted.ID} {
		k.probe.refines(id, next, 0, successorAnswer, api.CodeLeaseInvalid, func() error {
			_, err := k.holder.AnswerRequest(t.Context(), "late-"+id, k.task, id, api.CollabAnswer{Status: api.RequestAccepted}, successorProof)
			return err
		})
	}
	k.probe.refines(settled.ID, next, 0, successorAnswer, api.CodeLeaseInvalid, func() error {
		_, err := k.holder.AnswerRequest(t.Context(), "late-settled", k.task, settled.ID, api.CollabAnswer{Status: api.RequestAccepted}, successorProof)
		return err
	})
}
