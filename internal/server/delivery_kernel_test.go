package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/statemachine/serverdelivery"
)

// deliveryProbe projects the delivery record of a commit before and after an
// entrance and compares it with the kernel step.
type deliveryProbe struct {
	t    *testing.T
	f    *fixture
	p    *client.Project
	task string
}

var deliveryParams = serverdelivery.Params{MaxGen: 2, MaxRev: 2}

func (d deliveryProbe) state(commit string, gen, card uint64) serverdelivery.State {
	d.t.Helper()
	st, err := d.f.srv.DeliveryState(d.p.ID(), d.task, commit, gen, card)
	if err != nil {
		d.t.Fatal(err)
	}
	return st
}

// refines runs fn and asserts projection(after) = Step(projection(before), e),
// with the generation and the card revision of the operation, when code is
// empty, or the error code, a kernel refusal, and an unchanged projection.
func (d deliveryProbe) refines(commit string, gen, card uint64, e serverdelivery.Event, code string, fn func() error) {
	d.t.Helper()
	before := d.state(commit, gen, card)
	err := fn()
	after := d.state(commit, gen, card)
	want, ok := serverdelivery.Step(before, e, deliveryParams)
	switch {
	case ok != (code == ""):
		d.t.Fatalf("kernel acceptance of %T on %+v: %v, want %v", e, before, ok, code == "")
	case code == "" && err != nil:
		d.t.Fatalf("%T: %v", e, err)
	case code == "" && after != want:
		d.t.Fatalf("%T: projection %+v, kernel %+v", e, after, want)
	case code != "" && !api.IsCode(err, code):
		d.t.Fatalf("%T on %+v: error %v, want %s", e, before, err, code)
	case code != "" && after != before:
		d.t.Fatalf("refused %T changed the projection: %+v -> %+v", e, before, after)
	}
}

