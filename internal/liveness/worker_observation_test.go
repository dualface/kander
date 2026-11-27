package liveness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/worker"
)

// processNameProject is a Git project whose overlay names the codex
// foreground process differently from the scope configuration, observed from
// a process directory outside every project, as the worker observes it. Its
// pane p1 runs that process under the session marker id-1.
func processNameProject(t *testing.T) string {
	t.Helper()
	// The user environment is kept out before useFakeBackends saves the scope
	// configuration, whose launcher check loads the terminal definitions.
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	terminal.ReloadDefinitions()
	t.Cleanup(terminal.ReloadDefinitions)
	useFakeBackends(t, &fakeAgentBackend{name: "proc", process: true, facts: map[string]terminal.PaneFacts{
		"p1": {Command: "codex-project", Dead: "0", InMode: "0", SessionMarker: "id-1"},
	}})
	project := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	overlay := `{"agents":{"codex":{"process_name":"codex-project"}}}`
	if err := os.WriteFile(filepath.Join(project, config.OverlayFilename), []byte(overlay), 0o600); err != nil {
		t.Fatal(err)
	}
	return project
}

func requireAlive(t *testing.T, address worker.AgentAddress) {
	t.Helper()
	if got := observeWorkerAgent(t.Context(), address); got.Status != "alive" {
		t.Fatalf("observation = %+v, want alive", got)
	}
}

func TestWorkerObservationReadsTheProcessNameOfTheProjectOverlay(t *testing.T) {
	project := processNameProject(t)
	requireAlive(t, worker.AgentAddress{Window: "proc:p1", Session: "codex id-1", ConfigDir: project})
}

// A coordinator credential carries no directory of its own; the orchestrator
// launch context supplies it.
func TestWorkerObservationOfACoordinatorReadsTheProjectOverlay(t *testing.T) {
	project := processNameProject(t)
	home := t.TempDir()
	const server, client, projectID, execution = "http://server", "c-1", "project", "exec-orchestrator"
	if err := remote.SaveExecutionContext(home, remote.ExecutionContext{ServerURL: server, ClientID: client, ProjectID: projectID, ExecutionID: execution, Window: "proc:p1", Session: "codex id-1", ProjectDir: project}); err != nil {
		t.Fatal(err)
	}
	c := remote.Credential{ResourceKind: "group", ServerURL: server, ClientID: client, ProjectID: projectID, TaskID: "20261127-coordinated-group", ExecutionID: execution, Generation: 1, Token: "group-secret"}
	if err := remote.RefreshExecutionContext(home, &c); err != nil {
		t.Fatal(err)
	}
	requireAlive(t, worker.AgentAddress{Window: c.Window, Session: c.Session, ConfigDir: c.AgentConfigDir()})
}
