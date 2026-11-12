package board

import (
	"slices"
	"strings"
	"testing"

	obligations "github.com/dualface/kander/experiments/review-obligations"
)

// Reproduce the discovered abstraction gap through board entrance APIs. The
// existing helpers provide coordinator and Git evidence fixtures, not real agents.
func TestObligationRefinementReleasedRecovery(t *testing.T) {
	for _, drop := range []string{"dropReleased", "releaseDrop"} {
		t.Run(drop, func(t *testing.T) {
			root := tempBoard(t)
			id := recoveredWithoutPlan(t, root)
			p := ReviewPlan{Schema: 1, PlanID: "refinement-plan", Author: "fixture", Basis: "released recovery correspondence", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
				{BatchID: "batch", TaskIDs: []string{id}, Base: owedCommit(0), TargetCommit: owedCommit(1), Requirements: noReviewRequirements()},
			}}
			if err := CreateReviewPlan(root, p); err != nil {
				t.Fatal(err)
			}
			if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{}, nil); err != nil {
				t.Fatal(err)
			}
			p = readOwedPlan(t, root, p.PlanID)
			p = extendOwed(t, root, p, ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: []string{id}}, Decision: "release-no-role-recovery"})
			p = appendBatch(t, root, p, id, "required-tail", archiveRequirements())
			x := ReviewPlanExtension{DropBatches: []string{"required-tail"}}
			steps := []string{"reset", "planNA", "close", "releaseRecovery", "appendRequired"}
			if drop == "dropReleased" {
				x.Decision = "release-dropped-review-only"
				steps = append(steps, "dropReleased")
			} else {
				steps = append(steps, "drop", "releaseDrop")
			}
			p = extendOwed(t, root, p, x)
			if drop == "releaseDrop" {
				p = releaseDrop(t, root, p, []string{id})
			}
			p = extendOwed(t, root, p, ReviewPlanExtension{Seal: true})
			if len(p.DropObligations) != 0 {
				t.Fatal("drop debt was not released")
			}
			record, ok := recoveryRecord(t, root, id)
			if !ok || record.PlanID != "" || record.ReleaseDecision == "" || !slices.Equal(record.Roles, []string{"PMQA"}) {
				t.Fatalf("no-plan recovery history lost: %+v", record)
			}
			progress, err := ReviewTaskProgress(root, id)
			if err != nil || !slices.Contains(progress.Pending, "recovery-role:"+id+"/PMQA") {
				t.Fatalf("missing production debt: %+v %v", progress, err)
			}
			if err := deadEndDone(t, root, id); err == nil || !strings.Contains(err.Error(), "recovery-review:"+id) {
				t.Fatalf("done did not refuse recovery debt: %v", err)
			}
			steps = append(steps, "done")
			state := obligations.Initial(1)
			for _, event := range steps {
				next, ok := obligations.Step(state, 0, event, obligations.Bounds{MaxCycle: 3, MaxBatch: 3})
				if !ok {
					t.Fatalf("expected known prototype gap disappeared at %s; re-audit correspondence", event)
				}
				state = next
			}
			if state[0].Done != 1 {
				t.Fatal("prototype did not reach the documented unsafe abstraction state")
			}
			t.Logf("known model gap: %s; prototype done accepted, production done refused recovery debt", strings.Join(steps, " -> "))
		})
	}
}
