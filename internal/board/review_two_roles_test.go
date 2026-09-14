package board

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func legacyFourRolePlan(t *testing.T, root, id string) {
	t.Helper()
	plan := gatePlan(t, root, []string{id}, noReviewRequirements())
	// Seed the exact persisted shape of the old producer, before any run exists.
	err := WithTransaction(root, reviewScope([]string{id}, false), func(tx *Transaction) error {
		var stored ReviewPlan
		if _, err := readReviewJSON(tx, planName(plan.PlanID), &stored); err != nil {
			return err
		}
		var batch ReviewBatch
		if _, err := readReviewJSON(tx, reviewBatchName("batch"), &batch); err != nil {
			return err
		}
		requirements := map[string]string{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required"}
		stored.Batches[0].Requirements = requirements
		batch.Requirements = requirements
		if err := tx.PutGroup(reviewControlGroup, planName(plan.PlanID), reviewJSON(stored)); err != nil {
			return err
		}
		if err := tx.PutGroup(reviewControlGroup, reviewBatchName("batch"), reviewJSON(batch)); err != nil {
			return err
		}
		return tx.Put(id, "reviews/plan.json", reviewJSON(stored))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLegacyFourRoleEvidenceCheckAggregateCloseAndRecovery(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "four-role-history")
	legacyFourRolePlan(t, root, id)
	roles := map[string]ReviewRoleConclusion{}
	originals := map[string]string{}
	for _, role := range []string{"PM", "QA", "CSA", "Hacker"} {
		run := gateRun(t, root, archiveInput([]string{id}, "old-"+strings.ToLower(role), role), emptyFindings())
		assignGate(t, root, run, map[string][]string{})
		roles[role] = passRole(run)
		originals[run.RunID] = reviewJSON(run)
	}
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	closure, err := gateClose(t, root, roles)
	if err != nil || len(closure.RoleStatuses) != 4 {
		t.Fatalf("%+v %v", closure, err)
	}
	if code, out, stderr, err := CheckBoard(root, []string{id}, true); err != nil || code != 0 {
		t.Fatalf("%d %s %v %v", code, out, stderr, err)
	}
	// Same-run publication/recovery keeps the original bytes and legacy role names.
	for runID, original := range originals {
		recovered := publishRun(t, root, runID)
		if reviewJSON(recovered) != original {
			t.Fatal("recovery rewrote", runID)
		}
	}
	plan, err := ReadReviewPlan(root, "plan-"+id)
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateReviewPlan(root, plan); err != nil {
		t.Fatal("historical same-plan retry", err)
	}
	view, err := ReadReviewBatchView(root, "batch")
	if err != nil || !reflect.DeepEqual(view.Batch.Requirements, plan.Batches[0].Requirements) {
		t.Fatal("historical requirements rewritten", err)
	}
}

func TestNewRequirementsRejectLegacyAndMixedRoleSets(t *testing.T) {
	for _, requirements := range []map[string]string{
		{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required"},
		{"QA": "required"},
		{"QA": "required", "Security": "required", "PM": "N/A: ignored"},
		{"QA": "required", "CSA": "required"},
	} {
		root := tempBoard(t)
		id := gateCard(t, root, "invalid-roles")
		input := archiveInput([]string{id}, "new-run", "QA")
		if _, _, err := PrepareReviewRun(root, input, requirements, nil, archiveOriginals(), "test"); err == nil {
			t.Fatal("new legacy/mixed batch accepted", requirements)
		}
		plan := ReviewPlan{Schema: 1, Sealed: true, PlanID: "new-plan", Author: "coordinator", Basis: "current contract", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: input.Base, TargetCommit: input.Commit, Requirements: requirements}}}
		if err := CreateReviewPlan(root, plan); err == nil {
			t.Fatal("new legacy/mixed plan accepted", requirements)
		}
	}
}

func TestHistoricalWaiverReadValidationAndNewWriteRestriction(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "historical-waiver")
	legacyFourRolePlan(t, root, id)
	roles := map[string]ReviewRoleConclusion{}
	for _, role := range []string{"CSA", "Hacker"} {
		f := ReviewFinding{ID: role + "-01", Tier: "high", Text: "historical risk", Evidence: "file.go:1"}
		findings := emptyFindings()
		findings.Findings = append(findings.Findings, f)
		run := gateRun(t, root, archiveInput([]string{id}, strings.ToLower(role), role), findings)
		assignment := ReviewAssignment{Owners: map[string]string{id: "codex"}, RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "fixture", Items: map[string][]string{f.ID: {id}}}
		if err := AssignReviewFindings(root, assignment); err != nil {
			t.Fatal(err)
		}
		d := ReviewDisposition{RecordID: "legacy-" + strings.ToLower(role), RunID: run.RunID, BatchID: run.BatchID, FindingID: f.ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "waived", Basis: "historical decision", Waiver: &ReviewWaiver{Policy: "accepted-risk", Decision: "user accepted"}}
		if err := validateDisposition(d, run, f, assignment); err != nil {
			t.Fatal("historical waiver unreadable", err)
		}
		if err := SubmitReviewDisposition(root, d, transactionSnapshot(t, root, id).Revision); err == nil || !strings.Contains(err.Error(), "Security") {
			t.Fatal("new legacy waiver accepted", err)
		}
		// Seed an immutable waiver written by the historical producer, then use
		// only normal readers, aggregate, closure and same-record retry paths.
		d.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		d.SubmittedRevision = transactionSnapshot(t, root, id).Revision
		err := WithTransaction(root, reviewScope([]string{id}, false), func(tx *Transaction) error {
			var ledger dispositionLedger
			if _, err := readReviewJSON(tx, ledgerName(run.RunID), &ledger); err != nil {
				return err
			}
			ledger.Records = append(ledger.Records, d)
			if err := tx.PutGroup(reviewControlGroup, ledgerName(run.RunID), reviewJSON(ledger)); err != nil {
				return err
			}
			return tx.Put(id, dispositionPath(d), reviewJSON(d))
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := SubmitReviewDisposition(root, d, transactionSnapshot(t, root, id).Revision); err != nil {
			t.Fatal("historical waiver retry", err)
		}
		roles[role] = passRole(run)
	}
	for _, role := range []string{"PM", "QA"} {
		run := gateRun(t, root, archiveInput([]string{id}, strings.ToLower(role), role), emptyFindings())
		assignGate(t, root, run, map[string][]string{})
		roles[role] = passRole(run)
	}
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	closure, err := gateClose(t, root, roles)
	if err != nil || closure.RoleStatuses["CSA"] != "accepted-risk" || closure.RoleStatuses["Hacker"] != "accepted-risk" {
		t.Fatalf("%+v %v", closure, err)
	}
	if code, out, stderr, err := CheckBoard(root, []string{id}, true); err != nil || code != 0 {
		t.Fatalf("%d %s %v %v", code, out, stderr, err)
	}
}
