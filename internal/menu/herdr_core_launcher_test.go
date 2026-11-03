//go:build unix

package menu

import (
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// fakeHC answers --version and reports a running core unless HC_FAKE_DOWN is set.
const fakeHC = `#!/bin/sh
case "$1" in
--version) printf '%s\n' 'hc 0.1.17' ;;
server)
  if [ -n "${HC_FAKE_DOWN:-}" ]; then
    printf '%s\n' '{"error":{"kind":"unavailable","message":"the core is not running"}}' >&2
    exit 5
  fi
  printf '%s\n' '{"version":"0.1.17"}' ;;
esac
`

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
	h.setenv("HC_FAKE_DOWN", "1")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "herdr-core", "language": "en", "welcome_complete": true}))
	code, _, out := h.run("doctor")
	if code == 0 || !strings.Contains(out, "herdr-core is not running") || !strings.Contains(out, "the core is not running") {
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
