package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func launchCoordinatorFixture(t *testing.T) (string, string, board.DispatchInput, board.CoordinatorCheckpoint) {
	t.Helper()
	root, task, _, in := launchWrapFixture(t)
	s, e := board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	// Test-only group assignment predates the checkpoint; normal frozen contracts
	// use the decision producer, never this fixture transaction.
	text := strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: 20260908-launch-coordinator-group", 1)
	if e = board.WithTransaction(root, board.LockScope{Tasks: []string{task}}, func(tx *board.Transaction) error { return tx.Put(task, "spec.md", text) }); e != nil {
		t.Fatal(e)
	}
	c, e := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: "20260908-launch-coordinator-group", Owner: "coordinator", Token: "one-session", Basis: "测试授权", Members: []string{task}})
	if e != nil {
		t.Fatal(e)
	}
	return root, task, in, c
}

func launchCoordinatorRequest(t *testing.T, root, task string, c board.CoordinatorCheckpoint, d board.Dispatch) board.CoordinatorReconcile {
	t.Helper()
	s, e := board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	o := board.CoordinatorObservation{Revision: s.Revision, DispatchID: d.Input.ID, Epoch: d.Authorization.Epoch, Base: d.Input.Base}
	if d.Completed != nil {
		o.DeliveryCommit = d.Completed.DeliveryCommit
	}
	return board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: o}}
}

func TestCoordinatorRechecksActualGitAfterWrapUpRestart(t *testing.T) {
	root, task, in, c := launchCoordinatorFixture(t)
	d, e := PrepareBoundDispatch(root, in)
	if e != nil {
		t.Fatal(e)
	}
	c, e = ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, d))
	if e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"working", "done"} {
		s, e := board.ReadSnapshot(root, task)
		if e != nil {
			t.Fatal(e)
		}
		o := board.MoveOptions{Authorization: d.Authorization}
		if state == "done" {
			text := strings.ReplaceAll(s.Text, "<FILL_IN>", "测试完成")
			text = strings.Replace(text, "## SUMMARY\n", "## SUMMARY\n\n测试交付和收尾完成。\n", 1)
			if e = board.UpdateDocument(root, task, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, Authorization: d.Authorization}); e != nil {
				t.Fatal(e)
			}
			s, e = board.ReadSnapshot(root, task)
			if e != nil {
				t.Fatal(e)
			}
			o.DeliveryCommit = in.Base
			o.Result = "completed"
		}
		if _, e = board.MoveWithOptions(s.Entry, root, state, o); e != nil {
			t.Fatal(e)
		}
	}
	d, e = board.ReadDispatch(root, task, d.Input.ID)
	if e != nil {
		t.Fatal(e)
	}
	r := launchCoordinatorRequest(t, root, task, c, d)
	next, e := ReconcileCoordinator(context.Background(), root, r)
	if e != nil {
		t.Fatal(e)
	}
	if next.Members[task].Dispatch.PendingWrapUp {
		t.Fatal("completed wrap-up still pending")
	}
	// The integration artifact may name a worktree removed by cleanup. A
	// surviving checkout must prove exactly those same immutable Git objects.
	survivor := filepath.Join(t.TempDir(), "survivor")
	if out, e := exec.Command("git", "clone", "--no-hardlinks", in.Evidence.WrapUp.Git.CWD, survivor).CombinedOutput(); e != nil {
		t.Fatal(string(out), e)
	}
	if e = os.RemoveAll(in.Evidence.WrapUp.Git.CWD); e != nil {
		t.Fatal(e)
	}
	r = launchCoordinatorRequest(t, root, task, next, d)
	r.CWD = survivor
	recovered, e := ReconcileCoordinator(context.Background(), root, r)
	if e != nil || !reflect.DeepEqual(next, recovered) {
		t.Fatal("surviving worktree could not recover", e)
	}
	// Changing the actual target to a disjoint history cannot be hidden by
	// repeating a completed dispatch or trusting the old checkpoint.
	for _, args := range [][]string{{"checkout", "--orphan", "unrelated"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "unrelated"}, {"branch", "-f", "develop", "HEAD"}} {
		if out, e := exec.Command("git", append([]string{"-C", survivor}, args...)...).CombinedOutput(); e != nil {
			t.Fatal(string(out), e)
		}
	}
	if _, e = ReconcileCoordinator(context.Background(), root, r); e == nil {
		t.Fatal("stale integration accepted")
	}
	after, e := board.ReadCoordinatorCheckpoint(root, next.GroupID)
	if e != nil || !reflect.DeepEqual(next, after) {
		t.Fatal("failed Git changed cursor", e)
	}
}

func TestCoordinatorFirstDeliveryRequiresActualTaskHead(t *testing.T) {
	root, task, in, c := launchCoordinatorFixture(t)
	cwd := in.Evidence.WrapUp.Git.CWD
	if out, e := exec.Command("git", "-C", cwd, "branch", "delivery-test").CombinedOutput(); e != nil {
		t.Fatal(string(out), e)
	}
	s, e := board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	text := strings.Replace(s.Text, "- TASK_BRANCH: "+board.MetadataFrom(s.Text, "TASK_BRANCH"), "- TASK_BRANCH: delivery-test", 1)
	if e = board.WriteManagedDocument(root, s.Entry, text); e != nil {
		t.Fatal(e)
	}
	s, e = board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	r := board.CoordinatorReconcile{CWD: cwd, GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: {Revision: s.Revision, DeliveryCommit: strings.Repeat("f", 40)}}}
	if _, e = ReconcileCoordinator(context.Background(), root, r); e == nil {
		t.Fatal("invented delivery accepted")
	}
	r.Members[task] = board.CoordinatorObservation{Revision: s.Revision, DeliveryCommit: in.Base}
	next, e := ReconcileCoordinator(context.Background(), root, r)
	if e != nil {
		t.Fatal(e)
	}
	if next.Members[task].DeliveryCommit != in.Base || next.Members[task].Dispatch != nil {
		t.Fatal("first delivery became a dispatch receipt")
	}
}

func TestCoordinatorCommandRejectsInvalidInputBeforeClaim(t *testing.T) {
	root, _, _, c := launchCoordinatorFixture(t)
	p := filepath.Join(t.TempDir(), "request.json")
	if e := os.WriteFile(p, []byte(`{"group_id":"x","group_id":"y"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if RunCoordinator([]string{"claim", p}) == 0 {
		t.Fatal("ambiguous JSON accepted")
	}
	after, e := board.ReadCoordinatorCheckpoint(root, c.GroupID)
	if e != nil || !reflect.DeepEqual(c, after) {
		t.Fatal("invalid command changed checkpoint", e)
	}
}
