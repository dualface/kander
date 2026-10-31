package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/check"
	"github.com/dualface/kander/internal/liveness"
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

// launchCompletedWrapUp takes the single member of the fixture through wrap-up.
// cleanup runs just before the move into done, where the agent removes its worktree.
// It receives that move, so it can try it from where the agent stands.
func launchCompletedWrapUp(t *testing.T, cleanup ...func(board.DispatchInput, func() error)) (root, task string, in board.DispatchInput, d board.Dispatch, c board.CoordinatorCheckpoint) {
	t.Helper()
	root, task, in, c = launchCoordinatorFixture(t)
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
			for _, step := range cleanup {
				step(in, func() error { _, e := board.MoveWithOptions(s.Entry, root, state, o); return e })
			}
		}
		if _, e = board.MoveWithOptions(s.Entry, root, state, o); e != nil {
			t.Fatal(e)
		}
	}
	d, e = board.ReadDispatch(root, task, d.Input.ID)
	if e != nil {
		t.Fatal(e)
	}
	return root, task, in, d, c
}

// launchRewriteDevelop points develop at a disjoint history.
func launchRewriteDevelop(t *testing.T, cwd string) {
	t.Helper()
	for _, args := range [][]string{{"checkout", "--orphan", "unrelated"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "unrelated"}, {"branch", "-f", "develop", "HEAD"}} {
		if out, e := exec.Command("git", append([]string{"-C", cwd}, args...)...).CombinedOutput(); e != nil {
			t.Fatal(string(out), e)
		}
	}
}

func launchWrapUpEvidence(root, group, dispatch string) string {
	return filepath.Join(root, ".kander", "groups", group, "wrap-up-git", dispatch+".json")
}

func TestCompletedWrapUpVerifiesGitOnce(t *testing.T) {
	root, task, in, d, c := launchCompletedWrapUp(t)
	if _, e := os.Stat(launchWrapUpEvidence(root, c.GroupID, d.Input.ID)); e != nil {
		t.Fatal("move done wrote no evidence", e)
	}
	next, e := ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, d))
	if e != nil {
		t.Fatal(e)
	}
	if next.Members[task].Dispatch.PendingWrapUp {
		t.Fatal("completed wrap-up still pending")
	}
	// DE-18: the history under a completed wrap-up changes. Reconcile reads no Git,
	// so the worktree may even be gone; a person still finds the change.
	cwd := in.Evidence.WrapUp.Git.CWD
	launchRewriteDevelop(t, cwd)
	again, e := ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, next, d))
	if e != nil || !reflect.DeepEqual(next, again) {
		t.Fatal("rewritten history reached reconcile", e)
	}
	fenced, e := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: next.GroupID, ExpectedRevision: next.Revision, ExpectedEpoch: next.Authority.Epoch, Owner: "coordinator", Token: "two-session", Basis: "recovery", Members: []string{task}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, fenced, d)); e != nil {
		t.Fatal("a new coordinator session stopped at the rewritten history", e)
	}
	t.Chdir(cwd)
	if check.Run([]string{"delivery", "--base", in.Base, "--json"}) == 0 {
		t.Fatal("kander check delivery no longer reports the rewrite")
	}
}

// removeRecordedWorktree clones the recorded worktree and removes it.
func removeRecordedWorktree(t *testing.T, in board.DispatchInput) string {
	t.Helper()
	survivor := filepath.Join(t.TempDir(), "survivor")
	if out, e := exec.Command("git", "clone", "--no-hardlinks", in.Evidence.WrapUp.Git.CWD, survivor).CombinedOutput(); e != nil {
		t.Fatal(string(out), e)
	}
	if e := os.RemoveAll(in.Evidence.WrapUp.Git.CWD); e != nil {
		t.Fatal(e)
	}
	return survivor
}

