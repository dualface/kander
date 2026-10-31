//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

const (
	resolveStray = "stray-b2"
	resolveRun   = "stray-run"
	resolveCover = "b3"
)

// resolveWorld is a blocked card on a real repository: b1 closed from base to head,
// the stray batch reviewed head to strayTarget, and b3 closed from head to cover.
type resolveWorld struct {
	h           *reviewHarness
	root, id    string
	strayBase   string
	strayTarget string
	cover       string
	side        string
	later       string
}

func resolveFinding() board.ReviewFindings {
	return board.ReviewFindings{Findings: []board.ReviewFinding{{ID: "PMQA-001", Tier: "medium", Text: "stale document", Evidence: "docs/x.md:1"}}, NonBlocking: []board.ReviewFinding{}}
}

func publishResolveRun(t *testing.T, root string, in board.ReviewInput, requirements map[string]string, f board.ReviewFindings) board.ReviewRun {
	t.Helper()
	originals := map[string][]byte{"task-context.md": []byte("task\n"), "review-context.md": []byte("context\n")}
	run, _, err := board.PrepareReviewRun(root, in, requirements, nil, originals, "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	report, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if run, err = board.FinalizeReviewRun(root, run, []byte("```kander-findings\n"+string(report)+"\n```\n")); err != nil {
		t.Fatal(err)
	}
	if _, failures, err := board.PublishReviewRun(root, run.RunID); err != nil || len(failures) > 0 {
		t.Fatalf("publish: %v %v", failures, err)
	}
	return run
}

func closeResolveBatch(t *testing.T, root, batch string, roles map[string]board.ReviewRoleConclusion, cwd string) {
	t.Helper()
	v, err := board.ReadReviewBatchView(root, batch)
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: batch, ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "coordinator", Roles: roles}
	edges, _, err := board.ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.CloseReviewBatch(root, r, board.ReviewGitEvidence{CWD: cwd, Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}); err != nil {
		t.Fatal(err)
	}
}

// newResolveWorld writes the stray run as v1.0.0 did: with the plan pointer away,
// because develop refuses a batch outside the plan before it exists.
func newResolveWorld(t *testing.T, strayBase func(*resolveWorld) string, strayTarget func(*resolveWorld) string) *resolveWorld {
	t.Helper()
	h, root, _ := archiveHarness(t)
	w := &resolveWorld{h: h, root: root, id: "20260907-archive-test-task"}
	w.side = commitFile(t, h.repo, "side.txt", "side\n", "side")
	gitRepo(t, h.repo, "reset", "-q", "--hard", h.head)
	w.strayTarget = commitFile(t, h.repo, "c.txt", "c\n", "stray target")
	w.cover = commitFile(t, h.repo, "d.txt", "d\n", "fix")
	w.later = commitFile(t, h.repo, "e.txt", "e\n", "later")
	gitRepo(t, h.repo, "reset", "-q", "--hard", w.cover)
	w.strayBase = h.head
	repo := h.repoReal
	notApplicable := map[string]string{"PMQA": "N/A: fixture", "Security": "N/A: fixture"}
	required := map[string]string{"PMQA": "required", "Security": "N/A: fixture"}
	p := board.ReviewPlan{Schema: 1, PlanID: "stray-plan", Author: "coordinator", Basis: "fixture", CWD: repo, ReportLanguage: "zh-CN", TaskIDs: []string{w.id}, Batches: []board.ReviewPlanBatch{{BatchID: "b1", TaskIDs: []string{w.id}, Base: h.base, TargetCommit: h.head, Requirements: notApplicable}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	closeResolveBatch(t, root, "b1", map[string]board.ReviewRoleConclusion{}, repo)
	input := func(run, batch, base, commit string) board.ReviewInput {
		return board.ReviewInput{RunID: run, BatchID: batch, TaskIDs: []string{w.id}, Role: "PMQA", Reviewer: "codex", Model: "model", Effort: "high", CWD: repo, Base: base, Commit: commit, ReportLanguage: "zh-CN"}
	}
	pointer := filepath.Join(root, ".kander", "groups", "00000000-review-archive-group", "task-plans", w.id+".json")
	if err := os.Rename(pointer, pointer+".moved"); err != nil {
		t.Fatal(err)
	}
	publishResolveRun(t, root, input(resolveRun, resolveStray, strayBase(w), strayTarget(w)), required, resolveFinding())
	if err := os.Rename(pointer+".moved", pointer); err != nil {
		t.Fatal(err)
	}
	b3 := board.ReviewPlanBatch{BatchID: resolveCover, PreviousBatchID: "b1", TaskIDs: []string{w.id}, Base: h.head, TargetCommit: w.cover, Requirements: required}
	commandOK(t, "extend-plan", h.repo, dispositionJSON(t, h, "extend", board.ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, Batch: &b3, Author: "coordinator", Basis: "next batch"}))
	run := publishResolveRun(t, root, input("b3-run", resolveCover, h.head, w.cover), nil, board.ReviewFindings{Findings: []board.ReviewFinding{}, NonBlocking: []board.ReviewFinding{}})
	if err := board.AssignReviewFindings(root, board.ReviewAssignment{RunID: run.RunID, BatchID: resolveCover, Author: "coordinator", Basis: "no findings", Items: map[string][]string{}}); err != nil {
		t.Fatal(err)
	}
	closeResolveBatch(t, root, resolveCover, map[string]board.ReviewRoleConclusion{"PMQA": {RunID: run.RunID, PassedAt: run.Commit, Basis: "fixture"}}, repo)
	return w
}

func (w *resolveWorld) fixed(fix string) board.ReviewResolvedFinding {
	return board.ReviewResolvedFinding{RunID: resolveRun, FindingID: "PMQA-001", Status: "fixed", Basis: "fixed in the next commit", FixCommit: fix, Verification: "b3 reviewed the fix"}
}

// decided carries no fix commit, so only the coverage edges can refuse it.
func decided() board.ReviewResolvedFinding {
	return board.ReviewResolvedFinding{RunID: resolveRun, FindingID: "PMQA-001", Status: "rejected", Basis: "user accepted the wording", Decision: "user-1"}
}

func (w *resolveWorld) request(t *testing.T, finding board.ReviewResolvedFinding) string {
	t.Helper()
	p, err := board.ReadReviewPlan(w.root, "stray-plan")
	if err != nil {
		t.Fatal(err)
	}
	return dispositionJSON(t, w.h, "resolve", board.ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Author: "claude", Basis: "b3 reviewed the range", ResolveBatch: &board.ReviewBatchResolutionRequest{BatchID: resolveStray, CoveringBatchID: resolveCover, Findings: []board.ReviewResolvedFinding{finding}}})
}

