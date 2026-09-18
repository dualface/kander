package menu

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func usableAgentsFixture() map[string]agentState {
	return map[string]agentState{
		"claude": {Path: "claude", Version: "1", Execution: true, Review: true},
		"pi":     {Path: "pi", Version: "1", Execution: true, Review: true},
	}
}

func TestPlanToolReplacementsCoversBrokenFields(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgent = "devin"
	cfg.KanbanAgents["large"] = "devin"
	cfg.KanbanAgents["small"] = "claude"
	cfg.ChatAgent = "kimi"
	for _, scale := range config.TaskScales {
		for _, role := range config.ReviewRoles {
			cfg.Reviewers[scale][role] = "claude"
		}
	}
	cfg.Reviewers["large"]["PMQA"] = "devin"
	cfg.Reviewers["small"]["Security"] = "devin"

	plans := planToolReplacements(cfg, usableAgentsFixture(), true)
	var fields []string
	byField := map[string]toolReplacement{}
	for _, plan := range plans {
		fields = append(fields, plan.field)
		byField[plan.field] = plan
	}
	want := []string{"kanban_agent", "kanban_agents.large", "chat_agent", "reviewers.large.PMQA", "reviewers.small.Security"}
	if !slices.Equal(fields, want) {
		t.Fatalf("fields=%v want %v", fields, want)
	}
	for _, plan := range plans {
		if !slices.Equal(plan.candidates, []string{"claude", "pi"}) {
			t.Fatalf("%s candidates=%v", plan.field, plan.candidates)
		}
	}
	if byField["kanban_agents.large"].defaultFrom != "kanban_agent" || byField["kanban_agents.large"].fallback != "devin" {
		t.Fatalf("scale default chain wrong: %+v", byField["kanban_agents.large"])
	}
	if byField["reviewers.large.PMQA"].current != "devin" {
		t.Fatalf("current=%s", byField["reviewers.large.PMQA"].current)
	}
}

// A chat_agent absent from the scope document is derived from
// kanban_agents.large and follows the repaired value, so the plan must not
// prompt for it even when the loaded value looks broken.
func TestPlanToolReplacementsSkipsDerivedChat(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ChatAgent = "devin" // derived value after load fallbacks
	cfg.KanbanAgents["large"] = "devin"
	cfg.KanbanAgents["small"] = "claude"
	cfg.KanbanAgent = "claude"
	plans := planToolReplacements(cfg, usableAgentsFixture(), false)
	for _, plan := range plans {
		if plan.field == "chat_agent" {
			t.Fatalf("derived chat must not be planned: %+v", plan)
		}
	}
	// The same value plans a prompt once the document sets chat_agent itself.
	plans = planToolReplacements(cfg, usableAgentsFixture(), true)
	found := false
	for _, plan := range plans {
		if plan.field == "chat_agent" {
			found = true
		}
	}
	if !found {
		t.Fatal("explicit broken chat_agent must be planned")
	}
}

func TestCollectToolReplacementsDecidesAndDefaults(t *testing.T) {
	plans := []toolReplacement{
		{field: "kanban_agent", current: "devin", candidates: []string{"claude", "pi"}},
		{field: "kanban_agents.large", current: "devin", candidates: []string{"claude", "pi"}, defaultFrom: "kanban_agent", fallback: "devin"},
		{field: "reviewers.large.PMQA", current: "devin", candidates: []string{"claude", "pi"}},
	}
	var defaults []string
	answers := []string{"pi", "pi", "claude"}
	old := askDoctorAgentChoice
	askDoctorAgentChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		defaults = append(defaults, defaultValue)
		next := answers[0]
		answers = answers[1:]
		return next, nil
	}
	t.Cleanup(func() { askDoctorAgentChoice = old })
	text := func(id string, args ...any) string { return id }
	decisions := collectToolReplacements(plans, text)
	want := map[string]string{"kanban_agent": "pi", "kanban_agents.large": "pi", "reviewers.large.PMQA": "claude"}
	if len(decisions) != len(want) {
		t.Fatalf("decisions=%v", decisions)
	}
	for field, agent := range want {
		if decisions[field] != agent {
			t.Fatalf("decisions[%s]=%s want %s", field, decisions[field], agent)
		}
	}
	// The scale default follows the kanban_agent decision; the reviewer default is the first candidate.
	if !slices.Equal(defaults, []string{"claude", "pi", "claude"}) {
		t.Fatalf("defaults=%v", defaults)
	}
}

