package terminal

import (
	"regexp"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestPaneFactsMatchAgent(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	reported := func(app string) PaneFacts { return PaneFacts{ProgramStatus: true, AgentApp: app} }
	for name, tc := range map[string]struct {
		facts PaneFacts
		agent string
		want  AgentMatch
	}{
		"agent name match":         {PaneFacts{Agent: "claude"}, "claude", AgentMatches},
		"agent name differs":       {PaneFacts{Agent: "codex"}, "claude", AgentDiffers},
		"no agent":                 {PaneFacts{}, "claude", AgentDiffers},
		"declared app":             {reported("claude-code"), "claude", AgentMatches},
		"other app":                {reported("codex"), "claude", AgentDiffers},
		"agent that never reports": {reported("cursor"), "cursor", AgentDiffers},
		"not reported yet":         {reported(""), "claude", AgentUndecided},
		"unknown agent":            {reported("claude-code"), "missing-agent", AgentUndecided},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.facts.MatchAgent(tc.agent); got != tc.want {
				t.Fatalf("MatchAgent=%v, want %v", got, tc.want)
			}
		})
	}
}

// A custom agent declares its own app and is matched by it, not by name.
func TestPaneFactsMatchAgentCustomDeclaration(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Agents = map[string]config.AgentDefinition{
		"claude-work": {Dialect: "claude", ProgramStatus: &config.ProgramStatus{Reports: true, App: "claude-work-app"}},
	}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	facts := PaneFacts{ProgramStatus: true, AgentApp: "claude-work-app"}
	if facts.MatchAgent("claude-work") != AgentMatches || facts.MatchAgent("claude") != AgentDiffers {
		t.Fatal("custom declaration not honored")
	}
}

func TestLabelName(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z0-9-]{1,23}-[0-9a-f]{8}$`)
	for label, prefix := range map[string]string{
		"kb-Add herdr-core launcher backed by hc status": "kb-add-herdr-core-launc",
		"kb-修复 启动":              "kb",
		"中文":                    "kander",
		"chat-20261103-142500":  "chat-20261103-142500",
		"--Lead__and  spaces--": "lead-and-spaces",
	} {
		name := LabelName(label)
		if !shape.MatchString(name) || len(name) > 32 || name[:len(name)-9] != prefix {
			t.Fatalf("LabelName(%q)=%q, want prefix %q", label, name, prefix)
		}
	}
}
