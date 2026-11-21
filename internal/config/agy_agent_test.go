package config

import (
	"strings"
	"testing"
)

func TestAgyIsAnExecutionAndReviewAgent(t *testing.T) {
	setupHome(t)
	if !contains(ExecutionAgents, "agy") || !HasReviewTemplate(nil, "agy") {
		t.Fatal("agy missing")
	}
	if ExecutionAgents[len(ExecutionAgents)-1] != "agy" {
		t.Fatalf("agy must be listed last: %v", ExecutionAgents)
	}
	if AgentExecutableName("agy") != "agy" || AgentDisplayName("agy") != "Antigravity CLI" {
		t.Fatal(AgentExecutableName("agy"), AgentDisplayName("agy"))
	}
	spec, ok := RulesSpec("agy")
	if !ok || spec.Global != ".gemini/AGENTS.md" || spec.Project != "AGENTS.md" || spec.Integration != "markdown-reference" {
		t.Fatalf("%+v %v", spec, ok)
	}
	d := AgentFor(nil, "agy")
	if d.ProcessName != "agy" || d.Session == nil || d.Session.Mode != "hook:agy-transcript" || d.Session.Discovery != nil {
		t.Fatalf("%+v session=%+v", d, d.Session)
	}
	if d.ExitCommand == nil || *d.ExitCommand != "/exit" {
		t.Fatalf("exit_command=%v", d.ExitCommand)
	}
	if d.PromptDelivery == nil || d.PromptDelivery.Mode != "argv" {
		t.Fatalf("prompt_delivery=%+v", d.PromptDelivery)
	}
	if d.Review == nil || d.Review.HomePolicy != "optional" || d.Review.HomeEnv != "" || d.Review.Stdin != "none" || d.Review.OutputName != "output.json" || !d.Review.SnapshotSpec {
		t.Fatalf("review=%+v", d.Review)
	}
	if !EmbeddedReviewLastMessageReport("agy") {
		t.Fatal("agy reports its final message")
	}
}

func TestAgyDefaultModelsAndEfforts(t *testing.T) {
	cfg := DefaultConfig()
	kanban := cfg.Models.Kanban["agy"]
	if kanban["large_model"] != "" || kanban["small_model"] != "" || kanban["model"] != "" ||
		kanban["large_effort"] != "high" || kanban["small_effort"] != "medium" {
		t.Fatalf("kanban=%v", kanban)
	}
	for name, models := range map[string]map[string]string{"review": cfg.Models.Review["agy"], "chat": cfg.Models.Chat["agy"]} {
		if models["model"] != "" || models["effort"] != "high" {
			t.Fatalf("%s=%v", name, models)
		}
	}
}

func TestAgyConfigDirIsRejected(t *testing.T) {
	_, err := validateAgentDefinitions(map[string]any{"agy": map[string]any{"config_dir": "/tmp/agy"}})
	if err == nil || !strings.Contains(err.Error(), "agy") {
		t.Fatalf("a fixed config directory must be rejected: %v", err)
	}
}

func TestAgyArgvExpansion(t *testing.T) {
	d := AgentFor(nil, "agy")
	if got := strings.Join(ExpandAgentArgs(d.Args.Start, "m1", "high", ""), " "); got != "--model m1 --effort high --prompt-interactive" {
		t.Fatalf("start=%q", got)
	}
	if got := strings.Join(ExpandAgentArgs(d.Args.Resume, "m1", "high", "conv-1"), " "); got != "--conversation conv-1 --model m1 --effort high --prompt-interactive" {
		t.Fatalf("resume=%q", got)
	}
	// An empty model drops its flag, so agy keeps its own default model.
	if got := strings.Join(ExpandAgentArgs(d.Args.Start, "", "high", ""), " "); got != "--effort high --prompt-interactive" {
		t.Fatalf("start without a model=%q", got)
	}
}

func TestReviewArgvPlaceholdersIncludeRulesDirOnlyForArgv(t *testing.T) {
	if !contains(ReviewArgvPlaceholders(nil), "rules_dir") {
		t.Fatal("rules_dir must be an args.review placeholder")
	}
	if contains(ReviewEnvPlaceholders(), "rules_dir") || contains(ReviewPromptFilePlaceholders(), "rules_dir") {
		t.Fatal("rules_dir must not reach review.env or review.prompt_files")
	}
}