func TestCollectToolReplacementsStopsOnInputError(t *testing.T) {
	plans := []toolReplacement{
		{field: "kanban_agent", current: "devin", candidates: []string{"claude"}},
		{field: "chat_agent", current: "devin", candidates: []string{"claude"}},
	}
	old := askDoctorAgentChoice
	askDoctorAgentChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		return "", errors.New("input ended")
	}
	t.Cleanup(func() { askDoctorAgentChoice = old })
	decisions := collectToolReplacements(plans, func(id string, args ...any) string { return id })
	if len(decisions) != 0 {
		t.Fatalf("decisions=%v want none", decisions)
	}
}

func TestRepairConfiguredToolsAppliesDecisions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgent = "devin"
	cfg.KanbanAgents["large"] = "devin"
	cfg.KanbanAgents["small"] = "claude"
	cfg.ChatAgent = "kimi"
	cfg.Reviewers["large"]["PMQA"] = "devin"
	cfg.Reviewers["small"]["PMQA"] = "claude"
	decisions := toolRepairDecisions{chatExplicit: true, fields: map[string]string{
		"kanban_agent":         "pi",
		"kanban_agents.large":  "claude",
		"chat_agent":           "pi",
		"reviewers.large.PMQA": "claude",
		"reviewers.small.PMQA": "pi", // not broken: must be ignored
		"kanban_agents.small":  "pi", // not broken: must be ignored
	}}
	changes, warnings := repairConfiguredTools(cfg, cfg, usableAgentsFixture(), TerminalTools{}, decisions, nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings=%v", warnings)
	}
	if cfg.KanbanAgent != "pi" || cfg.KanbanAgents["large"] != "claude" || cfg.ChatAgent != "pi" {
		t.Fatalf("execution decisions not applied: %+v", cfg)
	}
	if cfg.Reviewers["large"]["PMQA"] != "claude" {
		t.Fatalf("reviewer decision not applied: %+v", cfg.Reviewers)
	}
	// Decisions naming healthy fields are ignored: only broken fields are rewritten.
	for _, change := range changes {
		if strings.HasPrefix(change, "kanban_agents.small") || strings.HasPrefix(change, "reviewers.small.PMQA") {
			t.Fatalf("healthy field was rewritten: %v", changes)
		}
	}
	if cfg.KanbanAgents["small"] != "claude" || cfg.Reviewers["small"]["PMQA"] != "claude" {
		t.Fatalf("healthy fields were rewritten: %+v", cfg)
	}
	if cfg.Models.ReviewRoles["PMQA"]["large_agent"] != "claude" {
		t.Fatalf("role model binding not synced: %+v", cfg.Models.ReviewRoles["PMQA"])
	}
	for _, want := range []string{"kanban_agent: devin -> pi", "kanban_agents.large: devin -> claude", "chat_agent: kimi -> pi", "reviewers.large.PMQA: devin -> claude"} {
		if !slices.Contains(changes, want) {
			t.Fatalf("changes missing %q: %v", want, changes)
		}
	}
}

func TestRepairConfiguredToolsWarnsAndKeepsWithoutCandidates(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgent = "devin"
	cfg.Launcher = "foreground"
	cfg.Reviewers["large"]["PMQA"] = "devin"
	changes, warnings := repairConfiguredTools(cfg, cfg, map[string]agentState{}, TerminalTools{}, toolRepairDecisions{chatExplicit: true}, nil)
	if cfg.KanbanAgent != "devin" || cfg.Reviewers["large"]["PMQA"] != "devin" {
		t.Fatalf("fields without candidates must keep their values: %+v", cfg)
	}
	if len(changes) != 0 {
		t.Fatalf("changes=%v want none", changes)
	}
	if len(warnings) == 0 {
		t.Fatal("missing no-candidate warnings")
	}
	joined := strings.Join(warnings, "\n")
	for _, field := range []string{"kanban_agent", "kanban_agents.large", "kanban_agents.small", "chat_agent", "reviewers.large.PMQA", "reviewers.large.Security", "reviewers.small.PMQA", "reviewers.small.Security"} {
		if !strings.Contains(joined, field) {
			t.Fatalf("warning missing %s:\n%s", field, joined)
		}
	}
}

