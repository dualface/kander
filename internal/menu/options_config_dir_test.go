package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install/installtest"
)

// The options save writes no agent rules file, in the default or a relocated config directory.
func TestFinishWritesNoGlobalRulesFile(t *testing.T) {
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
	for _, file := range []string{filepath.Join(relocated, "CLAUDE.md"), filepath.Join(home, ".claude", "CLAUDE.md")} {
		if _, err := os.Lstat(file); !os.IsNotExist(err) {
			t.Fatalf("options save wrote %s: %v", file, err)
		}
	}
}
