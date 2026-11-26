package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

func TestTaskListFiltersSearchAndKeysetPagination(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	c, _ := client.New(h.URL, "host-a")
	info, err := c.Register(t.Context(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	makeTask := func(slug, title, submitter, assignee, group string) api.Task {
		t.Helper()
		task, err := p.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: slug, Title: title, Submitter: submitter})
		if err != nil {
			t.Fatal(err)
		}
		if group != "" {
			text := strings.Replace(task.Text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
			task, err = p.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		if assignee != "" {
			task, err = p.Assign(t.Context(), "", task.TaskID, api.Assign{Assignee: assignee})
			if err != nil {
				t.Fatal(err)
			}
		}
		return task
	}
	const group = "20261125-release-group"
	chinese := makeTask("login-zh", `登录页改版 "100%_done"`, "lin", "mika", group)
	english := makeTask("login-en", "Redesign the Login page", "lin", "mika", "")
	other := makeTask("other", "Shipping times", "lee", "max", group)
	if _, err := p.SetDecisionMode(t.Context(), "", other.TaskID, api.SetDecisionMode{Mode: api.DecisionModeAssignee}); err != nil {
		t.Fatal(err)
	}
	zulu := makeTask("zulu", "Console login", "lee", "", "")
	differentState := makeTask("trash", "Obsolete login screen", "lee", "", "")
	if _, err := p.Move(t.Context(), "", differentState.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "discard fixture"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		query api.TaskListOptions
		want  []string
	}{
		{"assignee", api.TaskListOptions{Assignee: "mika"}, []string{chinese.TaskID, english.TaskID}},
		{"submitter", api.TaskListOptions{Submitter: "lin"}, []string{chinese.TaskID, english.TaskID}},
		{"group", api.TaskListOptions{Group: group}, []string{chinese.TaskID, other.TaskID}},
		{"assignee mode", api.TaskListOptions{DecisionMode: api.DecisionModeAssignee, Group: group}, []string{other.TaskID}},
		{"leader mode", api.TaskListOptions{DecisionMode: api.DecisionModeLeader, Group: group}, []string{chinese.TaskID}},
		{"english substring", api.TaskListOptions{State: "backlog", Search: "OGiN"}, []string{english.TaskID, zulu.TaskID}},
		{"chinese trigram", api.TaskListOptions{Search: "登录页"}, []string{chinese.TaskID}},
		{"short substring", api.TaskListOptions{Search: "登录"}, []string{chinese.TaskID}},
		{"literal FTS query", api.TaskListOptions{Search: `"100%_`}, []string{chinese.TaskID}},
		{"literal LIKE wildcards", api.TaskListOptions{Search: "%_"}, []string{chinese.TaskID}},
		{"combined", api.TaskListOptions{State: "backlog", Assignee: "mika", Submitter: "lin", Group: group, Search: "登录页"}, []string{chinese.TaskID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := p.ListWithOptions(t.Context(), tc.query)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, row := range out.Tasks {
				ids = append(ids, row.TaskID)
			}
			if !slices.Equal(slices.Sorted(slices.Values(ids)), slices.Sorted(slices.Values(tc.want))) {
				t.Fatalf("query matched %v, want %v", ids, tc.want)
			}
		})
	}
	all, err := p.List(t.Context(), "")
	if err != nil || len(all.Tasks) != 5 || all.NextCursor != "" {
		t.Fatalf("unpaginated list: %+v %v", all, err)
	}
	var paginated []api.TaskSummary
	q := api.TaskListOptions{Limit: 2}
	for page := range 3 {
		out, err := p.ListWithOptions(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		if out.Cursor != all.Cursor || (out.NextCursor == "") != (page == 2) || len(out.Tasks) != min(2, 5-2*page) {
			t.Fatalf("page %d: %+v", page+1, out)
		}
		if page == 0 {
			if _, err := p.ListWithOptions(t.Context(), api.TaskListOptions{Limit: 2, State: "trash", Cursor: out.NextCursor}); !api.IsCode(err, api.CodeInvalidRequest) {
				t.Fatalf("cursor reused with different filters: %v", err)
			}
			if _, err := p.ListWithOptions(t.Context(), api.TaskListOptions{Limit: 2, DecisionMode: api.DecisionModeLeader, Cursor: out.NextCursor}); !api.IsCode(err, api.CodeInvalidRequest) {
				t.Fatalf("cursor reused with decision mode filter: %v", err)
			}
		}
		paginated = append(paginated, out.Tasks...)
		q.Cursor = out.NextCursor
	}
	if !reflect.DeepEqual(paginated, all.Tasks) {
		t.Fatalf("paged order %v differs from unpaginated order %v", paginated, all.Tasks)
	}
	project, _ := s.project(info.ID)
	if err := project.checkIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"limit=0", "limit=1001", "limit=z", "cursor=opaque", "limit=2&cursor=!", "limit=2&limit=3", "q=%00", "decision_mode=", "decision_mode=other", "decision_mode=leader&decision_mode=assignee"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/projects/"+info.ID+"/tasks?"+invalid, nil)
		r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || !containsErrorCode(w, api.CodeInvalidRequest) {
			t.Errorf("invalid query %q returned %d %s", invalid, w.Code, w.Body)
		}
	}
}

func TestTaskListWarningsRemainBoardWide(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	// Simulate a legacy committed journal advisory without an invalid task.
	journal := filepath.Join(proj.board, ".kander", "operations")
	if err := writeJSON(proj.board, filepath.Join(".kander", "operations", "old-index-warning.json"), board.OperationRecord{
		Schema: 1, ID: "old-index-warning", Phase: "committed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Assign(t.Context(), "", id, api.Assign{Assignee: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := proj.checkIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	list, err := p.List(t.Context(), "trash")
	if err != nil || len(list.Tasks) != 0 || len(list.Warnings) == 0 {
		t.Fatalf("filtered list lost board-wide journal warnings: %+v %v", list, err)
	}
	if err := os.Remove(filepath.Join(journal, "old-index-warning.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Assign(t.Context(), "", id, api.Assign{Assignee: "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := proj.checkIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
}
