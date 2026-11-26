package main

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
)

func TestListIndexFlagsAndContinuation(t *testing.T) {
	session, _ := moveServiceFixture(t)
	for _, slug := range []string{"search-a", "search-b"} {
		task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "登录页改版 " + slug, Submitter: "lin"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(task.Text, "- TASK_GROUP:", "- TASK_GROUP: 20261125-list-group", 1)
		if _, err := session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Project.Assign(t.Context(), "", task.TaskID, api.Assign{Assignee: "mika"}); err != nil {
			t.Fatal(err)
		}
	}
	flags := []string{"list", "--search", "登录页", "--assignee", "mika", "--submitter", "lin", "--group", "20261125-list-group", "--limit", "1", "backlog"}
	first := requireResult(t, runKander(t, append(flags, "--json")...), "first page", "tasks", "cursor", "next_cursor")
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" || first["cursor"] == cursor {
		t.Fatalf("invalid continuation: %+v", first)
	}
	second := requireResult(t, runKander(t, append(flags, "--json", "--cursor", cursor)...), "second page", "tasks")
	firstTasks, secondTasks := first["tasks"].([]any), second["tasks"].([]any)
	if len(firstTasks) != 1 || len(secondTasks) != 1 || second["next_cursor"] != nil ||
		firstTasks[0].(map[string]any)["task_id"] == secondTasks[0].(map[string]any)["task_id"] {
		t.Fatalf("invalid paging: %+v -> %+v", first, second)
	}
	text := runKander(t, flags...)
	if text.code != 0 || !strings.Contains(text.stderr, cursor) {
		t.Fatalf("text page omitted continuation: %+v", text)
	}
	requireFailure(t, runKander(t, "list", "--json", "--limit", "0"), "zero limit", "usage", 2)
	requireFailure(t, runKander(t, "list", "--json", "--cursor", cursor), "missing limit", "usage", 2)
	requireFailure(t, runKander(t, "list", "--json", "--limit", "1", "--cursor", cursor), "changed filters", api.CodeInvalidRequest, 1)
}
