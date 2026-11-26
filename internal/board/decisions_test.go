package board

import (
	"strings"
	"testing"
)

func TestDecisionModeHeaderAndLegacyDefault(t *testing.T) {
	for _, text := range []string{
		renderContract("New card", "chore", "en", "Example", false),
		renderImportedContract(ImportRequest{Title: "Imported card", Kind: "chore", Language: "en"}, "small"),
	} {
		if mode := MetadataFrom(text, FieldDecisionMode); mode != string(DecisionModeLeader) {
			t.Fatalf("template mode = %q", mode)
		}
	}
	text := "# Historic card\n\n- SIZE: small\n\n## DISCUSSION\n\n- DECISION_MODE: assignee\n"
	if mode := DecisionModeOf(text); mode != DecisionModeLeader {
		t.Fatalf("legacy mode = %q", mode)
	}
	text = strings.Replace(text, "- SIZE: small", "- SIZE: small\n- DECISION_MODE: assignee", 1)
	if mode := DecisionModeOf(text); mode != DecisionModeAssignee {
		t.Fatalf("managed header mode = %q", mode)
	}
}

func TestDecisionInputsBoundPayloadAndPreserveFreeText(t *testing.T) {
	base := DecisionInput{Question: "What is the target?", Options: []DecisionOption{}, Answer: "Free text answer", Channel: "cli"}
	if err := validateDecision(DecisionHuman, base); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*DecisionInput)
	}{
		{"question", func(in *DecisionInput) { in.Question = strings.Repeat("界", 1366) }},
		{"answer", func(in *DecisionInput) { in.Answer = strings.Repeat("a", 4097) }},
		{"missing options", func(in *DecisionInput) { in.Options = nil }},
		{"many options", func(in *DecisionInput) { in.Options = make([]DecisionOption, 33) }},
		{"duplicate option", func(in *DecisionInput) { in.Options = []DecisionOption{{"a", "One"}, {"a", "Two"}} }},
		{"long option key", func(in *DecisionInput) { in.Options = []DecisionOption{{strings.Repeat("a", 129), "Label"}} }},
		{"long option label", func(in *DecisionInput) { in.Options = []DecisionOption{{"a", strings.Repeat("a", 513)}} }},
		{"channel", func(in *DecisionInput) { in.Channel = "unknown" }},
		{"human grant", func(in *DecisionInput) { in.Authorization = &DecisionAuthorization{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.change(&input)
			if err := validateDecision(DecisionHuman, input); err == nil {
				t.Fatal("accepted an invalid decision")
			}
		})
	}
}
