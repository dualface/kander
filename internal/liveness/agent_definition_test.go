package liveness

import (
	"context"
	"github.com/dualface/kander/internal/config"
	"path/filepath"
	"testing"
)

func TestConfiguredProcessNameReverseLookup(t *testing.T) {
	installPOSIXFakes(t, true)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"wrapped": {Dialect: "claude", ProcessName: "node"}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_TMUX_LIST_PANES", "%9\t$9\tnine\t@9\tnode\t0\twanted\t")
	got, err := TmuxReverseLookupContext(context.Background(), "tmux", TaskSession{Agent: "wrapped", Reference: "wanted"})
	if err != nil || got.PaneID != "%9" {
		t.Fatalf("%+v %v", got, err)
	}
	if ParseTaskSession("- SESSION: wrapped wanted\n") == nil {
		t.Fatal("custom metadata rejected")
	}
}
