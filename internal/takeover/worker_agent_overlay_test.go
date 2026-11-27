package takeover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/worker"
)

const overlayExitCommand = "/quit-project"

// isolateUserEnvironment keeps the user's home, its terminal definitions and
// its Git configuration out of the test: the definitions are read again from
// the temporary home, and again by the next test.
func isolateUserEnvironment(t *testing.T) {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	terminal.ReloadDefinitions()
	t.Cleanup(terminal.ReloadDefinitions)
}

// stopAddress is a lost codex execution on a tmux pane that runs the
// foreground process its project overlay names, stopped from a process
// directory outside every project, as the worker stops it. The overlay also
// names an exit command that the scope configuration does not have.
func stopAddress(t *testing.T) (worker.AgentAddress, string) {
	t.Helper()
	isolateUserEnvironment(t)
	t.Chdir(t.TempDir())
	root, _ := setupBoard(t)
	project := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	overlay := `{"agents":{"codex":{"process_name":"codex-project","exit_command":"` + overlayExitCommand + `"}}}`
	if err := os.WriteFile(filepath.Join(project, config.OverlayFilename), []byte(overlay), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "codex-project")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "id-1")
	return worker.AgentAddress{Window: "tmux:$88:@8:%1", Session: "codex id-1", ConfigDir: project}, filepath.Join(root, "tmux.log")
}

func TestWorkerStopTypesTheExitCommandOfTheProjectOverlay(t *testing.T) {
	address, log := stopAddress(t)
	if err := stopWorkerAgent(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	typed, err := os.ReadFile(log + ".instruction")
	if err != nil || !strings.Contains(string(typed), overlayExitCommand) {
		t.Fatalf("typed %q (%v), want the exit command of the project overlay", typed, err)
	}
}

// The pane still runs the agent on the first poll after the exit command; the
// wait must recognize it by the process name of the project overlay.
func TestWorkerStopWaitsWithTheProcessNameOfTheProjectOverlay(t *testing.T) {
	address, log := stopAddress(t)
	original, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  *pane_current_command*pane_in_mode*pane_dead*)
    if [ ! -f "$KANBAN_TMUX_LOG.exited" ]; then
      printf '%s\t0\t0\n' "$KANBAN_TMUX_CURRENT_COMMAND"
      exit 0
    fi
    ;;
esac
exec '` + original + `' "$@"
`
	if err := os.WriteFile(filepath.Join(wrapper, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
	polls := 0
	previous := sleepFn
	sleepFn = func(time.Duration) {
		polls++
		if err := os.WriteFile(log+".exited", nil, 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { sleepFn = previous })
	if err := stopWorkerAgent(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	if polls == 0 {
		t.Fatal("the wait never saw the running agent")
	}
}
