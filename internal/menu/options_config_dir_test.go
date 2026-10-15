package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install/installtest"
)

func TestFinishIntegratesRelocatedConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	relocated := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", relocated)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(relocated, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(got), "KANDER-AGENTS.md") {
		t.Fatalf("relocated CLAUDE.md not integrated: %v %q", err, got)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("default CLAUDE.md touched: %v", err)
	}
}
