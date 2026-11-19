package board

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// These probes pin how the two admissions of a plan treat a card that ran outside
// every plan: the creation of a plan and the add_members extension. Each case
// runs once with the unpublished run kept and once with it abandoned, so a test
// shows whether the abandonment changes the admission.

// strayFinished prepares and finalizes an unpublished run of card id in the batch
// batch, which no plan names. The run is the one an abandonment retires.
func strayFinished(t *testing.T, root, id, batch string) ReviewRun {
	t.Helper()
	in := archiveInput([]string{id}, "stray-lost", "PMQA")
	in.BatchID, in.Base, in.Commit = batch, sha("b"), sha("c")
	run, _, err := PrepareReviewRun(root, in, archiveRequirements(), nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	return run
}

// controlFiles reads the raw bytes of the named control files, keyed by name.
func controlFiles(t *testing.T, root string, names ...string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		for _, name := range names {
			raw, exists, e := tx.ReadGroup(reviewControlGroup, name)
			if e != nil {
				return e
			}
			got[name] = shownBytes(raw, exists)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// requireNoPlanFiles fails when a refused plan left a plan record, the pointer or
// the cycle record of the card, a record of its batch, or a history record.
func requireNoPlanFiles(t *testing.T, root, id, planID, batch string) {
	t.Helper()
	names := []string{
		planName(planID), taskPlanName(id), "tracked-cycles/" + id + ".json", reviewBatchName(batch),
		fmt.Sprintf("plan-history/%s/1.json", planID), fmt.Sprintf("plan-history/%s/2.json", planID),
	}
	for name, got := range controlFiles(t, root, names...) {
		if got != absent {
			t.Fatalf("a refused plan left %s: %s", name, got)
		}
	}
	err := WithTransaction(root, reviewScope([]string{id}, true), func(tx *Transaction) error {
		text, e := tx.Read(id, "reviews/plan.json")
		if !errors.Is(e, os.ErrNotExist) {
			return fmt.Errorf("the plan copy of %s is not absent after a refused plan: %q, %v", id, text, e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// cardSpec reads the card file of id.
func cardSpec(t *testing.T, root, id string) string {
	t.Helper()
	return transactionSnapshot(t, root, id).Text
}

// probePlan is a plan of one open batch over id. The batch does not share an ID
// with the stray one, so only the run record of the card can matter.
func probePlan(id string) ReviewPlan {
	return ReviewPlan{Schema: 1, PlanID: "probe-plan", Author: "coordinator", Basis: "probe", CWD: "/repo", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "first", TaskIDs: []string{id}, Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
	}}
}

// A card with no plan and an unpublished run in a batch of its own cannot enter a
// new plan whose batches do not name that batch, whether the run is kept or
// abandoned, and whether or not the input carries a batch resolution:
// createReviewPlan clears the input resolutions, and a resolution needs the plan
// that has the card.
func TestAbandonedOffPlanRunAndANewPlan(t *testing.T) {
	cases := []struct {
		name      string
		abandon   bool
		resolutes bool
	}{
		{"kept", false, false},
		{"abandoned", true, false},
		{"resolution in the input", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useEnglish(t)
			root := tempBoard(t)
			id := gateCard(t, root, "probe-new-plan")
			run := strayFinished(t, root, id, "stray")
			if c.abandon {
				if err := AbandonReviewRunWithTransaction(root, abandonRequest(run.RunID)); err != nil {
					t.Fatal(err)
				}
			}
			before := controlFiles(t, root, reviewRunName(run.RunID), reviewBatchName("stray"))
			spec := cardSpec(t, root, id)
			p := probePlan(id)
			if c.resolutes {
				p.ResolvedBatches = []ReviewBatchResolution{{Request: ReviewBatchResolutionRequest{BatchID: "stray"}}}
			}
			err := CreateReviewPlan(root, p)
			if err == nil || !strings.Contains(err.Error(), "unplanned execution batch: stray") {
				t.Fatalf("the plan was not refused for the stray batch: %v", err)
			}
			requireNoPlanFiles(t, root, id, p.PlanID, "first")
			after := controlFiles(t, root, reviewRunName(run.RunID), reviewBatchName("stray"))
			for name, was := range before {
				if after[name] != was {
					t.Fatalf("the refused plan changed %s", name)
				}
			}
			if cardSpec(t, root, id) != spec {
				t.Fatal("the refused plan changed the card file")
			}
		})
	}
}

// A started group card with no plan and an unpublished run outside the plan
// cannot be added to the plan of its group, kept or abandoned. The refusal names
// the card, the batch, and the plan, and changes nothing, the stray run and batch
// included. An abandoned card then gets a plan of its own that names the batch.
func TestAbandonedOffPlanRunAndAddMembers(t *testing.T) {
	for _, abandon := range []bool{false, true} {
		name := "kept"
		if abandon {
			name = "abandoned"
		}
		t.Run(name, func(t *testing.T) {
			w := newMembersWorld(t, "probe-join-"+name, false)
			run := strayFinished(t, w.root, w.second, "stray")
			if abandon {
				if err := AbandonReviewRunWithTransaction(w.root, abandonRequest(run.RunID)); err != nil {
					t.Fatal(err)
				}
			}
			stray := []string{reviewRunName(run.RunID), reviewBatchName("stray")}
			before := controlFiles(t, w.root, stray...)
			err := w.extend(ReviewPlanExtension{AddMembers: []string{w.second}})
			if err == nil {
				t.Fatal("the card joined the plan")
			}
			want := fmt.Sprintf("%s has a run in batch stray, which no batch of plan %s names", w.second, w.plan.PlanID)
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal does not say %q: %v", want, err)
			}
			w.untouched(t, "")
			for name, was := range controlFiles(t, w.root, stray...) {
				if was != before[name] {
					t.Fatalf("the refused request changed %s", name)
				}
			}
			if !abandon {
				return
			}
			// The refused card still gets a plan of its own that names the stray batch.
			if err := CreateReviewPlan(w.root, strayAdoptingPlan(w.second, "probe-own")); err != nil {
				t.Fatalf("the card got no plan of its own: %v", err)
			}
			if id, ok := w.planOf(t, w.second); !ok || id != "probe-own" {
				t.Fatalf("the plan pointer of the card is %q", id)
			}
		})
	}
}

// A resolution on the plan cannot excuse the run of an added member: the facts of
// the addition ignore every resolution of the plan.
func TestAbandonedOffPlanResolutionDoesNotExcuseAnAddedMember(t *testing.T) {
	w := newMembersWorld(t, "probe-resolved", false)
	run := strayFinished(t, w.root, w.second, "stray")
	if err := AbandonReviewRunWithTransaction(w.root, abandonRequest(run.RunID)); err != nil {
		t.Fatal(err)
	}
	p := w.plan
	p.ResolvedBatches = []ReviewBatchResolution{{Request: ReviewBatchResolutionRequest{BatchID: "stray"}}}
	err := WithTransaction(w.root, reviewScope([]string{w.first, w.second}, false), func(tx *Transaction) error {
		group, e := planGroup(tx, p)
		if e != nil {
			return e
		}
		ev, _, _, e := memberAdditionFacts(tx, p, group, w.second, true)
		if e != nil {
			return e
		}
		if ev.NoUnplannedRuns {
			t.Fatal("a resolution of the plan excused the run of an added member")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// strayAdoptingPlan is a plan whose only batch reuses the ID of the stray batch and
// repeats its binding, so the run of the card is a run of a planned batch.
func strayAdoptingPlan(id, planID string) ReviewPlan {
	return ReviewPlan{Schema: 1, PlanID: planID, Author: "coordinator", Basis: "probe", CWD: "/repo", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "stray", TaskIDs: []string{id}, Base: sha("b"), TargetCommit: sha("c"), Requirements: archiveRequirements()},
	}}
}

// The way out of the refusal is a plan that names the stray batch: the admission
// asks whether a batch of the plan names the run, and a plan batch with the ID and
// the binding of the stray one does. It holds whether the run is kept or
// abandoned, and a batch with another binding is refused.
func TestAbandonedOffPlanRunIsAdoptedByANewPlan(t *testing.T) {
	for _, abandon := range []bool{false, true} {
		name := "kept"
		if abandon {
			name = "abandoned"
		}
		t.Run(name, func(t *testing.T) {
			useEnglish(t)
			root := tempBoard(t)
			id := gateCard(t, root, "probe-adopt-"+name)
			run := strayFinished(t, root, id, "stray")
			if abandon {
				if err := AbandonReviewRunWithTransaction(root, abandonRequest(run.RunID)); err != nil {
					t.Fatal(err)
				}
			}
			other := strayAdoptingPlan(id, "probe-other")
			other.Batches[0].TargetCommit = sha("d")
			if err := CreateReviewPlan(root, other); err == nil || !strings.Contains(err.Error(), "existing batch/plan mismatch") {
				t.Fatalf("a batch with another binding was not refused: %v", err)
			}
			if err := CreateReviewPlan(root, strayAdoptingPlan(id, "probe-adopt")); err != nil {
				t.Fatalf("the plan that names the stray batch was refused: %v", err)
			}
			progress, err := ReviewTaskProgress(root, id)
			if err != nil || progress.PlanID != "probe-adopt" {
				t.Fatalf("the card is not planned: %+v, %v", progress, err)
			}
			if batch, err := ReadReviewBatch(root, "stray"); err != nil || batch.PlanID != "probe-adopt" {
				t.Fatalf("the stray batch is not in the plan: %+v, %v", batch, err)
			}
		})
	}
}
