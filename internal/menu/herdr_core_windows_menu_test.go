package menu

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal/builtin"
	"github.com/dualface/kander/internal/terminal/terminaltest"
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
	source, err := os.Open(fake.Program)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	copied, err := os.OpenFile(filepath.Join(bin, name), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(copied, source); err != nil {
		t.Fatal(err)
	}
	if err := copied.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return fake
}

func captureReport(t *testing.T) *[]ReportLine {
	t.Helper()
	saved, lines := reportSink, &[]ReportLine{}
	reportSink = lines
	t.Cleanup(func() { reportSink = saved })
	return lines
}

func sinkText(lines *[]ReportLine) string {
	var text strings.Builder
	for _, line := range *lines {
		text.WriteString(line.Text + "\n")
	}
	return text.String()
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
			lines := captureReport(t)
			reportTerminalTools(tools)
			text := sinkText(lines)
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
		lines := captureReport(t)
		reportTerminalTools(CheckTerminalTools())
		if !strings.Contains(sinkText(lines), config.Text("menu.not_installed", "hc")) {
			t.Fatalf("a missing hc is not reported:\n%s", sinkText(lines))
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
		lines := captureReport(t)
		validateConfiguredResources(cfg, map[string]agentState{}, config.InstallPaths{}, tools)
		if text := sinkText(lines); strings.Contains(text, config.Text("menu.the_configured_launcher_is_auto_but_herdr_is_not_available")) {
			t.Fatalf("auto reported unavailable:\n%s", text)
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
