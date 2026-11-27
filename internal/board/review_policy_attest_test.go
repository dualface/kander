package board

import (
	"reflect"
	"strings"
	"testing"
)

// A batch keeps the policy revision and the decisions of the request that
// created it, from a plan, an extension, or a run. The plan record keeps the
// decisions its author gave and never the policy revision, so an exact retry
// with another revision is the same plan; progress lists the batch records.
func TestReviewBatchesKeepTheAttestationOfTheRequestThatCreatedThem(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "attested")
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: fixture"}
	given := map[string]string{"Security": "decision-1", "PMQA": "unused"}
	p := ReviewPlan{Schema: 1, PlanID: "plan-attested", Author: "coordinator", Basis: "attest", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, PolicyRevision: 7,
		Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: requirements, RequirementDecisions: given}}}
	used := map[string]string{"Security": "decision-1"}
	if err := createReviewPlan(root, p, true, BatchAttestations{"batch": {PolicyRevision: 7, RequirementDecisions: used}}); err != nil {
		t.Fatal(err)
	}
	batch, err := ReadReviewBatch(root, "batch")
	if err != nil || batch.PolicyRevision != 7 || !reflect.DeepEqual(batch.RequirementDecisions, used) {
		t.Fatalf("planned batch: %+v %v", batch, err)
	}
	view, err := ReadReviewBatchView(root, "batch")
	if err != nil || view.Batch.PolicyRevision != 7 || !reflect.DeepEqual(view.Batch.RequirementDecisions, used) {
		t.Fatalf("aggregate view: %+v %v", view.Batch, err)
	}
	stored, err := ReadReviewPlan(root, p.PlanID)
	if err != nil || stored.PolicyRevision != 0 || !reflect.DeepEqual(stored.Batches[0].RequirementDecisions, given) {
		t.Fatalf("plan record: %+v %v", stored, err)
	}
	p.PolicyRevision = 9
	if err := createReviewPlan(root, p, true, nil); err != nil {
		t.Fatalf("exact retry with another policy revision: %v", err)
	}
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || !reflect.DeepEqual(progress.BatchPolicies, map[string]ReviewBatchPolicy{"batch": {PolicyRevision: 7, RequirementDecisions: used}}) {
		t.Fatalf("progress: %+v %v", progress, err)
	}

	run := gateRun(t, root, archiveInput([]string{id}, "pmqa", "PMQA"), emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	next := ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, Author: "coordinator", Basis: "append", PolicyRevision: 8,
		Batch: &ReviewPlanBatch{BatchID: "batch-two", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40), Requirements: requirements}}
	if err := ExtendReviewPlanWithTransaction(root, next, BatchAttestations{"batch-two": {PolicyRevision: 8}}); err != nil {
		t.Fatal(err)
	}
	appended, err := ReadReviewBatch(root, "batch-two")
	if err != nil || appended.PolicyRevision != 8 || appended.RequirementDecisions != nil {
		t.Fatalf("appended batch: %+v %v", appended, err)
	}
	progress, err = ReviewTaskProgress(root, id)
	if err != nil || progress.BatchPolicies["batch-two"].PolicyRevision != 8 || len(progress.BatchPolicies) != 2 {
		t.Fatalf("progress after extension: %+v %v", progress, err)
	}
}

// A run that creates its batch records the attestation; a later run of the
// same batch leaves the batch record as it was.
func TestReviewRunRecordsTheAttestationOnlyWhenItCreatesTheBatch(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "attested-run")
	attest := BatchAttestations{"batch": {PolicyRevision: 3, RequirementDecisions: map[string]string{"Security": "decision-2"}}}
	if _, fresh, err := PrepareReviewRunWithTransaction(root, archiveInput([]string{id}, "first", "PMQA"), archiveRequirements(), attest, nil, archiveOriginals(), "test"); err != nil || !fresh {
		t.Fatalf("first run: %v %v", fresh, err)
	}
	later := BatchAttestations{"batch": {PolicyRevision: 4}}
	if _, _, err := PrepareReviewRunWithTransaction(root, archiveInput([]string{id}, "second", "PMQA"), archiveRequirements(), later, nil, archiveOriginals(), "test"); err != nil {
		t.Fatal(err)
	}
	batch, err := ReadReviewBatch(root, "batch")
	if err != nil || batch.PolicyRevision != 3 || batch.RequirementDecisions["Security"] != "decision-2" {
		t.Fatalf("batch: %+v %v", batch, err)
	}
}

// A card without SIZE takes the size of its storage form.
func TestTaskSizeOfACardWithoutSize(t *testing.T) {
	for path, want := range map[string]string{"kanban/todo/20261127-old-task.md": "small", "kanban/todo/20261127-new-task": "large"} {
		if size, err := TaskSize(Entry{TaskID: "20261127-x-task", Path: path}, "# Old\n\n- TYPE: Feature\n"); err != nil || size != want {
			t.Fatalf("%s: %s %v", path, size, err)
		}
	}
	if size, err := TaskSize(Entry{Path: "kanban/todo/20261127-x-task"}, "# X\n\n- TYPE: Feature\n- SIZE: small\n"); err != nil || size != "small" {
		t.Fatalf("explicit size: %s %v", size, err)
	}
}
