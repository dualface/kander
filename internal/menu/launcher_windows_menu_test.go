package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestLauncherChoicesWindowsOnlyConsoleForeground(t *testing.T) {
	orig := windowsOS
	windowsOS = true
	t.Cleanup(func() { windowsOS = orig })

	home := t.TempDir()
	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	t.Setenv("HOME", home)
	t.Setenv("KANDER_CONFIG", cfgPath)
	t.Setenv("KANDER_LANG", "cn")

	cfg := config.DefaultConfig()
	cfg.Launcher = "console"
	cfg.WelcomeComplete = true
	session := &Session{Config: cfg, existing: cfg}
	choices := session.LauncherChoices()
	if len(choices) != 2 || choices[0].Value != "console" || choices[1].Value != "foreground" {
		t.Fatalf("%v", choices)
	}
	session.SetLauncher("foreground")
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["launcher"] != "foreground" {
		t.Fatalf("%v", saved)
	}
}
