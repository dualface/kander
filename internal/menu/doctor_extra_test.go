//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorAutoLauncherWithOnlyHerdr(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	h.fakeCommand("herdr", "")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "auto"}))
	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "launcher=auto 在启动时按当前环境选择") {
		t.Fatalf("%s", err)
	}
}

// With launcher=auto and neither herdr nor tmux usable, doctor's repair
// rewrites the launcher to foreground and the report still points the user at
// the herdr install command; the unavailable-launcher warning cannot fire
// because the config no longer names auto by validation time.
func TestDoctorAutoLauncherWithoutTerminalTools(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "auto"}))
	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	for _, want := range []string{"auto", "foreground", "curl -fsSL https://herdr.dev/install.sh"} {
		if !strings.Contains(err, want) {
			t.Fatalf("missing %q: %s", want, err)
		}
	}
	_, cfg, _ := h.run("config", "--json")
	if !strings.Contains(cfg, `"launcher": "foreground"`) {
		t.Fatalf("launcher was not repaired to foreground: %s", cfg)
	}
}

// herdr installed by the official installer but not yet on PATH must be reported
// by its location (reopen the terminal), never as "install one of them".
func TestDoctorAutoLauncherWithOffPathHerdr(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	bin := filepath.Join(h.home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	herdr := filepath.Join(bin, "herdr")
	if err := os.WriteFile(herdr, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.writeConfig(defaultPayload(map[string]any{"launcher": "auto"}))
	code, _, err := h.run("doctor")
	if code != 1 {
		t.Fatalf("auto still cannot resolve to an off-PATH herdr: %d %s", code, err)
	}
	if !strings.Contains(err, "已安装于 "+herdr) || !strings.Contains(err, "重开终端") {
		t.Fatalf("off-PATH herdr must be reported by its path: %s", err)
	}
	if strings.Contains(err, "请安装其一") {
		t.Fatalf("must not advise installing what is already installed: %s", err)
	}
}

func TestDoctorRejectsHerdrWithoutWorkspaceID(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("herdr", "")
	h.setenv("HERDR_ENV", "1")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "herdr"}))
	code, _, err := h.run("doctor")
	if code != 1 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "缺少 HERDR_WORKSPACE_ID") {
		t.Fatalf("%s", err)
	}
}
