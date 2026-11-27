package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// futureScope writes a scope config that a newer Kander could have written: an
// unknown key, an entry for an agent this version does not know, and roles that name it.
func futureScope(t *testing.T) (string, *config.Config) {
	t.Helper()
	isolateOverlay(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	base := config.DefaultConfig()
	base.WelcomeComplete = true
	base.Language = "en"
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["future_top"] = map[string]any{"enabled": true}
	doc["kanban_agent"] = "newagent"
	doc["kanban_agents"] = map[string]any{"large": "newagent", "small": "codex"}
	doc["chat_agent"] = "newagent"
	reviewers := doc["reviewers"].(map[string]any)
	reviewers["large"].(map[string]any)["PMQA"] = "newagent"
	config.OverlaySet(doc, map[string]any{"large_model": "m"}, "models", "kanban", "newagent")
	config.OverlaySet(doc, "x", "tui", "future")
	writeDoc(t, path, doc)
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatalf("a configuration with unknown content must load: %v", err)
	}
	return path, cfg
}

func writeDoc(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestOptionsShowAndKeepUnknownAgentReferences(t *testing.T) {
	path, cfg := futureScope(t)
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for name, choices := range map[string][]Choice{
		"execution": session.ExecutionChoicesFor("newagent"),
		"reviewer":  session.ReviewerChoicesFor("newagent"),
	} {
		var current *Choice
		for i := range choices {
			if choices[i].Value == "newagent" {
				current = &choices[i]
			}
		}
		if current == nil || current.Label == "" {
			t.Fatalf("%s: the unknown agent must stay selectable under its own name: %+v", name, choices)
		}
	}
	// Editing another field must not touch the unknown references.
	session.SetTUIField("theme", "dark")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	saved := readDoc(t, path)
	if saved["kanban_agent"] != "newagent" || saved["chat_agent"] != "newagent" {
		t.Fatalf("unknown references changed: %v %v", saved["kanban_agent"], saved["chat_agent"])
	}
	if got, _ := config.OverlayGet(saved, "kanban_agents", "large"); got != "newagent" {
		t.Fatalf("kanban_agents.large=%v", got)
	}
	if got, _ := config.OverlayGet(saved, "reviewers", "large", "PMQA"); got != "newagent" {
		t.Fatalf("reviewers.large.PMQA=%v", got)
	}
	for _, p := range [][]string{{"future_top", "enabled"}, {"tui", "future"}, {"models", "kanban", "newagent", "large_model"}} {
		if _, ok := config.OverlayGet(saved, p...); !ok {
			t.Fatalf("Options save lost %v", p)
		}
	}
	if got, _ := config.OverlayGet(saved, "tui", "theme"); got != "dark" {
		t.Fatalf("the edit was not saved: %v", got)
	}
}

func TestOptionsProjectSaveKeepsUnknownOverlayKeys(t *testing.T) {
	_, cfg := futureScope(t)
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	loc := config.OverlayLocation{ProjectRoot: dir, Path: filepath.Join(dir, config.OverlayFilename), Exists: true}
	overlay := map[string]any{
		"future_overlay": map[string]any{"a": 1},
		"tui":            map[string]any{"refresh": 45, "future": "y"},
		"kanban_agents":  map[string]any{"small": "otheragent"},
	}
	writeDoc(t, loc.Path, overlay)
	raw, err := config.ReadOverlayFile(loc.Path)
	if err != nil {
		t.Fatalf("an overlay with unknown keys must read: %v", err)
	}
	if err := session.AttachOverlay(loc, raw); err != nil {
		t.Fatalf("an overlay with unknown keys must attach: %v", err)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.SetTUIField("theme", "dark")
	if _, err := session.Save(); err != nil {
		t.Fatalf("an overlay with unknown keys must be saveable: %v", err)
	}
	saved := readDoc(t, loc.Path)
	for _, p := range [][]string{{"future_overlay", "a"}, {"tui", "future"}, {"tui", "refresh"}, {"kanban_agents", "small"}} {
		if _, ok := config.OverlayGet(saved, p...); !ok {
			t.Fatalf("overlay save lost %v: %v", p, saved)
		}
	}
	if got, _ := config.OverlayGet(saved, "kanban_agents", "small"); got != "otheragent" {
		t.Fatalf("an unknown agent reference in the overlay changed: %v", got)
	}
	if got, _ := config.OverlayGet(saved, "tui", "theme"); got != "dark" {
		t.Fatalf("the edit was not saved: %v", got)
	}
}

func TestDoctorReportsIgnoredContent(t *testing.T) {
	_, cfg := futureScope(t)
	text := reportText(CaptureReport(func() { reportIgnoredConfig(cfg) }))
	for _, want := range []string{"future_top", "tui.future", "models.kanban.newagent", "kanban_agent", "reviewers.large.PMQA", "newagent"} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output misses %q:\n%s", want, text)
		}
	}
}

