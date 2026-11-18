package remote_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// isolateGlobalGit keeps these tests away from the developer's own Git
// configuration, such as commit signing and global hooks.
func isolateGlobalGit(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// publishedBaseline claims the fixture task and publishes the baseline of its
// generation, the state in which the task branch used to be unpushable.
func (f *integrationFixture) publishedBaseline() api.Task {
	f.t.Helper()
	task := f.claimedTask()
	if _, err := f.a.PublishBaseline(f.t.Context(), f.first, task.TaskID, f.base, f.credential); err != nil {
		f.t.Fatal(err)
	}
	return task
}

// recordWorktreeIntent leaves the intent of an interrupted first attempt that
// named branch, as a start of an earlier release would have.
func (f *integrationFixture) recordWorktreeIntent(task api.Task, branch string) {
	f.t.Helper()
	root, err := f.a.LocalDataRoot()
	if err != nil {
		f.t.Fatal(err)
	}
	intent := remote.ExecutionWorktree{Path: filepath.Join(root, "worktrees", task.TaskID, "g1"), Branch: branch, Base: f.base, ExecutionID: f.credential.ExecutionID, Generation: f.credential.Generation, Phase: "creating"}
	data, err := json.Marshal(intent)
	if err != nil {
		f.t.Fatal(err)
	}
	path := filepath.Join(root, "worktree-intents", task.TaskID, "g1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *integrationFixture) remoteRefs(pattern string) string {
	f.t.Helper()
	return deliveryGit(f.t, f.bare, "for-each-ref", "--format=%(refname)", pattern)
}

func TestExecutionBranchPushesAfterBaselineAndDelivery(t *testing.T) {
	isolateGlobalGit(t)
	f := newIntegrationGitFixture(t)
	task := f.publishedBaseline()
	worktree, err := f.a.PrepareWorktree(t.Context(), f.first, f.base, f.credential)
	if err != nil {
		t.Fatal(err)
	}
	if want := api.ExecutionBranch(f.a.Info.ID, task.TaskID, f.credential.Generation); worktree.Branch != want {
		t.Fatalf("task branch = %q, want %q", worktree.Branch, want)
	}
	commitFile(t, worktree.Path, "first.txt", "first\n", "First work")
	deliveryGit(t, worktree.Path, "push", "origin", worktree.Branch)
	delivered := commitFile(t, worktree.Path, "second.txt", "second\n", "Second work")
	// The managed worktree is published through the same boundary as `kander move`.
	moved, err := f.a.MoveTask(t.Context(), task.TaskID, api.Move{State: "review", DeliveryCommit: delivered})
	if err != nil || moved.State != "review" || moved.Delivery == nil || moved.Delivery.Commit != delivered {
		t.Fatalf("delivery move: %+v %v", moved, err)
	}
	commitFile(t, worktree.Path, "third.txt", "third\n", "Work after the delivery")
	deliveryGit(t, worktree.Path, "push", "origin", worktree.Branch)
	if got := f.remoteRefs("refs/heads/kander-task"); got != "refs/heads/"+worktree.Branch {
		t.Fatalf("task branches on the common remote: %q", got)
	}
	for _, ref := range strings.Fields(f.remoteRefs("refs/heads/kander")) {
		if !strings.HasPrefix(ref, "refs/heads/kander/"+f.a.Info.ID+"/"+task.TaskID+"/g1/") {
			t.Fatalf("a ref other than evidence below refs/heads/kander/: %q", ref)
		}
	}
}

func TestExecutionBranchOfASuccessorGenerationPushesBesideThePredecessor(t *testing.T) {
	isolateGlobalGit(t)
	f := newIntegrationGitFixture(t)
	task := f.publishedBaseline()
	first, err := f.a.PrepareWorktree(t.Context(), f.first, f.base, f.credential)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, first.Path, "first.txt", "first\n", "First generation work")
	deliveryGit(t, first.Path, "push", "origin", first.Branch)
	own := *f.credential.Claim()
	if _, err := f.a.Project.ObserveAgent(t.Context(), "confirmed-exit", task.TaskID, api.AgentObservation{Status: "stopped", Reason: "explicit fixture exit"}, own); err != nil {
		t.Fatal(err)
	}
	state, err := f.a.Project.ExecutionState(t.Context(), task.TaskID, own)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Project.Execution(t.Context(), "release-source", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Decision: "source holder releases for explicit takeover", Presence: &api.AgentObservation{Status: "stopped"}}, own); err != nil {
		t.Fatal(err)
	}
	successor, err := f.b.Project.Takeover(t.Context(), "takeover-second-generation", task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "task-successor", Owner: "codex", Decision: "explicit takeover to publish a second generation", ExpectedGeneration: f.credential.Generation})
	if err != nil {
		t.Fatal(err)
	}
	next := remote.Credential{ServerURL: f.b.ServerURL, ClientID: "host-b", ProjectID: f.b.Info.ID, TaskID: task.TaskID, ExecutionID: successor.Claim.ExecutionID, Generation: successor.Claim.Generation, Token: successor.Token}
	if _, err := f.b.PublishBaseline(t.Context(), f.second, task.TaskID, f.base, next); err != nil {
		t.Fatal(err)
	}
	second, err := f.b.PrepareWorktree(t.Context(), f.second, f.base, next)
	if err != nil {
		t.Fatal(err)
	}
	if want := api.ExecutionBranch(f.b.Info.ID, task.TaskID, next.Generation); second.Branch != want || second.Branch == first.Branch {
		t.Fatalf("successor task branch = %q, want %q", second.Branch, want)
	}
	commitFile(t, second.Path, "second.txt", "second\n", "Second generation work")
	deliveryGit(t, second.Path, "push", "origin", second.Branch)
	remoteBranches := f.remoteRefs("refs/heads/kander-task")
	for _, branch := range []string{first.Branch, second.Branch} {
		if !strings.Contains(remoteBranches, "refs/heads/"+branch) {
			t.Fatalf("%s is missing on the common remote: %q", branch, remoteBranches)
		}
	}
}

