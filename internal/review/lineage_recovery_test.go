//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestLineageFailuresRecoverThroughControlledCLI(t *testing.T) {
	for _, kind := range []string{"no-predecessor", "duplicate-reference", "missing-item", "reused-id", "foreign-predecessor"} {
		t.Run(kind, func(t *testing.T) {
			h, root, _ := archiveHarness(t)
			ids := []string{"20260907-archive-test-task", "20260907-other-test-task"}
			for _, id := range ids {
				dir := filepath.Join(root, "working", id)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				text := "# Lineage fixture\n\n- TYPE: Chore\n- SIZE: small\n- LANGUAGE: zh-CN\n- TASK_GROUP: 20260907-lineage-group\n- OWNER: codex\n- STARTED_AT: 2026-01-01 00:00\n- TASK_BRANCH: fixture\n\n## GOAL\n\nVerify lineage recovery.\n\n## SUMMARY\n\nFixture delivery verified.\n"
				if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			requirements := map[string]string{"PM": "required", "QA": "N/A: fixture", "CSA": "N/A: fixture", "Hacker": "N/A: fixture"}
			p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan", Author: "main", Basis: "lineage recovery", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: ids, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: ids, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
			commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
			first := board.ReviewFinding{ID: "PM-01", Tier: "medium", Text: "Original finding", Evidence: "a.txt:1"}
			setReport := func(f board.ReviewFindings) string {
				data, err := json.Marshal(f)
				if err != nil {
					t.Fatal(err)
				}
				report := "```kander-findings\n" + string(data) + "\n```"
				t.Setenv("FAKE_CODEX_REPORT", report)
				return report + "\n"
			}
			args := func(id, commit, previous string) []string {
				a := []string{"codex", "--task", ids[0], "--task", ids[1], "--batch-id", "batch", "--run-id", id}
				if previous != "" {
					a = append(a, "--previous-run-id", previous)
				}
				return append(a, h.repo, h.base, commit, "PM", "Frozen lineage goal")
			}
			assign := func(id string, finding *board.ReviewFinding) {
				t.Helper()
				items := map[string][]string{}
				if finding != nil {
					items[finding.ID] = []string{ids[0]}
				}
				commandOK(t, "assign", h.repo, dispositionJSON(t, h, id+"-assignment", board.ReviewAssignment{RunID: id, BatchID: "batch", Author: "main", Basis: "exact report coverage", Items: items}))
				if finding == nil {
					return
				}
				run, err := board.ReadReviewRun(root, id)
				if err != nil {
					t.Fatal(err)
				}
				s, err := board.ReadSnapshot(root, ids[0])
				if err != nil {
					t.Fatal(err)
				}
				d := board.ReviewDisposition{RecordID: id + "-author", RunID: id, BatchID: "batch", FindingID: finding.ID, TaskID: ids[0], Author: "codex", ReportHash: run.Hashes["report.md"], Original: finding.Text, Status: "rejected", Basis: "Independently checked source; claimed defect is not present"}
				commandOK(t, "disposition", h.repo, dispositionJSON(t, h, id+"-author", d), itoa(int(s.Revision)))
			}
			previous, target := "", h.head
			if kind != "no-predecessor" {
				setReport(board.ReviewFindings{Findings: []board.ReviewFinding{first}, NonBlocking: []board.ReviewFinding{}})
				commandOK(t, args("first", h.head, "")...)
				assign("first", &first)
				previous = "first"
				target = commitFile(t, h.repo, "fix.txt", "fix scope\n", "fix")
				commandOK(t, "advance", h.repo, dispositionJSON(t, h, "advance", board.ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: board.ReviewAdvance{PreviousTarget: h.head, Target: target, Reason: "member delivery", Deliveries: map[string]string{target: ids[0]}}}))
			}
			bad := board.ReviewFindings{Findings: []board.ReviewFinding{{ID: "PM-02", Tier: "medium", Text: "Invalid relation", Evidence: "a.txt:1", Lineage: &board.FindingRef{RunID: "first", FindingID: first.ID}}}, NonBlocking: []board.ReviewFinding{}}
			want := "finding lineage must reference unique immediate predecessor item"
			switch kind {
			case "duplicate-reference":
				bad.NonBlocking = []board.ReviewFinding{{ID: "PM-03", Tier: "low", Text: "Duplicate relation", Evidence: "a.txt:1", Lineage: &board.FindingRef{RunID: "first", FindingID: first.ID}}}
			case "missing-item":
				bad.Findings[0].Lineage.FindingID = "PM-MISSING"
				want = "lineage item not in predecessor"
			case "reused-id":
				bad.Findings[0] = first
				want = "reused finding ID requires explicit lineage"
			case "foreign-predecessor":
				bad.Findings[0].Lineage.RunID = "foreign"
			}
			original := setReport(bad)
			if _, err := board.ParseReviewFindings([]byte(original)); err != nil {
				t.Fatalf("fixture must be syntactically valid: %v", err)
			}
			badArgs := args("invalid", target, previous)
			code, output, stderr := captureRun(t, badArgs)
			if code == 0 || output != original || !strings.Contains(stderr, want) {
				t.Fatalf("invalid relation accepted: %d %q %s", code, output, stderr)
			}
			failed, err := board.ReadReviewRun(root, "invalid")
			if err != nil || failed.ExecutionStatus != "failed" || failed.ExitCode == 0 || !strings.Contains(failed.FailureReason, want) {
				t.Fatalf("%+v %v", failed, err)
			}
			if code, _, _ := captureRun(t, badArgs); code == 0 {
				t.Fatal("retry promoted failed output")
			}
			if code, _, _ := captureRun(t, []string{"assign", h.repo, dispositionJSON(t, h, "bad-assignment", board.ReviewAssignment{RunID: "invalid", BatchID: "batch", Author: "main", Basis: "cannot use invalid output", Items: map[string][]string{}})}); code == 0 {
				t.Fatal("invalid relation assigned")
			}
			commandOK(t, "aggregate", h.repo, "batch")
			for _, id := range ids {
				commandOK(t, "progress", h.repo, id)
				if board.RunMove([]string{id, "done", "--result", "completed"}) == 0 {
					t.Fatal("invalid report completed member")
				}
			}
			if previous != "" {
				// Resume the last valid predecessor, retaining the failed attempt as evidence.
				first.Lineage = &board.FindingRef{RunID: "first", FindingID: first.ID}
				setReport(board.ReviewFindings{Findings: []board.ReviewFinding{first}, NonBlocking: []board.ReviewFinding{}})
			} else {
				t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
			}
			if code, _, _ := captureRun(t, args("invalid-predecessor", target, "invalid")); code == 0 {
				t.Fatal("failed report used as semantic predecessor")
			}
			commandOK(t, args("replacement", target, previous)...)
			if previous != "" {
				prompt, err := board.ReadReviewOriginal(root, "replacement", "prompt.txt")
				if err != nil || !strings.Contains(string(prompt), "Automatic archived review context:") || strings.Contains(string(prompt), "KANDER_AUTOMATIC_CONTEXT_BYTES:") || strings.Contains(string(prompt), "Additional caller-supplied review context:") {
					t.Fatalf("empty supplement prompt framing: %v %s", err, prompt)
				}
			}
			if previous == "" {
				assign("replacement", nil)
			} else {
				assign("replacement", &first)
			}
			v, err := board.ReadReviewBatchView(root, "batch")
			if err != nil {
				t.Fatal(err)
			}
			r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "main", Roles: map[string]board.ReviewRoleConclusion{"PM": {RunID: "replacement", PassedAt: target, Basis: "verified valid report and original author conclusions"}}}
			if code, _, _ := captureRun(t, []string{"close", h.repo, dispositionJSON(t, h, "close", r)}); code == 0 {
				t.Fatal("failed attempt implicitly discarded")
			}
			r.ResolvedFailures = map[string]string{"invalid": "replacement"}
			commandOK(t, "close", h.repo, dispositionJSON(t, h, "close", r))
			for _, id := range ids {
				if board.RunMove([]string{id, "done", "--result", "completed"}) != 0 {
					t.Fatal("recovered member cannot finish")
				}
			}
			after, err := board.ReadReviewRun(root, "invalid")
			if err != nil || !reflect.DeepEqual(after, failed) {
				t.Fatal("failed run rewritten")
			}
			saved, err := board.ReadReviewOriginal(root, "invalid", "report.md")
			if err != nil || string(saved) != original {
				t.Fatal("invalid original rewritten")
			}
			if problems := board.CheckReviewGate(root, ids); len(problems) > 0 {
				t.Fatal(problems)
			}
		})
	}
}
