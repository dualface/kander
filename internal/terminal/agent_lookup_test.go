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
	writeTestConfig(t)
	t.Chdir(t.TempDir())
	project := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	data, err := json.Marshal(map[string]any{"agents": map[string]any{"codex": codex}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, config.OverlayFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return config.AgentLookupIn(project)
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
