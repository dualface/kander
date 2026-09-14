//go:build unix

package review

import (
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestIntegratedReviewCLIAndAliases(t *testing.T) {
	for _, role := range []string{"PMQA", "Security"} {
		t.Run(role, func(t *testing.T) {
			_, root, args := archiveHarness(t)
			requirements := `{"PMQA":"required","Security":"required"}`
			for i, arg := range args {
				if arg == "--requirements-file" {
					if err := os.WriteFile(args[i+1], []byte(requirements), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			args[len(args)-2] = strings.ToLower(role)
			t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
			if code, _, stderr := captureRun(t, args); code != 0 {
				t.Fatalf("%d %s", code, stderr)
			}
			run, err := board.ReadReviewRun(root, "stable")
			if err != nil || run.Role != role || run.ExecutionStatus != "ok" {
				t.Fatalf("%+v %v", run, err)
			}
			// Omitting the explicit reviewer exercises the other role-resolution path.
			args[len(args)-2] = strings.ToUpper(role)
			if code, _, stderr := captureRun(t, args[1:]); code != 0 {
				t.Fatalf("alias/retry %d %s", code, stderr)
			}
		})
	}
}

func TestReviewCLIRejectsDeletedRoles(t *testing.T) {
	h := newCodexHarness(t)
	for _, role := range []string{"PM", "QA", "CSA", "Hacker", "CodeSecurityAnalyst"} {
		code, _, stderr := h.review("codex", role, "goal")
		if code != 2 || !strings.Contains(stderr, "unsupported role: "+role) {
			t.Fatalf("%s: %d %s", role, code, stderr)
		}
	}
}

func TestReviewCLIAcceptsHistoricalRoleOnRequiredBatch(t *testing.T) {
	h, root, args := archiveHarness(t)
	id := "20260907-archive-test-task"
	four := map[string]string{"PM": "required", "QA": "N/A: fixture", "CSA": "N/A: fixture", "Hacker": "N/A: fixture"}
	p := board.ReviewPlan{
		Schema: 1, Sealed: true, PlanID: "historical-pm", Author: "coordinator",
		Basis: "historical four-role fixture", CWD: h.repo, ReportLanguage: "zh-CN",
		TaskIDs: []string{id},
		Batches: []board.ReviewPlanBatch{{
			BatchID: "hist-pm", TaskIDs: []string{id}, Base: h.base, TargetCommit: h.head,
			Requirements: four,
		}},
	}
	if err := board.CreateHistoricalReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	req := `{"PM":"required","QA":"N/A: fixture","CSA":"N/A: fixture","Hacker":"N/A: fixture"}`
	for i, arg := range args {
		if arg == "--batch-id" {
			args[i+1] = "hist-pm"
		}
		if arg == "--requirements-file" {
			if err := os.WriteFile(args[i+1], []byte(req), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if arg == "--run-id" {
			args[i+1] = "hist-pm-run"
		}
	}
	args[len(args)-2] = "PM"
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	if code, _, stderr := captureRun(t, args); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "hist-pm-run")
	if err != nil || run.Role != "PM" || run.ExecutionStatus != "ok" {
		t.Fatalf("%+v %v", run, err)
	}
}

func TestReviewCLIRejectsHistoricalRoleOnTwoKeyBatch(t *testing.T) {
	_, _, args := archiveHarness(t)
	args[len(args)-2] = "PM"
	if code, _, stderr := captureRun(t, args); code != 2 || !strings.Contains(stderr, "unsupported role") {
		t.Fatalf("%d %s", code, stderr)
	}
}
