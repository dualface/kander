package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/server"
)

func openEmbedded(t *testing.T, options server.Options) *server.Server {
	t.Helper()
	if options.DataDir == "" {
		options.DataDir = t.TempDir()
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s, err := server.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

const assignmentPattern = "POST /api/v1/projects/{project}/tasks/{task}/assignee"

func TestEmbeddedAssignmentPolicyAndReplay(t *testing.T) {
	var (
		route  server.Request
		calls  []server.Assignment
		read   api.TaskAccess
		policy error
	)
	var s *server.Server
	s = openEmbedded(t, server.Options{Hooks: server.Hooks{
		Authorize: func(_ context.Context, r server.Request) error {
			if r.Pattern == assignmentPattern {
				route = r
			}
			return nil
		},
		AuthorizeAssignment: func(ctx context.Context, a server.Assignment) error {
			calls = append(calls, a)
			var err error
			read, err = s.TaskAccess(ctx, a.Request.ProjectID, a.Request.Task)
			if err != nil {
				t.Errorf("read access inside assignment hook: %v", err)
			}
			return policy
		},
	}})
	h := s.Handler()
	p := request[api.Project](t, h, http.MethodPost, "/api/v1/projects", `{"name":"assignment"}`, http.StatusCreated)
	tasks := "/api/v1/projects/" + p.ID + "/tasks"
	card := request[api.Task](t, h, http.MethodPost, tasks, `{"type":"feature","slug":"assignment","title":"Assignment test"}`, http.StatusCreated)
	endpoint := tasks + "/" + card.TaskID + "/assignee"
	list := func() api.TaskList { return request[api.TaskList](t, h, http.MethodGet, tasks, "", http.StatusOK) }
	checkCall := func(count int, assignee string, revision uint64) {
		t.Helper()
		if len(calls) != count {
			t.Fatalf("assignment policy called %d times, want %d", len(calls), count)
		}
		want := server.Assignment{Request: server.Request{
			Pattern: assignmentPattern, Method: http.MethodPost, Project: p.Name, ProjectID: p.ID, Task: card.TaskID,
		}, Assignee: "alice"}
		if calls[count-1] != want || route != want.Request {
			t.Fatalf("route scope = %+v, assignment = %+v, want %+v", route, calls[count-1], want)
		}
		if read.Assignee != assignee || read.Revision != revision {
			t.Errorf("hook read assignee %q at revision %d, want %q at %d", read.Assignee, read.Revision, assignee, revision)
		}
	}
	const body = `{"assignee":"  alice  "}`
	const key = "same-assignment-key"
	before := list()
	reject := func(status int, code string, callCount int, unchanged api.TaskList) {
		t.Helper()
		failure := request[api.Error](t, h, http.MethodPost, endpoint, body, status, key)
		if failure.Code != code || strings.Contains(failure.Message, "private reason") {
			t.Fatalf("unexpected denial: %+v", failure)
		}
		row := unchanged.Tasks[0]
		checkCall(callCount, row.Assignee, row.Revision)
		after := list()
		if after.Cursor != unchanged.Cursor || !reflect.DeepEqual(after.Tasks, unchanged.Tasks) {
			t.Fatalf("denial changed revision, assignee or events: before %+v, after %+v", unchanged, after)
		}
	}
	policy = fmt.Errorf("private reason: %w", server.ErrAssigneeRejected)
	reject(http.StatusBadRequest, api.CodeInvalidRequest, 1, before)
	policy = errors.New("private reason")
	reject(http.StatusForbidden, api.CodeForbidden, 2, before)

	policy = nil
	assigned := request[api.Task](t, h, http.MethodPost, endpoint, body, http.StatusOK, key)
	checkCall(3, "", card.Revision)
	written := list()
	if written.Tasks[0].Assignee != "alice" || written.Tasks[0].Revision != assigned.Revision ||
		assigned.Revision != card.Revision+1 || !strings.Contains(assigned.Text, "\n- ASSIGNEE: alice\n") {
		t.Fatalf("wrong assignment: task %+v, row %+v", assigned, written.Tasks)
	}

	replay := request[api.Task](t, h, http.MethodPost, endpoint, body, http.StatusOK, key)
	checkCall(4, "alice", assigned.Revision)
	if after := list(); !reflect.DeepEqual(replay, assigned) || after.Cursor != written.Cursor || after.Tasks[0].Revision != assigned.Revision {
		t.Fatalf("allowed replay changed the response, revision or cursor: replay %+v, list %+v", replay, after)
	}
	policy = fmt.Errorf("private reason on replay: %w", server.ErrAssigneeRejected)
	reject(http.StatusBadRequest, api.CodeInvalidRequest, 5, written)
	policy = errors.New("private reason on replay")
	reject(http.StatusForbidden, api.CodeForbidden, 6, written)
	policy = nil
	replay = request[api.Task](t, h, http.MethodPost, endpoint, body, http.StatusOK, key)
	checkCall(7, "alice", assigned.Revision)
	if !reflect.DeepEqual(replay, assigned) {
		t.Fatalf("denial overwrote the prior receipt: %+v", replay)
	}
}

func TestEmbeddedAssignmentPolicySkipsInvalidAndUnrelatedRequests(t *testing.T) {
	denyRoute := false
	h := openEmbedded(t, server.Options{Hooks: server.Hooks{
		Authorize: func(context.Context, server.Request) error {
			if denyRoute {
				return errors.New("private route refusal")
			}
			return nil
		},
		AuthorizeAssignment: func(_ context.Context, a server.Assignment) error {
			t.Errorf("unexpected assignment hook call: %+v", a)
			return nil
		},
	}}).Handler()
	p := request[api.Project](t, h, http.MethodPost, "/api/v1/projects", `{"name":"skip-policy"}`, http.StatusCreated)
	base := "/api/v1/projects/" + p.ID + "/tasks"
	card := request[api.Task](t, h, http.MethodPost, base, `{"type":"feature","slug":"skip-policy","title":"Skip policy"}`, http.StatusCreated)
	task := base + "/" + card.TaskID
	request[api.DecisionModeResult](t, h, http.MethodPost, task+"/decision-mode", `{"mode":"assignee"}`, http.StatusOK, "decision-mode")
	for _, tc := range []struct{ name, body, boardCode string }{
		{"malformed", `{"assignee":`, ""},
		{"control", `{"assignee":"alice\n"}`, "board.person_field_invalid"},
		{"too-long", assignBody(t, "  "+strings.Repeat("界", 86)+"  "), "board.person_field_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := request[api.Error](t, h, http.MethodPost, task+"/assignee", tc.body, http.StatusBadRequest, tc.name)
			if got.Code != api.CodeInvalidRequest || got.BoardCode != tc.boardCode {
				t.Fatalf("validation failure = %+v", got)
			}
		})
	}
	denyRoute = true
	got := request[api.Error](t, h, http.MethodPost, task+"/assignee", `{"assignee":"alice"}`, http.StatusForbidden, "denied-route")
	if got.Code != api.CodeForbidden {
		t.Fatalf("route denial = %+v", got)
	}
}

func TestEmbeddedAssignmentPolicyWithoutRouteHook(t *testing.T) {
	var got server.Assignment
	h := openEmbedded(t, server.Options{Hooks: server.Hooks{
		AuthorizeAssignment: func(_ context.Context, a server.Assignment) error {
			got = a
			return nil
		},
	}}).Handler()
	p := request[api.Project](t, h, http.MethodPost, "/api/v1/projects", `{"name":"assignment-only"}`, http.StatusCreated)
	base := "/api/v1/projects/" + p.ID + "/tasks"
	card := request[api.Task](t, h, http.MethodPost, base, `{"type":"feature","slug":"assignment-only","title":"Assignment only"}`, http.StatusCreated)
	request[api.Task](t, h, http.MethodPost, base+"/"+card.TaskID+"/assignee", `{"assignee":" alice "}`, http.StatusOK, "assign")
	want := server.Assignment{Request: server.Request{
		Pattern: assignmentPattern, Method: http.MethodPost, Project: p.Name, ProjectID: p.ID, Task: card.TaskID,
	}, Assignee: "alice"}
	if got != want {
		t.Fatalf("assignment policy without route policy got %+v, want %+v", got, want)
	}
}

func TestEmbeddedAssignmentWithoutValuePolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hooks server.Hooks
	}{
		{name: "no hooks"},
		{name: "route hook only", hooks: server.Hooks{
			Authorize: func(context.Context, server.Request) error { return nil },
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := openEmbedded(t, server.Options{Hooks: tc.hooks}).Handler()
			p := request[api.Project](t, h, http.MethodPost, "/api/v1/projects", `{"name":"no-value-policy"}`, http.StatusCreated)
			project := "/api/v1/projects/" + p.ID
			card := request[api.Task](t, h, http.MethodPost, project+"/tasks", `{"type":"feature","slug":"no-policy","title":"No value policy"}`, http.StatusCreated)
			path := project + "/tasks/" + card.TaskID + "/assignee"
			before := request[api.TaskList](t, h, http.MethodGet, project+"/tasks", "", http.StatusOK)
			invalid := request[api.Error](t, h, http.MethodPost, path, `{"assignee":"alice\n"}`, http.StatusBadRequest, "invalid")
			if invalid.Code != api.CodeInvalidRequest || invalid.BoardCode != "board.person_field_invalid" {
				t.Fatalf("validation error = %+v", invalid)
			}
			body := `{"assignee":"  alice  "}`
			mismatch := request[api.Error](t, h, http.MethodPost, path, body, http.StatusUnprocessableEntity, "invalid")
			if mismatch.Code != api.CodeIdempotencyMismatch {
				t.Fatalf("different body did not conflict with the recorded validation error: %+v", mismatch)
			}
			assigned := request[api.Task](t, h, http.MethodPost, path, body, http.StatusOK, "valid")
			if !strings.Contains(assigned.Text, "\n- ASSIGNEE: alice\n") {
				t.Fatalf("normalized assignment missing: %s", assigned.Text)
			}
			page := request[api.EventPage](t, h, http.MethodGet, project+"/events?cursor="+before.Cursor, "", http.StatusOK)
			if page.Reset || len(page.Events) != 1 || page.Events[0].Kind != api.EventTaskChanged ||
				page.Events[0].TaskID != card.TaskID || page.Events[0].Revision != assigned.Revision {
				t.Fatalf("want one assignment event, got %+v", page)
			}
		})
	}
}

func assignBody(t *testing.T, assignee string) string {
	t.Helper()
	body, err := json.Marshal(api.Assign{Assignee: assignee})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
