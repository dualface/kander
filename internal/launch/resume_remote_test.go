package launch

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
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
	if code := RunResumeRemote([]string{task.TaskID, "--message", "Continue the preserved local edits"}); code != 0 {
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
	if code := RunResumeRemote([]string{task.TaskID, "--message", "Read this collaborative update"}); code != 0 {
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

// A message to a live executor starts nothing, so a default launcher that takes the terminal does
// not refuse it with --json: the request is queued and the terminal is not called.
func TestRemoteResumeOfLiveAgentWithJSONIgnoresATerminalDefaultLauncher(t *testing.T) {
	skipOnWindows(t)
	session, task, _ := remoteGitFixture(t, "resume-live-json")
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
	// The default launcher of the configuration takes the terminal.
	loaded := loadEffective
	t.Cleanup(func() { loadEffective = loaded })
	loadEffective = func() (*config.Config, error) {
		cfg, err := loaded()
		if err != nil {
			return nil, err
		}
		terminal := *cfg
		terminal.Launcher = "foreground"
		return &terminal, nil
	}
	if effective, err := loadEffective(); err != nil || effective.Launcher != "foreground" {
		t.Fatalf("the default launcher is %q (%v)", effective.Launcher, err)
	}
	log := os.Getenv("KANBAN_TMUX_LOG")
	prior, _ := os.ReadFile(log)
	code, stdout, stderr := commandtest.Capture(func() int {
		return RunResumeRemote([]string{"--json", task.TaskID, "--message", "Read this update with --json"})
	})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "request") && !strings.Contains(stdout, "status") {
		t.Fatalf("resume: code=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	actual, _ := os.ReadFile(log)
	if !bytes.Equal(prior, actual) {
		t.Fatal("requesting client invoked the terminal")
	}
	requests, err := session.Project.Requests(t.Context(), task.TaskID)
	if err != nil || len(requests) != 1 || !strings.Contains(requests[0].Message, "with --json") {
		t.Fatalf("requests: %+v %v", requests, err)
	}
}

// withTerminalDefaultLauncher makes the default launcher of the configuration one that takes the
// terminal, and returns the function that restores it.
func withTerminalDefaultLauncher(t *testing.T) (restore func()) {
	t.Helper()
	loaded := loadEffective
	loadEffective = func() (*config.Config, error) {
		cfg, err := loaded()
		if err != nil {
			return nil, err
		}
		terminal := *cfg
		terminal.Launcher = "foreground"
		return &terminal, nil
	}
	return func() { loadEffective = loaded }
}

// A stopped executor of this host is continued by an implicit resume. With --json and a default
// launcher that takes the terminal the command is refused before its receipt changes, so the
// same command without --json launches; and once it completed, repeating it with --json replays
// the receipt even after the continued executor stopped too, because a replay starts nothing.
func TestImplicitNativeResumeWithJSONRefusesBeforeTheReceiptAndReplaysCompletedOnes(t *testing.T) {
	skipOnWindows(t)
	session, task, _ := remoteGitFixture(t, "implicit-native-json")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g1"))
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	markRemoteExecutorStopped(t, session, task.TaskID)
	args := []string{task.TaskID, "--message", "Continue after the stop"}
	log := os.Getenv("KANBAN_TMUX_LOG")
	prior, _ := os.ReadFile(log)
	receipts := func() []string {
		entries, _ := os.ReadDir(filepath.Join(root, "launch-actions"))
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	receiptsBefore := receipts()

	restore := withTerminalDefaultLauncher(t)
	code, stdout, stderr := commandtest.Capture(func() int { return RunResumeRemote(append([]string{"--json"}, args...)) })
	restore()
	if code != 2 || stdout != "" || !strings.Contains(stderr, "interactive_required") {
		t.Fatalf("refusal: code=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	if actual, _ := os.ReadFile(log); !bytes.Equal(prior, actual) {
		t.Fatal("the refused command invoked the terminal")
	}
	if after := receipts(); !slices.Equal(receiptsBefore, after) {
		t.Fatalf("the refusal wrote a receipt: before %v, after %v", receiptsBefore, after)
	}
	// The refusal left no pending receipt: the same command without --json continues the executor,
	// in the worktree of the next generation.
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g2"))
	if code := RunResumeRemote(args); code != 0 {
		t.Fatalf("retry without --json: exit %d", code)
	}
	if actual, _ := os.ReadFile(log); bytes.Equal(prior, actual) {
		t.Fatal("the retry did not launch")
	}
	// The continued executor stops: repeating the command is a new intentional continuation, which
	// starts an agent, so --json refuses it before the receipt changes.
	markRemoteExecutorStopped(t, session, task.TaskID)

	restore = withTerminalDefaultLauncher(t)
	code, stdout, stderr = commandtest.Capture(func() int { return RunResumeRemote(append([]string{"--json"}, args...)) })
	restore()
	if code != 2 || !strings.Contains(stderr, "interactive_required") {
		t.Fatalf("new continuation: code=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	// Another command takes the card over. When that executor has stopped too, the completed receipt
	// of the implicit command belongs to an older execution, so repeating it is a replay that starts
	// nothing, and --json must not refuse it for the launcher.
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g3"))
	if code := RunResumeRemote([]string{task.TaskID, "--agent", "claude", "--decision", "test-takeover", "--message", "Take over"}); code != 0 {
		t.Fatalf("takeover: exit %d", code)
	}
	markRemoteExecutorStopped(t, session, task.TaskID)
	afterTakeover, _ := os.ReadFile(log)
	restore = withTerminalDefaultLauncher(t)
	defer restore()
	code, stdout, stderr = commandtest.Capture(func() int { return RunResumeRemote(append([]string{"--json"}, args...)) })
	if code != 0 || strings.Contains(stderr, "interactive_required") || !strings.Contains(stdout, "replayed") {
		t.Fatalf("replay: code=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	if actual, _ := os.ReadFile(log); !bytes.Equal(afterTakeover, actual) {
		t.Fatal("the replay invoked the terminal")
	}
}

// An implicit resume that was interrupted after its launch completed leaves the command receipt
// pending and the launch receipt complete. Repeating it with --json under a terminal default
// launcher replays the completed launch: it starts nothing, so the launcher does not matter.
func TestImplicitNativeResumeWithJSONReplaysACompletedLaunchOfAPendingCommand(t *testing.T) {
	skipOnWindows(t)
	session, task, _ := remoteGitFixture(t, "implicit-native-pending")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g1"))
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	markRemoteExecutorStopped(t, session, task.TaskID)
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g2"))
	args := []string{task.TaskID, "--message", "Continue after the stop"}
	if code := RunResumeRemote(args); code != 0 {
		t.Fatalf("resume: exit %d", code)
	}
	// Interrupt the command after its launch: the command receipt goes back to pending.
	dir := filepath.Join(root, "launch-actions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var receipt map[string]any
		if err := json.Unmarshal(data, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt["kind"] == "native-resume-command" {
			receipt["phase"] = "pending"
			data, _ = json.Marshal(receipt)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			reopened++
		}
	}
	if reopened != 1 {
		t.Fatalf("found %d command receipts", reopened)
	}
	log := os.Getenv("KANBAN_TMUX_LOG")
	before, _ := os.ReadFile(log)
	restore := withTerminalDefaultLauncher(t)
	defer restore()
	code, stdout, stderr := commandtest.Capture(func() int { return RunResumeRemote(append([]string{"--json"}, args...)) })
	if code != 0 || strings.Contains(stderr, "interactive_required") || !strings.Contains(stdout, "replayed") {
		t.Fatalf("replay: code=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	if after, _ := os.ReadFile(log); !bytes.Equal(before, after) {
		t.Fatal("the replay invoked the terminal")
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
	// With --json the release is one object and nothing on stderr.
	code, released, diagnostics := commandtest.Capture(func() int {
		return RunResumeRemote([]string{task.TaskID, "--release", "--json", "--decision", "Release stopped executor for another host"})
	})
	var releaseResult struct {
		Action string `json:"action"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(released), &releaseResult); code != 0 || diagnostics != "" || err != nil || releaseResult.Action != "release" || releaseResult.TaskID != task.TaskID {
		t.Fatalf("release exit %d stdout=%q stderr=%q %v", code, released, diagnostics, err)
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
	if code := RunResumeRemote([]string{task.TaskID, "--agent", "claude", "--decision", "Explicit cross-host handoff", "--message", "Continue from the published source"}); code != 0 {
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

// receiptFiles returns the contents of every receipt below root, by file name.
func receiptFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	dir := filepath.Join(root, "launch-actions")
	entries, _ := os.ReadDir(dir)
	files := map[string]string{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(data)
	}
	return files
}

// setReceiptPhase rewrites the phase of every receipt of the kind and returns the names changed.
func setReceiptPhase(t *testing.T, root, kind, phase string) []string {
	t.Helper()
	dir := filepath.Join(root, "launch-actions")
	entries, _ := os.ReadDir(dir)
	var changed []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var receipt map[string]any
		if err := json.Unmarshal(data, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt["kind"] != kind {
			continue
		}
		receipt["phase"] = phase
		data, _ = json.Marshal(receipt)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		changed = append(changed, entry.Name())
	}
	return changed
}

// A failed launch receipt is not overwritten by a --json retry that the launcher gate refuses:
// BeginAction would start a new attempt over it, so the retry is refused first, with the receipt
// as it was.
func TestFailedLaunchReceiptSurvivesARefusedJSONRetry(t *testing.T) {
	skipOnWindows(t)
	session, task, _ := remoteGitFixture(t, "failed-receipt-json")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g1"))
	if _, err := StartRemote("claude", "", task.TaskID); err != nil {
		t.Fatal(err)
	}
	if changed := setReceiptPhase(t, root, "task-launch", "failed"); len(changed) != 1 {
		t.Fatalf("changed %v", changed)
	}
	before := receiptFiles(t, root)
	log := os.Getenv("KANBAN_TMUX_LOG")
	prior, _ := os.ReadFile(log)
	restore := withTerminalDefaultLauncher(t)
	defer restore()
	code, stdout, stderr := commandtest.Capture(func() int { return RunStartRemote([]string{"--json", "--agent", "claude", task.TaskID}) })
	if code != 2 || stdout != "" || !strings.Contains(stderr, "interactive_required") {
		t.Fatalf("code=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	if after := receiptFiles(t, root); !maps.Equal(before, after) {
		t.Fatalf("the refused retry changed the receipts:\nbefore %v\nafter  %v", before, after)
	}
	if actual, _ := os.ReadFile(log); !bytes.Equal(prior, actual) {
		t.Fatal("the refused retry invoked the terminal")
	}
}

// Two overlapping retries of an implicit resume whose launch completed but whose command receipt
// is still pending: the first one completes the command receipt between the read-only decision
// of the second (a replay) and its lock. Under the lock the second one sees a completed command
// and a stopped executor, which is a new continuation, and is refused with the receipt untouched.
func TestOverlappingImplicitResumeRetriesRefuseUnderTheLockWithoutRewriting(t *testing.T) {
	skipOnWindows(t)
	session, task, _ := remoteGitFixture(t, "implicit-native-overlap")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g1"))
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	markRemoteExecutorStopped(t, session, task.TaskID)
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g2"))
	args := []string{task.TaskID, "--message", "Continue after the stop"}
	if code := RunResumeRemote(args); code != 0 {
		t.Fatalf("resume: exit %d", code)
	}
	complete := receiptFiles(t, root)
	if changed := setReceiptPhase(t, root, "native-resume-command", "pending"); len(changed) != 1 {
		t.Fatalf("changed %v", changed)
	}
	pending := receiptFiles(t, root)
	markRemoteExecutorStopped(t, session, task.TaskID)
	// The other retry completes the command receipt after this one decided.
	old := afterEarlyNativeGate
	t.Cleanup(func() { afterEarlyNativeGate = old })
	afterEarlyNativeGate = func() {
		for name, text := range complete {
			if text != pending[name] {
				if err := os.WriteFile(filepath.Join(root, "launch-actions", name), []byte(text), 0o600); err != nil {
					t.Error(err)
				}
			}
		}
	}
	log := os.Getenv("KANBAN_TMUX_LOG")
	prior, _ := os.ReadFile(log)
	restore := withTerminalDefaultLauncher(t)
	defer restore()
	code, stdout, stderr := commandtest.Capture(func() int { return RunResumeRemote(append([]string{"--json"}, args...)) })
	if code != 2 || stdout != "" || !strings.Contains(stderr, "interactive_required") {
		t.Fatalf("code=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	if after := receiptFiles(t, root); !maps.EqualFunc(after, complete, func(a, b string) bool {
		return a == b || strings.TrimSpace(a) == strings.TrimSpace(b)
	}) {
		t.Fatalf("the refusal rewrote a receipt:\nwant %v\ngot  %v", complete, after)
	}
	if actual, _ := os.ReadFile(log); !bytes.Equal(prior, actual) {
		t.Fatal("the refused retry invoked the terminal")
	}
}
