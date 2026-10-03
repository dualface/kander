//go:build unix

package menu

import (
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestDoctorLuvusLauncher(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("luvus", "")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "luvus", "language": "cn"}))
	code, _, out := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	for _, want := range []string{"luvus: " + h.fakeBin, "launcher=luvus 需要当前处于 luvus pane (LUVUS_ENV=1)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	h.setenv("LUVUS_ENV", "1")
	if code, _, out = h.run("doctor"); code == 0 || !strings.Contains(out, "LUVUS_PANE_ID 为空") {
		t.Fatalf("%d %s", code, out)
	}
	h.setenv("LUVUS_PANE_ID", "3")
	if code, _, out = h.run("doctor"); code != 0 || !strings.Contains(out, "launcher=luvus 会在 luvus 新标签中打开 pane") {
		t.Fatalf("%d %s", code, out)
	}
	if cfg := readDoctorConfig(t, h); cfg.Launcher != "luvus" {
		t.Fatalf("launcher=%s", cfg.Launcher)
	}
}

// Without luvus on PATH, doctor repairs a luvus launcher like an unavailable
// herdr; auto stays when luvus is the only terminal tool.
func TestDoctorRepairsUnavailableLuvus(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "luvus"}))
	if code, _, out := h.run("doctor"); code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	if cfg := readDoctorConfig(t, h); cfg.Launcher != "tmux-session" {
		t.Fatalf("launcher=%s", cfg.Launcher)
	}

	h = newHarness(t)
	h.installFake(false)
	h.fakeCommand("luvus", "")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "auto"}))
	code, _, out := h.run("doctor")
	if code != 0 || strings.Contains(out, "https://herdr.dev/install.sh") {
		t.Fatalf("%d %s", code, out)
	}
	if !strings.Contains(out, "处于 luvus 则新建 luvus tab") {
		t.Fatalf("auto hint does not mention luvus: %s", out)
	}
	if cfg := readDoctorConfig(t, h); cfg.Launcher != "auto" {
		t.Fatalf("launcher=%s", cfg.Launcher)
	}
}

func TestLauncherChoicesOfferLuvus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANDER_LANG", "en")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	session := &Session{Config: cfg, existing: cfg}

	t.Setenv("PATH", t.TempDir())
	if got := launcherValues(session.LauncherChoices()); slices.Contains(got, "luvus") {
		t.Fatalf("luvus offered without luvus: %v", got)
	}
	cfg.Launcher = "luvus"
	choices := session.LauncherChoices()
	index := slices.Index(launcherValues(choices), "luvus")
	if index < 0 || choices[index].Label != config.Text("luvus.menu_choice")+config.Text("menu.not_currently_installed") {
		t.Fatalf("configured luvus: %+v", choices)
	}

	bin := t.TempDir()
	writeFakeExecutable(t, bin, "luvus")
	writeFakeExecutable(t, bin, "tmux")
	t.Setenv("PATH", bin)
	cfg.Launcher = "auto"
	choices = session.LauncherChoices()
	got := launcherValues(choices)
	if !slices.Equal(got[:4], []string{"auto", "tmux", "tmux-session", "luvus"}) {
		t.Fatalf("with luvus: %v", got)
	}
	if choices[0].Label != "choose herdr, luvus, or tmux from the current environment" || choices[3].Label != config.Text("luvus.menu_choice") {
		t.Fatalf("labels: %+v", choices)
	}
}
