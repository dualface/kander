package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

// projectLookup is the agent lookup of a Git project whose overlay sets the
// codex definition, read from a process directory outside any project.
func projectLookup(t *testing.T, codex map[string]any) config.AgentLookup {
	t.Helper()
	return config.AgentLookupIn(overlayProject(t, map[string]any{"agents": map[string]any{"codex": codex}}))
}

// overlayProject creates a Git project with the given overlay, and moves the
// process into a directory outside any project. The move comes before the
// scope configuration is saved: its launcher check loads the terminal
// definitions of the process directory's project.
func overlayProject(t *testing.T, overlay map[string]any) string {
	t.Helper()
	isolateUserEnvironment(t)
	t.Chdir(t.TempDir())
	writeTestConfig(t)
	project := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	data, err := json.Marshal(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, config.OverlayFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return project
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
	ReloadDefinitions()
	t.Cleanup(ReloadDefinitions)
}

func TestMatchAgentWithReadsTheProgramStatusOfTheLookup(t *testing.T) {
	resetLanguage(t)
	lookup := projectLookup(t, map[string]any{"program_status": map[string]any{"reports": true, "app": "project-codex"}})
	pane := PaneFacts{ProgramStatus: true, AgentApp: "project-codex"}
	if got := pane.MatchAgentWith("codex", lookup); got != AgentMatches {
		t.Fatalf("project declaration: %v, want a match", got)
	}
	if got := pane.MatchAgent("codex"); got != AgentDiffers {
		t.Fatalf("scope declaration: %v, want another agent", got)
	}
}

func TestSessionRowReverseLookupReadsTheSessionFileOfTheLookup(t *testing.T) {
	resetLanguage(t)
	lookup := projectLookup(t, map[string]any{"session": map[string]any{"mode": "hook:codex-rollout", "file": map[string]any{"format": "jsonl_header", "id_field": "id"}}})
	backend := sessionLookupBackend(t)
	path := writeSessionJSONL(t, "s1", "")
	list := `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "codex", `{"kind":"path","value":"`+jsonPath(path)+`"}`) + `]}}`
	fake := terminaltest.New(t, reply(list, "pane", "list"), reply(list, "pane", "list"))
	got, err := backend.ReverseLookup(context.Background(), fakeConn(fake), Identity{Agent: "codex", Reference: "s1", Lookup: lookup})
	if err != nil || got != (Address{Container: "w1:t1", Pane: "w1:p1"}) {
		t.Fatalf("project session file: address=%+v err=%v", got, err)
	}
	var incomplete *IncompleteLookupError
	if _, err := backend.ReverseLookup(context.Background(), fakeConn(fake), Identity{Agent: "codex", Reference: "s1"}); !errors.As(err, &incomplete) {
		t.Fatalf("scope definition without a session file: err=%v, want an incomplete lookup", err)
	}
}

// The retired project install share directory is no longer searched: a launcher that only a
// definition below <project>/.kander/share/terminals/ provides is refused for the project
// configuration, read from outside the project or inside it.
func TestAgentLookupRefusesALauncherOfTheRetiredProjectShare(t *testing.T) {
	resetLanguage(t)
	project := overlayProject(t, map[string]any{"launcher": "faketerm", "agents": map[string]any{"codex": map[string]any{"process_name": "codex-project"}}})
	definitions := filepath.Join(project, config.LegacyProjectInstallDirname, "share", DefinitionsDirName)
	if err := os.MkdirAll(definitions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(definitions, "faketerm.json"), []byte(fixtureDefinition), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.AgentLookupIn(project).Agent("codex"); err == nil {
		t.Fatal("the launcher of the retired project share was accepted")
	}
	t.Chdir(project)
	ReloadDefinitions()
	if _, ok := Lookup("faketerm"); ok {
		t.Fatal("a definition of the retired project share was loaded")
	}
}
