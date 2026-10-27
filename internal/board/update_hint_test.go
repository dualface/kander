package board

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestUpdateRejectionsCarryRecoveryHint(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "update-hints", false)
	old := readyText(s)
	header := func(line string) string { return strings.Replace(old, "- RESULT:", "- RESULT:\n"+line, 1) }
	field := func(name, value string) string {
		return FieldLineRe(name).ReplaceAllString(old, "- "+name+": "+value)
	}
	section := func(name string) string { return strings.TrimRight(old, "\n") + "\n\n## " + name + "\n\nmisplaced\n" }
	cases := []struct {
		name, next, state, hint string
	}{
		{"OWNER", field(FieldOwner, "someone"), "backlog", "board.update_hint_owner"},
		{"STARTED_AT", field(FieldStartedAt, "2026-01-01 00:00"), "backlog", "board.update_hint_owner"},
		{"SESSION", field(FieldSession, "x"), "backlog", "board.update_hint_session"},
		{"WINDOW", field(FieldWindow, "x"), "backlog", "board.update_hint_session"},
		{"FINISHED_AT", field(FieldFinishedAt, "2026-01-01 00:00"), "backlog", "board.update_hint_result"},
		{"RESULT", field(FieldResult, "completed"), "backlog", "board.update_hint_result"},
		{"LANGUAGE", field(FieldLanguage, "ja"), "backlog", "board.update_hint_created"},
		{"CREATED_AT", field(FieldCreatedAt, "2020-01-01 00:00"), "backlog", "board.update_hint_created"},
		{"DISPATCH_ID", header("- DISPATCH_ID: x"), "backlog", "board.update_hint_dispatch_field"},
		{"EXECUTION_EPOCH", header("- EXECUTION_EPOCH: 1"), "backlog", "board.update_hint_dispatch_field"},
		{"REVISION", header("- REVISION: 9"), "backlog", "board.update_hint_internal"},
		{"OPERATION_ID", header("- OPERATION_ID: x"), "backlog", "board.update_hint_internal"},
		{"REVIEWS field", header("- REVIEWS: x"), "backlog", "board.update_hint_internal"},
		{"EXEC_RESOLVED", header("- " + FieldExecResolved + ": x"), "backlog", "board.update_hint_resolved"},
		{"REVIEWS section", section("REVIEWS"), "backlog", "board.update_hint_reviews_section"},
		{"DISPATCHES section", section("DISPATCHES"), "backlog", "board.update_hint_dispatches_section"},
		{"REVIEW_DISPOSITIONS section", section("REVIEW_DISPOSITIONS"), "backlog", "board.update_hint_dispositions_section"},
		{"CONTRACT_DECISIONS section", section("CONTRACT_DECISIONS"), "backlog", "board.update_hint_contract_decisions_section"},
		{"LIFECYCLE_DECISION section", section("LIFECYCLE_DECISION"), "backlog", "board.update_hint_lifecycle_section"},
		{"duplicate section", section(SectionGoal), "backlog", "board.update_hint_duplicate"},
		{"duplicate field", header("- SIZE: small"), "backlog", "board.update_hint_duplicate"},
		{"frozen contract", strings.Replace(old, "已确定的任务范围", "改写的目标", 1), "todo", "board.update_hint_frozen"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.next == old {
				t.Fatal("fixture did not change the card")
			}
			_, err := validSpecUpdate(old, c.next, c.state, "")
			if err == nil || !strings.Contains(err.Error(), config.Text(c.hint)) {
				t.Fatalf("missing %s: %v", c.hint, err)
			}
		})
	}
	err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "reviews/x.md", Text: "x", ExpectedRevision: s.Revision})
	if err == nil || !strings.Contains(err.Error(), config.Text("board.update_hint_managed_path")) {
		t.Fatalf("managed path hint missing: %v", err)
	}
}

func TestMoveDecisionErrorKeepsSharedText(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "move-text", false)
	_, err := MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "cancelled"})
	if err == nil || err.Error() != config.Text("board.transaction_decision_required") {
		t.Fatalf("move error text changed: %v", err)
	}
}