// withTaskBranch records the task branch a card needs before it moves to review.
func withTaskBranch(t *testing.T, p *client.Project, card api.Task) api.Task {
	t.Helper()
	text := strings.Replace(card.Text, "- TASK_BRANCH:", "- TASK_BRANCH: task/"+card.TaskID, 1)
	card, err := p.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func (d deliveryProbe) evidence(grant api.ClaimGrant, commit string, revision uint64) api.DeliveryEvidence {
	return api.DeliveryEvidence{TaskID: d.task, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, CardRevision: revision, Commit: commit,
		Ref: api.DeliveryRef(d.p.ID(), d.task, grant.Claim.Generation, commit), Remote: "git@example.invalid:demo.git", ObservedAt: d.f.clock.now()}
}

// Publishing and guarding deliveries refine the kernel: the holder publishes
// evidence of its own execution and generation for the card's current revision,
// a commit keeps the previous version of its record, the latest index follows
// the last publish, and the guard of a move to review accepts only a delivery of
// the holder for the card's current revision, never a baseline.
func TestDeliveryEntrancesRefineTheKernel(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	card := withTaskBranch(t, a, todoTask(t, a, "delivery-kernel"))
	grant, err := claim(t, a, card.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	d := deliveryProbe{t: t, f: f, p: a, task: card.TaskID}
	ctx := t.Context()
	gen, first := grant.Claim.Generation, grant.Task.Revision
	commitA, commitB, commitC := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	publish := serverdelivery.Publish{Holder: true, Exec: true, Gen: 1, Cgen: 1, Rev: 1, Card: 1}
	send := func(key string, value api.DeliveryEvidence) func() error {
		return func() error {
			_, err := a.PublishDelivery(ctx, key, value, proof)
			return err
		}
	}

	d.refines(commitA, gen, first, publish, "", send("publish-a", d.evidence(grant, commitA, first)))
	// The baseline of another commit is the latest now, and commit A is not.
	baseline := publish
	baseline.Baseline = true
	beforeA := d.state(commitA, gen, first)
	value := d.evidence(grant, commitB, first)
	value.Purpose = "baseline"
	d.refines(commitB, gen, first, baseline, "", send("publish-b", value))
	afterA := d.state(commitA, gen, first)
	if want, _ := serverdelivery.Step(beforeA, serverdelivery.Other{}, deliveryParams); afterA != want || afterA.Latest != serverdelivery.LatestOther {
		t.Fatalf("commit A is still the latest: projection %+v, kernel %+v", afterA, want)
	}

	// Every fact of a publish decides: another execution, another generation, and
	// another card revision are refused and leave no record.
	foreign := d.evidence(grant, commitC, first)
	foreign.ExecutionID = "another-execution"
	exec := publish
	exec.Exec = false
	d.refines(commitC, gen, first, exec, api.CodeLeaseInvalid, send("foreign", foreign))
	late := d.evidence(grant, commitC, first)
	late.Generation, late.Ref = gen+1, api.DeliveryRef(a.ID(), card.TaskID, gen+1, commitC)
	other := publish
	other.Gen = 2
	d.refines(commitC, gen, first, other, api.CodeLeaseInvalid, send("late", late))
	stale := d.evidence(grant, commitC, first+5)
	moved := publish
	moved.Rev = 2
	d.refines(commitC, gen, first, moved, api.CodeRevisionConflict, send("stale", stale))

	// The card changes: the earlier delivery no longer passes the guard, and the
	// baseline never does.
	next, err := a.PutDocument(ctx, "progress", card.TaskID, api.Document{Path: "progress.md", Text: "later content", ExpectedRevision: first}, &proof)
	if err != nil {
		t.Fatal(err)
	}
	guard := serverdelivery.Guard{Holder: true, Exec: true, Cgen: 1, Card: 1}
	d.refines(commitA, gen, next.Revision, guard, api.CodeEvidenceIncomplete, func() error {
		_, err := a.Move(ctx, "stale-delivery", card.TaskID, api.Move{State: "review", DeliveryCommit: commitA}, &proof)
		return err
	})
	d.refines(commitB, gen, next.Revision, guard, api.CodeEvidenceIncomplete, func() error {
		_, err := a.Move(ctx, "baseline-delivery", card.TaskID, api.Move{State: "review", DeliveryCommit: commitB}, &proof)
		return err
	})

	// A record of the holder's generation and the card's revision that names
	// another execution does not pass the guard either.
	commitD := strings.Repeat("d", 40)
	elsewhere := d.evidence(grant, commitD, next.Revision)
	elsewhere.ExecutionID = "another-execution"
	if err := f.srv.SeedDelivery(a.ID(), elsewhere); err != nil {
		t.Fatal(err)
	}
	foreignGuard := guard
	foreignGuard.Exec = false
	d.refines(commitD, gen, next.Revision, foreignGuard, api.CodeEvidenceIncomplete, func() error {
		_, err := a.Move(ctx, "other-execution", card.TaskID, api.Move{State: "review", DeliveryCommit: commitD}, &proof)
		return err
	})

	// Publishing commit A again replaces its record and keeps the previous
	// version in the history; the delivery then passes the guard.
	if count, err := f.srv.DeliveryHistory(a.ID(), card.TaskID); err != nil || count != 0 {
		t.Fatalf("history before the republish: %d %v", count, err)
	}
	d.refines(commitA, gen, next.Revision, publish, "", send("publish-a-again", d.evidence(grant, commitA, next.Revision)))
	if count, err := f.srv.DeliveryHistory(a.ID(), card.TaskID); err != nil || count != 1 {
		t.Fatalf("history after the republish: %d %v", count, err)
	}
	d.refines(commitA, gen, next.Revision, guard, "", func() error {
		_, err := a.Move(ctx, "delivery", card.TaskID, api.Move{State: "review", DeliveryCommit: commitA}, &proof)
		return err
	})
}

// A successor does not pass the guard with the evidence of its predecessor's
// execution, although the generation of the record is the one it replaced.
func TestDeliveryGuardRefusesAnotherExecutionThroughTheKernel(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	card := withTaskBranch(t, a, todoTask(t, a, "delivery-kernel-successor"))
	grant, err := claim(t, a, card.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	d := deliveryProbe{t: t, f: f, p: a, task: card.TaskID}
	commit := strings.Repeat("d", 40)
	value := d.evidence(grant, commit, grant.Task.Revision)
	if _, err := a.PublishDelivery(t.Context(), "publish", value, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(91 * time.Second)
	successor, err := b.Takeover(t.Context(), "", card.TaskID, api.ClaimRequest{BranchMode: "main-develop", ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "explicit takeover", ExpectedGeneration: grant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: successor.Token, Generation: successor.Claim.Generation}
	d.refines(commit, successor.Claim.Generation, successor.Task.Revision, serverdelivery.Guard{Holder: true, Exec: false, Cgen: 1, Card: 1}, api.CodeEvidenceIncomplete, func() error {
		_, err := b.Move(t.Context(), "old-execution", card.TaskID, api.Move{State: "review", DeliveryCommit: commit}, &proof)
		return err
	})
}
