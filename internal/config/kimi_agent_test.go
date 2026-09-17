package config

import (
	"strings"
	"testing"
)

func TestKimiIsAnExecutionAndReviewAgent(t *testing.T) {
	setupHome(t)
	if !contains(ExecutionAgents, "kimi") || !HasReviewTemplate(nil, "kimi") {
		t.Fatal("kimi missing")
	}
	if AgentExecutableName("kimi") != "kimi" {
		t.Fatal(AgentExecutableName("kimi"))
	}
	spec, ok := RulesSpec("kimi")
	if !ok || spec.Global != ".kimi-code/AGENTS.md" || spec.Project != "AGENTS.md" || spec.Integration != "markdown-reference" {
		t.Fatalf("%+v %v", spec, ok)
	}
	d := AgentFor(nil, "kimi")
	if d.ProcessName != "kimi" || d.PromptDelivery == nil || d.PromptDelivery.Mode != "pane" {
		t.Fatalf("%+v", d)
	}
	if d.Session == nil || d.Session.Mode != "discovered" || d.Session.Discovery == nil {
		t.Fatalf("session=%+v", d.Session)
	}
	disc := d.Session.Discovery
	if strings.Join(disc.Args, " ") != "session list --json --all" ||
		disc.Format != "json" || disc.IDField != "id" ||
		len(disc.Match) != 1 || disc.Match[0].Field != "workDir" || disc.Match[0].Equals != "{cwd}" {
		t.Fatalf("discovery=%+v", disc)
	}
	if d.ExitCommand == nil || *d.ExitCommand != "/exit" {
		t.Fatalf("exit_command=%v", d.ExitCommand)
	}
	if d.Args == nil || len(d.Args.Review) == 0 || d.Review == nil {
		t.Fatalf("review definition=%+v", d)
	}
	if d.Review.HomePolicy != "optional" || d.Review.HomeEnv != "KIMI_CODE_HOME" ||
		d.Review.Stdin != "none" || d.Review.OutputName != "output.jsonl" {
		t.Fatalf("review=%+v", d.Review)
	}
	joined := strings.Join(d.Args.Review, " ")
	for _, want := range []string{"--prompt", "{instruction}", "--output-format stream-json", "--agent-file {prompt_file:kimi-reviewer}", "--add-dir {runtime}"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("review args missing %q: %v", want, d.Args.Review)
		}
	}
	for _, banned := range []string{"--auto", "--yolo", "--continue"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("review args must not widen permissions or resume: %v", d.Args.Review)
		}
	}
	if len(d.Review.PromptFiles) != 1 || d.Review.PromptFiles[0].Name != "kimi-reviewer" || d.Review.PromptFiles[0].Path != "kimi-reviewer.md" {
		t.Fatalf("prompt_files=%+v", d.Review.PromptFiles)
	}
	template := d.Review.PromptFiles[0].Template
	for _, want := range []string{"tools: [Read, Grep, Glob]", "disallowedTools", "Bash", "Write", "Edit", "Agent", "AgentSwarm"} {
		if !strings.Contains(template, want) {
			t.Fatalf("reviewer agent file missing %q", want)
		}
	}
	out := d.Review.Output
	if out == nil || out.Format != "ndjson" || out.Parse != "json_field:content" || out.Join == nil || *out.Join != "\n" {
		t.Fatalf("output=%+v", out)
	}
	if len(out.Select) != 2 || out.Select[0].JSONField != "role" || string(out.Select[0].Equals) != `"assistant"` ||
		out.Select[1].JSONField != "tool_calls" || out.Select[1].Absent == nil || !*out.Select[1].Absent {
		t.Fatalf("select=%+v", out.Select)
	}
	if len(out.Success) != 1 || out.Success[0].JSONField != "type" || string(out.Success[0].Equals) != `"session.resume_hint"` {
		t.Fatalf("success=%+v", out.Success)
	}
}

func TestKimiPaneDeliveryDeclaresReadyAndTrustBlock(t *testing.T) {
	d := AgentFor(nil, "kimi")
	delivery := d.PromptDelivery
	if delivery == nil || delivery.Mode != "pane" || delivery.Ready == nil {
		t.Fatalf("delivery=%+v", delivery)
	}
	if delivery.Ready.Match != "context:" || delivery.Ready.TimeoutMS <= 0 {
		t.Fatalf("ready=%+v", delivery.Ready)
	}
	if len(delivery.Blocked) != 1 || delivery.Blocked[0].Match != "Trust this folder?" || strings.TrimSpace(delivery.Blocked[0].Reason) == "" {
		t.Fatalf("blocked=%+v", delivery.Blocked)
	}
}

func TestKimiStartArgvOmitsEmptyModelAndNeverCarriesEffort(t *testing.T) {
	d := AgentFor(nil, "kimi")
	if got := ExpandAgentArgs(d.Args.Start, "k2", "", ""); strings.Join(got, " ") != "--auto --model k2" {
		t.Fatalf("start=%q", got)
	}
	if got := ExpandAgentArgs(d.Args.Resume, "k2", "", "ses_1"); strings.Join(got, " ") != "--auto --model k2 --session ses_1" {
		t.Fatalf("resume=%q", got)
	}
	if got := ExpandAgentArgs(d.Args.Start, "", "", ""); strings.Join(got, " ") != "--auto" {
		t.Fatalf("empty start=%q", got)
	}
	// Kimi has no effort flag: the placeholder must never reach start or resume argv.
	for _, args := range [][]string{d.Args.Start, d.Args.Resume, d.Args.Review} {
		if strings.Contains(strings.Join(args, " "), "{effort}") {
			t.Fatalf("effort leaked into argv: %v", args)
		}
	}
}

func TestKimiDefaultModelsStayEmpty(t *testing.T) {
	cfg := DefaultConfig()
	kanban := cfg.Models.Kanban["kimi"]
	if kanban["large_model"] != "" || kanban["small_model"] != "" {
		t.Fatalf("kanban=%v", kanban)
	}
	if _, ok := kanban["large_effort"]; ok {
		t.Fatalf("kimi must not gain effort keys: %v", kanban)
	}
	if cfg.Models.Review["kimi"]["model"] != "" || cfg.Models.Chat["kimi"]["model"] != "" {
		t.Fatalf("models=%v", cfg.Models)
	}
}
