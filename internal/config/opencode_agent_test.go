package config

import (
	"strings"
	"testing"
)

func TestOpenCodeIsAnExecutionAndReviewAgent(t *testing.T) {
	setupHome(t)
	if !contains(ExecutionAgents, "opencode") || !HasReviewTemplate(nil, "opencode") {
		t.Fatal("opencode missing")
	}
	if AgentExecutableName("opencode") != "opencode" {
		t.Fatal(AgentExecutableName("opencode"))
	}
	spec, ok := RulesSpec("opencode")
	if !ok || spec.Global != ".config/opencode/AGENTS.md" || spec.Project != "AGENTS.md" || spec.Integration != "markdown-reference" {
		t.Fatalf("%+v %v", spec, ok)
	}
	d := AgentFor(nil, "opencode")
	if d.ProcessName != "opencode" || d.PromptDelivery.Mode != "argv" {
		t.Fatalf("%+v", d)
	}
	if d.Session == nil || d.Session.Mode != "discovered" || d.Session.Discovery == nil {
		t.Fatalf("session=%+v", d.Session)
	}
	disc := d.Session.Discovery
	if len(disc.Args) == 0 || strings.Join(disc.Args, " ") != "session list --format json" ||
		disc.Format != "json" || disc.IDField != "id" ||
		len(disc.Match) != 1 || disc.Match[0].Field != "directory" || disc.Match[0].Equals != "{cwd}" {
		t.Fatalf("discovery=%+v", disc)
	}
	if d.ExitCommand == nil || *d.ExitCommand != "/exit" {
		t.Fatalf("exit_command=%v", d.ExitCommand)
	}
	if d.Args == nil || len(d.Args.Review) == 0 || d.Review == nil {
		t.Fatalf("review definition=%+v", d)
	}
	if d.Review.HomePolicy != "optional" || d.Review.Stdin != "none" || d.Review.OutputName != "output.jsonl" {
		t.Fatalf("review=%+v", d.Review)
	}
	joined := strings.Join(d.Args.Review, " ")
	for _, want := range []string{"run", "--pure", "--variant", "--format json", "{instruction}", "kander-findings"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("review args missing %q: %v", want, d.Args.Review)
		}
	}
	if strings.Contains(joined, "--permission-mode") || strings.Contains(joined, "--auto") {
		t.Fatalf("review args must not auto-approve: %v", d.Args.Review)
	}
	permission := d.Review.Env["OPENCODE_PERMISSION"]
	if permission == "" || !strings.Contains(permission, `{{"*":"deny"`) {
		t.Fatalf("OPENCODE_PERMISSION=%q", permission)
	}
	if d.Review.Output == nil || d.Review.Output.Format != "ndjson" || d.Review.Output.Parse != "json_field:part.text" {
		t.Fatalf("output=%+v", d.Review.Output)
	}
}

func TestOpenCodeStartArgvKeepsTopLevelFlags(t *testing.T) {
	d := AgentFor(nil, "opencode")
	if got := ExpandAgentArgs(d.Args.Start, "provider/model", "high", ""); strings.Join(got, " ") != "--model provider/model --auto --prompt" {
		t.Fatalf("start=%q", got)
	}
	if got := ExpandAgentArgs(d.Args.Resume, "provider/model", "high", "ses_1"); strings.Join(got, " ") != "--model provider/model --session ses_1 --auto --prompt" {
		t.Fatalf("resume=%q", got)
	}
	if got := ExpandAgentArgs(d.Args.Start, "", "", ""); strings.Join(got, " ") != "--auto --prompt" {
		t.Fatalf("empty start=%q", got)
	}
	// The TUI has no --variant: effort must never reach start or resume argv.
	for _, args := range [][]string{d.Args.Start, d.Args.Resume} {
		if strings.Contains(strings.Join(args, " "), "{effort}") || strings.Contains(strings.Join(args, " "), "variant") {
			t.Fatalf("effort leaked into interactive argv: %v", args)
		}
	}
}

func TestOpenCodeDefaultModels(t *testing.T) {
	cfg := DefaultConfig()
	for _, section := range []map[string]string{cfg.Models.Kanban["opencode"], cfg.Models.Review["opencode"], cfg.Models.Chat["opencode"]} {
		if section["model"] != "opencode-go/deepseek-v4.1-flash" &&
			section["large_model"] != "opencode-go/deepseek-v4.1-flash" {
			t.Fatalf("missing default model: %v", section)
		}
	}
	if cfg.Models.Kanban["opencode"]["large_model"] != "opencode-go/deepseek-v4.1-flash" ||
		cfg.Models.Kanban["opencode"]["small_model"] != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("kanban=%v", cfg.Models.Kanban["opencode"])
	}
	if cfg.Models.Review["opencode"]["effort"] == "" || cfg.Models.Chat["opencode"]["effort"] == "" {
		t.Fatalf("effort keys missing: %v", cfg.Models)
	}
}
