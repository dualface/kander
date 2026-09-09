package review

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewExecutableIgnoresExecutionDefinitions(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Agents = map[string]config.AgentDefinition{}
	for _, agent := range config.ReviewAgentNames(nil) {
		cfg.Agents[agent] = config.AgentDefinition{Path: exe, ProcessName: "wrapper"}
	}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	for _, agent := range config.ReviewAgentNames(nil) {
		key := strings.ToUpper(agent) + "_REVIEW_BIN"
		t.Setenv(key, "")
		settings, err := agentSettingsFor(agent, "PM")
		if err != nil {
			t.Fatal(err)
		}
		if settings.executable != config.AgentExecutableName(agent) {
			t.Fatal(settings.executable)
		}
		t.Setenv(key, "review-only-wrapper")
		settings, err = agentSettingsFor(agent, "PM")
		if err != nil || settings.executable != "review-only-wrapper" {
			t.Fatalf("%+v %v", settings, err)
		}
	}
	if _, err := agentSettingsFor("custom", "PM"); err == nil {
		t.Fatal("custom reviewer accepted")
	}
	cfg.Agents["helper"] = config.AgentDefinition{
		Path: exe,
		Args: &config.AgentArgs{Start: []string{}, Resume: []string{}, Review: []string{"--review"}},
		Session: &config.AgentSessionDefinition{Mode: "none"},
		Review: &config.AgentReview{
			CWD: config.ReviewCWDRuntime, OutputName: "out.txt",
			Output: &process.OutputSpec{Source: process.SourceFile, Parse: process.ParseRaw},
		},
	}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	settings, err := agentSettingsFor("helper", "PM")
	if err != nil || settings.executable != exe {
		t.Fatalf("custom path fallback %+v %v", settings, err)
	}
}
