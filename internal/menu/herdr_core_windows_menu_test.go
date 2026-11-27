package menu

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/terminal/builtin"
	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

func useWindowsMenu(t *testing.T, windows bool) {
	t.Helper()
	saved := windowsOS
	windowsOS = windows
	t.Cleanup(func() { windowsOS = saved })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANDER_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("KANDER_LANG", "en")
	// Only the embedded definitions: on Windows HOME does not move the share
	// directory, and a user's definition could name the real hc.
	useDefinitionDirs(t)
}

func useDefinitionDirs(t *testing.T, dirs ...terminal.DefinitionDir) {
	t.Helper()
	saved := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir { return dirs }
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = saved
		terminal.ReloadDefinitions()
	})
}

// installFakeHC copies the test binary onto an otherwise empty PATH as hc.
func installFakeHC(t *testing.T, replies ...terminaltest.Reply) *terminaltest.Fake {
	t.Helper()
	fake := terminaltest.New(t, replies...)
	bin := t.TempDir()
	name := builtin.HerdrCoreExecutable
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	terminaltest.CopyExecutable(t, filepath.Join(bin, name))
	t.Setenv("PATH", bin)
	return fake
}

func TestWindowsHerdrCoreDoctor(t *testing.T) {
	version := terminaltest.Reply{Args: []string{"--version"}, Stdout: "hc 0.1.21\n"}
	status := []string{"server", "status", "--json"}
	for _, tc := range []struct {
		name  string
		reply terminaltest.Reply
		want  string
	}{
		{"running", terminaltest.Reply{Stdout: `{"pid":42,"version":"0.1.21"}`}, "running"},
		{"unavailable on stderr", terminaltest.Reply{Code: 5, Stderr: `{"error":{"kind":"unavailable","message":"no core"}}`}, "down"},
		{"unavailable on stdout", terminaltest.Reply{Code: 5, Stdout: `{"error":{"kind":"unavailable","message":"no core"}}`}, "down"},
		{"exit 0 without pid", terminaltest.Reply{Stdout: `{"version":"0.1.21"}`}, "unknown"},
		{"exit 0 with a textual pid", terminaltest.Reply{Stdout: `{"pid":"42"}`}, "unknown"},
		{"exit 0 not JSON", terminaltest.Reply{Stdout: "running"}, "unknown"},
		{"exit 0 unavailable", terminaltest.Reply{Stdout: `{"error":{"kind":"unavailable"}}`}, "unknown"},
		{"other failure", terminaltest.Reply{Code: 1, Stderr: `{"error":{"kind":"usage"}}`}, "unknown"},
		{"timeout", terminaltest.Reply{Sleep: 8 * time.Second}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useWindowsMenu(t, true)
			if tc.name == "timeout" {
				saved := herdrCoreStatusTimeout
				herdrCoreStatusTimeout = 300 * time.Millisecond
				t.Cleanup(func() { herdrCoreStatusTimeout = saved })
			}
			tc.reply.Args = status
			installFakeHC(t, version, tc.reply)
			tools := CheckTerminalTools()
			got := "running"
			switch {
			case tools.HerdrCoreUnknown != "":
				got = "unknown"
			case tools.HerdrCoreDown != "":
				got = "down"
			}
			if !tools.HerdrCore.Available() || got != tc.want {
				t.Fatalf("state=%s, want %s: %+v", got, tc.want, tools)
			}
			text := reportText(CaptureReport(func() { reportTerminalTools(tools) }))
			if !strings.Contains(text, "hc: ") {
				t.Fatalf("doctor does not report hc:\n%s", text)
			}
			if said := strings.Contains(text, config.Text("herdr_core.doctor_running")); said != (tc.want == "running") {
				t.Fatalf("reported running=%t:\n%s", said, text)
			}
			if tc.want == "unknown" && !strings.Contains(text, tools.HerdrCoreUnknown) {
				t.Fatalf("the undecidable state is not reported:\n%s", text)
			}
			if tc.want == "down" && !strings.Contains(text, config.Text("herdr_core.doctor_not_running", tools.HerdrCoreDown)) {
				t.Fatalf("the stopped state is not reported:\n%s", text)
			}
			cfg := config.DefaultConfig()
			if healthy := validateHerdrCoreLauncher(cfg, tools); healthy != (tc.want == "running") {
				t.Fatalf("configured launcher healthy=%t", healthy)
			}
		})
	}
	t.Run("missing hc", func(t *testing.T) {
		useWindowsMenu(t, true)
		t.Setenv("PATH", t.TempDir())
		text := reportText(CaptureReport(func() { reportTerminalTools(CheckTerminalTools()) }))
		if !strings.Contains(text, config.Text("menu.not_installed", "hc")) {
			t.Fatalf("a missing hc is not reported:\n%s", text)
		}
	})
	t.Run("POSIX never asks hc", func(t *testing.T) {
		useWindowsMenu(t, false)
		t.Setenv("HC_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
		fake := installFakeHC(t, version)
		CheckTerminalTools()
		for _, call := range fake.Calls(t) {
			if slices.Equal(call, status) {
				t.Fatal("POSIX ran hc server status, which starts a missing core")
			}
		}
	})
	t.Run("auto with only hc", func(t *testing.T) {
		useWindowsMenu(t, true)
		tools := TerminalTools{HerdrCore: TerminalTool{Path: `C:\hc.exe`}}
		cfg := config.DefaultConfig()
		cfg.Launcher, cfg.WelcomeComplete = "auto", true
		text := reportText(CaptureReport(func() {
			validateConfiguredResources(cfg, map[string]agentState{}, tools)
		}))
		if strings.Contains(text, config.Text("menu.the_configured_launcher_is_auto_but_herdr_is_not_available")) {
			t.Fatalf("auto reported unavailable:\n%s", text)
		}
		ready := config.Text("menu.launcher_auto_chooses_at_start_on_windows")
		if !strings.Contains(text, ready) || strings.Contains(ready, "tmux") || strings.Contains(ready, "luvus") {
			t.Fatalf("the Windows auto hint is wrong:\n%s", text)
		}
	})
	for _, launcher := range []string{"auto", builtin.HerdrCore} {
		t.Run("repair keeps "+launcher, func(t *testing.T) {
			useWindowsMenu(t, true)
			tools := TerminalTools{HerdrCore: TerminalTool{Path: `C:\hc.exe`}}
			cfg := config.DefaultConfig()
			cfg.Launcher = launcher
			repairConfiguredTools(cfg, cfg, map[string]agentState{}, tools, toolRepairDecisions{}, nil)
			if cfg.Launcher != launcher {
				t.Fatalf("repair rewrote %s to %s", launcher, cfg.Launcher)
			}
		})
	}
}

