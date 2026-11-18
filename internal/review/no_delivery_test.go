//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

const noDeliveryTask = "20260907-archive-test-task"

var noDeliveryNA = map[string]string{"PMQA": "N/A: no delivery commit", "Security": "N/A: no delivery commit"}

func planNoDelivery(t *testing.T, h *reviewHarness, batches ...board.ReviewPlanBatch) {
	t.Helper()
	p := board.ReviewPlan{Schema: 1, PlanID: "plan", Author: "owner", Basis: "the card has no delivery commit", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{noDeliveryTask}, Batches: batches}
	p.Sealed = true
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
}

func naBatch(id, previous, base, target string) board.ReviewPlanBatch {
	return board.ReviewPlanBatch{BatchID: id, PreviousBatchID: previous, TaskIDs: []string{noDeliveryTask}, Base: base, TargetCommit: target, Requirements: noDeliveryNA}
}

// closeFile aggregates the batch and writes the close request for its view.
func closeFile(t *testing.T, h *reviewHarness, root, batch string) string {
	t.Helper()
	commandOK(t, "aggregate", h.repo, batch)
	v, err := board.ReadReviewBatchView(root, batch)
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: batch, ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "owner", Roles: map[string]board.ReviewRoleConclusion{}}
	return dispositionJSON(t, h, "close-"+batch, r)
}

func closureOf(t *testing.T, out string) board.ReviewClosure {
	t.Helper()
	var c board.ReviewClosure
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return c
}

