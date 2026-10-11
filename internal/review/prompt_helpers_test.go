package review

import (
	"testing"

	"github.com/dualface/kander/internal/process"
)

func mustBuildPrompt(t *testing.T, ctx reviewContext, evidence, task string) string {
	t.Helper()
	text, err := buildPrompt(ctx, evidence, task)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func mustBuildContract(t *testing.T, ctx reviewContext) string {
	t.Helper()
	text, err := buildReviewContract(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func mustRoleRules(t *testing.T, role string) string {
	t.Helper()
	text, err := roleRules(role)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func mustLastMessageContract(t *testing.T) string {
	t.Helper()
	text, err := lastMessageOutputContract()
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func mustReportLanguageRule(t *testing.T, language string) string {
	t.Helper()
	text, err := reportLanguageRule(language)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func mustInstruction(t *testing.T, head, path string) string {
	t.Helper()
	text, err := process.TaskFileInstruction("", head, path)
	if err != nil {
		t.Fatal(err)
	}
	return text
}
