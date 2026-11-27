package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/statemachine/serverproposal"
	"github.com/dualface/kander/v2/internal/statemachine/serverrequest"
)

// preparedHolder is a claimed task whose execution is confirmed and whose task
// branch is recorded: the state in which the holder prepares a dispatch.
type preparedHolder struct {
	*kernelFixture
	card api.Task
}

func newPreparedHolder(t *testing.T, slug string) *preparedHolder {
	t.Helper()
	k := newKernelFixtureWith(t, slug, &api.StartInput{Session: "codex " + slug, Window: "tmux:$1:@1:%1"})
	card, err := k.holder.Execution(t.Context(), "", k.task, api.ExecutionUpdate{Action: api.ExecutionConfirm}, k.proof)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(card.Text, "- TASK_BRANCH:", "- TASK_BRANCH: "+slug+"-branch", 1)
	card, err = k.holder.PutDocument(t.Context(), "task-branch", k.task, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, &k.proof)
	if err != nil {
		t.Fatal(err)
	}
	return &preparedHolder{kernelFixture: k, card: card}
}

func (h *preparedHolder) input(id string) board.DispatchInput {
	return board.DispatchInput{ID: id, TaskID: h.task, Kind: "sync", Message: "Synchronize the verified target commit", Base: strings.Repeat("a", 40)}
}

func (h *preparedHolder) propose(t *testing.T, key string, input board.DispatchInput, window time.Duration) api.CollabRequest {
	t.Helper()
	return h.create(t, key, api.NewCollabRequest{Kind: api.RequestSync, Message: input.Message, Dispatch: &input, CardRevision: h.card.Revision, TargetGeneration: h.grant.Claim.Generation, AcceptWithin: api.DurationOf(window)})
}

func (h *preparedHolder) prepare(key string, input board.DispatchInput) error {
	_, err := h.holder.DispatchAction(h.f.t.Context(), key, h.task, "prepare", api.DispatchCommand{Input: &input, CardRevision: h.card.Revision}, h.proof)
	return err
}

