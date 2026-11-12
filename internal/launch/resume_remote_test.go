package launch

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
)

func remoteGitFixture(t *testing.T, slug string) (*remote.Session, api.Task, string) {
	t.Helper()
	source, head := integrationGit(t)
	lifecycleGit(t, source, "branch", "v2", head)
	bare := filepath.Join(t.TempDir(), "common.git")
	lifecycleGit(t, source, "clone", "--bare", source, bare)
	_, session, task, forbidden, _ := remoteLifecycleFixture(t, slug, bare)
	return session, task, forbidden
}

func markRemoteExecutorStopped(t *testing.T, session *remote.Session, task string) remote.Credential {
	t.Helper()
	credential, found, err := session.Credential(task)
	if err != nil || !found {
		t.Fatalf("credential: %v", err)
	}
	child := exec.CommandContext(t.Context(), "sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill() })
	facts, err := process.ObserveProcess(t.Context(), child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := process.BootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	credential.AgentPID, credential.AgentStart, credential.BootID = facts.PID, facts.Start, boot
	if err := remote.WriteCredential(session.Home, credential); err != nil {
		t.Fatal(err)
	}
	if got := observeLocalExecution(t.Context(), credential); got.Status != "stopped" {
		t.Fatalf("presence: %+v", got)
	}
	return credential
}

func TestRemoteNativeResumePreservesIndexWorktreeAndUntrackedFiles(t *testing.T) {
	skipOnWindows(t)
	session, task, forbidden := remoteGitFixture(t, "native-local-continuation")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "worktrees", task.TaskID, "g1")
	t.Setenv("KANBAN_LAUNCH_CWD", first)
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(first, "staged.txt")
	if err := os.WriteFile(file, []byte("staged version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycleGit(t, first, "add", "staged.txt")
	if err := os.WriteFile(file, []byte("unstaged version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := []byte{0, 1, 2, 0, 255, '\n'}
	if err := os.WriteFile(filepath.Join(first, "untracked.bin"), binary, 0o600); err != nil {
		t.Fatal(err)
	}
	old := markRemoteExecutorStopped(t, session, task.TaskID)
	second := filepath.Join(root, "worktrees", task.TaskID, "g2")
	t.Setenv("KANBAN_LAUNCH_CWD", second)
	if code := RunResume([]string{task.TaskID, "--message", "Continue the preserved local edits"}); code != 0 {
		t.Fatalf("resume exit %d", code)
	}
	next, found, err := session.Credential(task.TaskID)
	if err != nil || !found || next.Generation != old.Generation+1 || next.ExecutionID == old.ExecutionID || next.Session != old.Session || next.Worktree != second {
		t.Fatalf("continuation credential: %+v %v", next, err)
	}
	for _, dir := range []string{first, second} {
		if actual := lifecycleGit(t, dir, "show", ":staged.txt"); actual != "staged version" {
			t.Fatalf("index changed in %s: %s", dir, actual)
		}
		if actual, err := os.ReadFile(filepath.Join(dir, "staged.txt")); err != nil || string(actual) != "unstaged version\n" {
			t.Fatalf("working content: %q %v", actual, err)
		}
		if actual, err := os.ReadFile(filepath.Join(dir, "untracked.bin")); err != nil || !bytes.Equal(actual, binary) {
			t.Fatalf("untracked bytes: %q %v", actual, err)
		}
	}
	if _, err := session.Project.Renew(t.Context(), task.TaskID, *old.Claim()); err == nil {
		t.Fatal("predecessor retained write authority")
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("local board was used: %v", err)
	}
}

func TestRemoteResumeOfLiveAgentQueuesRequestWithoutLaunching(t *testing.T) {
	skipOnWindows(t)
	session, task, forbidden := remoteGitFixture(t, "resume-live-collaboration")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g1"))
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	before, _, _ := session.Credential(task.TaskID)
	facts, err := process.ObserveProcess(t.Context(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	boot, err := process.BootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	before.AgentPID, before.AgentStart, before.BootID = facts.PID, facts.Start, boot
	if err := remote.WriteCredential(session.Home, before); err != nil {
		t.Fatal(err)
	}
	log := os.Getenv("KANBAN_TMUX_LOG")
	prior, _ := os.ReadFile(log)
	if code := RunResume([]string{task.TaskID, "--message", "Read this collaborative update"}); code != 0 {
		t.Fatalf("resume exit %d", code)
	}
	after, _, _ := session.Credential(task.TaskID)
	if before.ExecutionID != after.ExecutionID {
		t.Fatal("live resume replaced its executor")
	}
	actual, _ := os.ReadFile(log)
	if !bytes.Equal(prior, actual) {
		t.Fatal("requesting client invoked the terminal")
	}
	requests, err := session.Project.Requests(t.Context(), task.TaskID)
	if err != nil || len(requests) != 1 || !strings.Contains(requests[0].Message, "collaborative update") {
		t.Fatalf("requests: %+v %v", requests, err)
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("local board was used: %v", err)
	}
}

func TestRemoteReleaseAndFreshTakeoverAcrossIndependentClones(t *testing.T) {
	skipOnWindows(t)
	session, task, forbidden := remoteGitFixture(t, "cross-clone-takeover")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "worktrees", task.TaskID, "g1")
	t.Setenv("KANBAN_LAUNCH_CWD", first)
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "delivered.txt"), []byte("shared takeover source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycleGit(t, first, "add", "delivered.txt")
	lifecycleGit(t, first, "commit", "-m", "Publish takeover source")
	commit := lifecycleGit(t, first, "rev-parse", "HEAD")
	old, _, _ := session.Credential(task.TaskID)
	if _, err := session.PublishCommit(t.Context(), first, task.TaskID, commit, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "private-dirty.txt"), []byte("preserve predecessor scene"), 0o600); err != nil {
		t.Fatal(err)
	}
	old = markRemoteExecutorStopped(t, session, task.TaskID)
	if code := RunResume([]string{task.TaskID, "--release", "--decision", "Release stopped executor for another host"}); code != 0 {
		t.Fatalf("release exit %d", code)
	}
	lost, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil || lost.Claim.Status != api.ClaimLost {
		t.Fatalf("release: %+v %v", lost, err)
	}
	second := t.TempDir()
	lifecycleGit(t, second, "clone", "--no-local", "--single-branch", "--branch", "v2", session.Info.Remote, second)
	if lifecycleGit(t, second, "rev-parse", "--path-format=absolute", "--git-common-dir") == lifecycleGit(t, first, "rev-parse", "--path-format=absolute", "--git-common-dir") {
		t.Fatal("clones share object storage")
	}
	if err := exec.CommandContext(t.Context(), "git", "-C", second, "cat-file", "-e", commit+"^{commit}").Run(); err == nil {
		t.Fatal("fresh clone already had delivery object")
	}
	t.Chdir(second)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "second-client"))
	cfg, err := loadEffective()
	if err != nil {
		t.Fatal(err)
	}
	successor, err := remote.Open(t.Context(), cfg, second)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot, err := successor.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	nextWorktree := filepath.Join(secondRoot, "worktrees", task.TaskID, "g2")
	t.Setenv("KANBAN_LAUNCH_CWD", nextWorktree)
	if code := RunResume([]string{task.TaskID, "--agent", "claude", "--decision", "Explicit cross-host handoff", "--message", "Continue from the published source"}); code != 0 {
		t.Fatalf("takeover exit %d", code)
	}
	next, found, err := successor.Credential(task.TaskID)
	if err != nil || !found || next.Generation != 2 || next.ClientID == old.ClientID || next.BaseCommit != commit {
		t.Fatalf("new holder: %+v %v", next, err)
	}
	if actual, err := os.ReadFile(filepath.Join(nextWorktree, "delivered.txt")); err != nil || string(actual) != "shared takeover source\n" {
		t.Fatalf("shared delivery: %q %v", actual, err)
	}
	if actual, err := os.ReadFile(filepath.Join(first, "private-dirty.txt")); err != nil || string(actual) != "preserve predecessor scene" {
		t.Fatalf("predecessor scene: %q %v", actual, err)
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("local board was used: %v", err)
	}
}
