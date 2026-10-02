//go:build unix

package review

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// pinnedArchiveHarness pins the PMQA reviewer on the archive card, requires both
// roles, and reviews the live card spec instead of a goal string.
func pinnedArchiveHarness(t *testing.T, pins string) (*reviewHarness, string, []string, string) {
	t.Helper()
	h, root, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_REPORT", "```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```")
	id := args[2]
	spec := filepath.Join(root, "working", id, "spec.md")
	text := strings.Replace(readFile(t, spec), "- TASK_BRANCH: task\n", "- TASK_BRANCH: task\n"+pins, 1)
	if err := os.WriteFile(spec, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(args[10], []byte(`{"PMQA":"required","Security":"required"}`), 0600); err != nil {
		t.Fatal(err)
	}
	args[len(args)-1] = spec
	return h, root, args, spec
}

func TestPinnedReviewRecordsKeepTheBatchContextFrozen(t *testing.T) {
	h, root, args, spec := pinnedArchiveHarness(t, "- REVIEW_PMQA_AGENT: codex\n- REVIEW_PMQA_MODEL: pinned-review\n")
	id := args[2]
	if code, _, stderr := captureRun(t, args); code != 0 {
		t.Fatalf("PMQA: %d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil || run.Model != "pinned-review" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	if argv := readFile(t, h.argvLog); !strings.Contains(argv, "pinned-review") {
		t.Fatalf("argv=%s", argv)
	}
	if card := readFile(t, spec); !strings.Contains(card, "- REVIEW_PMQA_RESOLVED: agent=codex(forced) model=pinned-review(forced) effort=") {
		t.Fatalf("card=%s", card)
	}
	frozenPath := filepath.Join(filepath.Dir(spec), "reviews", "stable", "task-context.md")
	if frozen := readFile(t, frozenPath); strings.Contains(frozen, "_RESOLVED:") || !strings.Contains(frozen, "- REVIEW_PMQA_MODEL: pinned-review\n") {
		t.Fatalf("frozen context=%s", frozen)
	}

	// Later runs of the batch pass the frozen snapshot, as for every card whose
	// live spec gained a REVIEWS index line; the records never enter it, so the
	// pins still come from the bound card itself.
	security := append([]string(nil), args...)
	security[6], security[len(security)-2], security[len(security)-1] = "security", "Security", frozenPath
	if code, _, stderr := captureRun(t, security); code != 0 {
		t.Fatalf("Security: %d %s", code, stderr)
	}
	if card := readFile(t, spec); !strings.Contains(card, "- REVIEW_SECURITY_RESOLVED: agent=codex(cli) model=") || !strings.Contains(card, "(config:small)") {
		t.Fatalf("card=%s", card)
	}

	// An executor relaunch rewrites EXEC_RESOLVED before the incremental round.
	text, err := board.WithResolvedRecord(readFile(t, spec), board.FieldExecResolved, "agent=claude(config:small)")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{"stable", "security"} {
		if err := board.AssignReviewFindings(root, board.ReviewAssignment{RunID: runID, BatchID: "batch", Author: "coordinator", Basis: "empty structured report", Items: map[string][]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	next := commitFile(t, h.repo, "fix.txt", "fix", "fix")
	advance := filepath.Join(h.root, "advance.json")
	data, _ := json.Marshal(board.ReviewAdvance{PreviousTarget: h.head, Target: next, Reason: "fix", Deliveries: map[string]string{next: id}})
	if err := os.WriteFile(advance, data, 0600); err != nil {
		t.Fatal(err)
	}
	incremental := []string{"codex", "--task", id, "--batch-id", "batch", "--run-id", "fixed", "--previous-run-id", "stable", "--advance-file", advance, h.repo, h.base, next, "PMQA", frozenPath, "fix context", h.head}
	if code, _, stderr := captureRun(t, incremental); code != 0 {
		t.Fatalf("incremental: %d %s", code, stderr)
	}
	if fixed, err := board.ReadReviewRun(root, "fixed"); err != nil || fixed.Model != "pinned-review" {
		t.Fatalf("incremental run=%+v err=%v", fixed, err)
	}
}

func TestPinnedReviewConflictsFailBeforeTheReviewer(t *testing.T) {
	for name, setup := range map[string]func(args []string) []string{
		"cli reviewer": func(args []string) []string {
			args[0] = "claude"
			return args
		},
		"environment model": func(args []string) []string {
			t.Setenv("CODEX_REVIEW_MODEL", "other-model")
			return args
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, root, args, _ := pinnedArchiveHarness(t, "- REVIEW_PMQA_AGENT: codex\n- REVIEW_PMQA_MODEL: pinned-review\n")
			code, _, stderr := captureRun(t, setup(args))
			if code != 2 || !strings.Contains(stderr, "REVIEW_PMQA_") {
				t.Fatalf("%d %s", code, stderr)
			}
			if _, exists, err := board.LookupReviewRun(root, "stable"); err != nil || exists {
				t.Fatalf("run persisted: %v %v", exists, err)
			}
			if _, err := os.Stat(h.argvLog); err == nil {
				t.Fatal("reviewer started")
			}
		})
	}
}

func TestBoundReviewPinningRequiresOneSettingPerRole(t *testing.T) {
	_, root, args, spec := pinnedArchiveHarness(t, "- REVIEW_PMQA_AGENT: codex\n")
	first := args[2]
	second := "20260907-archive-other-task"
	if err := os.MkdirAll(filepath.Join(root, "working", second), 0700); err != nil {
		t.Fatal(err)
	}
	other := strings.Replace(readFile(t, spec), "- SIZE: small\n", "- SIZE: large\n", 1)
	if err := os.WriteFile(filepath.Join(root, "working", second, "spec.md"), []byte(other), 0600); err != nil {
		t.Fatal(err)
	}
	pinning, err := boundReviewPinning(root, []string{first, second}, "PMQA")
	if err != nil || pinning.pin.Agent != "codex" || pinning.scale != "large" {
		t.Fatalf("pinning=%+v err=%v", pinning, err)
	}
	unpinned := strings.Replace(other, "- REVIEW_PMQA_AGENT: codex\n", "", 1)
	if err := os.WriteFile(filepath.Join(root, "working", second, "spec.md"), []byte(unpinned), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := boundReviewPinning(root, []string{first, second}, "PMQA"); err == nil || !strings.Contains(err.Error(), second) {
		t.Fatalf("forced vs unset accepted: %v", err)
	}
	if pinning, err := boundReviewPinning(root, []string{second}, "PMQA"); err != nil || pinning.any || pinning.scale != "" {
		t.Fatalf("unpinned card pinning=%+v err=%v", pinning, err)
	}
}

func TestPlanNoticeForPinnedNotApplicableRole(t *testing.T) {
	_, root, args, _ := pinnedArchiveHarness(t, "- REVIEW_SECURITY_AGENT: codex\n")
	plan := board.ReviewPlan{Batches: []board.ReviewPlanBatch{{BatchID: "b1", TaskIDs: []string{args[2]}, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: skipped by review_stages"}}}}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = write
	notePinnedNotApplicable(root, plan)
	os.Stderr = old
	_ = write.Close()
	out, _ := io.ReadAll(read)
	if strings.Count(string(out), "\n") != 1 || !strings.Contains(string(out), args[2]) || !strings.Contains(string(out), "Security") {
		t.Fatalf("notice=%q", out)
	}
}
