package launch

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
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

func remoteLifecycleFixture(t *testing.T, slug string) (*config.Config, *remote.Session, api.Task, string, string) {
	t.Helper()
	fakeRoot, _, _ := setupBoard(t)
	cwd := t.TempDir()
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