func (w *resolveWorld) refused(t *testing.T, finding board.ReviewResolvedFinding) {
	t.Helper()
	before, err := board.ReadReviewPlan(w.root, "stray-plan")
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr := captureRun(t, []string{"extend-plan", w.h.repo, w.request(t, finding)})
	if code == 0 || !strings.Contains(stderr, "Git ancestry") {
		t.Fatalf("accepted: %d %s", code, stderr)
	}
	after, err := board.ReadReviewPlan(w.root, "stray-plan")
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || len(after.ResolvedBatches) != 0 {
		t.Fatalf("plan changed: %+v", after)
	}
}

func stayOnHead(w *resolveWorld) string    { return w.h.head }
func onStrayTarget(w *resolveWorld) string { return w.strayTarget }

func TestResolveBatchRecordsVerifiedGitThroughCLI(t *testing.T) {
	w := newResolveWorld(t, stayOnHead, onStrayTarget)
	if code, _, stderr := captureRun(t, []string{"progress", w.h.repo, w.id}); code == 0 || !strings.Contains(stderr, "unplanned execution batch: "+resolveStray) {
		t.Fatalf("stray batch not reported: %d %s", code, stderr)
	}
	commandOK(t, "extend-plan", w.h.repo, w.request(t, w.fixed(w.cover)))
	p, err := board.ReadReviewPlan(w.root, "stray-plan")
	if err != nil {
		t.Fatal(err)
	}
	git := p.ResolvedBatches[0].Git
	if git.CWD != w.h.repoReal || len(git.Edges) != 3 {
		t.Fatalf("Git evidence: %+v", git)
	}
	if out := commandOK(t, "progress", w.h.repo, w.id); !strings.Contains(out, `"resolved_batches"`) || !strings.Contains(out, `"PMQA-001"`) || !strings.Contains(out, w.cover) {
		t.Fatal(out)
	}
	if problems := board.CheckReviewGate(w.root, []string{w.id}); len(problems) > 0 {
		t.Fatalf("%+v", problems)
	}
}

func TestResolveBatchRefusesACoverOutsideTheRange(t *testing.T) {
	t.Run("target", func(t *testing.T) {
		w := newResolveWorld(t, stayOnHead, func(w *resolveWorld) string { return w.side })
		w.refused(t, decided())
	})
	// A range that spans two closed batches has no single cover: the known limit.
	t.Run("base", func(t *testing.T) {
		w := newResolveWorld(t, func(w *resolveWorld) string { return w.h.base }, onStrayTarget)
		w.refused(t, decided())
	})
}

func TestResolveBatchRefusesAFixOutsideTheReviewedRange(t *testing.T) {
	w := newResolveWorld(t, stayOnHead, onStrayTarget)
	t.Run("not after the reviewed commit", func(t *testing.T) { w.refused(t, w.fixed(w.side)) })
	t.Run("after the conclusion run", func(t *testing.T) { w.refused(t, w.fixed(w.later)) })
	t.Run("the reviewed commit", func(t *testing.T) {
		code, _, stderr := captureRun(t, []string{"extend-plan", w.h.repo, w.request(t, w.fixed(w.strayTarget))})
		if code == 0 || !strings.Contains(stderr, w.strayTarget) {
			t.Fatalf("accepted: %d %s", code, stderr)
		}
	})
}

func TestExtendPlanSchemaDescribesResolveBatch(t *testing.T) {
	text, err := evidenceSchema("extend-plan")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resolve_batch", "resolve_batch.batch_id", "resolve_batch.covering_batch_id", "resolve_batch.findings", "resolve_batch.findings[].run_id", "resolve_batch.findings[].finding_id", "resolve_batch.findings[].status", "resolve_batch.findings[].basis", "resolve_batch.findings[].fix_commit", "resolve_batch.findings[].verification", "resolve_batch.findings[].decision"} {
		if !strings.Contains(text, "field: "+field+"\n") {
			t.Fatalf("schema misses %s", field)
		}
	}
}