// The formal preparation of a dispatch refines the kernel: the holder prepares
// exactly the proposed material for the card version the proposal binds, which
// accepts the proposal and increases its revision, also when the proposal was
// already accepted. Other material, an elapsed window, an ambiguous dispatch ID,
// and a superseded proposal are refused and leave the proposal as it is.
func TestDispatchPreparationRefinesTheKernel(t *testing.T) {
	prepared := serverrequest.Prepare{Unique: true, Holder: true, Cgen: 1, Window: true, Card: 1, Material: true}

	t.Run("material", func(t *testing.T) {
		h := newPreparedHolder(t, "request-kernel-material")
		gen, card := h.grant.Claim.Generation, h.card.Revision
		input := h.input("kernel-sync")
		proposal := h.propose(t, "proposal", input, 120*time.Second)
		unstamped := input
		input = *proposal.Dispatch
		changed := input
		changed.Message = "Different proposed work"
		refused := prepared
		refused.Material = false
		h.probe.refines(proposal.ID, gen, card, refused, api.CodeIdempotencyMismatch, func() error { return h.prepare("changed-material", changed) })
		h.probe.refines(proposal.ID, gen, card, prepared, "", func() error { return h.prepare("prepare", input) })
		// After the preparation the card moved, so the same proposal can no longer
		// be created: it does not bind the inspected card version.
		current, err := h.holder.Show(t.Context(), h.task)
		if err != nil {
			t.Fatal(err)
		}
		stale := serverrequest.Bind{TaskOk: true, InRev: 2, Card: 1}
		h.probe.refines(proposal.ID, gen, current.Revision, stale, api.CodeRevisionConflict, func() error {
			_, err := h.sender.CreateRequest(t.Context(), "proposal-after", h.task, api.NewCollabRequest{Kind: api.RequestSync, Message: unstamped.Message, Dispatch: &unstamped, CardRevision: card, TargetGeneration: gen, AcceptWithin: api.DurationOf(120 * time.Second)})
			return err
		})
	})

	t.Run("accepted", func(t *testing.T) {
		h := newPreparedHolder(t, "request-kernel-accepted")
		gen, card := h.grant.Claim.Generation, h.card.Revision
		input := h.input("kernel-accepted")
		proposal := h.propose(t, "proposal", input, 120*time.Second)
		input = *proposal.Dispatch
		accept := serverrequest.Answer{Holder: true, Cgen: 1, Ans: serverrequest.StatusAccepted, Window: true, Card: 1}
		h.probe.refines(proposal.ID, gen, card, accept, "", func() error {
			_, err := h.answer("accept", proposal.ID, api.CollabAnswer{Status: api.RequestAccepted})
			return err
		})
		h.probe.refines(proposal.ID, gen, card, prepared, "", func() error { return h.prepare("prepare", input) })
		after, _ := h.probe.state(proposal.ID, gen, card, h.probe.revision(proposal.ID))
		if after.Status != serverrequest.StatusAccepted || after.Rev != 3 {
			t.Fatalf("a prepared accepted proposal: %+v", after)
		}
	})

	t.Run("window", func(t *testing.T) {
		h := newPreparedHolder(t, "request-kernel-window")
		gen, card := h.grant.Claim.Generation, h.card.Revision
		input := h.input("kernel-window")
		proposal := h.propose(t, "proposal", input, 120*time.Second)
		input = *proposal.Dispatch
		h.f.clock.advance(61 * time.Second)
		if _, err := h.holder.Renew(t.Context(), h.task, h.proof); err != nil {
			t.Fatal(err)
		}
		h.f.clock.advance(60 * time.Second)
		late := prepared
		late.Window = false
		h.probe.refines(proposal.ID, gen, card, late, api.CodeEvidenceIncomplete, func() error { return h.prepare("prepare", input) })
		// The same window refuses the first acceptance of the proposal.
		answer := serverrequest.Answer{Holder: true, Cgen: 1, Ans: serverrequest.StatusAccepted, Window: false, Card: 1}
		h.probe.refines(proposal.ID, gen, card, answer, api.CodeEvidenceIncomplete, func() error {
			_, err := h.answer("late", proposal.ID, api.CollabAnswer{Status: api.RequestAccepted})
			return err
		})
	})

	t.Run("ambiguous", func(t *testing.T) {
		h := newPreparedHolder(t, "request-kernel-ambiguous")
		gen, card := h.grant.Claim.Generation, h.card.Revision
		input := h.input("kernel-ambiguous")
		proposal := h.propose(t, "proposal", input, 120*time.Second)
		input = *proposal.Dispatch
		twin := proposal
		twin.ID, twin.CreatedAt = "req-twin-proposal", proposal.CreatedAt.Add(time.Second)
		if err := h.f.srv.SeedRequest(h.holder.ID(), twin); err != nil {
			t.Fatal(err)
		}
		ambiguous := prepared
		ambiguous.Unique = false
		h.probe.refines(proposal.ID, gen, card, ambiguous, api.CodeEvidenceIncomplete, func() error { return h.prepare("prepare", input) })
	})

	t.Run("card", func(t *testing.T) {
		h := newPreparedHolder(t, "request-kernel-card")
		gen := h.grant.Claim.Generation
		input := h.input("kernel-card")
		proposal := h.propose(t, "proposal", input, 120*time.Second)
		input = *proposal.Dispatch
		// The card changes after the proposal bound its version.
		moved, err := h.holder.PutDocument(t.Context(), "progress", h.task, api.Document{Path: "progress.md", Text: "later content", ExpectedRevision: h.card.Revision}, &h.proof)
		if err != nil {
			t.Fatal(err)
		}
		h.card = moved
		h.probe.refines(proposal.ID, gen, moved.Revision, prepared, api.CodeRevisionConflict, func() error { return h.prepare("prepare", input) })
		answer := serverrequest.Answer{Holder: true, Cgen: 1, Ans: serverrequest.StatusAccepted, Window: true, Card: 1}
		h.probe.refines(proposal.ID, gen, moved.Revision, answer, api.CodeRevisionConflict, func() error {
			_, err := h.answer("accept", proposal.ID, api.CollabAnswer{Status: api.RequestAccepted})
			return err
		})
	})

	t.Run("superseded", func(t *testing.T) {
		h := newPreparedHolder(t, "request-kernel-superseded")
		input := h.input("kernel-superseded")
		proposal := h.propose(t, "proposal", input, 120*time.Second)
		input = *proposal.Dispatch
		h.f.clock.advance(91 * time.Second)
		successor, err := h.sender.Takeover(t.Context(), "", h.task, api.ClaimRequest{BranchMode: "main-develop", ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "test explicit handoff", ExpectedGeneration: h.grant.Claim.Generation})
		if err != nil {
			t.Fatal(err)
		}
		proof := client.Claim{Token: successor.Token, Generation: successor.Claim.Generation}
		if _, err := h.sender.Execution(t.Context(), "", h.task, api.ExecutionUpdate{Action: api.ExecutionConfirm}, proof); err != nil {
			t.Fatal(err)
		}
		current, err := h.sender.Show(t.Context(), h.task)
		if err != nil {
			t.Fatal(err)
		}
		h.probe.refines(proposal.ID, successor.Claim.Generation, current.Revision, prepared, api.CodeLeaseInvalid, func() error {
			_, err := h.sender.DispatchAction(t.Context(), "prepare", h.task, "prepare", api.DispatchCommand{Input: &input, CardRevision: current.Revision}, proof)
			return err
		})
	})
}

