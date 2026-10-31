//go:build unix

package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

const staleCycleGroup = "20260907-stale-group"

// staleCycleRun runs path 2 up to the plan: a passing run through the command, a
// member the coordinator cannot follow, and a reset.
func staleCycleRun(t *testing.T, h *reviewHarness, root string, args []string) string {
	t.Helper()
	id := "20260907-stale-cycle-task"
	dir := filepath.Join(root, "working", id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	text := "# Stale cycle\n\n- TYPE: Chore\n- SIZE: small\n- LANGUAGE: zh-CN\n- TASK_GROUP: " + staleCycleGroup + "\n- TASK_BRANCH: task\n- OWNER: codex\n- STARTED_AT: 2026-01-01 00:00\n\n## LIFECYCLE_DECISION\n\n{\"kind\":\"claim\",\"claim_id\":\"00000000000000000000000000000001\"}\n\n## GOAL\n\nReview recovery\n\n## SUMMARY\n\nFixture delivery verified.\n"
	path := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: staleCycleGroup, Owner: "coordinator", Token: "session", Basis: "fixture", Members: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	reconcile := func() {
		t.Helper()
		s, e := board.ReadSnapshot(root, id)
		if e != nil {
			t.Fatal(e)
		}
		c, e = board.ReconcileCoordinator(context.Background(), root, board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{id: {Revision: s.Revision}}}, nil)
		var blocked *board.CoordinatorBlockedError
		if e != nil && !errors.As(e, &blocked) {
			t.Fatal(e)
		}
		if blocked != nil {
			if c, e = board.ReadCoordinatorCheckpoint(root, staleCycleGroup); e != nil {
				t.Fatal(e)
			}
		}
	}
	reconcile()
	commandOK(t, staleCycleArgs(args, id, "old-run")...)
	published, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(strings.Replace(string(published), "00000000000000000000000000000001", "00000000000000000000000000000002", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	reconcile()
	s, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.RecoverCoordinator(context.Background(), root, board.CoordinatorRecover{GroupID: c.GroupID, Member: id, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Decision: "user-decision", Reason: "fixture"}, s.Revision); err != nil {
		t.Fatal(err)
	}
	return id
}

func staleCycleArgs(args []string, id, run string) []string {
	out := append([]string(nil), args...)
	out[2], out[4], out[6] = id, id, run
	return out
}

func TestOldRunCannotReachDoneThroughTheReviewCommand(t *testing.T) {
	h, root, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	id := staleCycleRun(t, h, root, args)
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: project"}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "stale-plan", Author: "coordinator", Basis: "fixture", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	commandOK(t, "assign", h.repo, dispositionJSON(t, h, "assign", board.ReviewAssignment{RunID: "old-run", BatchID: "batch", Author: "coordinator", Basis: "no findings", Items: map[string][]string{}}))
	v, err := board.ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{"PMQA": {RunID: "old-run", PassedAt: h.head, Basis: "old evidence"}}}
	code, _, stderr := captureRun(t, []string{"close", h.repo, dispositionJSON(t, h, "close", r)})
	if code == 0 || !strings.Contains(stderr, id) || !strings.Contains(stderr, "PMQA") {
		t.Fatalf("the run of the old cycle closed the batch: %d %s", code, stderr)
	}
	if board.RunMove([]string{id, "done", "--result", "completed"}) == 0 {
		t.Fatal("the run of the old cycle reached done")
	}
	var progress board.ReviewProgress
	if err = json.Unmarshal([]byte(commandOK(t, "progress", h.repo, id)), &progress); err != nil {
		t.Fatal(err)
	}
	want := "stale-run:batch/PMQA/" + id
	if !strings.Contains(strings.Join(progress.Pending, " "), want) {
		t.Fatalf("%+v", progress.Pending)
	}

	commandOK(t, staleCycleArgs(args, id, "new-run")...)
	commandOK(t, "assign", h.repo, dispositionJSON(t, h, "assign-new", board.ReviewAssignment{RunID: "new-run", BatchID: "batch", Author: "coordinator", Basis: "no findings", Items: map[string][]string{}}))
	if v, err = board.ReadReviewBatchView(root, "batch"); err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision, r.ViewHash = v.Batch.Revision, board.ReviewViewDigest(v)
	r.Roles["PMQA"] = board.ReviewRoleConclusion{RunID: "new-run", PassedAt: h.head, Basis: "the new cycle reviewed the delivery"}
	r.SupersededRuns = map[string]board.ReviewSupersededRun{"old-run": {Replacement: "new-run", Basis: "parallel run of the old cycle"}}
	commandOK(t, "close", h.repo, dispositionJSON(t, h, "close", r))
	if board.RunMove([]string{id, "done", "--result", "completed"}) != 0 {
		t.Fatal("the real way out failed")
	}
}
