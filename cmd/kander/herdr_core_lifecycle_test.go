package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

const fakeHCEnv = "KANDER_FAKE_HC_DIR"

// serveFakeHC turns the test binary into an hc with one terminal, t8, whose
// agent reports idle. The terminal disappears when it gets /exit, as a --rm
// terminal does once its shell exits, or when it is killed.
func serveFakeHC() {
	dir := os.Getenv(fakeHCEnv)
	if dir == "" || len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-test.") {
		return
	}
	os.Exit(fakeHC(dir, os.Args[1:]))
}

func fakeHC(dir string, args []string) int {
	record, err := json.Marshal(args)
	if err != nil {
		return 90
	}
	log, err := os.OpenFile(filepath.Join(dir, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 91
	}
	_, err = log.Write(append(record, '\n'))
	if closeErr := log.Close(); err != nil || closeErr != nil {
		return 92
	}
	gone := filepath.Join(dir, "gone")
	_, err = os.Stat(gone)
	live := err != nil
	end := func() int {
		if os.WriteFile(gone, nil, 0o600) != nil {
			return 93
		}
		_, _ = os.Stdout.WriteString("{}")
		return 0
	}
	missing := func() int {
		_, _ = os.Stderr.WriteString(`{"error":{"kind":"not_found","message":"no terminal"}}`)
		return 3
	}
	session, _ := os.ReadFile(filepath.Join(dir, "session")) // absent until labelled
	row := map[string]any{
		"address": "t8",
		"state":   "live",
		"labels":  map[string]string{"kander.session": string(session)},
		"status":  map[string]string{"app": "claude-code", "state": "idle"},
	}
	rows := []any{}
	if live {
		rows = append(rows, row)
	}
	var reply any = map[string]any{}
	switch args[0] {
	case "run":
		reply = map[string]string{"address": "t8"}
	case "read":
		reply = map[string]string{"text": "PS C:\\> "}
	case "label":
		if value, ok := strings.CutPrefix(args[len(args)-1], "kander.session="); ok {
			if os.WriteFile(filepath.Join(dir, "session"), []byte(value), 0o600) != nil {
				return 94
			}
		}
	case "send":
		if args[len(args)-1] == "/exit" {
			return end()
		}
	case "kill", "rm":
		if !live {
			return missing()
		}
		return end()
	case "get":
		if !live {
			return missing()
		}
		reply = row
	case "ls":
		reply = map[string]any{"terminals": rows}
	case "connect":
		reply = map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"terminals": rows}}
	case "--version":
		_, _ = os.Stdout.WriteString("hc 0.1.21")
		return 0
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return 95
	}
	if _, err := os.Stdout.Write(append(data, '\n')); err != nil {
		return 96
	}
	return 0
}

// start, liveness and dismiss through a fake hc, on the platform the test runs
// on: windows-latest takes the native Windows branch of the definition.
func TestWindowsHerdrCoreLifecycle(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Chdir(t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	terminal.ReloadDefinitions()
	t.Cleanup(terminal.ReloadDefinitions)

	bin, state := t.TempDir(), t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hc, claude, body := "hc", "claude", "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		hc, claude, body = "hc.exe", "claude.cmd", "@exit /b 0\r\n"
	}
	copyExecutable(t, self, filepath.Join(bin, hc))
	if err := os.WriteFile(filepath.Join(bin, claude), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakeHCEnv, state)

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
	cfg.Launcher = "herdr-core"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	id := readyCard(t, root, "hc-lifecycle")
	runKander(t, "start", id).require(t, "start")
	if window := cardField(t, root, id, board.FieldWindow); window != "herdr-core:t8" {
		t.Fatalf("WINDOW=%q", window)
	}
	calls, err := os.ReadFile(filepath.Join(state, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "powershell.exe"
	}
	if !strings.Contains(string(calls), shell) {
		t.Fatalf("the container shell is not %s:\n%s", shell, calls)
	}

	liveness := func() string {
		result := runKander(t, "check", id)
		result.require(t, "check")
		return result.stdout + result.stderr
	}
	if out := liveness(); !strings.Contains(out, "alive") {
		t.Fatalf("the running agent is not alive:\n%s", out)
	}
	gone := filepath.Join(state, "gone")
	if err := os.WriteFile(gone, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if out := liveness(); strings.Contains(out, "alive") || !strings.Contains(out, "stopped") {
		t.Fatalf("a removed terminal is not stopped:\n%s", out)
	}

	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	runKander(t, "move", id, "archived", "--result", "cancelled", "--reason", "test", "--decision", "test").require(t, "archive")
	runKander(t, "dismiss", id).require(t, "dismiss")
	if _, err := os.Stat(gone); err != nil {
		t.Fatalf("dismiss did not close the terminal: %v", err)
	}
}

func copyExecutable(t *testing.T, from, to string) {
	t.Helper()
	source, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
}