func TestDoctorRepairKeepsUnknownAgentReferences(t *testing.T) {
	_, cfg := futureScope(t)
	// With no agent installed every known agent probes unavailable; the unknown
	// references still must not be offered a replacement.
	for _, plan := range planToolReplacements(cfg, map[string]agentState{}, true) {
		if plan.current == "newagent" {
			t.Fatalf("repair plans to replace an unknown agent reference: %+v", plan)
		}
	}
	changes, _ := repairConfiguredTools(cfg, cfg, map[string]agentState{}, TerminalTools{}, toolRepairDecisions{}, nil)
	for _, change := range changes {
		if strings.Contains(change, "newagent") {
			t.Fatalf("repair changed an unknown agent reference: %s", change)
		}
	}
	if cfg.KanbanAgent != "newagent" || cfg.ChatAgent != "newagent" || cfg.Reviewers["large"]["PMQA"] != "newagent" {
		t.Fatalf("references changed: %+v", cfg)
	}
}

// attachProject opens a Project-tab session over an overlay file.
func attachProject(t *testing.T, cfg *config.Config, overlay map[string]any) (*Session, string) {
	t.Helper()
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	loc := config.OverlayLocation{ProjectRoot: dir, Path: filepath.Join(dir, config.OverlayFilename), Exists: true}
	writeDoc(t, loc.Path, overlay)
	raw, err := config.ReadOverlayFile(loc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AttachOverlay(loc, raw); err != nil {
		t.Fatal(err)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	return session, loc.Path
}

func TestOptionsEditsDoNotClearUnknownOverlayContent(t *testing.T) {
	_, cfg := futureScope(t)
	overlay := map[string]any{
		"kanban_agents": map[string]any{"large": "newagent"},
		"models":        map[string]any{"kanban": map[string]any{"newagent": map[string]any{"large_model": "m", "large_effort": "high"}}},
		"tui":           map[string]any{"refresh": 45},
	}
	session, path := attachProject(t, cfg, overlay)
	// Switching away from the unknown agent must not delete its model overrides.
	session.SetExecutionAgent("large", "codex")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	saved := readDoc(t, path)
	if got, _ := config.OverlayGet(saved, "models", "kanban", "newagent", "large_model"); got != "m" {
		t.Fatalf("switching the agent cleared the unknown agent's overrides: %v", saved)
	}
	// Restoring the execution page clears known overrides and keeps the unknown ones.
	if err := session.RestoreSection("execution"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	saved = readDoc(t, path)
	if got, _ := config.OverlayGet(saved, "models", "kanban", "newagent", "large_effort"); got != "high" {
		t.Fatalf("restoring the page cleared unknown content: %v", saved)
	}
	if _, ok := config.OverlayGet(saved, "kanban_agents", "large"); ok {
		t.Fatalf("restoring the page kept a known override: %v", saved)
	}
}

// Restoring a page keeps unknown overlay content as the overlay holds it, not as the
// merge with the scope file shows it.
func TestRestoreKeepsTheOverlaysOwnUnknownValues(t *testing.T) {
	path, cfg := futureScope(t)
	scope := readDoc(t, path)
	config.OverlaySet(scope, "scope-large", "models", "kanban", "newagent", "large_model")
	writeDoc(t, path, scope)
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	overlay := map[string]any{"models": map[string]any{"kanban": map[string]any{"newagent": map[string]any{"small_model": "own"}}}}
	session, overlayPath := attachProject(t, cfg, overlay)
	if err := session.RestoreSection("execution"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	saved := readDoc(t, overlayPath)
	if got, _ := config.OverlayGet(saved, "models", "kanban", "newagent", "small_model"); got != "own" {
		t.Fatalf("the overlay's own value was lost: %v", saved)
	}
	if _, ok := config.OverlayGet(saved, "models", "kanban", "newagent", "large_model"); ok {
		t.Fatalf("a scope value was copied into the overlay: %v", saved)
	}
}

func TestOptionsOffersNoModelFieldsForUnknownAgents(t *testing.T) {
	_, cfg := futureScope(t)
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.ExecutionModelFieldsFor("large"); len(got) != 0 {
		t.Fatalf("execution fields for an unknown agent: %+v", got)
	}
	if got := session.ChatModelFieldsFor(); len(got) != 0 {
		t.Fatalf("chat fields for an unknown agent: %+v", got)
	}
	if got := session.ReviewModelFieldsFor("PMQA", "large"); len(got) != 0 {
		t.Fatalf("review fields for an unknown reviewer: %+v", got)
	}
}

func TestEditingTheSmallScaleLeavesKanbanAgentAlone(t *testing.T) {
	_, cfg := futureScope(t)
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.SetExecutionAgent("small", "claude")
	if session.Config.KanbanAgent != "newagent" {
		t.Fatalf("kanban_agent was overwritten: %q", session.Config.KanbanAgent)
	}
	// Editing the large scale leaves a kanban_agent that names an unknown agent alone,
	// whether it equals the replaced value or not.
	session.Config.KanbanAgent = "independentagent"
	session.SetExecutionAgent("large", "claude")
	if session.Config.KanbanAgent != "independentagent" {
		t.Fatalf("an independent kanban_agent was overwritten: %q", session.Config.KanbanAgent)
	}
	session.Config.KanbanAgent = "newagent"
	session.Config.KanbanAgents["large"] = "newagent"
	session.SetExecutionAgent("large", "codex")
	if session.Config.KanbanAgent != "newagent" {
		t.Fatalf("an unknown kanban_agent was overwritten by a scale edit: %q", session.Config.KanbanAgent)
	}
	session.Config.KanbanAgent = "claude"
	session.Config.KanbanAgents["large"] = "claude"
	session.SetExecutionAgent("large", "codex")
	if session.Config.KanbanAgent != "codex" {
		t.Fatalf("a mirroring kanban_agent must follow the large scale: %q", session.Config.KanbanAgent)
	}
}

// Turning an agent's own switch on again removes its only known key and leaves an
// entry of unknown content; a second save in the same session must not see that as
// someone else's edit.
func TestOptionsSwitchThenSecondSaveInOneSession(t *testing.T) {
	isolateOverlay(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	base := config.DefaultConfig()
	base.WelcomeComplete = true
	base.Language = "en"
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["agents"] = map[string]any{
		"codex":  map[string]any{"path": "codex"},
		"claude": map[string]any{"install_rules": false, "session": map[string]any{"Mode": "generated"}},
	}
	writeDoc(t, path, doc)
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetAgentInstallRules("claude", true); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Save(); err != nil {
		t.Fatalf("the first save: %v", err)
	}
	if got, _ := config.OverlayGet(readDoc(t, path), "agents", "claude", "session", "Mode"); got != "generated" {
		t.Fatalf("the unknown content of the entry was lost: %v", got)
	}
	session.SetTUIField("theme", "dark")
	if _, err := session.Save(); err != nil {
		t.Fatalf("the second save in the same session was refused: %v", err)
	}
}