func TestPrepareWorktreeRetryKeepsTheNameItsIntentRecorded(t *testing.T) {
	for name, tc := range map[string]struct {
		branch func(f *integrationFixture, task api.Task) string
		ok     bool
	}{
		"legacy name": {func(f *integrationFixture, task api.Task) string {
			return api.LegacyExecutionBranch(f.a.Info.ID, task.TaskID, f.credential.Generation)
		}, true},
		"unknown name": {func(*integrationFixture, api.Task) string { return "kander-task/other/task/g1" }, false},
	} {
		t.Run(name, func(t *testing.T) {
			isolateGlobalGit(t)
			f := newIntegrationGitFixture(t)
			task := f.publishedBaseline()
			branch := tc.branch(f, task)
			f.recordWorktreeIntent(task, branch)
			result, err := f.a.PrepareWorktree(t.Context(), f.first, f.base, f.credential)
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), config.Text("remote.worktree_recovery_required")) {
					t.Fatalf("an intent naming %q was accepted: %v", branch, err)
				}
				return
			}
			if err != nil || result.Branch != branch {
				t.Fatalf("retry: %+v %v", result, err)
			}
			if head := deliveryGit(t, result.Path, "symbolic-ref", "HEAD"); head != "refs/heads/"+branch {
				t.Fatalf("worktree is on %q", head)
			}
			saved, found, err := f.a.Credential(task.TaskID)
			if err != nil || !found || saved.Branch != branch {
				t.Fatalf("credential branch: %+v %v", saved, err)
			}
		})
	}
}

