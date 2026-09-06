//go:build unix

package menu

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func readDoctorConfig(t *testing.T, h *harness) *config.Config {
	t.Helper()
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ValidateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDoctorCreatesAndRepairsConfig(t *testing.T) {
	for _, original := range []string{"", "broken JSON", `{"schema_version":1,"language":"en","kanban_agent":"grok"}`, `{"language":"cn"}`, `{}`} {
		t.Run(original, func(t *testing.T) {
			h := newHarness(t)
			h.fakeCommand("claude", "")
			if original != "" {
				if err := os.MkdirAll(filepath.Dir(h.configPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(h.configPath, []byte(original), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			code, _, out := h.run("doctor")
			if code != 0 {
				t.Fatalf("doctor=%d %s", code, out)
			}
			feedback := "Updated and saved config.json: "
			unchanged := "config.json is unchanged: "
			language := "en"
			if original == `{"language":"cn"}` {
				feedback = "已更新并保存 config.json: "
				unchanged = "config.json 无需更新: "
				language = "cn"
			}
			if original == "" {
				feedback = "Created and saved config.json: "
			}
			if !strings.Contains(out, feedback+h.configPath) {
				t.Fatalf("missing config save feedback: %s", out)
			}
			cfg := readDoctorConfig(t, h)
			if cfg.Language != language {
				t.Fatalf("language=%s want %s", cfg.Language, language)
			}
			code, _, out = h.run("doctor")
			if code != 0 || !strings.Contains(out, unchanged+h.configPath) {
				t.Fatalf("missing unchanged feedback: doctor=%d %s", code, out)
			}
			if cfg.KanbanAgent != "claude" || cfg.KanbanAgents["large"] != "claude" || cfg.KanbanAgents["small"] != "claude" || cfg.Launcher != "foreground" || !cfg.WelcomeComplete {
				t.Fatalf("unusable result: %+v", cfg)
			}
			for _, role := range config.ReviewRoles {
				if cfg.Reviewers[role] != "claude" || cfg.Models.ReviewRoles[role]["model"] != cfg.Models.Review["claude"]["model"] {
					t.Fatalf("role %s not repaired: %+v", role, cfg)
				}
			}
			backups, err := filepath.Glob(h.configPath + ".bak.*")
			if err != nil {
				t.Fatal(err)
			}
			if original == "" && len(backups) != 0 {
				t.Fatal("new config should not have an original backup")
			}
			if original != "" {
				if len(backups) != 1 {
					t.Fatalf("backups=%v", backups)
				}
				data, err := os.ReadFile(backups[0])
				if err != nil || !bytes.Equal(data, []byte(original)) {
					t.Fatalf("backup lost original: %v", err)
				}
			}
		})
	}
}

func TestDoctorRepairsUnavailableLaunchers(t *testing.T) {
	for _, tc := range []struct {
		launcher    string
		tmux, herdr bool
		want        string
	}{
		{"tmux", false, false, "foreground"},
		{"tmux-session", false, false, "foreground"},
		{"auto", false, false, "foreground"},
		{"herdr", true, false, "tmux-session"},
		{"console", true, false, "auto"},
		{"tmux", false, true, "herdr"},
	} {
		t.Run(tc.launcher+"-"+tc.want, func(t *testing.T) {
			h := newHarness(t)
			h.installFake(tc.tmux)
			if tc.herdr {
				h.fakeCommand("herdr", "")
			}
			h.writeConfig(defaultPayload(map[string]any{"launcher": tc.launcher}))
			code, _, out := h.run("doctor")
			if code != 0 {
				t.Fatalf("%d %s", code, out)
			}
			if cfg := readDoctorConfig(t, h); cfg.Launcher != tc.want {
				t.Fatalf("launcher=%s want %s", cfg.Launcher, tc.want)
			}
		})
	}
}

func TestDoctorWindowsLauncherKeepsHerdrWhenInstalled(t *testing.T) {
	previous := windowsOS
	windowsOS = true
	t.Cleanup(func() { windowsOS = previous })
	cfg := config.DefaultConfig()
	cfg.Launcher = "herdr"
	agents := map[string]agentState{"codex": {Path: "codex.exe", Version: "1", Review: true}}
	repairConfiguredTools(cfg, agents, TerminalTools{Herdr: TerminalTool{Path: "herdr.exe"}})
	if cfg.Launcher != "herdr" {
		t.Fatalf("launcher=%s", cfg.Launcher)
	}
}

func TestDoctorWindowsLauncherFallbackWithoutHerdr(t *testing.T) {
	previous := windowsOS
	windowsOS = true
	t.Cleanup(func() { windowsOS = previous })
	cfg := config.DefaultConfig()
	cfg.Launcher = "herdr"
	agents := map[string]agentState{"codex": {Path: "codex.exe", Version: "1", Review: true}}
	repairConfiguredTools(cfg, agents, TerminalTools{})
	if cfg.Launcher != "console" {
		t.Fatalf("launcher=%s", cfg.Launcher)
	}
}