func TestWindowsHerdrCoreOptions(t *testing.T) {
	useWindowsMenu(t, true)
	bin := t.TempDir()
	writeFakeExecutable(t, bin, builtin.HerdrCoreExecutable)
	t.Setenv("PATH", bin)

	cfg := config.DefaultConfig()
	cfg.Launcher = "auto"
	session := &Session{Config: cfg, existing: config.DefaultConfig()}
	session.existing.WelcomeComplete = false
	session.normalizeLauncher(cfg)
	if cfg.Launcher != "auto" {
		t.Fatalf("auto rewritten to %s although hc is installed", cfg.Launcher)
	}
	if got := launcherValues(session.LauncherChoices()); !slices.Contains(got, builtin.HerdrCore) {
		t.Fatalf("choices lack herdr-core: %v", got)
	}
}

// A user definition that still says posix replaces the built-in one: the menus
// must refuse what auto skips and start refuses.
func TestWindowsHerdrCorePOSIXOverride(t *testing.T) {
	useWindowsMenu(t, true)
	useHerdrCoreOverride(t, `"platform": "posix"`)
	if backend, ok := terminal.Lookup(builtin.HerdrCore); !ok || !backend.Capabilities().POSIXOnly {
		t.Fatal("the override is not active")
	}
	fake := installFakeHC(t, terminaltest.Reply{Args: []string{"--version"}, Stdout: "hc 0.1.21\n"})

	tools := CheckTerminalTools()
	if tools.HerdrCore.Available() || len(fake.Calls(t)) != 0 {
		t.Fatalf("an unsupported definition was probed as available: %+v", tools)
	}
	unsupported := config.Text("terminal.platform_unsupported_posix", builtin.HerdrCore)
	if text := reportText(CaptureReport(func() { reportTerminalTools(tools) })); !strings.Contains(text, unsupported) {
		t.Fatalf("doctor does not explain the override:\n%s", text)
	}
	if text := reportText(CaptureReport(func() {
		if validateHerdrCoreLauncher(config.DefaultConfig(), tools) {
			t.Error("the configured launcher is healthy")
		}
	})); !strings.Contains(text, unsupported) {
		t.Fatalf("the configured launcher is not explained:\n%s", text)
	}
	for _, launcher := range []string{"auto", builtin.HerdrCore} {
		if doctorLauncherAvailable(launcher, tools) {
			t.Errorf("doctor repair keeps %s", launcher)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Launcher = "auto"
	session := &Session{Config: cfg, existing: config.DefaultConfig()}
	session.existing.WelcomeComplete = false
	CaptureReport(func() { session.normalizeLauncher(cfg) })
	if cfg.Launcher != "console" {
		t.Errorf("normalization kept %s", cfg.Launcher)
	}
	if got := launcherValues(session.LauncherChoices()); slices.Contains(got, builtin.HerdrCore) {
		t.Errorf("options offer herdr-core: %v", got)
	}

	// auto still resolves to herdr, or to nothing: neither hint may offer herdr-core.
	auto := config.DefaultConfig()
	auto.Launcher, auto.WelcomeComplete = "auto", true
	for key, available := range map[string]TerminalTools{
		"menu.launcher_auto_chooses_at_start_on_windows_only_herdr":   {Herdr: TerminalTool{Path: `C:\herdr.exe`}},
		"menu.the_configured_launcher_is_auto_but_only_herdr_can_run": {},
	} {
		text := reportText(CaptureReport(func() {
			validateConfiguredResources(auto, map[string]agentState{}, available)
		}))
		if !strings.Contains(text, config.Text(key)) {
			t.Errorf("want %s:\n%s", key, text)
		}
		for _, wrong := range []string{"menu.launcher_auto_chooses_at_start_on_windows", "menu.the_configured_launcher_is_auto_but_herdr_is_not_available"} {
			if strings.Contains(text, config.Text(wrong)) {
				t.Errorf("%s offers the skipped definition:\n%s", wrong, text)
			}
		}
	}
}

// useHerdrCoreOverride activates a user copy of the built-in definition with
// the given platform line and further replacements.
func useHerdrCoreOverride(t *testing.T, platform string, replacements ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(packageDir, "..", "terminal", "builtin", "definitions", "herdr-core.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"platform": "any"`) {
		t.Fatal("the embedded platform was not found")
	}
	pairs := append([]string{`"platform": "any"`, platform}, replacements...)
	share := t.TempDir()
	dir := filepath.Join(share, terminal.DefinitionsDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "herdr-core.json"), []byte(strings.NewReplacer(pairs...).Replace(string(data))), 0o600); err != nil {
		t.Fatal(err)
	}
	useDefinitionDirs(t, terminal.DefinitionDir{Source: terminal.SourceGlobal, Root: share, Path: dir})
}

// The platform check works in both directions: POSIX does not offer a
// Windows-only override whose program is installed, because start refuses it.
func TestWindowsHerdrCoreOnlyOverrideOnPOSIX(t *testing.T) {
	useWindowsMenu(t, false)
	const wrapper = "kander-hc-wrapper"
	useHerdrCoreOverride(t, `"platform": "windows"`, `"binary": "hc"`, `"binary": "`+wrapper+`"`)
	backend, ok := terminal.Lookup(builtin.HerdrCore)
	if !ok || backend.Executable() != wrapper {
		t.Fatal("the override is not active")
	}
	bin := t.TempDir()
	writeFakeExecutable(t, bin, wrapper)
	t.Setenv("PATH", bin)
	cfg := config.DefaultConfig()
	cfg.Launcher = "foreground"
	session := &Session{Config: cfg, existing: cfg}
	if got := launcherValues(session.LauncherChoices()); slices.Contains(got, builtin.HerdrCore) {
		t.Fatalf("options offer a Windows-only definition on POSIX: %v", got)
	}
	tools := CheckTerminalTools()
	if tools.HerdrCore.Available() || doctorLauncherAvailable(builtin.HerdrCore, tools) {
		t.Fatalf("a Windows-only definition counts as available: %+v", tools.HerdrCore)
	}
	unsupported := config.Text("terminal.platform_unsupported_windows", builtin.HerdrCore)
	if text := reportText(CaptureReport(func() { reportTerminalTools(tools) })); !strings.Contains(text, unsupported) {
		t.Fatalf("doctor does not explain the override:\n%s", text)
	}
	cfg.Launcher = builtin.HerdrCore
	if label := session.LauncherChoices(); !slices.Contains(label, Choice{Value: builtin.HerdrCore, Label: unsupported}) {
		t.Fatalf("the configured launcher is not labelled: %v", label)
	}
}
