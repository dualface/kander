//go:build windows

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// Native Windows has no tmux, so doctor must neither probe nor report it — even
// when a runnable tmux (msys/cygwin) happens to sit on PATH, because kander
// still rejects the tmux launcher on Windows.
func TestCheckTerminalToolsSkipsTmuxOnWindows(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux.cmd"), []byte("@echo off"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if found := lookPath("tmux"); found == "" {
		t.Fatal("fake tmux is not discoverable; the assertion below would be vacuous")
	}
	if tools := CheckTerminalTools(); tools.Tmux != (TerminalTool{}) {
		t.Fatalf("tmux probed on windows: %+v", tools.Tmux)
	}
}

// The official installer only writes herdr into the registry PATH, which the
// current process cannot see. Finding it in the default install directory must
// not be reported as "not installed", nor prompt for another install.
func TestHerdrDetectedOutsidePathOnWindows(t *testing.T) {
	local := t.TempDir()
	bin := filepath.Join(local, "Programs", "Herdr", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(bin, "herdr.exe")
	if err := os.WriteFile(binary, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", local)

	tools := CheckTerminalTools()
	if tools.Herdr.Available() {
		t.Fatalf("off-PATH herdr must not count as available: %+v", tools.Herdr)
	}
	if tools.Herdr.OffPath != binary {
		t.Fatalf("off-PATH herdr not found: %+v", tools.Herdr)
	}
	if tools.NeedsHerdrInstall() {
		t.Fatal("herdr is already installed; must not offer to install it again")
	}
}

// herdr has a native Windows build, so doctor must stop calling herdr/auto unavailable.
func TestDoctorLauncherAvailableOnWindows(t *testing.T) {
	installed := TerminalTools{Herdr: TerminalTool{Path: `C:\herdr.exe`}}
	// Installed but not on PATH yet: doctor should say to reopen the terminal rather than rewrite the launcher.
	offPath := TerminalTools{Herdr: TerminalTool{OffPath: `C:\herdr.exe`}}
	missing := TerminalTools{}
	for _, tc := range []struct {
		launcher string
		tools    TerminalTools
		want     bool
	}{
		{"console", missing, true},
		{"foreground", missing, true},
		{"herdr", installed, true},
		{"herdr", offPath, true},
		{"herdr", missing, false},
		{"auto", installed, true},
		{"auto", offPath, true},
		{"auto", missing, false},
		{"tmux", installed, false},
		{"tmux-session", installed, false},
	} {
		if got := doctorLauncherAvailable(tc.launcher, tc.tools); got != tc.want {
			t.Fatalf("%s (herdr=%v): got %v want %v", tc.launcher, tc.tools.Herdr.Available(), got, tc.want)
		}
	}
}

// doctor's launcher validation must not hard-reject herdr on Windows either:
// with herdr installed and the process already inside herdr, it should only hint
// that herdr creates a tab in the current workspace, never warn about herdr.
func TestValidateConfiguredResourcesAcceptsHerdrOnWindows(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")

	cfg := config.DefaultConfig()
	cfg.Launcher = "herdr"
	cfg.WelcomeComplete = true
	agents := map[string]agentState{}
	for _, name := range config.ExecutionAgents {
		agents[name] = agentState{Path: `C:\agent.exe`, Version: "1", Review: true}
	}
	paths := config.InstallPaths{Mode: config.ModeGlobal}

	herdrWarning := func(tools TerminalTools) (bool, []ReportLine) {
		var warned bool
		lines := CaptureReport(func() {
			validateConfiguredResources(cfg, agents, paths, tools)
		})
		for _, line := range lines {
			if line.Level == LevelWarning && strings.Contains(line.Text, "herdr") {
				warned = true
			}
		}
		return warned, lines
	}

	installed := TerminalTools{Herdr: TerminalTool{Path: `C:\herdr.exe`}}
	warned, lines := herdrWarning(installed)
	if warned {
		t.Fatalf("herdr 在 Windows 上仍被硬拒: %+v", lines)
	}
	accepted := false
	for _, line := range lines {
		if strings.Contains(line.Text, "herdr") {
			accepted = true
		}
	}
	if !accepted {
		t.Fatalf("没有走到 herdr 分支: %+v", lines)
	}

	// It must still warn when herdr is unavailable, otherwise the assertion above is vacuous.
	if warned, lines := herdrWarning(TerminalTools{}); !warned {
		t.Fatalf("herdr 不可用时应给出警告: %+v", lines)
	}
}
