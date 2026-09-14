package board

import (
	"strings"
	"testing"
)

func TestValidateRequirementsAcceptsTwoFourAndSixKeys(t *testing.T) {
	two := map[string]string{"PMQA": "required", "Security": "N/A: no security surface"}
	four := map[string]string{"PM": "required", "QA": "N/A: x", "CSA": "N/A: x", "Hacker": "N/A: x"}
	six := map[string]string{"PM": "N/A: x", "QA": "N/A: x", "CSA": "N/A: x", "Hacker": "N/A: x", "PMQA": "required", "Security": "N/A: x"}
	for _, requirements := range []map[string]string{two, four, six} {
		if err := validateRequirements(requirements); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateNewRequirements(two); err != nil {
		t.Fatal(err)
	}
	if err := validateNewRequirements(four); err == nil || !strings.Contains(err.Error(), "PMQA and Security") {
		t.Fatalf("new four-key: %v", err)
	}
	if err := validateNewRequirements(six); err == nil {
		t.Fatal("new six-key accepted")
	}
	if err := validateRequirements(map[string]string{"PMQA": "required"}); err == nil {
		t.Fatal("one key accepted")
	}
	if !reviewRole("PM") || !reviewRole("QA") || !reviewRole("CSA") || !reviewRole("Hacker") || !reviewRole("PMQA") || !reviewRole("Security") {
		t.Fatal("historical role names must remain readable")
	}
	if reviewRole("Owner") {
		t.Fatal("unknown role")
	}
}

func TestCreateReviewPlanRejectsNewFourAndSixKeyBatches(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "new-shape")
	four := map[string]string{"PM": "required", "QA": "N/A: x", "CSA": "N/A: x", "Hacker": "N/A: x"}
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-new", Author: "coordinator", Basis: "shape", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: four}}}
	if err := CreateReviewPlan(root, p); err == nil || !strings.Contains(err.Error(), "PMQA and Security") {
		t.Fatalf("new four-key plan: %v", err)
	}
	p.Batches[0].Requirements = map[string]string{"PM": "N/A: x", "QA": "N/A: x", "CSA": "N/A: x", "Hacker": "N/A: x", "PMQA": "required", "Security": "N/A: x"}
	if err := CreateReviewPlan(root, p); err == nil {
		t.Fatal("new six-key plan accepted")
	}
}

func TestHistoricalFourKeyPlanRetryAndNewBatchAreTwoKey(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "legacy-plan")
	four := map[string]string{"PM": "required", "QA": "N/A: fixture", "CSA": "N/A: fixture", "Hacker": "N/A: fixture"}
	p := historicalPlanWithSeal(t, root, []string{id}, four, false)
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal("exact historical retry", err)
	}
	next := ReviewPlanExtension{
		PlanID: p.PlanID, ExpectedRevision: 1, Author: "coordinator", Basis: "append two-key batch",
		Batch: &ReviewPlanBatch{
			BatchID: "batch-two", PreviousBatchID: "batch", TaskIDs: []string{id},
			Base: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40),
			Requirements: map[string]string{"PMQA": "required", "Security": "N/A: fixture"},
		},
	}
	// The previous batch is still open, so the extension should fail on closure,
	// but the requirements shape must be accepted as two-key. Close first.
	run := gateRun(t, root, archiveInput([]string{id}, "pm", "PM"), emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PM": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	next.ExpectedRevision = 1
	if err := ExtendReviewPlan(root, next); err != nil {
		t.Fatal(err)
	}
	fourNext := next
	fourNext.Batch = &ReviewPlanBatch{
		BatchID: "batch-four", PreviousBatchID: "batch-two", TaskIDs: []string{id},
		Base: strings.Repeat("c", 40), TargetCommit: strings.Repeat("d", 40),
		Requirements: four,
	}
	fourNext.ExpectedRevision = 2
	if err := ExtendReviewPlan(root, fourNext); err == nil {
		t.Fatal("new four-key extension accepted")
	}
}

func TestHistoricalOpenFourKeyBatchAcceptsPMRunAndCloses(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "open-pm")
	requirements := historicalFourRoleRequirements()
	requirements["PM"] = "required"
	historicalPlan(t, root, []string{id}, requirements)
	run := gateRun(t, root, archiveInput([]string{id}, "pm", "PM"), emptyFindings())
	if run.Role != "PM" {
		t.Fatalf("role=%s", run.Role)
	}
	assignGate(t, root, run, map[string][]string{})
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	closure, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PM": passRole(run)})
	if err != nil || closure.RoleStatuses["PM"] != "PASS" {
		t.Fatalf("%+v %v", closure, err)
	}
}

func TestHistoricalReviewViewDigestIsByteStable(t *testing.T) {
	frozen := []byte(`{
  "schema": 1,
  "batch": {
    "task_context_hash": "",
    "schema": 1,
    "batch_id": "batch",
    "task_ids": [
      "card"
    ],
    "base": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "target_commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "report_language": "",
    "requirements": {
      "CSA": "N/A: fixture",
      "Hacker": "N/A: fixture",
      "PM": "required",
      "QA": "N/A: fixture"
    },
    "advances": null,
    "revision": 1
  },
  "runs": []
}
`)
	var view ReviewBatchView
	if err := DecodeReviewJSON(frozen, &view); err != nil {
		t.Fatal(err)
	}
	encoded := reviewJSON(view)
	if encoded != string(frozen) {
		t.Fatalf("historical view encoding changed\ngot:\n%s\nwant:\n%s", encoded, frozen)
	}
	if ReviewViewDigest(view) != ReviewDigest(frozen) {
		t.Fatal("ReviewViewDigest drifted from frozen bytes")
	}
}
