package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/server"
)

type callerKey struct{}

// B5: the caller the host attributes a request to is part of its receipt
// scope. Two callers that send the same key and body each get their own
// result, and each caller's retry replays its own first response.
func TestEmbeddedReceiptsAreScopedByTheAttributedCaller(t *testing.T) {
	s := openEmbedded(t, server.Options{Hooks: server.Hooks{Attribution: func(ctx context.Context) (string, bool) {
		caller, ok := ctx.Value(callerKey{}).(string)
		return caller, ok
	}}})
	handler := s.Handler()
	post := func(caller, path, key, body string) (int, []byte) {
		t.Helper()
		ctx := context.WithValue(t.Context(), callerKey{}, caller)
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body))
		r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
		r.Header.Set(api.HeaderClientID, "embedder")
		r.Header.Set(api.HeaderIdempotencyKey, key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	status, raw := post("alice", "/api/v1/projects", "register", `{"name":"scoped"}`)
	var project api.Project
	if err := json.Unmarshal(raw, &project); err != nil || status != http.StatusCreated {
		t.Fatalf("register = %d %s", status, raw)
	}
	tasks := "/api/v1/projects/" + project.ID + "/tasks"
	const body = `{"type":"feature","slug":"scoped","title":"Scoped task"}`
	statusA, first := post("alice", tasks, "shared-key", body)
	if statusA != http.StatusCreated {
		t.Fatalf("A = %d %s", statusA, first)
	}
	// B's creation runs: the slug is taken, so B gets its own refusal, not
	// A's created task.
	statusB, other := post("bob", tasks, "shared-key", body)
	var refusal api.Error
	if err := json.Unmarshal(other, &refusal); err != nil || statusB != http.StatusUnprocessableEntity || refusal.BoardCode != "board.task_already_exists" {
		t.Fatalf("B = %d %s, want its own creation attempt", statusB, other)
	}
	again, replay := post("alice", tasks, "shared-key", body)
	if again != statusA || !bytes.Equal(replay, first) {
		t.Fatalf("A's retry = %d %s, want %d %s", again, replay, statusA, first)
	}
}
