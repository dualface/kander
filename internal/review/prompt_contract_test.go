package review

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func promptContractContext(agent string) reviewContext {
	return reviewContext{
		agent:          agent,
		role:           "QA",
		root:           "/worktree",
		base:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		commit:         "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		reviewContext:  emptyReviewContext,
		reportLanguage: "zh-CN",
		settings:       agentSettings{inspectionRules: "Prefer tests."},
	}
}

func TestLastMessageOutputContractText(t *testing.T) {
	for _, phrase := range []string{
		"final message is the complete report",
		"kander-findings",
		"Write that one message and stop",
		"Delete the task file before writing the final message",
		"do not send any follow-up",
	} {
		if !strings.Contains(lastMessageOutputContract, phrase) {
			t.Fatalf("output contract missing %q: %q", phrase, lastMessageOutputContract)
		}
	}
}

func TestBuildPromptLastMessageOutputContract(t *testing.T) {
	evidence := "/runtime/evidence.txt"
	task := "Authoritative task goal: fix the report."
	codex := buildPrompt(promptContractContext("codex"), evidence, task)
	if strings.Contains(codex, lastMessageOutputContract) {
		t.Fatal("codex prompt must not include last-message output contract")
	}
	want := codex + "\n" + lastMessageOutputContract + "\n"
	for _, agent := range []string{"claude", "cursor", "grok"} {
		got := buildPrompt(promptContractContext(agent), evidence, task)
		if got != want {
			t.Fatalf("%s prompt remainder drifted from the shared prompt", agent)
		}
	}
}

func TestBuildPromptLastMessageOutputContractIncremental(t *testing.T) {
	ctx := promptContractContext("codex")
	ctx.reviewed = "cccccccccccccccccccccccccccccccccccccccc"
	evidence := "/runtime/evidence.txt"
	task := "Authoritative task goal: fix the report."
	codex := buildPrompt(ctx, evidence, task)
	if !strings.Contains(codex, "incremental re-review") {
		t.Fatal("expected incremental prompt body")
	}
	if strings.Contains(codex, lastMessageOutputContract) {
		t.Fatal("codex incremental prompt must not include last-message output contract")
	}
	want := codex + "\n" + lastMessageOutputContract + "\n"
	for _, agent := range []string{"claude", "cursor", "grok"} {
		agentCtx := promptContractContext(agent)
		agentCtx.reviewed = ctx.reviewed
		got := buildPrompt(agentCtx, evidence, task)
		if got != want {
			t.Fatalf("%s incremental prompt remainder drifted from the shared prompt", agent)
		}
	}
}

func TestReviewerArgumentsClaudeAppendsSystemPrompt(t *testing.T) {
	inv := mustReviewerArgs(t, "claude")
	assertArg(t, inv.Argv, "--append-system-prompt", lastMessageOutputContract)
	assertArg(t, inv.Argv, "--permission-mode", "bypassPermissions")
	assertArg(t, inv.Argv, "--disallowedTools", "Edit,Write")
	if !slices.Contains(inv.Argv, "--print") {
		t.Fatalf("claude argv lost --print: %v", inv.Argv)
	}
}

func TestReviewerArgumentsNonClaudeOmitSystemPrompt(t *testing.T) {
	codex := mustReviewerArgs(t, "codex")
	if slices.Contains(codex.Argv, "--append-system-prompt") {
		t.Fatalf("codex argv unexpectedly has --append-system-prompt: %v", codex.Argv)
	}
	if !slices.Contains(codex.Argv, "--output-last-message") {
		t.Fatalf("codex argv lost --output-last-message: %v", codex.Argv)
	}
	cursor := mustReviewerArgs(t, "cursor")
	if slices.Contains(cursor.Argv, "--append-system-prompt") {
		t.Fatalf("cursor argv unexpectedly has --append-system-prompt: %v", cursor.Argv)
	}
	if !slices.Contains(cursor.Argv, "--print") || !slices.Contains(cursor.Argv, "--trust") {
		t.Fatalf("cursor argv lost isolation flags: %v", cursor.Argv)
	}
	grok := mustReviewerArgs(t, "grok")
	if slices.Contains(grok.Argv, "--append-system-prompt") {
		t.Fatalf("grok argv unexpectedly has --append-system-prompt: %v", grok.Argv)
	}
	if !slices.Contains(grok.Argv, "--prompt-file") {
		t.Fatalf("grok argv lost --prompt-file: %v", grok.Argv)
	}
}

func mustReviewerArgs(t *testing.T, agent string) process.ProcessInvocation {
	t.Helper()
	runtime := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "missing.json"))
	settings, err := agentSettingsFor(agent, "PM")
	if err != nil {
		t.Fatal(err)
	}
	settings.effort = "high"
	settings.reviewHome = t.TempDir()
	settings.model = "test-model"
	ctx := reviewContext{
		agent:    agent,
		root:     t.TempDir(),
		program:  process.AgentProgram{Path: agent},
		settings: settings,
	}
	inv, _, err := reviewerArguments(ctx, runtime, runtime+"/output", runtime+"/prompt.txt")
	if err != nil {
		t.Fatal(err)
	}
	return inv
}
