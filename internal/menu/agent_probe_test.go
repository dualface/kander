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
	if states["codex"].Path != path || states["helper"].Version != "renamed-version" || states["helper"].Review {
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
