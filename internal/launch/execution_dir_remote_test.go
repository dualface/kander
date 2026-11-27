package launch

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

// realDir is dir with its symbolic links resolved, as Git reports a main
// worktree.
func realDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// enterSubdirectory turns the process directory into a subdirectory of a
// checkout, so the execution directory must be found as its main worktree.
func enterSubdirectory(t *testing.T, checkout string) {
	t.Helper()
	sub := filepath.Join(checkout, "nested", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
}

func requireExecutionDir(t *testing.T, got, checkout string) {
	t.Helper()
	if got == "" || realDir(t, got) != realDir(t, checkout) {
		t.Fatalf("execution directory = %q, want the main checkout %q", got, checkout)
	}
}

func TestRemoteStartWithAWorktreeRecordsTheMainCheckout(t *testing.T) {
	skipOnWindows(t)
	source, head := integrationGit(t)
	lifecycleGit(t, source, "branch", "v2", head)
	bare := filepath.Join(t.TempDir(), "common.git")
	lifecycleGit(t, source, "clone", "--bare", source, bare)
	_, session, card, _, _ := remoteLifecycleFixture(t, "execution-dir-worktree", bare)
	checkout, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktrees", card.TaskID, "g1")
	t.Setenv("KANBAN_LAUNCH_CWD", worktree)
	enterSubdirectory(t, checkout)
	if _, err := StartRemote("claude", "tmux", card.TaskID); err != nil {
		t.Fatal(err)
	}
	credential, found, err := session.Credential(card.TaskID)
	if err != nil || !found || credential.Worktree != worktree {
		t.Fatalf("managed workspace: %+v %v", credential, err)
	}
	requireExecutionDir(t, credential.ProjectDir, checkout)
}

func TestRemoteStartWithoutAWorktreeRecordsTheMainCheckout(t *testing.T) {
	skipOnWindows(t)
	_, session, card, _, _ := remoteLifecycleFixture(t, "execution-dir-in-place")
	checkout, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	lifecycleGit(t, checkout, "init", "-q")
	lifecycleGit(t, checkout, "commit", "--allow-empty", "-q", "-m", "fixture")
	enterSubdirectory(t, checkout)
	if _, err := StartRemote("claude", "tmux", card.TaskID); err != nil {
		t.Fatal(err)
	}
	credential, found, err := session.Credential(card.TaskID)
	if err != nil || !found || credential.Worktree != "" {
		t.Fatalf("a project without a Git remote got a worktree: %+v %v", credential, err)
	}
	requireExecutionDir(t, credential.ProjectDir, checkout)
}

func TestRemoteOrchestratorRecordsTheMainCheckout(t *testing.T) {
	skipOnWindows(t)
	_, _, _ = setupBoard(t)
	checkout := t.TempDir()
	lifecycleGit(t, checkout, "init", "-q")
	lifecycleGit(t, checkout, "commit", "--allow-empty", "-q", "-m", "fixture")
	enterSubdirectory(t, checkout)
	t.Setenv("KANBAN_LAUNCH_CWD", checkout)
	t.Setenv(board.EnvBoardDir, filepath.Join(checkout, "forbidden-board"))
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(remote.EnvClientHome, home)
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	connection, err := client.New(httpServer.URL, "fixture-host")
	if err != nil {
		t.Fatal(err)
	}
	info, err := connection.Register(t.Context(), "execution-dir-project", "")
	if err != nil {
		t.Fatal(err)
	}
	project := connection.Project(info.ID)
	const group = "20261127-execution-dir-group"
	task, err := project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "execution-dir-member", Title: "Record the execution directory", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	if _, err := project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	cfg := envConfig("claude", "tmux", nil)
	cfg.Server.URL, cfg.Project.Name = httpServer.URL, info.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	if _, err := StartOrchestrator(OrchestrateRequest{References: []string{group}, Handover: "Execute the group.", Agent: "claude", Launcher: "tmux"}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("missing private session context: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var context remote.ExecutionContext
	if err := json.Unmarshal(data, &context); err != nil {
		t.Fatal(err)
	}
	requireExecutionDir(t, context.ProjectDir, checkout)
}
