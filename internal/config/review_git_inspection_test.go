package config

import (
	"slices"
	"testing"
)

func TestEmbeddedReviewGitInspectionDeclarations(t *testing.T) {
	want := []string{"agy", "grok", "kimi", "opencode"}
	var disabled []string
	for name := range embeddedAgents {
		if !EmbeddedReviewGitInspection(name) {
			disabled = append(disabled, name)
		}
	}
	slices.Sort(disabled)
	if !slices.Equal(disabled, want) {
		t.Fatalf("reviewers without Git inspection = %v, want %v", disabled, want)
	}
	if !EmbeddedReviewGitInspection("not-a-builtin") {
		t.Fatal("custom reviewers must default to Git inspection")
	}
}
