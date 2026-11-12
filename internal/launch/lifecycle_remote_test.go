package launch

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func remoteLifecycleFixture(t *testing.T, slug string, origin ...string) (*config.Config, *remote.Session, api.Task, string, string) {
	t.Helper()
	fakeRoot, _, _ := setupBoard(t)
	cwd := t.TempDir()
	if len(origin) != 0 {
		lifecycleGit(t, cwd, "clone", "--branch", "v2", origin[0], cwd)
	}
	t.Chdir(cwd)
	t.Setenv("KANBAN_LAUNCH_CWD", cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	cfg := envConfig("codex", "tmux", nil)
	cfg.Server.URL, cfg.Project.Name = httpServer.URL, "lifecycle-project"
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	card, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: slug, Title: "Verify remote lifecycle", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: remote-fixture-branch", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	card, err = session.Project.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	card, err = session.Project.Move(t.Context(), "", card.TaskID, api.Move{State: "todo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	previousWorker := ensureRemoteWorker
	ensureRemoteWorker = func(string) error { return nil }
	t.Cleanup(func() { ensureRemoteWorker = previousWorker })
	return cfg, session, card, forbidden, fakeRoot
}

func TestRemoteAdoptionProvesLocalProcessWithoutReadingBoard(t *testing.T) {
	cfg, session, card, forbidden, _ := remoteLifecycleFixture(t, "adopt-local-process")
	grant, err := session.Project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "adopt-process", Owner: "codex", Start: &api.StartInput{Session: "codex local-process", Window: "foreground"}})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := process.ObserveProcess(t.Context(), os.Getpid())
	if err != nil {
		t.Skipf("native process observation unavailable: %v", err)
	}
	boot, err := process.BootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: session.ServerURL, ClientID: session.Client.ClientID(), ProjectID: session.Info.ID, TaskID: card.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, AgentPID: facts.PID, AgentStart: facts.Start, BootID: boot, Session: "codex local-process", Window: "foreground"}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if err := commandRemoteAdopt(cfg, card.TaskID, "explicit local process adoption"); err != nil {
		t.Fatal(err)
	}
	state, err := session.Project.ExecutionState(t.Context(), card.TaskID, *credential.Claim())
	if err != nil || state.Record.Status != board.ExecutionRunning || state.Task.Revision != grant.Task.Revision {
		t.Fatalf("adoption changed content or failed: %+v %v", state, err)
	}
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "foreign-client"))
	if err := commandRemoteAdopt(cfg, card.TaskID, "foreign attempt"); err == nil {
		t.Fatal("foreign host adopted a local executor")
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("adoption read a local board: %v", err)
	}
}

func TestRemoteLateLaunchFailureKeepsExecutorProgressAndRefusesRelaunch(t *testing.T) {
	skipOnWindows(t)
	_, session, card, forbidden, fakeRoot := remoteLifecycleFixture(t, "late-remote-start")
	lateDiscoveryFailure(t, func() {
		credential, found, err := session.Credential(card.TaskID)
		if err != nil || !found {
			t.Fatalf("missing launch credential: %v", err)
		}
		current, err := session.Project.Show(t.Context(), card.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Project.PutDocument(t.Context(), "executor-progress", card.TaskID, api.Document{Path: "progress.md", Text: "executor progress must survive late failure", ExpectedRevision: current.Revision}, credential.Claim()); err != nil {
			t.Fatal(err)
		}
	})
	result, err := StartRemote("devin", "tmux", card.TaskID)
	requireUnresolved(t, err)
	if containerClosed(fakeRoot) {
		t.Fatal("remote launch closed an executor after progress")
	}
	credential, found, err := session.Credential(card.TaskID)
	if err != nil || !found {
		t.Fatalf("credential lost: %v", err)
	}
	state, err := session.Project.ExecutionState(t.Context(), card.TaskID, *credential.Claim())
	if err != nil || state.Record.Status != board.ExecutionUnresolved || !state.Standing.Evidence {
		t.Fatalf("late failure state: %+v %v", state, err)
	}
	if result.Outcome.Container == "" {
		t.Fatal("unresolved result lost the actual pane")
	}
	if _, err := session.Project.Document(t.Context(), card.TaskID, "progress.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRemote("devin", "tmux", card.TaskID); err == nil {
		t.Fatal("a retry launched a second executor")
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("start touched a local board: %v", err)
	}
}

func lifecycleGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Git %v: %s: %v", args, data, err)
	}
	return strings.TrimSpace(string(data))
}

func TestRemoteStartCreatesExecutionWorktreeAndPublishesItsActualHead(t *testing.T) {
	skipOnWindows(t)
	source, head := integrationGit(t)
	lifecycleGit(t, source, "branch", "v2", head)
	bare := filepath.Join(t.TempDir(), "common.git")
	lifecycleGit(t, source, "clone", "--bare", source, bare)
	_, session, card, forbidden, _ := remoteLifecycleFixture(t, "managed-execution-worktree", bare)
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktrees", card.TaskID, "g1")
	t.Setenv("KANBAN_LAUNCH_CWD", worktree)
	result, err := StartRemote("claude", "tmux", card.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	credential, found, err := session.Credential(card.TaskID)
	if err != nil || !found || credential.Worktree != worktree || credential.Branch == "" || credential.BaseCommit != head {
		t.Fatalf("managed workspace: %+v %v", credential, err)
	}
	if result.Outcome.Container == "" {
		t.Fatal("missing launched container")
	}
	baseline, err := session.Project.Delivery(t.Context(), card.TaskID, head)
	if err != nil || baseline.Purpose != "baseline" {
		t.Fatalf("baseline publication: %+v %v", baseline, err)
	}
	_, err = session.Project.Move(t.Context(), "baseline-is-not-completion", card.TaskID, api.Move{State: "review", DeliveryCommit: head}, credential.Claim())
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("baseline granted completion authority: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "implemented.txt"), []byte("execution implementation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycleGit(t, worktree, "add", "implemented.txt")
	lifecycleGit(t, worktree, "commit", "-m", "Implement execution work")
	implemented := lifecycleGit(t, worktree, "rev-parse", "HEAD")
	// The invoking view still runs in the main checkout. The shared MoveTask
	// boundary must publish this execution's worktree HEAD, not that checkout.
	moved, err := session.MoveTask(t.Context(), card.TaskID, api.Move{State: "review"})
	if err != nil || moved.State != "review" {
		t.Fatalf("delivery move: %+v %v", moved, err)
	}
	delivery, err := session.Project.Delivery(t.Context(), card.TaskID, implemented)
	if err != nil || delivery.Purpose != "delivery" || delivery.Generation != credential.Generation {
		t.Fatalf("wrong implementation delivered: %+v %v", delivery, err)
	}
	if lifecycleGit(t, worktree, "ls-remote", "--refs", "origin", delivery.Ref) != implemented+"\t"+delivery.Ref {
		t.Fatal("implementation is absent from common remote")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if lifecycleGit(t, cwd, "rev-parse", "HEAD") != head {
		t.Fatal("start or delivery changed the source checkout")
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("managed start used a local board: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "kanban")); !os.IsNotExist(err) {
		t.Fatalf("managed workspace contains a client board: %v", err)
	}
}
