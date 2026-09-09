package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func TestAgentFieldsShareDefinitionsAndPreserveTemplates(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Agents = map[string]config.AgentDefinition{"helper": {Path: executable, Args: &config.AgentArgs{Start: []string{}, Resume: []string{}}, Session: &config.AgentSessionDefinition{Mode: "generated"}}}
	cfg.KanbanAgents["large"] = "helper"
	cfg.KanbanAgents["small"] = "helper"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, choice := range s.ExecutionChoices() {
		if choice.Value == "helper" {
			found = true
		}
	}
	if !found {
		t.Fatal("custom agent absent")
	}
	large, small := s.AgentExecutableFields("large"), s.AgentExecutableFields("small")
	if len(large) != 2 || large[0].Key() != small[0].Key() {
		t.Fatal("fields do not deduplicate")
	}
	large[1].Set("node")
	if small[1].Value() != "node" || cfg.Agents["helper"].ProcessName != "" {
		t.Fatal("editing does not isolate/share correctly")
	}
	if _, err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents["helper"].Args == nil || got.Agents["helper"].Session.Mode != "generated" || config.AgentProcessName(got, "helper") != "node" {
		t.Fatal(got.Agents)
	}
	s.SetExecutionAgent("large", "codex")
	fields := s.AgentExecutableFields("large")
	fields[0].Set(executable)
	fields[0].Set("")
	if _, ok := s.Config.Agents["codex"]; ok {
		t.Fatal("blank built-in edit should remove override")
	}
}

func TestReviewerChoicesFollowReviewTemplates(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Agents = map[string]config.AgentDefinition{
		"helper": {
			Path: exe,
			Args: &config.AgentArgs{Start: []string{}, Resume: []string{}, Review: []string{"--x"}},
			Session: &config.AgentSessionDefinition{Mode: "none"},
			Review: &config.AgentReview{
				CWD: config.ReviewCWDRuntime, OutputName: "out.txt",
				Output: &process.OutputSpec{Source: process.SourceFile, Parse: process.ParseRaw},
			},
		},
		"plain": {Path: exe, Args: &config.AgentArgs{Start: []string{}, Resume: []string{}}, Session: &config.AgentSessionDefinition{Mode: "none"}},
	}
	s, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	foundHelper, foundPlain := false, false
	for _, choice := range s.ReviewerChoicesFor("codex") {
		if choice.Value == "helper" {
			foundHelper = true
		}
		if choice.Value == "plain" {
			foundPlain = true
		}
	}
	if !foundHelper || foundPlain {
		t.Fatalf("reviewer choices=%v", s.ReviewerChoicesFor("codex"))
	}
}

func TestReviewModelFieldsHideCursorEffortAfterArgsOverlay(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"cursor": {Args: &config.AgentArgs{Start: []string{"x"}, Resume: []string{}}}}
	cfg.Reviewers["PM"] = "cursor"
	s, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range s.ReviewModelFieldsFor("PM") {
		if field.field == "effort" {
			t.Fatal("review effort shown for cursor")
		}
	}
}
