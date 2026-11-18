package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

// The production package graph must register check: the no-mode form goes
// through the service and never touches a local board.

func TestCheckCommandUsesLivenessInFullBinary(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})

	if _, _, err := cli.NewRoot().Find([]string{"check"}); err != nil {
		t.Fatalf("check is not registered: %v", err)
	}

	t.Chdir(t.TempDir())
	root := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, root)
	configDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, filepath.Join(configDir, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	cfg.AgentLanguage = "en"
	service := commandService(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	id := readyCard(t, "full-binary-check")
	grant, err := service.Project.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: service.Client.ClientID(), ExecutionID: "check-observation", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	out := runKander(t, "check", id).require(t, "service check")
	if !strings.Contains(out.stdout, id) || !strings.Contains(out.stdout, "status=unknown") {
		t.Fatalf("service liveness output missing: %+v", out)
	}
	after, err := service.Project.Show(t.Context(), id)
	if err != nil || after.Revision != grant.Task.Revision {
		t.Fatal("check changed the task")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("check used local board: %v", err)
	}
}

func TestCheckCommandDeliveryBoardFree(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "missing-board"))
	t.Chdir(t.TempDir())

	// An analysis that could not finish is the error object, never a result.
	result := runKander(t, "check", "delivery", "--base", "HEAD", "--json")
	if result.code != 3 || result.stdout != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", result.code, result.stdout, result.stderr)
	}
	if want := `{"error":{"code":"check_not_repository","message":"not a git repository"}}` + "\n"; result.stderr != want {
		t.Fatalf("stderr=%q, want %q", result.stderr, want)
	}
}

// The full binary binds the agent status observer of liveness to list: a working
// card with an observable terminal carries an agent object under --agent-status,
// and a card without --agent-status carries none. A list command built without
// the observer would print null in both forms.
func TestListCommandObservesAgentStatusInFullBinary(t *testing.T) {
	fixture := fakeMuxFixture(t)
	id := readyCard(t, "list-agent-status")
	runKander(t, "start", "--launcher", "fakemux", id).require(t, "start")
	waitCommandFact(t, func() bool {
		task, err := fixture.service.Project.Show(t.Context(), id)
		return err == nil && task.State == "working" && board.MetadataFrom(task.Text, board.FieldWindow) != ""
	})

	agentOf := func(listed map[string]any) any {
		tasks, _ := listed["tasks"].([]any)
		if len(tasks) != 1 || tasks[0].(map[string]any)["task_id"] != id {
			t.Fatalf("list: %v", listed)
		}
		return tasks[0].(map[string]any)["agent"]
	}
	observed := requireResult(t, runKander(t, "list", "--json", "--agent-status", "working"), "list --agent-status", "schema_version", "tasks")
	agent, ok := agentOf(observed).(map[string]any)
	if !ok {
		t.Fatalf("a working card with a window has no agent observation: %v", observed)
	}
	for _, key := range []string{"status", "runtime_state", "observed_at", "valid"} {
		if _, present := agent[key]; !present {
			t.Fatalf("agent lacks %q: %v", key, agent)
		}
	}
	if status, _ := agent["status"].(string); status == "" {
		t.Fatalf("agent status is empty: %v", agent)
	}
	plain := requireResult(t, runKander(t, "list", "--json", "working"), "list", "schema_version", "tasks")
	if got := agentOf(plain); got != nil {
		t.Fatalf("agent without --agent-status: %v", got)
	}
}
