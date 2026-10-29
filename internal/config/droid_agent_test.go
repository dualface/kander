package config

import (
	"strings"
	"testing"
)

func TestDroidIsAnExecutionAndReviewAgent(t *testing.T) {
	setupHome(t)
	if !contains(ExecutionAgents, "droid") || !HasReviewTemplate(nil, "droid") {
		t.Fatal("droid missing")
	}
	if AgentExecutableName("droid") != "droid" || AgentDisplayName("droid") != "Droid" {
		t.Fatal(AgentExecutableName("droid"), AgentDisplayName("droid"))
	}
	spec, ok := RulesSpec("droid")
	if !ok || spec.Global != ".factory/AGENTS.md" || spec.Project != "AGENTS.md" || spec.Integration != "markdown-reference" {
		t.Fatalf("%+v %v", spec, ok)
	}
	d := AgentFor(nil, "droid")
	if d.ProcessName != "droid" || d.Session == nil || d.Session.Mode != "hook:droid-sessions" || d.Session.Discovery != nil {
		t.Fatalf("%+v session=%+v", d, d.Session)
	}
	if d.ExitCommand == nil || *d.ExitCommand != "/quit" {
		t.Fatalf("exit_command=%v", d.ExitCommand)
	}
	if d.Review == nil || d.Review.HomePolicy != "optional" || d.Review.HomeEnv != "" || d.Review.Stdin != "none" || d.Review.OutputName != "output.json" {
		t.Fatalf("review=%+v", d.Review)
	}
	out := d.Review.Output
	if out == nil || out.Parse != "json_field:result" || len(out.Success) != 3 {
		t.Fatalf("output=%+v", out)
	}
}

func TestDroidPaneDeliveryDeclaresReadyAndTrustBlock(t *testing.T) {
	delivery := AgentFor(nil, "droid").PromptDelivery
	if delivery == nil || delivery.Mode != "pane" || delivery.Ready == nil {
		t.Fatalf("delivery=%+v", delivery)
	}
	if delivery.Ready.Match != "? for help" || delivery.Ready.TimeoutMS <= 0 {
		t.Fatalf("ready=%+v", delivery.Ready)
	}
	// Accepting the trust dialog resets the session to Auto (Off), so a trust
	// prompt must fail the launch instead of being answered in the pane.
	if len(delivery.Blocked) != 1 || delivery.Blocked[0].Match != "Trust this folder?" || strings.TrimSpace(delivery.Blocked[0].Reason) == "" {
		t.Fatalf("blocked=%+v", delivery.Blocked)
	}
}

func TestDroidArgvCarriesNoModelOrEffort(t *testing.T) {
	d := AgentFor(nil, "droid")
	if got := ExpandAgentArgs(d.Args.Start, "ignored", "high", ""); strings.Join(got, " ") != "--auto high" {
		t.Fatalf("start=%q", got)
	}
	if got := ExpandAgentArgs(d.Args.Resume, "ignored", "high", "1a2b"); strings.Join(got, " ") != "--auto high --resume 1a2b" {
		t.Fatalf("resume=%q", got)
	}
	// Interactive droid has no model or effort flag, and review leaves effort to
	// the model's own default.
	for _, args := range [][]string{d.Args.Start, d.Args.Resume, d.Args.Review} {
		if strings.Contains(strings.Join(args, " "), "{effort}") {
			t.Fatalf("effort leaked into argv: %v", args)
		}
	}
	review := strings.Join(d.Args.Review, " ")
	for _, banned := range []string{"--auto", "--skip-permissions-unsafe", "--session-id", "--fork"} {
		if strings.Contains(review, banned) {
			t.Fatalf("review args must stay read-only and fresh: %v", d.Args.Review)
		}
	}
	if got := ExpandAgentArgs(d.Args.Review, "", "", ""); strings.Contains(strings.Join(got, " "), "--model") {
		t.Fatalf("empty review model must drop --model: %q", got)
	}
}

func TestDroidDefaultModelsStayEmpty(t *testing.T) {
	cfg := DefaultConfig()
	kanban := cfg.Models.Kanban["droid"]
	if kanban["large_model"] != "" || kanban["small_model"] != "" {
		t.Fatalf("kanban=%v", kanban)
	}
	if _, ok := kanban["large_effort"]; ok {
		t.Fatalf("droid must not gain effort keys: %v", kanban)
	}
	if cfg.Models.Review["droid"]["model"] != "" || cfg.Models.Chat["droid"]["model"] != "" {
		t.Fatalf("models=%v", cfg.Models)
	}
}
