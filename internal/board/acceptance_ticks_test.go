package board

import (
	"errors"
	"strings"
	"testing"
)

const tickCriteria = "- [ ] 第一条\n- [ ] 第二条\n  - [ ] 子条目"

// tickTodoCard returns a todo card whose acceptance criteria hold two
// top-level items and one indented item.
func tickTodoCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text := strings.Replace(readyText(s), "- [ ] 验证任务行为", tickCriteria, 1)
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: task/"+slug, 1)
	updateSnapshot(t, root, s, text)
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, todo.TaskID)
}

func tickFirst(text string) string {
	return strings.Replace(text, "- [ ] 第一条", "- [x] 第一条", 1)
}

func tryUpdate(root string, s Snapshot, text string) error {
	return UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision})
}

func requireDecisionError(t *testing.T, err error, label string) {
	t.Helper()
	var ke *Error
	if !errors.As(err, &ke) || ke.Code != "board.transaction_decision_required" {
		t.Fatalf("%s: want board.transaction_decision_required, got %v", label, err)
	}
}

func TestAcceptanceTicksAllowedWhileWorking(t *testing.T) {
	root := tempBoard(t)
	s := tickTodoCard(t, root, "tick-working")
	working, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	if err = tryUpdate(root, s, tickFirst(s.Text)); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	if !strings.Contains(s.Text, "- [x] 第一条") || strings.Contains(s.Text, "CONTRACT_DECISIONS") {
		t.Fatal("tick not published or recorded as a contract decision")
	}
	if err = tryUpdate(root, s, strings.Replace(s.Text, "- [x] 第一条", "- [ ] 第一条", 1)); err != nil {
		t.Fatalf("untick rejected: %v", err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	rejected := map[string]string{
		"uppercase":   strings.Replace(s.Text, "- [ ] 第一条", "- [X] 第一条", 1),
		"indented":    strings.Replace(s.Text, "  - [ ] 子条目", "  - [x] 子条目", 1),
		"wording":     strings.Replace(tickFirst(s.Text), "第二条", "第二条已改", 1),
		"whitespace":  strings.Replace(tickFirst(s.Text), "第二条", "第二条 ", 1),
		"added item":  strings.Replace(tickFirst(s.Text), "- [ ] 第二条", "- [ ] 第二条\n- [ ] 第三条", 1),
		"other field": strings.Replace(tickFirst(s.Text), "## GOAL\n\n已确定的任务范围", "## GOAL\n\n新目标", 1),
	}
	for label, text := range rejected {
		if text == s.Text {
			t.Fatalf("%s: fixture did not change the card", label)
		}
		requireDecisionError(t, tryUpdate(root, s, text), label)
	}
}

func TestAcceptanceTicksAllowedInReview(t *testing.T) {
	root := tempBoard(t)
	s := tickTodoCard(t, root, "tick-review")
	working, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	review, err := MoveEntry(working, root, "review")
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, review.TaskID)
	if err = tryUpdate(root, s, tickFirst(s.Text)); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, review.TaskID)
	if !strings.Contains(s.Text, "- [x] 第一条") || strings.Contains(s.Text, "CONTRACT_DECISIONS") {
		t.Fatal("tick not published or recorded as a contract decision")
	}
}

func TestAcceptanceTicksFrozenOutsideExecution(t *testing.T) {
	root := tempBoard(t)
	s := tickTodoCard(t, root, "tick-todo")
	requireDecisionError(t, tryUpdate(root, s, tickFirst(s.Text)), "todo")

	backlog, err := MoveEntry(s.Entry, root, "backlog")
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, backlog.TaskID)
	requireDecisionError(t, tryUpdate(root, s, tickFirst(s.Text)), "frozen backlog")

	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	working, err := MoveWithOptions(todo, root, "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	updateSnapshot(t, root, s, strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\n已实现。", 1))
	exemptReviewFixture(t, root, working.TaskID)
	s = transactionSnapshot(t, root, working.TaskID)
	done, err := MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, done.TaskID)
	requireDecisionError(t, tryUpdate(root, s, tickFirst(s.Text)), "done")
}
