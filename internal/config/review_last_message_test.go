package config

import (
	"reflect"
	"slices"
	"testing"
)

func TestEmbeddedReviewLastMessageReportDeclarations(t *testing.T) {
	var declared []string
	for name, emb := range embeddedAgents {
		if emb.Review.LastMessageReport {
			declared = append(declared, name)
		}
	}
	slices.Sort(declared)
	if want := []string{"claude", "cursor", "droid", "grok", "kimi"}; !slices.Equal(declared, want) {
		t.Fatalf("built-in definitions declaring last_message_report: %v, want %v", declared, want)
	}
	for _, name := range declared {
		if !EmbeddedReviewLastMessageReport(name) {
			t.Fatalf("%s: lookup disagrees with the definition", name)
		}
	}
	if EmbeddedReviewLastMessageReport("codex") || EmbeddedReviewLastMessageReport("no-such-agent") {
		t.Fatal("an agent without the declaration must report false")
	}
}

// The field exists only on the built-in wrapper, so a user definition cannot declare it.
func TestLastMessageReportIsNotAUserReviewField(t *testing.T) {
	if _, ok := reflect.TypeOf(AgentReview{}).FieldByName("LastMessageReport"); ok {
		t.Fatal("AgentReview carries LastMessageReport")
	}
	if _, ok := jsonKeys(reflect.TypeOf(AgentReview{}))["last_message_report"]; ok {
		t.Fatal("a user review definition accepts last_message_report")
	}
}