func TestRepairDoctorConfigInteractiveCollectsBeforeRepair(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgPath := filepath.Join(home, "config.json")
	cfg := config.DefaultConfig()
	cfg.KanbanAgent = "devin"
	cfg.KanbanAgents["large"] = "devin"
	cfg.KanbanAgents["small"] = "claude"
	cfg.ChatAgent = "kimi"
	cfg.Reviewers["large"]["PMQA"] = "devin"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CONFIG", cfgPath)

	var prompts []string
	answers := map[string]string{
		"kanban_agent":         "pi",
		"kanban_agents.large":  "pi",
		"chat_agent":           "claude",
		"reviewers.large.PMQA": "claude",
	}
	old := askDoctorAgentChoice
	askDoctorAgentChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		prompts = append(prompts, prompt)
		for field, answer := range answers {
			if strings.Contains(prompt, field) {
				return answer, nil
			}
		}
		return defaultValue, nil
	}
	t.Cleanup(func() { askDoctorAgentChoice = old })

	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, true)
	if !ok || repaired == nil {
		t.Fatal("interactive repair failed")
	}
	// Every broken field got its own prompt.
	for _, field := range []string{"kanban_agent", "kanban_agents.large", "chat_agent", "reviewers.large.PMQA"} {
		found := false
		for _, prompt := range prompts {
			if strings.Contains(prompt, field) {
				found = true
			}
		}
		if !found {
			t.Fatalf("no prompt for %s: %v", field, prompts)
		}
	}
	if repaired.KanbanAgent != "pi" || repaired.KanbanAgents["large"] != "pi" || repaired.ChatAgent != "claude" || repaired.Reviewers["large"]["PMQA"] != "claude" {
		t.Fatalf("user choices were not written: %+v", repaired)
	}
	// The written config agrees with the returned one.
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.KanbanAgent != "pi" || saved.ChatAgent != "claude" || saved.Reviewers["large"]["PMQA"] != "claude" {
		t.Fatalf("saved config ignores user choices: %+v", saved)
	}
}

// The non-interactive repair keeps the automatic first-usable replacement and
// never prompts, even when a prompt seam would answer.
func TestRepairDoctorConfigNonInteractiveNeverPrompts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgPath := filepath.Join(home, "config.json")
	cfg := config.DefaultConfig()
	cfg.KanbanAgent = "devin"
	cfg.Reviewers["large"]["PMQA"] = "devin"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CONFIG", cfgPath)

	old := askDoctorAgentChoice
	called := false
	askDoctorAgentChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		called = true
		return "pi", nil
	}
	t.Cleanup(func() { askDoctorAgentChoice = old })

	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, false)
	if !ok || repaired == nil {
		t.Fatal("non-interactive repair failed")
	}
	if called {
		t.Fatal("non-interactive repair prompted")
	}
	if repaired.KanbanAgent != "claude" || repaired.Reviewers["large"]["PMQA"] != "claude" {
		t.Fatalf("automatic first-usable repair changed: %+v", repaired)
	}
}

// PMQA-1 regression: a document that omits chat_agent derives the field from
// kanban_agents.large; the interactive repair must not prompt for it, and the
// written config follows the repaired large value, not the first candidate.
func TestRepairDoctorConfigDerivedChatFollowsLarge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgPath := filepath.Join(home, "config.json")
	payload := `{
		"schema_version": 1, "welcome_complete": true, "language": "en",
		"kanban_agent": "claude",
		"kanban_agents": {"large": "devin", "small": "claude"},
		"launcher": "foreground",
		"reviewers": {"large": {"PMQA": "claude", "Security": "claude"}, "small": {"PMQA": "claude", "Security": "claude"}}
	}`
	if err := os.WriteFile(cfgPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CONFIG", cfgPath)

	var prompts []string
	old := askDoctorAgentChoice
	askDoctorAgentChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		prompts = append(prompts, prompt)
		return "pi", nil
	}
	t.Cleanup(func() { askDoctorAgentChoice = old })

	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, true)
	if !ok || repaired == nil {
		t.Fatal("interactive repair failed")
	}
	for _, prompt := range prompts {
		if strings.Contains(prompt, "chat_agent") {
			t.Fatalf("derived chat_agent must not prompt: %v", prompts)
		}
	}
	found := false
	for _, prompt := range prompts {
		if strings.Contains(prompt, "kanban_agents.large") {
			found = true
		}
	}
	if !found {
		t.Fatalf("kanban_agents.large should have prompted: %v", prompts)
	}
	if repaired.ChatAgent != "pi" {
		t.Fatalf("derived chat must follow the repaired large agent: %s", repaired.ChatAgent)
	}
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ChatAgent != "pi" || saved.KanbanAgents["large"] != "pi" {
		t.Fatalf("saved config wrong: %+v", saved)
	}
}
