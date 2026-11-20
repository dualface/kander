package board

import (
	"os"
	"strings"
	"testing"
)

// ruleExample returns the JSON of the first fenced block that follows the line
// containing lead in the rules text of kander dispatch, without the indent of
// the list item that holds it.
func ruleExample(t *testing.T, lead string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../rules/snippets/commands/dispatch.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if !strings.Contains(line, lead) {
			continue
		}
		var block []string
		open := false
		for _, next := range lines[i+1:] {
			switch trimmed := strings.TrimSpace(next); {
			case !open && trimmed == "```json":
				open = true
			case open && trimmed == "```":
				return []byte(strings.Join(block, "\n"))
			case open:
				block = append(block, next)
			}
		}
		t.Fatalf("no closed json block after %q", lead)
	}
	t.Fatalf("lead %q not found in dispatch.md", lead)
	return nil
}

// The examples of the dispatch rules decode with the strict entrance that
// notify, resume and prepare use, so they cannot drift from the structs.
func TestDispatchRuleExamplesDecode(t *testing.T) {
	var evidence DispatchEvidence
	if err := DecodeReviewJSON(ruleExample(t, "- Fix evidence file."), &evidence); err != nil {
		t.Fatalf("evidence example: %v", err)
	}
	fix := evidence.Fix
	if fix == nil || len(fix.Findings) != 1 || fix.Findings[0].PreviousRunID == "" || len(fix.Authors) != 1 {
		t.Fatalf("evidence example lost its shape: %+v", evidence)
	}
	var intent DispatchInput
	if err := DecodeReviewJSON(ruleExample(t, "- Fix intent for `dispatch prepare`."), &intent); err != nil {
		t.Fatalf("intent example: %v", err)
	}
	if intent.TaskID == "" || intent.Kind != "fix" || intent.Evidence.Fix == nil || intent.CreatedAt.IsZero() || intent.ConfirmBy.IsZero() {
		t.Fatalf("intent example lost its shape: %+v", intent)
	}
}

// The rules say that a task_id at the top of an authors entry is refused.
func TestDispatchRuleExampleRefusesATaskIDBesideTheFinding(t *testing.T) {
	text := string(ruleExample(t, "- Fix evidence file."))
	mutated := strings.Replace(text, `"record_id": "author-1",`, `"record_id": "author-1", "task_id": "20261118-example-task",`, 1)
	if mutated == text {
		t.Fatal("the example has no record_id line to extend")
	}
	var evidence DispatchEvidence
	if err := DecodeReviewJSON([]byte(mutated), &evidence); err == nil {
		t.Fatal("a task_id at the top of an authors entry was accepted")
	}
}
