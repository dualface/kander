//go:build unix

package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// writeSessionOverlayConfig writes a scope config whose agents.pi overlay
// declares session without file, the shape that silently lost session.file
// when the embedded pi definition added it.
func writeSessionOverlayConfig(t *testing.T, cfgPath string, extra map[string]any) {
	t.Helper()
	agent := map[string]any{"session": map[string]any{"mode": "generated"}}
	for key, value := range extra {
		agent[key] = value
	}
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"language":         "en",
		"kanban_agent":     "claude",
		"kanban_agents":    map[string]any{"large": "claude", "small": "claude"},
		"chat_agent":       "claude",
		"launcher":         "foreground",
		"reviewers": map[string]any{
			"large": map[string]any{"PMQA": "claude", "Security": "claude"},
			"small": map[string]any{"PMQA": "claude", "Security": "claude"},
		},
		"agents": map[string]any{"pi": agent},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sessionFileRepairHome(t *testing.T, extra map[string]any) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgPath := filepath.Join(home, "config.json")
	writeSessionOverlayConfig(t, cfgPath, extra)
	t.Setenv("KANDER_CONFIG", cfgPath)
	return cfgPath
}

func stubSessionFileChoice(t *testing.T, answer string, prompts *[]string) {
	t.Helper()
	old := askDoctorAgentChoice
	askDoctorAgentChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		*prompts = append(*prompts, prompt)
		if strings.Contains(prompt, "session.file") {
			return answer, nil
		}
		return defaultValue, nil
	}
	t.Cleanup(func() { askDoctorAgentChoice = old })
}

// Interactive repair asks once per inheriting agent and writes only the file
// key into the scope overlay when the user confirms.
func TestDoctorRepairStoresSessionFileOnConfirm(t *testing.T) {
	cfgPath := sessionFileRepairHome(t, map[string]any{"exit_command": "/quit"})
	var prompts []string
	stubSessionFileChoice(t, "store", &prompts)

	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, true)
	if !ok || repaired == nil {
		t.Fatal("interactive repair failed")
	}
	found := false
	for _, prompt := range prompts {
		if strings.Contains(prompt, "session.file") && strings.Contains(prompt, "pi") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no session.file prompt for pi: %v", prompts)
	}
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	agent := saved.Agents["pi"]
	if agent.Session == nil || agent.Session.File == nil {
		t.Fatalf("session.file not stored: %+v", agent.Session)
	}
	file := agent.Session.File
	if file.Format != "jsonl_header" || file.IDField != "id" || file.TypeField != "type" || file.TypeValue != "session" {
		t.Fatalf("stored file=%+v", file)
	}
	if agent.Session.Mode != "generated" || agent.ExitCommand == nil || *agent.ExitCommand != "/quit" {
		t.Fatalf("other overlay fields were touched: %+v", agent)
	}

	// Idempotent: with file now stored the agent no longer qualifies, so a
	// second repair never prompts and leaves the stored value alone.
	prompts = nil
	repaired, ok = repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, true)
	if !ok {
		t.Fatal("second repair failed")
	}
	for _, prompt := range prompts {
		if strings.Contains(prompt, "session.file") {
			t.Fatalf("second repair prompted again: %v", prompts)
		}
	}
	saved, err = config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Agents["pi"].Session.File == nil {
		t.Fatal("stored session.file lost on second repair")
	}
	_ = cfgPath
}

// Declining the prompt keeps the overlay untouched: nothing is written and
// the session.file keeps applying by inheritance only.
func TestDoctorRepairKeepsSessionFileOnDecline(t *testing.T) {
	sessionFileRepairHome(t, nil)
	var prompts []string
	stubSessionFileChoice(t, "keep", &prompts)

	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, true)
	if !ok || repaired == nil {
		t.Fatal("interactive repair failed")
	}
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if agent := saved.Agents["pi"]; agent.Session == nil || agent.Session.File != nil {
		t.Fatalf("declined store still wrote file: %+v", agent.Session)
	}
}

// A non-interactive repair never asks and never stores the inherited file.
func TestDoctorRepairNonInteractiveNeverStoresSessionFile(t *testing.T) {
	sessionFileRepairHome(t, nil)
	old := askDoctorAgentChoice
	called := false
	askDoctorAgentChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		called = true
		return "store", nil
	}
	t.Cleanup(func() { askDoctorAgentChoice = old })

	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, false)
	if !ok || repaired == nil {
		t.Fatal("non-interactive repair failed")
	}
	if called {
		t.Fatal("non-interactive repair prompted")
	}
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if agent := saved.Agents["pi"]; agent.Session != nil && agent.Session.File != nil {
		t.Fatalf("non-interactive repair stored file: %+v", agent.Session)
	}
}

// A session overlay living in the project .kander-config.json is reported but
// never stored into the scope config.json.
func TestDoctorRepairProjectOverlaySessionFileNotStored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgPath := filepath.Join(home, "config.json")
	writeSessionOverlayConfig(t, cfgPath, nil)
	// The scope file drops its agents section; the pi session overlay moves to
	// the project overlay of a non-git directory the repair runs in.
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"language":         "en",
		"kanban_agent":     "claude",
		"kanban_agents":    map[string]any{"large": "claude", "small": "claude"},
		"chat_agent":       "claude",
		"launcher":         "foreground",
		"reviewers": map[string]any{
			"large": map[string]any{"PMQA": "claude", "Security": "claude"},
			"small": map[string]any{"PMQA": "claude", "Security": "claude"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CONFIG", cfgPath)
	project := t.TempDir()
	overlay := `{"agents": {"pi": {"session": {"mode": "generated"}}}}`
	if err := os.WriteFile(filepath.Join(project, ".kander-config.json"), []byte(overlay), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)

	var prompts []string
	stubSessionFileChoice(t, "store", &prompts)
	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, true)
	if !ok || repaired == nil {
		t.Fatal("interactive repair failed")
	}
	for _, prompt := range prompts {
		if strings.Contains(prompt, "session.file") {
			t.Fatalf("project-overlay session must not prompt a scope store: %v", prompts)
		}
	}
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := saved.Agents["pi"]; exists {
		t.Fatalf("scope file gained a pi agent: %+v", saved.Agents)
	}
}

// The doctor report surfaces an information-level hint for an overlay that
// inherits session.file, without counting it as an error or writing the file
// under a non-interactive run.
func TestDoctorReportsInheritedSessionFile(t *testing.T) {
	h := newHarness(t)
	h.fakeCommand("claude", "")
	h.writeConfig(defaultPayload(map[string]any{
		"kanban_agent": "claude",
		"launcher":     "foreground",
		"agents": map[string]any{
			"pi": map[string]any{"session": map[string]any{"mode": "generated"}},
		},
	}))
	code, _, out := h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	if !strings.Contains(out, "session.file") || !strings.Contains(out, "pi") {
		t.Fatalf("missing session.file inheritance hint: %s", out)
	}
	cfg := readDoctorConfig(t, h)
	if agent := cfg.Agents["pi"]; agent.Session != nil && agent.Session.File != nil {
		t.Fatalf("non-interactive doctor stored file: %+v", agent.Session)
	}
}
