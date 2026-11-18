package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestClientRuntimeNeverFallsBackToLocalBoard(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	forbidden := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	input := filepath.Join(t.TempDir(), "input.md")
	if err := os.WriteFile(input, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := [][]string{{"list"}, {"show", "absent"}, {"new", "chore", "unbound", "Unbound task"}, {"update", "absent", "--document", "progress.md", "--file", input, "--expect-revision", "1"}, {"move", "absent", "todo"}, {"pick", "absent"}, {"start", "absent"}, {"resume", "absent", "--message", "continue"}, {"notify", "absent", "--message", "continue"}, {"dismiss", "absent"}, {"check"}, {"subscribe", "--watch", "absent"}, {"dispatch", "show", "absent", "round"}, {"coordinator", "show", "group"}, {"orchestrate", "--message", "handover", "absent"}, {"issue", "import", "42"}, {"issue", "triage", "42"}, {"issue", "result", "42", "--card", "absent"}, {"review", "progress", "/tmp", "absent"}, {"guard-write", "source.go"}, {"init"}}
	for _, args := range calls {
		result := runKander(t, args...)
		if result.code == 0 || !strings.Contains(result.stderr, "kander init --project <name>") {
			t.Fatalf("unbound command %v: %+v", args, result)
		}
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("unbound command used KANBAN_DIR: %v", err)
	}
	if _, err := os.Lstat("kanban"); !os.IsNotExist(err) {
		t.Fatalf("unbound command created local storage: %v", err)
	}
	// A malformed configuration also fails rather than selecting storage mode.
	if err := os.WriteFile(os.Getenv(config.EnvConfig), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runKander(t, "list"); result.code == 0 {
		t.Fatal("invalid config selected a fallback")
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatal("invalid config touched local storage")
	}
}
func TestClientInitBindsIndependentDirectoriesAndPreservesScope(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	cfg := config.DefaultConfig()
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(os.Getenv(config.EnvConfig))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	forbidden := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	first, second := t.TempDir(), t.TempDir()
	t.Chdir(first)
	bound := runKander(t, "init", "--server", httpServer.URL, "--project", "shared-project").require(t, "bind first clone")
	created := runKander(t, "new", "chore", "shared-service", "Shared service task").require(t, "create through REST")
	connection, err := client.New(httpServer.URL, "inspection-client")
	if err != nil {
		t.Fatal(err)
	}
	info, err := connection.Register(t.Context(), "shared-project", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.BoardPath == "" || !strings.Contains(bound.stdout, info.BoardPath) {
		t.Fatalf("init output omitted the server board path %q: %s", info.BoardPath, bound.stdout)
	}
	tasks, err := connection.Project(info.ID).List(t.Context(), "")
	if err != nil || len(tasks.Tasks) != 1 {
		t.Fatalf("central task: %+v %v", tasks, err)
	}
	if !strings.Contains(created.stdout, tasks.Tasks[0].TaskID) {
		t.Fatalf("CLI output omitted central identity: %s", created.stdout)
	}
	t.Chdir(second)
	runKander(t, "init", first).require(t, "repeat existing binding by explicit path")
	runKander(t, "init", "--server", httpServer.URL, "--project", "shared-project").require(t, "bind second clone")
	listed := runKander(t, "list", "--json").require(t, "read shared project")
	if !strings.Contains(listed.stdout, tasks.Tasks[0].TaskID) {
		t.Fatal("same name did not share the project")
	}
	runKander(t, "guard-write", "source.go").require(t, "ordinary code advisory")
	if result := runKander(t, "guard-write", filepath.Join(second, "kanban", "backlog", tasks.Tasks[0].TaskID, "spec.md")); result.code == 0 {
		t.Fatal("guard allowed direct formal document write")
	}
	for _, dir := range []string{first, second} {
		raw, err := os.ReadFile(filepath.Join(dir, config.OverlayFilename))
		if err != nil {
			t.Fatal(err)
		}
		var overlay map[string]any
		if err := json.Unmarshal(raw, &overlay); err != nil {
			t.Fatal(err)
		}
		if len(overlay) != 2 {
			t.Fatalf("init copied scope settings: %s", raw)
		}
		if _, err := os.Lstat(filepath.Join(dir, "kanban")); !os.IsNotExist(err) {
			t.Fatal("client initialized a local board")
		}
	}
	after, err := os.ReadFile(os.Getenv(config.EnvConfig))
	if err != nil || string(after) != string(before) {
		t.Fatal("binding changed scope config")
	}
	task, err := connection.Project(info.ID).Show(t.Context(), tasks.Tasks[0].TaskID)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, board.Placeholder, "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	if _, err := connection.Project(info.ID).PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	picked := runKanderInput(t, "invalid\n1\n", "pick").require(t, "interactive service pick")
	if !strings.Contains(picked.stdout, task.TaskID) {
		t.Fatal("interactive pick omitted the service identity")
	}
	pickedTask, err := connection.Project(info.ID).Show(t.Context(), task.TaskID)
	if err != nil || pickedTask.State != "todo" {
		t.Fatal("interactive pick did not move the service task")
	}
	httpServer.Close()
	if result := runKander(t, "new", "chore", "offline", "Offline task"); result.code == 0 {
		t.Fatal("offline write succeeded")
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatal("client used KANBAN_DIR")
	}

}

// A command that needs a project binding refuses to run in a checkout without one, before it
// parses nothing further or touches any storage.
func TestUnboundCheckoutRefusesEveryCommandThatNeedsAProject(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "forbidden-board"))
	cfg := config.DefaultConfig()
	cfg.Server.URL = "http://127.0.0.1:1"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	var checked int
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		if leaf.Annotations[command.NeedsProject] != "true" {
			continue
		}
		checked++
		args := append(strings.Fields(leaf.path), sampleArguments(t, leaf)...)
		if leaf.path == "coordinator release" {
			// The confirmation is an argument check, which precedes the binding check.
			args = []string{"coordinator", "release", "--end-execution", "/abs/release.json"}
		}
		// Argument errors precede the binding check, so the command line is a complete one.
		args = append(args, requiredFlags[leaf.path]...)
		result := runKander(t, args...)
		if result.code != 1 || !strings.Contains(result.stderr, config.Text("remote.not_configured")) {
			t.Errorf("%s: exit=%d stderr=%q", leaf.path, result.code, result.stderr)
		}
	}
	if checked == 0 {
		t.Fatal("no command declares the project binding")
	}
	if _, err := os.Lstat(os.Getenv(board.EnvBoardDir)); !os.IsNotExist(err) {
		t.Fatal("an unbound command selected local storage")
	}
}

// requiredFlags are the flags a command line of each command path needs to be a complete one.
var requiredFlags = map[string][]string{
	"issue result": {"--card", sampleTask},
	"notify":       {"--message", "text"},
	"orchestrate":  {"--message", "text"},
	"resume":       {"--message", "text"},
	"update":       {"--document", "spec.md", "--file", "/nonexistent/input.md", "--expect-revision", "1"},
}
