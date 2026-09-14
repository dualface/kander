package board

import (
	"strings"
	"testing"
)

func TestFinalizeRejectsInvalidFindingRelationships(t *testing.T) {
	for _, kind := range []string{"no-predecessor", "duplicate-reference", "missing-item", "reused-id"} {
		t.Run(kind, func(t *testing.T) {
			root := tempBoard(t)
			id := gateCard(t, root, "lineage")
			gatePlan(t, root, []string{id}, archiveRequirements())
			input := archiveInput([]string{id}, "bad", "PM")
			input.FindingsSchema = 1
			originals := archiveOriginals()
			var advance *ReviewAdvance
			if kind != "no-predecessor" {
				prior := input
				prior.RunID = "first"
				f := emptyFindings()
				f.NonBlocking = []ReviewFinding{{ID: "PM-01", Tier: "low", Text: "Prior item", Evidence: "a:1"}}
				run := gateRun(t, root, prior, f)
				assignGate(t, root, run, map[string][]string{"PM-01": {id}})
				gateRecord(t, root, run, id, f.NonBlocking[0], "deferred")
				context, err := ReviewIncrementalContext(root, run.RunID)
				if err != nil {
					t.Fatal(err)
				}
				originals["review-context.md"] = MergeReviewContext(context, "")
				input.PreviousRunID, input.ReviewedCommit, input.Commit = run.RunID, run.Commit, strings.Repeat("c", 40)
				advance = &ReviewAdvance{PreviousTarget: run.Commit, Target: input.Commit, Reason: "fix scope", Deliveries: map[string]string{input.Commit: id}}
			}
			f := emptyFindings()
			f.Findings = []ReviewFinding{{ID: "PM-02", Tier: "medium", Text: "Invalid lineage", Evidence: "a:1", Lineage: &FindingRef{RunID: "first", FindingID: "PM-01"}}}
			switch kind {
			case "duplicate-reference":
				f.NonBlocking = []ReviewFinding{{ID: "PM-03", Tier: "low", Text: "Duplicate lineage", Evidence: "a:1", Lineage: &FindingRef{RunID: "first", FindingID: "PM-01"}}}
			case "missing-item":
				f.Findings[0].Lineage.FindingID = "MISSING"
			case "reused-id":
				f.Findings[0].ID, f.Findings[0].Lineage = "PM-01", nil
			}
			report := structuredReport(f)
			if _, err := ParseReviewFindings(report); err != nil {
				t.Fatal(err)
			}
			run, _, err := PrepareReviewRun(root, input, nil, advance, originals, "test")
			if err != nil {
				t.Fatal(err)
			}
			run.LaunchStatus, run.ExecutionStatus = "started", "ok"
			run, err = FinalizeReviewRun(root, run, report)
			if err != nil || run.ExecutionStatus != "failed" || run.ExitCode == 0 || run.FailureReason == "" {
				t.Fatalf("invalid relation became ok: %+v %v", run, err)
			}
			publishRun(t, root, run.RunID)
			if _, err = ReadReviewBatchView(root, "batch"); err != nil {
				t.Fatalf("failed relation poisoned aggregate: %v", err)
			}
			if _, err = gateClose(t, root, map[string]ReviewRoleConclusion{"PM": passRole(run)}); err == nil {
				t.Fatal("invalid relation established PASS")
			}
			bytes, err := ReadReviewOriginal(root, run.RunID, "report.md")
			if err != nil || string(bytes) != string(report) {
				t.Fatal("report bytes changed")
			}
		})
	}
}