func TestLegacyTaskBranchPushesToTheTaskNamespaceWithAnExplicitRefspec(t *testing.T) {
	isolateGlobalGit(t)
	f := newIntegrationGitFixture(t)
	task := f.publishedBaseline()
	legacy := api.LegacyExecutionBranch(f.a.Info.ID, task.TaskID, f.credential.Generation)
	deliveryGit(t, f.first, "checkout", "-q", "-b", legacy, f.base)
	commitFile(t, f.first, "legacy.txt", "legacy\n", "Work on a legacy task branch")
	// The legacy name still collides with the evidence refs of its generation.
	if out, err := exec.CommandContext(t.Context(), "git", "-C", f.first, "push", "origin", legacy).CombinedOutput(); err == nil {
		t.Fatalf("pushing %s under its own name succeeded: %s", legacy, out)
	}
	target := "refs/heads/kander-task/" + strings.TrimPrefix(legacy, "kander/")
	deliveryGit(t, f.first, "push", "origin", legacy+":"+target)
	if got := f.remoteRefs(target); got != target {
		t.Fatalf("remote refs: %q", got)
	}
}

func TestIntegrationOfACardRecordingALegacyTaskBranch(t *testing.T) {
	isolateGlobalGit(t)
	f := newIntegrationGitFixture(t)
	f.legacyBranch = true
	task := f.publishedBaseline()
	if want := api.LegacyExecutionBranch(f.a.Info.ID, task.TaskID, f.credential.Generation); !strings.Contains(task.Text, "- TASK_BRANCH: "+want+"\n") {
		t.Fatalf("card does not record %q", want)
	}
	// The execution of an in-flight card holds the legacy name in its worktree
	// and credential, as the card does.
	legacy := api.LegacyExecutionBranch(f.a.Info.ID, task.TaskID, f.credential.Generation)
	f.recordWorktreeIntent(task, legacy)
	worktree, err := f.a.PrepareWorktree(t.Context(), f.first, f.base, f.credential)
	if err != nil {
		t.Fatal(err)
	}
	if saved, _, err := f.a.Credential(task.TaskID); err != nil || worktree.Branch != legacy || saved.Branch != legacy {
		t.Fatalf("legacy execution: worktree %q, credential %+v, %v", worktree.Branch, saved, err)
	}
	deliveryGit(t, worktree.Path, "merge", "--ff-only", f.commit)
	moved, err := f.a.MoveTask(t.Context(), task.TaskID, api.Move{State: "review", DeliveryCommit: f.commit})
	if err != nil || moved.State != "review" {
		t.Fatalf("delivery move: %+v %v", moved, err)
	}
	f.closeReviewPlan(moved)
	result, err := f.a.Integrate(t.Context(), f.first, f.options("legacy-task-branch"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Commit != f.commit || f.targetHead() != f.commit {
		t.Fatalf("receipt: %+v, target %s", result, f.targetHead())
	}
}

func TestIntegrationViaCommitPushedOnTheTaskBranchName(t *testing.T) {
	isolateGlobalGit(t)
	f := newIntegrationFixture(t)
	foreign := f.advanceTarget("other.txt")
	deliveryGit(t, f.first, "fetch", "-q", "origin")
	deliveryGit(t, f.first, "merge", "--no-edit", "origin/v2")
	carrier := deliveryGit(t, f.first, "rev-parse", "HEAD")
	// The fixture already published the delivery below g1/, which is what made
	// the legacy name of this branch unpushable.
	branch := api.ExecutionBranch(f.a.Info.ID, f.task.TaskID, f.credential.Generation)
	deliveryGit(t, f.first, "push", "origin", carrier+":refs/heads/"+branch)
	if got := f.targetHead(); got != foreign {
		t.Fatal("pushing the task branch moved the target")
	}
	options := f.options("via-on-the-task-branch")
	options.ViaCommit = carrier
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Containment != api.IntegrationVia || result.Request.ViaCommit != carrier || result.ObservedTarget != carrier {
		t.Fatalf("receipt: %+v", result)
	}
	f.complete(result)
}
