package notify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

// Build structurally valid stored evidence with a removed Git worktree. This
// fixture claims no actual integration; reconciliation must not require Git.
func acceptedWrapUpFixture(t *testing.T, onBehalf bool) (string, string, board.Dispatch) {
	t.Helper()
	root, _ := setupBoard(t)
	task, path := makeReview(t, root, "wrap-receipt")
	setWindow(t, path, "herdr:w1:t9:w1:p9")
	cwd := filepath.Join(root, "removed-worktree")
	base, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "wrap-plan", Author: "fixture", Basis: "structural receipt fixture", CWD: cwd, ReportLanguage: "zh-CN", TaskIDs: []string{task}, Batches: []board.ReviewPlanBatch{{BatchID: "wrap-batch", TaskIDs: []string{task}, Base: base, TargetCommit: head, Requirements: map[string]string{"PMQA": "N/A: fixture", "Security": "N/A: fixture"}}}}
	if err := board.CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	view, err := board.ReadReviewBatchView(root, "wrap-batch")
	if err != nil {
		t.Fatal(err)
	}
	close := board.ReviewCloseRequest{BatchID: "wrap-batch", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "fixture", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, close)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.CloseReviewBatch(root, close, board.ReviewGitEvidence{CWD: cwd, Head: head, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}); err != nil {
		t.Fatal(err)
	}
	in := board.DispatchInput{ID: "wrap-receipt", TaskID: task, Kind: "wrap-up", Message: "完成收尾", Base: head,
		Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Artifact: board.ArtifactReference{TaskID: task, Path: "dispatches/wrap-receipt/integration.json"}, Git: board.DispatchIntegration{DispatchID: "wrap-receipt", TaskID: task, CWD: cwd, SourceCommit: head, ReviewTarget: head, ReviewBase: base, TargetCommit: head, TargetRef: "refs/heads/develop", Author: "fixture", Basis: "stored evidence after cleanup", VerifiedAt: time.Now().UTC()}}}}
	d, err := board.PrepareDispatch(root, in)
	if err != nil {
		t.Fatal(err)
	}
	if onBehalf {
		s, err := board.ReadSnapshot(root, task)
		if err != nil {
			t.Fatal(err)
		}
		exit := board.WrapUpExitEvidence{Outcome: "stopped", CardRevision: s.Revision, Session: board.MetadataFrom(s.Text, "SESSION"), Window: board.MetadataFrom(s.Text, "WINDOW"), Owner: board.MetadataFrom(s.Text, "OWNER"), StartedAt: board.MetadataFrom(s.Text, "STARTED_AT"), ObservedAt: time.Now().UTC()}
		d, err = board.AuthorizeDispatchWrapUp(root, task, d.Input.ID, d.Revision, "coordinator", "stopped fixture", exit)
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	d, err = board.ReadDispatch(root, task, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	return root, task, d
}

func TestAcceptedWrapUpNotifyReconcilesWithoutActionEvidence(t *testing.T) {
	for _, onBehalf := range []bool{false, true} {
		for _, status := range []string{"alive", "unknown", "stopped"} {
			t.Run(map[bool]string{false: "executor", true: "on-behalf"}[onBehalf]+"/"+status, func(t *testing.T) {
				root, task, d := acceptedWrapUpFixture(t, onBehalf)
				if err := board.ValidateDispatchEvidence(root, task, d.Input.ID); err != nil {
					t.Fatal("invalid structural fixture", err)
				}
				if err := launch.ValidateActionEvidence(context.Background(), root, d); err == nil {
					t.Fatal("fixture must reject launch evidence")
				}
				if status == "unknown" {
					t.Setenv("KANBAN_HERDR_GET_FAIL", "1")
				}
				if status == "stopped" {
					t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p9")
					t.Setenv("KANBAN_HERDR_LIST_JSON", `{"result":{"panes":[]}}`)
				}
				out, _, err := capture(t, func() error {
					return commandNotify(root, task, d.Input.Message, "", "", true, 61, launch.DispatchOptions{ID: d.Input.ID})
				})
				if err != nil || !strings.Contains(out, `"state":"accepted"`) {
					t.Fatalf("receipt lost: %s %v", out, err)
				}
				current, err := board.ReadDispatch(root, task, d.Input.ID)
				if err != nil || current.Authorization != d.Authorization || current.Revision != d.Revision {
					t.Fatalf("receipt query changed execution: %+v %v", current, err)
				}
				for _, file := range []string{"herdr.log.run", "herdr.log.prompt"} {
					if _, err = os.Stat(filepath.Join(root, file)); !os.IsNotExist(err) {
						t.Fatal("receipt query launched or sent", file)
					}
				}
			})
		}
	}
}
