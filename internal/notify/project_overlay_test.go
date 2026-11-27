package notify

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/worker"
)

// The background worker runs in its client home, outside every project. These
// tests resolve requests from such a directory, with the scope configuration
// of setupBoard and a Git project whose overlay changes the codex definition.

const herdrWindow = "herdr:w1:t9:w1:p9"

// outsideProjects moves the process into a directory that belongs to no
// project, as the worker's directory does, with the user environment kept out.
// It runs before setupBoard, whose configuration save loads the terminal
// definitions.
func outsideProjects(t *testing.T) {
	t.Helper()
	isolateUserEnvironment(t)
	t.Chdir(t.TempDir())
}

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

// overlayProject creates a Git project whose overlay holds the given text.
func overlayProject(t *testing.T, overlay string) string {
	t.Helper()
	project := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(project, config.OverlayFilename), []byte(overlay), 0o600); err != nil {
		t.Fatal(err)
	}
	return project
}

// codexProject creates a Git project whose overlay sets the codex definition.
func codexProject(t *testing.T, codex map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"agents": map[string]any{"codex": codex}})
	if err != nil {
		t.Fatal(err)
	}
	return overlayProject(t, string(data))
}

func noneSessionProject(t *testing.T) string {
	t.Helper()
	return codexProject(t, map[string]any{"session": map[string]any{"mode": "none"}})
}

func requireReason(t *testing.T, err error, id string) {
	t.Helper()
	if want := config.Text(id, "codex"); err == nil || err.Error() != want {
		t.Fatalf("reason = %v, want %q", err, want)
	}
}

func bareSessionAddress(dir string) worker.AgentAddress {
	return worker.AgentAddress{Window: herdrWindow, Session: "codex", ConfigDir: dir}
}

func TestResolveRequestExplainsABareSessionWithTheProjectOverlay(t *testing.T) {
	outsideProjects(t)
	root, fakeBin := setupBoard(t)
	calls := recordTerminalCalls(t, root, fakeBin)
	_, err := resolveRequest(bareSessionAddress(noneSessionProject(t)))
	requireReason(t, err, "notify.request_session_mode_none")
	_, err = resolveRequest(bareSessionAddress(codexProject(t, map[string]any{"process_name": "codex"})))
	requireReason(t, err, "notify.request_session_without_id")
	requireNoTerminalCall(t, calls)
}

// A recorded directory that no longer exists never selects the overlay of a
// directory above it.
func TestResolveRequestIgnoresTheOverlayAboveAMissingDirectory(t *testing.T) {
	outsideProjects(t)
	_, _ = setupBoard(t)
	missing := filepath.Join(noneSessionProject(t), "removed", "checkout")
	_, err := resolveRequest(bareSessionAddress(missing))
	requireReason(t, err, "notify.request_session_without_id")
}

// An invalid overlay fails the read of the definition: the scope configuration,
// which records no session ID for codex here, is not read instead.
func TestResolveRequestRefusesAnInvalidProjectOverlay(t *testing.T) {
	outsideProjects(t)
	_, _ = setupBoard(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Session: &config.AgentSessionDefinition{Mode: "none"}}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	_, err := resolveRequest(bareSessionAddress(overlayProject(t, "{not json")))
	requireReason(t, err, "notify.request_session_without_id")
}

func TestResolveRequestKeepsTheOverlayOfEachProject(t *testing.T) {
	outsideProjects(t)
	_, _ = setupBoard(t)
	none, plain := noneSessionProject(t), codexProject(t, map[string]any{"process_name": "codex"})
	for range 2 {
		_, err := resolveRequest(bareSessionAddress(none))
		requireReason(t, err, "notify.request_session_mode_none")
		_, err = resolveRequest(bareSessionAddress(plain))
		requireReason(t, err, "notify.request_session_without_id")
	}
}

// processNameProject is a project whose overlay names the codex foreground
// process differently from the scope configuration, with a tmux pane that
// runs it under the session marker of the card.
func processNameProject(t *testing.T) string {
	t.Helper()
	outsideProjects(t)
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "codex-project")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "id-1")
	return codexProject(t, map[string]any{"process_name": "codex-project"})
}

func TestResolveRequestMatchesTheProcessNameOfTheProjectOverlay(t *testing.T) {
	project := processNameProject(t)
	target, err := resolveRequest(worker.AgentAddress{Window: "tmux:$1:@1:%1", Session: "codex id-1", ConfigDir: project})
	if err != nil || target == nil || target.Window != "" {
		t.Fatalf("recorded pane: target=%+v err=%v", target, err)
	}
}

func TestResolveRequestLooksUpTheProcessNameOfTheProjectOverlay(t *testing.T) {
	project := processNameProject(t)
	t.Setenv("KANBAN_TMUX_STALE_PANE", "%1")
	t.Setenv("KANBAN_TMUX_LIST_PANES", "%2\t$1\tfake-session\t@1\tcodex-project\t0\tid-1\t")
	target, err := resolveRequest(worker.AgentAddress{Window: "tmux:$1:@1:%1", Session: "codex id-1", ConfigDir: project})
	if err != nil || target == nil || target.Window != "tmux:$1:@1:%2" {
		t.Fatalf("looked up pane: target=%+v err=%v", target, err)
	}
}