func TestNoDeliveryBatchClosesAfterHeadMovedOnAndWorktreeIsDirty(t *testing.T) {
	h, root, _ := archiveHarness(t)
	planNoDelivery(t, h, naBatch("batch", "", h.head, h.head))
	later := commitFile(t, h.repo, "later.txt", "another card's delivery\n", "later delivery")
	if err := os.WriteFile(filepath.Join(h.repo, "uncommitted"), []byte("user change"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := closeFile(t, h, root, "batch")
	out := commandOK(t, "close", h.repo, file)
	c := closureOf(t, out)
	if !c.Git.NoDelivery || c.Git.ObservedHead != later || c.Git.Head != h.head || c.TargetCommit != h.head {
		t.Fatalf("closure evidence: %+v", c.Git)
	}
	if problems := board.CheckReviewGate(root, []string{noDeliveryTask}); len(problems) > 0 {
		t.Fatalf("%v", problems)
	}
	if out = commandOK(t, "progress", h.repo, noDeliveryTask); !strings.Contains(out, `"status": "closed"`) {
		t.Fatal(out)
	}
}

func TestNoDeliveryBatchRefusesATargetOutsideHeadHistory(t *testing.T) {
	h, root, _ := archiveHarness(t)
	side := commitFile(t, h.repo, "side.txt", "side\n", "side")
	gitRepo(t, h.repo, "reset", "-q", "--hard", h.base)
	t.Run("plan", func(t *testing.T) {
		p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan", Author: "owner", Basis: "fixture", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{noDeliveryTask}, Batches: []board.ReviewPlanBatch{naBatch("batch", "", side, side)}}
		code, _, stderr := captureRun(t, []string{"plan", h.repo, dispositionJSON(t, h, "plan", p)})
		if code == 0 || !strings.Contains(stderr, side) || !strings.Contains(stderr, h.base) {
			t.Fatalf("accepted: %d %s", code, stderr)
		}
		if _, err := board.ReadReviewPlan(root, "plan"); err == nil {
			t.Fatal("plan written")
		}
	})
	t.Run("close", func(t *testing.T) {
		gitRepo(t, h.repo, "reset", "-q", "--hard", side)
		planNoDelivery(t, h, naBatch("batch", "", side, side))
		file := closeFile(t, h, root, "batch")
		gitRepo(t, h.repo, "reset", "-q", "--hard", h.base)
		code, _, stderr := captureRun(t, []string{"close", h.repo, file})
		if code == 0 || !strings.Contains(stderr, side) || !strings.Contains(stderr, h.base) {
			t.Fatalf("accepted: %d %s", code, stderr)
		}
		if progress, err := board.ReviewTaskProgress(root, noDeliveryTask); err != nil || progress.Status == "closed" {
			t.Fatalf("the refused close left the batch closed: %+v %v", progress, err)
		}
		gitRepo(t, h.repo, "reset", "-q", "--hard", side)
		commandOK(t, "close", h.repo, file)
	})
}

func TestBatchWithADeliveryKeepsTheStrictClosureChecks(t *testing.T) {
	t.Run("N/A roles, base differs from target", func(t *testing.T) {
		h, root, _ := archiveHarness(t)
		planNoDelivery(t, h, naBatch("batch", "", h.base, h.head))
		file := closeFile(t, h, root, "batch")
		commitFile(t, h.repo, "later.txt", "later\n", "later")
		code, _, stderr := captureRun(t, []string{"close", h.repo, file})
		if code == 0 || !strings.Contains(stderr, "clean worktree") {
			t.Fatalf("moved HEAD accepted: %d %s", code, stderr)
		}
		gitRepo(t, h.repo, "reset", "-q", "--hard", h.head)
		if err := os.WriteFile(filepath.Join(h.repo, "uncommitted"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, _, stderr = captureRun(t, []string{"close", h.repo, file})
		if code == 0 || !strings.Contains(stderr, "clean worktree") {
			t.Fatalf("dirty worktree accepted: %d %s", code, stderr)
		}
	})
	t.Run("required role, base equals target", func(t *testing.T) {
		h, root, _ := archiveHarness(t)
		b := naBatch("batch", "", h.head, h.head)
		b.Requirements = map[string]string{"PMQA": "required", "Security": "N/A: project"}
		planNoDelivery(t, h, b)
		file := closeFile(t, h, root, "batch")
		commitFile(t, h.repo, "later.txt", "later\n", "later")
		code, _, stderr := captureRun(t, []string{"close", h.repo, file})
		if code == 0 || !strings.Contains(stderr, "clean worktree") {
			t.Fatalf("moved HEAD accepted: %d %s", code, stderr)
		}
	})
}

func TestTaskGroupBatchWithNoDeliveryClosesAfterItsPredecessor(t *testing.T) {
	h, root, _ := archiveHarness(t)
	planNoDelivery(t, h, naBatch("first", "", h.base, h.head), naBatch("second", "first", h.head, h.head))
	commandOK(t, "close", h.repo, closeFile(t, h, root, "first"))
	commitFile(t, h.repo, "later.txt", "later\n", "later")
	c := closureOf(t, commandOK(t, "close", h.repo, closeFile(t, h, root, "second")))
	if !c.Git.NoDelivery || c.Git.Head != h.head {
		t.Fatalf("%+v", c.Git)
	}
	if problems := board.CheckReviewGate(root, []string{noDeliveryTask}); len(problems) > 0 {
		t.Fatalf("%v", problems)
	}
}

func TestExtendPlanAppendsNoDeliveryBatchOnlyInsideHeadHistory(t *testing.T) {
	h, root, _ := archiveHarness(t)
	p := board.ReviewPlan{Schema: 1, PlanID: "plan", Author: "owner", Basis: "fixture", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{noDeliveryTask}, Batches: []board.ReviewPlanBatch{naBatch("first", "", h.base, h.head)}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	commandOK(t, "close", h.repo, closeFile(t, h, root, "first"))
	side := commitFile(t, h.repo, "side.txt", "side\n", "side")
	gitRepo(t, h.repo, "reset", "-q", "--hard", h.head)
	extend := func(target string) (int, string) {
		stored, err := board.ReadReviewPlan(root, "plan")
		if err != nil {
			t.Fatal(err)
		}
		b := naBatch("second", "first", target, target)
		code, _, stderr := captureRun(t, []string{"extend-plan", h.repo, dispositionJSON(t, h, "extend", board.ReviewPlanExtension{PlanID: "plan", ExpectedRevision: stored.Revision, Batch: &b, Author: "owner", Basis: "next batch"})})
		return code, stderr
	}
	if code, stderr := extend(side); code == 0 || !strings.Contains(stderr, side) {
		t.Fatalf("accepted: %d %s", code, stderr)
	}
	if stored, _ := board.ReadReviewPlan(root, "plan"); len(stored.Batches) != 1 {
		t.Fatalf("plan written: %+v", stored.Batches)
	}
	if code, stderr := extend(h.head); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestRetargetCorrectsWrongTargetThroughTheCLI(t *testing.T) {
	h, root, _ := archiveHarness(t)
	// The plan names the older commit, which is a valid but wrong target.
	planNoDelivery(t, h, naBatch("batch", "", h.base, h.base))
	staleFile := closeFile(t, h, root, "batch")
	retarget := func(r board.ReviewBatchRetarget) (int, string) {
		stored, err := board.ReadReviewPlan(root, "plan")
		if err != nil {
			t.Fatal(err)
		}
		x := board.ReviewPlanExtension{PlanID: "plan", ExpectedRevision: stored.Revision, RetargetBatches: map[string]board.ReviewBatchRetarget{"batch": r}, Author: "owner", Basis: "the target was the previous commit"}
		code, _, stderr := captureRun(t, []string{"extend-plan", h.repo, dispositionJSON(t, h, "retarget", x)})
		return code, stderr
	}
	side := commitFile(t, h.repo, "side.txt", "side\n", "side")
	gitRepo(t, h.repo, "reset", "-q", "--hard", h.head)
	t.Run("base is not an ancestor of the target", func(t *testing.T) {
		code, stderr := retarget(board.ReviewBatchRetarget{PreviousBase: h.base, PreviousTarget: h.base, Base: h.head, Target: h.base})
		if code == 0 || !strings.Contains(stderr, "Git ancestry") {
			t.Fatalf("accepted: %d %s", code, stderr)
		}
	})
	t.Run("no-delivery target outside HEAD history", func(t *testing.T) {
		code, stderr := retarget(board.ReviewBatchRetarget{PreviousBase: h.base, PreviousTarget: h.base, Base: side, Target: side})
		if code == 0 || !strings.Contains(stderr, side) {
			t.Fatalf("accepted: %d %s", code, stderr)
		}
	})
	if stored, _ := board.ReadReviewPlan(root, "plan"); stored.Revision != 1 || stored.Batches[0].TargetCommit != h.base {
		t.Fatalf("a refused request changed the plan: %+v", stored)
	}
	if code, stderr := retarget(board.ReviewBatchRetarget{PreviousBase: h.base, PreviousTarget: h.base, Base: h.head, Target: h.head}); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	// The view the batch had before the correction no longer closes it.
	if code, _, stderr := captureRun(t, []string{"close", h.repo, staleFile}); code == 0 {
		t.Fatalf("stale view closed the batch: %s", stderr)
	}
	c := closureOf(t, commandOK(t, "close", h.repo, closeFile(t, h, root, "batch")))
	if c.TargetCommit != h.head || !c.Git.NoDelivery {
		t.Fatalf("%+v", c)
	}
	if problems := board.CheckReviewGate(root, []string{noDeliveryTask}); len(problems) > 0 {
		t.Fatalf("%v", problems)
	}
}

func TestExtendPlanSchemaListsRetargetBatches(t *testing.T) {
	setupLang(t, filepath.Join(t.TempDir(), "config.json"))
	code, out, stderr := captureRun(t, []string{"extend-plan", "--schema"})
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	for _, field := range []string{"retarget_batches", "previous_base", "previous_target"} {
		if !strings.Contains(out, field) {
			t.Errorf("schema lacks %s:\n%s", field, out)
		}
	}
}