func TestCompletionNeedsACheckoutAndThenSurvivesARewrite(t *testing.T) {
	var survivor string
	root, task, _, d, c := launchCompletedWrapUp(t, func(in board.DispatchInput, move func() error) {
		survivor = removeRecordedWorktree(t, in)
		t.Chdir(t.TempDir())
		if e := move(); e == nil || !strings.Contains(e.Error(), "checkout") {
			t.Fatal("a completion passed with no evidence", e)
		}
		t.Chdir(survivor)
	})
	if _, e := os.Stat(launchWrapUpEvidence(root, c.GroupID, d.Input.ID)); e != nil {
		t.Fatal("the move did not verify in the checkout it runs from", e)
	}
	launchRewriteDevelop(t, survivor)
	next, e := ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, d))
	if e != nil || len(next.BlockedMembers()) != 0 {
		t.Fatal(next.Members[task], e)
	}
}

func TestOldWrapUpVerifiesOnceAndConfirmationEndsAFailure(t *testing.T) {
	root, task, in, d, c := launchCompletedWrapUp(t)
	path := launchWrapUpEvidence(root, c.GroupID, d.Input.ID)
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	// An old wrap-up verifies through a surviving checkout, once.
	survivor := filepath.Join(t.TempDir(), "survivor")
	if out, e := exec.Command("git", "clone", "--no-hardlinks", in.Evidence.WrapUp.Git.CWD, survivor).CombinedOutput(); e != nil {
		t.Fatal(string(out), e)
	}
	r := launchCoordinatorRequest(t, root, task, c, d)
	r.CWD = survivor
	next, e := ReconcileCoordinator(context.Background(), root, r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("reconcile wrote no evidence", e)
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	// Failed verification blocks the member; only a confirmation ends that.
	launchRewriteDevelop(t, survivor)
	r = launchCoordinatorRequest(t, root, task, next, d)
	r.CWD = survivor
	blocked, e := ReconcileCoordinator(context.Background(), root, r)
	var all *board.CoordinatorBlockedError
	if !errors.As(e, &all) || blocked.Members[task].Blocked == nil {
		t.Fatal("rewritten history accepted", e)
	}
	recovered, e := RecoverCoordinator(context.Background(), root, board.CoordinatorRecover{GroupID: blocked.GroupID, Member: task, ExpectedRevision: blocked.Revision, ExpectedEpoch: blocked.Authority.Epoch, Decision: "user-decision", Reason: "the rewrite was deliberate"})
	if e != nil {
		t.Fatal(e)
	}
	r = launchCoordinatorRequest(t, root, task, recovered, d)
	r.CWD = survivor
	final, e := ReconcileCoordinator(context.Background(), root, r)
	if e != nil || len(final.BlockedMembers()) != 0 || final.Recoveries[0].Mode != board.RecoverConfirm {
		t.Fatal(final, e)
	}
}

func TestRejectedCompletionKeepsTheCardInWorking(t *testing.T) {
	root, task, in, c := launchCoordinatorFixture(t)
	d, e := PrepareBoundDispatch(root, in)
	if e != nil {
		t.Fatal(e)
	}
	if c, e = ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, d)); e != nil {
		t.Fatal(e)
	}
	s, e := board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); e != nil {
		t.Fatal(e)
	}
	launchRewriteDevelop(t, in.Evidence.WrapUp.Git.CWD)
	s, e = board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	text := strings.ReplaceAll(s.Text, "<FILL_IN>", "测试完成")
	text = strings.Replace(text, "## SUMMARY\n", "## SUMMARY\n\n测试交付和收尾完成。\n", 1)
	if e = board.UpdateDocument(root, task, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, Authorization: d.Authorization}); e != nil {
		t.Fatal(e)
	}
	s, e = board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = board.MoveWithOptions(s.Entry, root, "done", board.MoveOptions{Authorization: d.Authorization, DeliveryCommit: in.Base, Result: "completed"}); e == nil {
		t.Fatal("wrap-up completed over a failed Git check")
	}
	if s, e = board.ReadSnapshot(root, task); e != nil || s.Entry.State != "working" {
		t.Fatal(s.Entry.State, e)
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
	if e = writeCardFixture(root, s.Entry, text); e != nil {
		t.Fatal(e)
	}
	s, e = board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	r := board.CoordinatorReconcile{CWD: cwd, GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: {Revision: s.Revision, DeliveryCommit: strings.Repeat("f", 40)}}}
	blocked, e := ReconcileCoordinator(context.Background(), root, r)
	if e == nil || blocked.Members[task].Blocked == nil {
		t.Fatal("invented delivery accepted")
	}
	r.ExpectedRevision = blocked.Revision
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