// Completing the dispatch of an accepted proposal refines the kernel: the move
// into done settles the proposal of that dispatch for the claim's generation
// and no other request; an exact replay of the completion settles a proposal an
// earlier server left accepted.
func TestCompletedDispatchSettlesItsProposalThroughTheKernel(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	member, first := integrationTask(t, f, a, "host-a", "request-kernel-complete")
	task := member.TaskID
	proof := client.Claim{Token: first.Token, Generation: first.Claim.Generation}
	integration := integrate(t, a, integrationInput(t, a, member, first.Claim.ExecutionID, "complete-integration"), "g1", proof)
	current, err := a.Show(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	gen, card := first.Claim.Generation, current.Revision
	input := wrapUpRound(t, a, member, integration, "kernel-wrap-up")
	proposal, err := b.CreateRequest(t.Context(), "wrap-up", task, api.NewCollabRequest{Kind: api.RequestSync, Message: input.Message, Dispatch: &input, CardRevision: card, TargetGeneration: gen})
	if err != nil {
		t.Fatal(err)
	}
	bystander, err := b.CreateRequest(t.Context(), "bystander", task, api.NewCollabRequest{Kind: api.RequestFix, Message: "unrelated", TargetGeneration: gen})
	if err != nil {
		t.Fatal(err)
	}
	// Another proposal of the same generation is accepted too: only the one of the
	// completed dispatch may settle.
	twin := board.DispatchInput{ID: "kernel-other-proposal", TaskID: task, Kind: "sync", Message: "Another proposed round", Base: strings.Repeat("a", 40)}
	other, err := b.CreateRequest(t.Context(), "other-proposal", task, api.NewCollabRequest{Kind: api.RequestSync, Message: twin.Message, Dispatch: &twin, CardRevision: card, TargetGeneration: gen})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AnswerRequest(t.Context(), "accept-other", task, other.ID, api.CollabAnswer{Status: api.RequestAccepted}, proof); err != nil {
		t.Fatal(err)
	}
	probe := requestProbe{t: t, f: f, p: a, task: task}
	probe.refines(proposal.ID, gen, card, serverrequest.Prepare{Unique: true, Holder: true, Cgen: 1, Window: true, Card: 1, Material: true}, "", func() error {
		prepared, err := a.DispatchAction(t.Context(), "prepare", task, "prepare", api.DispatchCommand{Input: &input, CardRevision: card}, proof)
		if err != nil {
			return err
		}
		_, err = a.Move(t.Context(), "accept", task, api.Move{State: "working", Authorization: prepared.Authorization}, &proof)
		return err
	})
	round, err := a.Dispatch(t.Context(), task, input.ID)
	if err != nil || round.State != board.DispatchAccepted {
		t.Fatalf("accepted round: %+v %v", round, err)
	}
	complete := func(key string) error {
		_, err := a.Move(t.Context(), key, task, api.Move{State: "done", Result: "completed", Authorization: round.Authorization, DeliveryCommit: member.DeliveryCommit}, &proof)
		return err
	}
	settle := serverrequest.Complete{Completed: true, Found: true, Match: true, Cgen: 1}
	elsewhere := settle
	elsewhere.Match = false
	// The move settles the proposal of the dispatch; the other requests stay.
	type kept struct {
		id     string
		rev    uint64
		state  serverrequest.State
		params serverrequest.Params
	}
	var others []kept
	for _, id := range []string{bystander.ID, other.ID} {
		rev := probe.revision(id)
		state, params := probe.state(id, gen, card, rev)
		others = append(others, kept{id, rev, state, params})
	}
	probe.refines(proposal.ID, gen, card, settle, "", func() error { return complete("complete") })
	for _, o := range others {
		after, _ := probe.state(o.id, gen, card, o.rev)
		if _, ok := serverrequest.Step(o.state, elsewhere, o.params); ok || after != o.state {
			t.Fatalf("the completion changed request %s: %+v -> %+v", o.id, o.state, after)
		}
	}
	if others[1].state.Status != serverrequest.StatusAccepted {
		t.Fatalf("the other proposal is not accepted: %+v", others[1].state)
	}
	// An exact replay of the completion settles a proposal an earlier server left
	// accepted, and leaves a settled one as it is.
	requests, err := a.Requests(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	var settled api.CollabRequest
	for _, r := range requests {
		if r.ID == proposal.ID {
			settled = r
		}
	}
	if settled.Status != api.RequestDone {
		t.Fatalf("completion left the proposal %+v", settled)
	}
	left := settled
	left.Status, left.Revision = api.RequestAccepted, settled.Revision+1
	if err := f.srv.SeedRequest(a.ID(), left); err != nil {
		t.Fatal(err)
	}
	probe.refines(proposal.ID, gen, card, settle, "", func() error { return complete("replay") })
	probe.unchanged(proposal.ID, gen, card, settle, func() error { return complete("replay-again") })
}

// A dispatch ID identifies one proposal: the slot of the ID refines its kernel
// through the entrances. A first creation takes the slot, every later creation
// finds the proposal and never creates a second one, and the formal preparation
// needs exactly one; a task directory with two proposals of one ID (a
// hand-edited one) admits no creation and refuses the preparation.
func TestProposalSlotRefinesTheKernel(t *testing.T) {
	h := newPreparedHolder(t, "request-kernel-slot")
	ctx := t.Context()
	input := h.input("kernel-slot")
	slot := func() (serverproposal.State, serverproposal.Params) {
		t.Helper()
		st, params, err := h.f.srv.ProposalSlot(h.holder.ID(), h.task, input.ID)
		if err != nil {
			t.Fatal(err)
		}
		return st, params
	}
	create := func(key string) (api.CollabRequest, error) {
		return h.sender.CreateRequest(ctx, key, h.task, api.NewCollabRequest{Kind: api.RequestSync, Message: input.Message, Dispatch: &input, CardRevision: h.card.Revision, TargetGeneration: h.grant.Claim.Generation, AcceptWithin: api.DurationOf(120 * time.Second)})
	}
	check := func(e serverproposal.Event, before serverproposal.State, wantAccepted bool) {
		t.Helper()
		after, params := slot()
		want, ok := serverproposal.Step(before, e, params)
		if ok != wantAccepted || (ok && after != want) || (!ok && after != before) {
			t.Fatalf("%T on %+v: accepted %v, projection %+v, kernel %+v", e, before, ok, after, want)
		}
	}

	empty, _ := slot()
	first, err := create("slot-first")
	if err != nil {
		t.Fatal(err)
	}
	check(serverproposal.Admit{}, empty, true)
	taken, _ := slot()
	again, err := create("slot-again")
	if err != nil || again.ID != first.ID {
		t.Fatalf("a second creation: %+v %v", again, err)
	}
	check(serverproposal.Repeat{}, taken, true)
	if requests, err := h.holder.Requests(ctx, h.task); err != nil || len(requests) != 1 {
		t.Fatalf("a second creation made a second proposal: %+v %v", requests, err)
	}

	// Two proposals of one ID: no creation takes the slot, the first is found,
	// and the formal preparation is ambiguous.
	twin := first
	twin.ID, twin.CreatedAt = "req-twin-slot", first.CreatedAt.Add(time.Second)
	if err := h.f.srv.SeedRequest(h.holder.ID(), twin); err != nil {
		t.Fatal(err)
	}
	crowded, params := slot()
	if _, ok := serverproposal.Step(crowded, serverproposal.Admit{}, params); ok || crowded.Count != 2 {
		t.Fatalf("a slot with two proposals: %+v", crowded)
	}
	if _, ok := serverproposal.Step(crowded, serverproposal.Lookup{}, params); ok {
		t.Fatal("the slot looked up one of two proposals")
	}
	if found, err := create("slot-crowded"); err != nil || found.ID != first.ID {
		t.Fatalf("a creation in a crowded slot: %+v %v", found, err)
	}
	check(serverproposal.Repeat{}, crowded, true)
	stamped := *first.Dispatch
	err = h.prepare("slot-prepare", stamped)
	expectCode(t, err, api.CodeEvidenceIncomplete)
}

// The check of the evidence belongs to the shell: after the kernel accepted the
// creation and the slot, a refusal writes nothing, so neither the request nor
// the projection of its proposal changes.
func TestFixProposalRefusalIsAShellRefusal(t *testing.T) {
	h := newReviewedHolder(t, "fix-proposal-shell")
	gen, card := h.grant.Claim.Generation, h.card.Revision
	in := h.fixInput("fix-shell", func(f *board.DispatchFixBinding) { f.Findings[0].FindingID = "PM-404" })
	slot, _, err := h.f.srv.ProposalSlot(h.holder.ID(), h.task, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	create := serverrequest.Create{Held: true, Standing: true, Target: 1, Prop: true, InRev: 1}
	h.probe.shellRefuses(api.CollaborationRequestID("fix-shell"), gen, card, create, api.CodeBoardRejected, func() error {
		_, err := h.sendFix("fix-shell", in)
		return err
	})
	if after, _, err := h.f.srv.ProposalSlot(h.holder.ID(), h.task, in.ID); err != nil || after != slot {
		t.Fatalf("the slot of a refused proposal changed: %+v -> %+v %v", slot, after, err)
	}
}

// A plain move out of review asks the request kernel whether it would accept the
// Prepare of a pending proposal, and never steps the kernel: the move changes no
// request. When the kernel accepts, the move is refused and the projection is
// kept; when it refuses (here an elapsed window), the move succeeds and the
// projection is kept.
func TestBareMoveGuardAsksTheKernelForPrepare(t *testing.T) {
	prepared := serverrequest.Prepare{Unique: true, Holder: true, Cgen: 1, Window: true, Card: 1, Material: true}

	t.Run("accepts", func(t *testing.T) {
		m := newReviewMember(t, "request-kernel-bare-accepts", "20261119-kernel-group")
		proposal := m.propose(t, api.RequestFix, "proposal", "kernel-bare-fix")
		probe := requestProbe{t: t, f: m.f, p: m.holder, task: m.task}
		probe.acceptedAndKept(proposal.ID, m.grant.Claim.Generation, m.card.Revision, prepared, api.CodeEvidenceIncomplete, func() error {
			_, err := m.bareMove("bare")
			return err
		})
	})

	t.Run("refuses", func(t *testing.T) {
		m := newReviewMember(t, "request-kernel-bare-refuses", "20261119-kernel-group")
		proposal := m.propose(t, api.RequestFix, "proposal", "kernel-bare-late")
		m.f.clock.advance(61 * time.Second)
		if _, err := m.holder.Renew(t.Context(), m.task, m.proof); err != nil {
			t.Fatal(err)
		}
		m.f.clock.advance(60 * time.Second)
		late := prepared
		late.Window = false
		probe := requestProbe{t: t, f: m.f, p: m.holder, task: m.task}
		probe.unchanged(proposal.ID, m.grant.Claim.Generation, m.card.Revision, late, func() error {
			_, err := m.bareMove("bare")
			return err
		})
	})
}

// acceptedAndKept runs fn, an entrance that asks the kernel for e without
// stepping it: the kernel accepts e, the entrance refuses with code, and the
// projection of the request is kept.
func (r requestProbe) acceptedAndKept(id string, gen, card uint64, e serverrequest.Event, code string, fn func() error) {
	r.t.Helper()
	rev := r.revision(id)
	before, params := r.state(id, gen, card, rev)
	err := fn()
	after, _ := r.state(id, gen, card, rev)
	if _, ok := serverrequest.Step(before, e, params); !ok || !api.IsCode(err, code) || after != before {
		r.t.Fatalf("%T on %+v: kernel accepted %v, error %v, after %+v", e, before, ok, err, after)
	}
}
