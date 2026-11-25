package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/server"
)

func TestEmbeddedHandlerAuthorizesBeforeWritingAndPublishesEvent(t *testing.T) {
	var forbidCreation bool
	dir := t.TempDir()
	options := server.Options{
		DataDir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hooks: server.Hooks{Authorize: func(_ context.Context, request server.Request) error {
			if request.Pattern == "POST /api/v1/projects/{project}/tasks" && forbidCreation {
				return errors.New("host refused creation")
			}
			return nil
		}},
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
	handler := s.Handler()
	project := request[api.Project](t, handler, http.MethodPost, "/api/v1/projects", `{"name":"embedder"}`, http.StatusCreated)
	tasks := "/api/v1/projects/" + project.ID + "/tasks"
	before := request[api.TaskList](t, handler, http.MethodGet, tasks, "", http.StatusOK)
	events, err := s.Subscribe(t.Context(), project.ID, before.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	const body = `{"type":"feature","slug":"embedded","title":"Embedded task"}`
	forbidCreation = true
	denial := request[api.Error](t, handler, http.MethodPost, tasks, body, http.StatusForbidden)
	if denial.Code != api.CodeForbidden {
		t.Fatalf("rejection code = %q", denial.Code)
	}
	afterDenial := request[api.TaskList](t, handler, http.MethodGet, tasks, "", http.StatusOK)
	if len(afterDenial.Tasks) != 0 || afterDenial.Cursor != before.Cursor {
		t.Fatalf("rejected write changed the board: %+v", afterDenial)
	}
	// The same key still works. A denied request did not reserve it.
	forbidCreation = false
	created := request[api.Task](t, handler, http.MethodPost, tasks, body, http.StatusCreated)
	list := request[api.TaskList](t, handler, http.MethodGet, tasks, "", http.StatusOK)
	if created.Revision == 0 || len(list.Tasks) != 1 || list.Tasks[0].TaskID != created.TaskID {
		t.Fatalf("created = %+v, list = %+v", created, list)
	}
	select {
	case got := <-events:
		if got.Reset || got.Cursor != list.Cursor || got.Event.Kind != api.EventTaskChanged ||
			got.Event.TaskID != created.TaskID || got.Event.Revision != created.Revision {
			t.Fatalf("created task event = %+v, task = %+v", got, created)
		}
	case <-time.After(time.Second):
		t.Fatal("task creation did not notify subscriber")
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-events:
		if open {
			t.Fatal("unexpected event after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close")
	}
	reopened, err := server.Open(options)
	if err != nil {
		t.Fatalf("Close did not release the data lock: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTaskAccessCanBeCalledFromAnAuthorizationHook(t *testing.T) {
	var s *server.Server
	var seen api.TaskAccess
	options := server.Options{
		DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hooks: server.Hooks{Authorize: func(ctx context.Context, request server.Request) error {
			if request.Task == "" {
				return nil
			}
			access, err := s.TaskAccess(ctx, request.ProjectID, request.Task)
			seen = access
			return err
		}},
	}
	var err error
	s, err = server.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	handler := s.Handler()
	project := request[api.Project](t, handler, http.MethodPost, "/api/v1/projects", `{"name":"access"}`, http.StatusCreated)
	base := "/api/v1/projects/" + project.ID + "/tasks"
	created := request[api.Task](t, handler, http.MethodPost, base, `{"type":"feature","slug":"access","title":"Access task","submitter":"alice"}`, http.StatusCreated)
	path := base + "/" + created.TaskID
	request[api.Task](t, handler, http.MethodPost, path+"/assignee", `{"assignee":"bob"}`, http.StatusOK, "assign-access-task")
	if seen != (api.TaskAccess{Submitter: "alice", State: "backlog", Revision: created.Revision}) {
		t.Fatalf("hook before assignment: %+v", seen)
	}
	shown := request[api.Task](t, handler, http.MethodGet, path, "", http.StatusOK)
	if seen != (api.TaskAccess{Submitter: "alice", Assignee: "bob", State: "backlog", Revision: shown.Revision}) {
		t.Fatalf("hook after assignment: %+v", seen)
	}
	if _, err := s.TaskAccess(t.Context(), project.ID, "20261125-missing-task"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("missing task: %v", err)
	}
}

func request[T any](t *testing.T, handler http.Handler, method, path, body string, status int, key ...string) T {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	r.Header.Set(api.HeaderClientID, "embedder")
	r.Header.Set(api.HeaderIdempotencyKey, "embedded-task")
	if len(key) > 0 {
		r.Header.Set(api.HeaderIdempotencyKey, key[0])
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	var value T
	if err := json.NewDecoder(w.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
