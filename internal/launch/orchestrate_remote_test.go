package launch

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestRemoteOrchestratorUsesServiceCardsAndRetainsItsPrivateIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX terminal fake")
	}
	fakeRoot, _, _ := setupBoard(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("KANBAN_LAUNCH_CWD", cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
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
	info, err := connection.Register(t.Context(), "orchestrate-project", "")
	if err != nil {
		t.Fatal(err)
	}
	project := connection.Project(info.ID)
	group := "20261112-orchestrated-group"
	var tasks []api.Task
	for _, slug := range []string{"orchestrate-remote-first", "orchestrate-remote-second"} {
		task, err := project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Orchestrate remote card", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		task, err = project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	cfg := envConfig("claude", "tmux", nil)
	cfg.Server.URL = httpServer.URL
	cfg.Project.Name = info.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	launchRequest := OrchestrateRequest{Root: forbidden, References: []string{group}, Handover: "Execute this confirmed group in plan order.", Agent: "claude", Launcher: "tmux"}
	result, err := StartOrchestrator(launchRequest)
	if err != nil {
		t.Fatal(err)
	}
	beforeRetry, _ := os.ReadFile(os.Getenv("KANBAN_TMUX_LOG"))
	replay, err := StartOrchestrator(launchRequest)
	if err != nil || replay.Address != result.Address {
		t.Fatalf("completed receipt replay: %+v %v", replay, err)
	}
	afterRetry, _ := os.ReadFile(os.Getenv("KANBAN_TMUX_LOG"))
	if string(beforeRetry) != string(afterRetry) {
		t.Fatal("completed orchestrator launch was repeated")
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("incomplete group expansion: %+v", result.Tasks)
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", info.ID, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("missing private session context: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var identity remote.ExecutionContext
	if err := json.Unmarshal(data, &identity); err != nil {
		t.Fatal(err)
	}
	if identity.ExecutionID == "" || identity.Session == "" || !strings.HasPrefix(identity.Window, "tmux:") {
		t.Fatalf("incomplete private routing identity: %+v", identity)
	}
	command := lastCommand(t, fakeRoot)
	for _, name := range []string{remote.EnvCoordinatorExecutionID, remote.EnvExecutionID} {
		if !strings.Contains(command, name+"="+identity.ExecutionID) {
			t.Fatalf("launch did not freeze %s: %s", name, command)
		}
	}
	t.Setenv(remote.EnvCoordinatorExecutionID, identity.ExecutionID)
	t.Setenv(remote.EnvExecutionID, identity.ExecutionID)
	request := board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "public-user-label", Basis: "confirmed orchestration", Members: []string{tasks[0].TaskID, tasks[1].TaskID}}
	path := filepath.Join(cwd, "claim.json")
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	oldWorker := ensureRemoteWorker
	ensureRemoteWorker = func(string) error { return nil }
	t.Cleanup(func() { ensureRemoteWorker = oldWorker })
	code, out := captureStdout(t, func() int { return RunCoordinator([]string{"claim", path}) })
	if code != 0 {
		t.Fatalf("remote claim: %d %s", code, out)
	}
	all, err := remote.Credentials(home)
	if err != nil || len(all) != 1 {
		t.Fatalf("group credential: %+v %v", all, err)
	}
	if all[0].ResourceKind != "group" || all[0].ExecutionID != identity.ExecutionID || all[0].Window != identity.Window || all[0].Session != identity.Session {
		t.Fatalf("group lease lost its launcher identity: %+v", all[0])
	}
	for _, before := range tasks {
		after, err := project.Show(t.Context(), before.TaskID)
		if err != nil || after.Revision != before.Revision || after.State != before.State {
			t.Fatal("orchestration or checkpoint claim mutated a card")
		}
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("remote orchestration used a local board: %v", err)
	}
}
func TestTaskExecutorClearsInheritedCoordinatorAuthority(t *testing.T) {
	source := process.ProcessInvocation{Argv: []string{"codex"}, Env: map[string]string{remote.EnvCoordinatorExecutionID: "parent-coordinator", remote.EnvExecutionID: "parent-task"}}
	invocation := withExecutionIdentity(LaunchPlan{Launcher: "foreground"}, source, "child-task")
	if invocation.Env[remote.EnvCoordinatorExecutionID] != "" || invocation.Env[remote.EnvExecutionID] != "child-task" {
		t.Fatal("task executor inherited coordinator authority")
	}
	if source.Env[remote.EnvCoordinatorExecutionID] != "parent-coordinator" {
		t.Fatal("child environment mutated the parent's invocation")
	}
}
