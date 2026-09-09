//go:build unix

package menu

import (
	"github.com/dualface/kander/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorAndPanelProbeAgentOverride(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.fakeBin, "renamed-agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho renamed-version\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", h.fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "foreground"
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Path: path}, "helper": {Path: path, Dialect: "claude"}}
	states := findAgents(cfg)
	t.Setenv("PATH", h.fakeBin)
	states = findAgents(cfg)
	if reviewerUsable(states["codex"]) {
		t.Fatal("execution wrapper offered as reviewer")
	}
	if states["helper"].Review {
		t.Fatal("dialect wrapper offered as reviewer")
	}
	if states["codex"].Path != path || states["helper"].Version != "renamed-version" {
		t.Fatal(states)
	}
	t.Setenv(config.EnvConfig, h.configPath)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	_, out, diagnostic := h.run("doctor")
	out += diagnostic
	if !strings.Contains(out, "Codex: renamed-version") || !strings.Contains(out, "helper: renamed-version") {
		t.Fatalf("%s %s", out, diagnostic)
	}
}

func TestPanelCanConfigureUnselectedRenamedBuiltin(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	t.Setenv(config.EnvConfig, h.configPath)
	wrapper := filepath.Join(h.fakeBin, "kander-codex")
	for _, name := range []string{"claude", "kander-codex"} {
		if err := os.WriteFile(filepath.Join(h.fakeBin, name), []byte("#!/bin/sh\necho fixture\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "foreground"
	cfg.KanbanAgent = "claude"
	cfg.KanbanAgents = map[string]string{"large": "claude", "small": "claude"}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, choice := range s.ExecutionChoicesFor("claude") {
		if choice.Value == "codex" {
			found = true
		}
	}
	if !found {
		t.Fatal("unavailable Codex cannot be selected for path editing")
	}
	s.SetExecutionAgent("small", "codex")
	s.AgentExecutableFields("small")[0].Set(wrapper)
	if _, err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(false)
	if err != nil || config.AgentPath(got, "codex") != wrapper {
		t.Fatalf("%+v %v", got, err)
	}
}
