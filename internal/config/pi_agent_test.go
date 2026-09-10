package config

import (
	"strings"
	"testing"
)

func TestPiIsAnExecutionAndReviewAgent(t *testing.T) {
	setupHome(t)
	if !contains(ExecutionAgents, "pi") || !HasReviewTemplate(nil, "pi") {
		t.Fatal("pi missing")
	}
	if AgentExecutableName("pi") != "pi" {
		t.Fatal(AgentExecutableName("pi"))
	}
	spec, ok := RulesSpec("pi")
	if !ok || spec.Global != ".pi/agent/AGENTS.md" || spec.Project != "AGENTS.md" || spec.Integration != "markdown-reference" {
		t.Fatalf("%+v %v", spec, ok)
	}
	d := AgentFor(nil, "pi")
	if d.ProcessName != "pi" || d.Session.Mode != "generated" || d.PromptDelivery.Mode != "argv" {
		t.Fatalf("%+v", d)
	}
	if d.ExitCommand == nil || *d.ExitCommand != "/quit" {
		t.Fatalf("exit_command=%v", d.ExitCommand)
	}
	if d.Args == nil || len(d.Args.Review) == 0 || d.Review == nil {
		t.Fatalf("review definition=%+v", d)
	}
	if d.Review.HomePolicy != "optional" {
		t.Fatalf("review home policy=%q", d.Review.HomePolicy)
	}
	joined := strings.Join(d.Args.Review, " ")
	for _, want := range []string{"--print", "--no-session", "--tools", "read,bash,grep,find,ls", "--append-system-prompt", "kander-findings"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("review args missing %q: %v", want, d.Args.Review)
		}
	}
}
