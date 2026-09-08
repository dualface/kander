package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/process"
)

// writeKimiSession lays out one kimi-code session the way the CLI does:
// <root>/sessions/<work-dir-key>/<session-id>/agents/main/wire.jsonl, where the transcript
// opens with metadata and configuration records before the first user turn.
func writeKimiSession(t *testing.T, root, workDirKey, sessionID string, prompt string, modified time.Time) {
	t.Helper()
	dir := filepath.Join(root, "sessions", workDirKey, sessionID, "agents", "main")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := `{"type":"metadata","protocol_version":"1.4","created_at":1788596622059}` + "\n" +
		`{"type":"config.update","profileName":"agent","systemPrompt":"You are Kimi Code CLI","time":1788596622060}` + "\n" +
		`{"type":"tools.set_active_tools","names":["Read","Grep"],"time":1788596622060}` + "\n" +
		`{"type":"config.update","modelAlias":"kimi-code/k3","thinkingEffort":"max","time":1788596622060}` + "\n"
	if prompt != "" {
		encoded, err := json.Marshal(prompt)
		if err != nil {
			t.Fatal(err)
		}
		lines += `{"type":"turn.prompt","input":[{"type":"text","text":` + string(encoded) + `}],"origin":{"kind":"user"},"time":1788596637082}` + "\n"
	}
	path := filepath.Join(dir, "wire.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func TestKimiSessionDiscoveryMatchesTheTaskPrompt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", root)
	prompt := launchPromptPrefixes("task-1")[0] + " full instructions are in the task file"
	other := launchPromptPrefixes("task-2")[0] + " another task"
	base := time.Now().Add(-time.Hour)

	writeKimiSession(t, root, "wd_repo_abc", "session_11111111-1111-4111-8111-111111111111", other, base)
	writeKimiSession(t, root, "wd_repo_abc", "session_22222222-2222-4222-8222-222222222222", prompt, base.Add(time.Minute))

	got, err := findKimiSession("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session_22222222-2222-4222-8222-222222222222" {
		t.Fatalf("session=%q", got)
	}
	// task-2 must not be matched by the task-1 prompt, and vice versa.
	if got, err := findKimiSession("task-2"); err != nil || got != "session_11111111-1111-4111-8111-111111111111" {
		t.Fatalf("session=%q err=%v", got, err)
	}
}

// The most recently touched transcript wins, the way a Codex rollout scan resolves ties.
func TestKimiSessionDiscoveryPrefersTheNewestTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", root)
	prompt := launchPromptPrefixes("task-1")[0] + " instructions"
	base := time.Now().Add(-time.Hour)

	writeKimiSession(t, root, "wd_one_aaa", "session_aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", prompt, base)
	writeKimiSession(t, root, "wd_two_bbb", "session_bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", prompt, base.Add(10*time.Minute))

	sessions, err := kimiSessionsForTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0] != "session_bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" {
		t.Fatalf("sessions=%v", sessions)
	}
}

func TestKimiSessionDiscoveryReportsMissingStoreAndNoMatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", filepath.Join(root, "absent"))
	if _, err := findKimiSession("task-1"); err == nil {
		t.Fatal("missing store accepted")
	}

	t.Setenv("KIMI_CODE_HOME", root)
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A session with no user turn cannot be attributed to any task.
	writeKimiSession(t, root, "wd_repo_abc", "session_cccccccc-cccc-4ccc-8ccc-cccccccccccc", "", time.Now())
	if _, err := findKimiSession("task-1"); err == nil {
		t.Fatal("unrelated session matched")
	}
}

// A discovered dialect must never receive a caller-minted reference at session creation.
func TestKimiAgentSessionStartsWithoutAReference(t *testing.T) {
	session, err := newAgentSession("kimi", &process.AgentProgram{})
	if err != nil {
		t.Fatal(err)
	}
	if session.Reference != "" {
		t.Fatalf("kimi must not mint a session id: %q", session.Reference)
	}
}

// codex/claude/grok/cursor take the opening instruction as a positional argument; kimi-code
// parses a bare argument as a subcommand, so its prompt has to be typed into the pane.
func TestStartArgumentsSplitPromptByDialect(t *testing.T) {
	args := []string{"--auto"}
	for _, launcher := range []string{"tmux", "tmux-session", "herdr"} {
		plan := LaunchPlan{Launcher: launcher}
		argv, typed, err := startArguments(plan, "claude", args, "INSTRUCTION")
		if err != nil || typed != "" || len(argv) != 2 || argv[1] != "INSTRUCTION" {
			t.Fatalf("claude/%s: argv=%v typed=%q err=%v", launcher, argv, typed, err)
		}
		argv, typed, err = startArguments(plan, "kimi", args, "INSTRUCTION")
		if err != nil || typed != "INSTRUCTION" || len(argv) != 1 {
			t.Fatalf("kimi/%s: argv=%v typed=%q err=%v", launcher, argv, typed, err)
		}
	}
}

// Starting kimi without a pane would leave an agent running that was never told what to do,
// so the launchers that spawn a bare process are refused instead.
func TestStartArgumentsRejectPanelessLaunchersForKimi(t *testing.T) {
	for _, launcher := range []string{"console", "foreground"} {
		plan := LaunchPlan{Launcher: launcher}
		if _, _, err := startArguments(plan, "kimi", []string{"--auto"}, "INSTRUCTION"); err == nil {
			t.Fatalf("%s accepted for kimi", launcher)
		}
		if _, typed, err := startArguments(plan, "codex", []string{}, "INSTRUCTION"); err != nil || typed != "" {
			t.Fatalf("codex/%s must keep working: typed=%q err=%v", launcher, typed, err)
		}
	}
}

func TestPromptTyperOnlyExistsForTypedPrompts(t *testing.T) {
	if promptTyper(LaunchPlan{Launcher: "tmux"}, "claude", "") != nil {
		t.Fatal("argument prompts must not be typed")
	}
	if promptTyper(LaunchPlan{Launcher: "tmux"}, "kimi", "INSTRUCTION") == nil {
		t.Fatal("typed prompt has no typer")
	}
}
