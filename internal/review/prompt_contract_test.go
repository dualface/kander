package review

import (
	"os"
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
		role:           "PMQA",
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
	if !strings.Contains(codex, "/runtime/review-contract.md") || strings.Contains(codex, lastMessageOutputContract) {
		t.Fatal("bootstrap must point at the contract without inlining it")
	}
	codexContract := buildReviewContract(promptContractContext("codex"))
	if strings.Contains(codexContract, lastMessageOutputContract) {
		t.Fatal("codex contract must not include last-message output contract")
	}
	for _, agent := range []string{"claude", "cursor", "grok"} {
		ctx := promptContractContext(agent)
		if got := buildPrompt(ctx, evidence, task); got != codex {
			t.Fatalf("%s bootstrap drifted from the shared prompt", agent)
		}
		if got := buildReviewContract(ctx); !strings.Contains(got, lastMessageOutputContract) {
			t.Fatalf("%s contract is missing last-message output rules", agent)
		}
	}
}

func TestBuildPromptLastMessageOutputContractIncremental(t *testing.T) {
	ctx := promptContractContext("codex")
	ctx.reviewed = "cccccccccccccccccccccccccccccccccccccccc"
	evidence := "/runtime/evidence.txt"
	task := "Authoritative task goal: fix the report."
	bootstrap := buildPrompt(ctx, evidence, task)
	contract := buildReviewContract(ctx)
	if strings.Contains(bootstrap, "incremental re-review") || !strings.Contains(contract, "incremental re-review") {
		t.Fatal("incremental rules must live only in the review contract")
	}
	for _, agent := range []string{"claude", "cursor", "grok"} {
		agentCtx := promptContractContext(agent)
		agentCtx.reviewed = ctx.reviewed
		if got := buildPrompt(agentCtx, evidence, task); got != bootstrap {
			t.Fatalf("%s incremental bootstrap drifted from the shared prompt", agent)
		}
		if got := buildReviewContract(agentCtx); !strings.Contains(got, lastMessageOutputContract) {
			t.Fatalf("%s incremental contract is missing output rules", agent)
		}
	}
}

func TestReviewPromptResourcesRenderCompletely(t *testing.T) {
	for _, role := range []string{"PMQA", "Security", "PM", "QA", "CSA", "Hacker"} {
		for _, reviewed := range []string{"", "cccccccccccccccccccccccccccccccccccccccc"} {
			ctx := promptContractContext("claude")
			ctx.role = role
			ctx.reviewed = reviewed
			rendered := map[string]string{
				"bootstrap": buildPrompt(ctx, "/runtime/evidence.txt", "Authoritative task goal: verify prompts."),
				"contract":  buildReviewContract(ctx),
			}
			for name, text := range rendered {
				if strings.Contains(text, "{{") || strings.Contains(text, "}}") {
					t.Fatalf("%s/%s left an unrendered template action:\n%s", role, name, text)
				}
			}
		}
	}
}

func TestReviewSizeRuleMatchesReleasedRules(t *testing.T) {
	released, err := os.ReadFile(filepath.Join("..", "..", "rules", "KANDER-REVIEW-RULES.md"))
	if err != nil {
		t.Fatal(err)
	}
	promptText := strings.ToLower(roleRules["QA"])
	releasedText := strings.ToLower(string(released))
	for _, phrase := range []string{
		"file added this round exceeds 1000",
		"touched file that was at or under 1000 lines at the base exceeds 1000 now",
		"a file already above 1000 lines at the base is not measured",
	} {
		if !strings.Contains(promptText, phrase) || !strings.Contains(releasedText, phrase) {
			t.Fatalf("review size policy drifted at %q", phrase)
		}
	}
	if strings.Contains(promptText, "increases the final physical-line count") {
		t.Fatal("review prompt retained the obsolete already-oversized-file rule")
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
	settings, err := agentSettingsFor(agent, "PMQA", "large")
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
