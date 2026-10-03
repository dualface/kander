package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

// fakeLuvusScript answers the luvus CLI commands of the embedded definition.
// A split opens pane 7 in the --cwd directory, or in the anchor's directory
// when $FAKELUVUS_IGNORE_CWD is set, as an older CLI or server does.
const fakeLuvusScript = `#!/bin/sh
state="$FAKELUVUS_STATE"
printf '%s\n' "$*" >> "$state/calls.log"
case "$1 $2" in
  "pane split")
    cwd="$FAKELUVUS_ANCHOR_CWD"
    if [ -z "$FAKELUVUS_IGNORE_CWD" ]; then
      while [ $# -gt 0 ]; do [ "$1" = "--cwd" ] && cwd="$2"; shift; done
    fi
    printf '%s' "$cwd" > "$state/cwd"
    printf '{"id":"1","result":{"type":"pane","pane":"7"}}\n' ;;
  "agent get")
    printf '{"id":"1","result":{"type":"agent","pane":"7","agent":"zsh","status":"idle","session":null,"cwd":"%s"}}\n' "$(cat "$state/cwd")" ;;
  "pane read") printf '{"id":"1","result":{"type":"pane_read","pane":"7","text":"$ "}}\n' ;;
  "pane close") rm -f "$state/cwd"; printf '{"id":"1","result":{}}\n' ;;
  *) printf '{"id":"1","result":{}}\n' ;;
esac
`

// The built-in luvus launcher starts a card in a pane opened in the requested
// directory; a luvus that ignores --cwd rolls the card back to todo after
// closing the pane, before any command reaches it.
func TestLuvusLauncherStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake luvus is a POSIX shell script")
	}
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Chdir(t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	terminal.ReloadDefinitions()
	t.Cleanup(terminal.ReloadDefinitions)

	bin := t.TempDir()
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "luvus"), []byte(fakeLuvusScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKELUVUS_STATE", state)
	t.Setenv("FAKELUVUS_ANCHOR_CWD", home)
	t.Setenv("LUVUS_ENV", "1")
	t.Setenv("LUVUS_PANE_ID", "3")
	for _, name := range []string{"TMUX", "TMUX_PANE", "HERDR_ENV"} {
		t.Setenv(name, "")
	}

	root := t.TempDir()
	for _, name := range board.States {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	t.Setenv(config.EnvConfig, filepath.Join(home, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language, cfg.AgentLanguage = "en", "en"
	cfg.KanbanAgent = "claude"
	cfg.KanbanAgents = map[string]string{"large": "claude", "small": "claude"}
	cfg.Launcher = "auto"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	calls := func() string {
		data, _ := os.ReadFile(filepath.Join(state, "calls.log"))
		_ = os.Remove(filepath.Join(state, "calls.log"))
		return string(data)
	}

	t.Setenv("FAKELUVUS_IGNORE_CWD", "1")
	rejected := readyCard(t, root, "luvus-old")
	result := runKander(t, "start", rejected)
	if result.code == 0 {
		t.Fatalf("start accepted a pane in the wrong directory:\n%s", result.stdout)
	}
	if !strings.Contains(result.stdout+result.stderr, "restart the luvus server") {
		t.Fatalf("start does not ask to upgrade luvus:\n%s%s", result.stdout, result.stderr)
	}
	if snapshot, err := board.ReadSnapshot(root, rejected); err != nil || snapshot.Entry.State != "todo" {
		t.Fatalf("card entry=%+v err=%v", snapshot.Entry, err)
	}
	log := calls()
	if !strings.Contains(log, "pane close 7") || strings.Contains(log, "pane run") || strings.Contains(log, "pane move") {
		t.Fatalf("luvus calls:\n%s", log)
	}

	t.Setenv("FAKELUVUS_IGNORE_CWD", "")
	started := readyCard(t, root, "luvus-new")
	runKander(t, "start", started).require(t, "start")
	if window := cardField(t, root, started, board.FieldWindow); window != "luvus:7:7" {
		t.Fatalf("WINDOW=%q", window)
	}
	log = calls()
	for _, want := range []string{"--no-focus --cwd ", "pane move 7 --new-tab", "pane focus 3", "pane run 7 "} {
		if !strings.Contains(log, want) {
			t.Fatalf("luvus calls lack %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "pane close") {
		t.Fatalf("the started pane was closed:\n%s", log)
	}
}
