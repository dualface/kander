//go:build unix

package menu

import (
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// fakeHC answers --version only: doctor must not ask hc about the core,
// because every hc command starts a missing one.
const fakeHC = `#!/bin/sh
case "$1" in
--version) printf '%s\n' 'hc 0.1.17' ;;
*) printf '%s\n' "doctor ran hc $*" >&2; exit 9 ;;
esac
`

// listenCore serves a fake core socket for the doctor subprocess and returns
// its path.
func listenCore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "core.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return path
}

func TestDoctorReportsMissingHC(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "herdr-core", "language": "en", "welcome_complete": true}))
	code, _, out := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	if !strings.Contains(out, "hc: not installed") && !strings.Contains(out, config.Text("menu.not_installed", "hc")) {
		t.Fatalf("missing hc not reported: %s", out)
	}
	// Doctor repairs a herdr-core launcher without hc like any unavailable launcher.
	if cfg := readDoctorConfig(t, h); cfg.Launcher == "herdr-core" {
		t.Fatalf("launcher=%s", cfg.Launcher)
	}
}

func TestDoctorReportsHerdrCoreNotRunning(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("hc", fakeHC)
	missing := filepath.Join(t.TempDir(), "core.sock")
	h.setenv("HC_SOCKET", missing)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "herdr-core", "language": "en", "welcome_complete": true}))
	code, _, out := h.run("doctor")
	if code == 0 || !strings.Contains(out, "herdr-core is not running") || !strings.Contains(out, "no core listens at "+missing) || strings.Contains(out, "doctor ran hc") {
		t.Fatalf("%d %s", code, out)
	}
	if cfg := readDoctorConfig(t, h); cfg.Launcher != "herdr-core" {
		t.Fatalf("launcher=%s", cfg.Launcher)
	}
}

// With herdr-core configured, doctor names the execution agents (both task
// scales and the chat agent) that do not report through OSC 7501.
func TestDoctorListsHerdrCoreAgentsWithoutProgramStatus(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("hc", fakeHC)
	h.setenv("HC_SOCKET", listenCore(t))
	h.fakeCommand("cursor-agent", "")
	h.fakeCommand("opencode", "")
	payload := defaultPayload(map[string]any{"launcher": "herdr-core", "language": "en", "welcome_complete": true,
		"kanban_agents": map[string]any{"large": "claude", "small": "cursor"}, "chat_agent": "opencode"})
	h.writeConfig(payload)
	code, _, out := h.run("doctor")
	if code == 0 || !strings.Contains(out, "herdr-core cannot run them: cursor, opencode") {
		t.Fatalf("%d %s", code, out)
	}
	payload["kanban_agents"] = map[string]any{"large": "claude", "small": "claude"}
	payload["chat_agent"] = "claude"
	h.writeConfig(payload)
	if code, _, out = h.run("doctor"); code != 0 || !strings.Contains(out, "launcher=herdr-core opens a new herdr-core terminal") || !strings.Contains(out, "herdr-core: running") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestLauncherChoicesOfferHerdrCore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANDER_LANG", "en")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	session := &Session{Config: cfg, existing: cfg}

	t.Setenv("PATH", t.TempDir())
	if got := launcherValues(session.LauncherChoices()); slices.Contains(got, "herdr-core") {
		t.Fatalf("herdr-core offered without hc: %v", got)
	}
	cfg.Launcher = "herdr-core"
	choices := session.LauncherChoices()
	index := slices.Index(launcherValues(choices), "herdr-core")
	if index < 0 || choices[index].Label != config.Text("herdr_core.menu_choice")+config.Text("menu.not_currently_installed") {
		t.Fatalf("configured herdr-core: %+v", choices)
	}

	bin := t.TempDir()
	writeFakeExecutable(t, bin, "hc")
	t.Setenv("PATH", bin)
	cfg.Launcher = "auto"
	choices = session.LauncherChoices()
	index = slices.Index(launcherValues(choices), "herdr-core")
	if index < 0 || choices[index].Label != config.Text("herdr_core.menu_choice") {
		t.Fatalf("with hc: %+v", choices)
	}
}