func captureStdout(t *testing.T, run func() int) (int, []byte) {
	t.Helper()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()
	status := run()
	w.Close()
	var out bytes.Buffer
	if _, e = io.Copy(&out, r); e != nil {
		t.Fatal(e)
	}
	return status, out.Bytes()
}

func TestReconcileOutcomeReachesExitCodeAndJSON(t *testing.T) {
	blocked := board.CoordinatorCheckpoint{Members: map[string]board.CoordinatorMember{"a": {Blocked: &board.CoordinatorBlock{Reason: "x"}}, "b": {}}}
	for _, tc := range []struct {
		name   string
		c      board.CoordinatorCheckpoint
		err    error
		status int
		result string
	}{
		{"complete", board.CoordinatorCheckpoint{Members: map[string]board.CoordinatorMember{"a": {}}}, nil, 0, "complete"},
		{"partial", blocked, nil, 3, "partial"},
		{"failed", blocked, &board.CoordinatorBlockedError{Group: "g", Reason: "x"}, 1, "failed"},
		{"refused", board.CoordinatorCheckpoint{}, errors.New("checkpoint CAS"), 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, out := captureStdout(t, func() int { return finishReconcile(tc.c, tc.err) })
			if status != tc.status {
				t.Fatalf("status %d", status)
			}
			if tc.result == "" {
				if len(out) != 0 {
					t.Fatalf("a refused request printed %q", out)
				}
				return
			}
			var decoded struct {
				Result string `json:"result"`
			}
			if e := json.Unmarshal(out, &decoded); e != nil || decoded.Result != tc.result {
				t.Fatalf("%s %v", out, e)
			}
		})
	}
}

func TestRecoverReachesTheCommandAndRefusesAProvenExecutor(t *testing.T) {
	root, task, _, c := launchCoordinatorFixture(t)
	s, e := board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	r := board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: {Revision: s.Revision, DispatchID: "stale-round", Epoch: 1}}}
	blocked, _ := ReconcileCoordinator(context.Background(), root, r)
	if blocked.Members[task].Blocked == nil {
		t.Fatal("no block")
	}
	request := board.CoordinatorRecover{GroupID: blocked.GroupID, Member: task, ExpectedRevision: blocked.Revision, ExpectedEpoch: blocked.Authority.Epoch, Decision: "user-decision", Reason: "reset"}
	saved := recoverLivenessFn
	t.Cleanup(func() { recoverLivenessFn = saved })
	for _, status := range []string{liveness.Alive, liveness.AliveOrphaned} {
		recoverLivenessFn = func(context.Context, board.Snapshot) liveness.Report { return liveness.Report{Status: status} }
		if _, e = RecoverCoordinator(context.Background(), root, request); e == nil || !strings.Contains(e.Error(), "resume --agent") {
			t.Fatal(status, e)
		}
	}
	recoverLivenessFn = func(context.Context, board.Snapshot) liveness.Report {
		return liveness.Report{Status: liveness.Unknown}
	}
	next, e := RecoverCoordinator(context.Background(), root, request)
	if e != nil {
		t.Fatal(e)
	}
	if len(next.Recoveries) != 1 || next.Recoveries[0].Mode != board.RecoverReset {
		t.Fatal(next.Recoveries)
	}
	// T-02 holds for a recovered card, before any dispatch binds.
	if e = RequireSessionExecution(root, task); e == nil || !strings.Contains(e.Error(), board.ExecutionRecover) {
		t.Fatal(e)
	}
}

func TestRecoverCommandRequiresAnAbsoluteRequest(t *testing.T) {
	root, _, _, _ := launchCoordinatorFixture(t)
	t.Chdir(filepath.Dir(root))
	if status := RunCoordinator([]string{"recover", "request.json"}); status != 2 {
		t.Fatal(status)
	}
}
