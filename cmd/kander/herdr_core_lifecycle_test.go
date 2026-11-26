package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

const (
	fakeHCEnv      = "KANDER_FAKE_HC_DIR"
	fakeHCAgentEnv = "KANDER_FAKE_HC_AGENT"
)

// serveFakeHC turns the test binary into an hc with one terminal, t8, whose
// agent reports idle once the line that starts it was sent there; before that
// the terminal has reported nothing. The terminal disappears when it receives
// /exit, as a --rm terminal does once its shell exits, or when it is killed.
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
	started := filepath.Join(dir, "started")
	row := map[string]any{
		"address": "t8",
		"state":   "live",
		"labels":  map[string]string{"kander.session": string(session)},
		"status":  nil,
	}
	if _, err := os.Stat(started); err == nil {
		row["status"] = map[string]string{"app": "claude-code", "state": "idle"}
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
		if len(args) != 4 || args[2] != "t8" {
			return missing()
		}
		text, agent := args[3], os.Getenv(fakeHCAgentEnv)
		if text == "/exit" {
			return end()
		}
		if agent != "" && strings.Contains(text, agent) {
			if os.WriteFile(started, []byte(text), 0o600) != nil {
				return 97
			}
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
	// Only the embedded definitions: on Windows HOME does not move the share
	// directory, and a user's definition could name the real hc.
	savedDirs := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir { return nil }
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = savedDirs
		terminal.ReloadDefinitions()
	})

	bin, state := t.TempDir(), t.TempDir()
	hc, claude, body := "hc", "claude", "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		hc, claude, body = "hc.exe", "claude.cmd", "@exit /b 0\r\n"
	}
	terminaltest.CopyExecutable(t, filepath.Join(bin, hc))
	agent := filepath.Join(bin, claude)
	if err := os.WriteFile(agent, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// Nothing of the host is reachable, and the agent is named by path because
	// Windows prefers any claude.exe on PATH over a claude.cmd before it.
	t.Setenv("PATH", bin)
	t.Setenv(fakeHCEnv, state)
	t.Setenv(fakeHCAgentEnv, agent)

	t.Setenv(config.EnvConfig, filepath.Join(home, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language, cfg.AgentLanguage = "en", "en"
	cfg.KanbanAgent = "claude"
	cfg.KanbanAgents = map[string]string{"large": "claude", "small": "claude"}
	cfg.Launcher = "herdr-core"
	cfg.Agents = map[string]config.AgentDefinition{"claude": {Path: agent}}
	service := commandService(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	id := readyCard(t, "hc-lifecycle")
	runKander(t, "start", id).require(t, "start")
	if window := cardField(t, id, board.FieldWindow); window != "herdr-core:t8" {
		t.Fatalf("WINDOW=%q", window)
	}
	line, err := os.ReadFile(filepath.Join(state, "started"))
	if err != nil {
		t.Fatalf("the line that starts the agent was not sent to t8: %v", err)
	}
	calls, err := os.ReadFile(filepath.Join(state, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "powershell.exe"
		if !strings.Contains(string(line), "$env:KANDER_EXECUTION_ID=") {
			t.Fatal("PowerShell line did not pass the distributed execution identity")
		}
		clientHome := "$env:KANDER_CLIENT_HOME=" + terminal.PowerShellQuote(service.Home) + "; "
		if !strings.Contains(string(line), clientHome) {
			t.Fatal("PowerShell line did not pass the service fixture's client home")
		}
	}
	if !strings.Contains(string(calls), shell) {
		t.Fatalf("the container shell is not %s:\n%s", shell, calls)
	}

	requireLiveness := func(status string) {
		t.Helper()
		waitCommandFact(t, func() bool {
			task, err := service.Project.Show(t.Context(), id)
			return err == nil && task.Claim != nil && task.Claim.Observation != nil &&
				task.Claim.Observation.Status == status
		})
		out := runKander(t, "check", id).require(t, "check")
		if !strings.Contains(out.stdout, "status="+status) {
			t.Fatalf("check did not report %q:\n%s", status, out.stdout)
		}
	}
	requireLiveness("alive")
	gone := filepath.Join(state, "gone")
	if err := os.WriteFile(gone, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	requireLiveness("stopped")

	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	runKander(t, "move", id, "archived", "--result", "cancelled", "--reason", "test", "--decision", "test").require(t, "archive")
	runKander(t, "dismiss", id).require(t, "dismiss")
	if _, err := os.Stat(gone); err != nil {
		t.Fatalf("dismiss did not close the terminal: %v", err)
	}
}
