package server

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

	"github.com/dualface/kander/api"
)

func TestEveryRegisteredRoutePassesAuthorizationExceptProbes(t *testing.T) {
	var calls []Request
	s, err := Open(Options{
		DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hooks: Hooks{Authorize: func(_ context.Context, r Request) error {
			calls = append(calls, r)
			return errors.New("private policy reason")
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	project, _, err := s.register(api.RegisterProject{Name: "route-test"})
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	const task, group = "20261125-route-test-task", "20261125-route-test-group"
	for _, route := range Routes() {
		t.Run(route.Pattern, func(t *testing.T) {
			calls = nil
			method, path, _ := strings.Cut(route.Pattern, " ")
			want := Request{Pattern: route.Pattern, Method: method}
			var parts []string
			for part := range strings.SplitSeq(path, "/") {
				switch part {
				case "{project}":
					want.Project, want.ProjectID, part = project.Name, project.ID, project.ID
				case "{task}":
					want.Task, part = task, task
				case "{group}":
					want.Group, part = group, group
				default:
					if strings.HasPrefix(part, "{") {
						// Use a value distinct from any literal sibling of this wildcard.
						part = "test-" + strings.Trim(part, "{}")
					}
				}
				parts = append(parts, part)
			}
			r := httptest.NewRequestWithContext(t.Context(), method, strings.Join(parts, "/"), nil)
			r.RemoteAddr = "127.0.0.1:3210"
			for _, key := range []string{"project", "task", "group"} {
				r.SetPathValue(key, "outer-router")
			}
			r.Header.Set(api.HeaderAdminToken, s.adminToken)
			r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if r.Pattern != route.Pattern {
				t.Fatalf("request matched %q instead of %q", r.Pattern, route.Pattern)
			}
			if !route.Authorized {
				if w.Code != http.StatusOK || len(calls) != 0 {
					t.Fatalf("probe response = %d, authorization calls = %v", w.Code, calls)
				}
				return
			}
			var denied api.Error
			err := json.NewDecoder(w.Body).Decode(&denied)
			if w.Code != http.StatusForbidden || err != nil || denied.Code != api.CodeForbidden || strings.Contains(denied.Message, "private policy reason") {
				t.Fatalf("status = %d, denial = %+v, decode = %v", w.Code, denied, err)
			}
			if len(calls) != 1 || calls[0] != want {
				t.Fatalf("authorization calls = %+v, want one %+v", calls, want)
			}
		})
	}
	if s.shutdown.Err() != nil {
		t.Fatalf("denied shutdown = %v", s.shutdown.Err())
	}
}

func TestRouteInventoryMatchesHandlerRegistrations(t *testing.T) {
	registered := make(map[string]bool)
	for _, r := range (&Server{}).routes() {
		if registered[r.pattern] {
			t.Fatalf("duplicate registration %q", r.pattern)
		}
		registered[r.pattern] = true
	}
	for _, route := range Routes() {
		if !registered[route.Pattern] {
			t.Fatalf("unregistered or duplicate inventory pattern %q", route.Pattern)
		}
		delete(registered, route.Pattern)
	}
	if len(registered) != 0 {
		t.Errorf("registrations missing from the inventory: %v", registered)
	}
}

func TestAttributionSelectsHostOrClientWithoutFallback(t *testing.T) {
	type callerKey struct{}
	for _, tc := range []struct {
		name string
		hook func(context.Context) (string, bool)
		want string
		code string
	}{
		{name: "unconfigured", want: "reported"},
		{name: "host caller", hook: func(ctx context.Context) (string, bool) {
			caller, ok := ctx.Value(callerKey{}).(string)
			return caller, ok
		}, want: "authenticated"},
		{name: "missing host caller", hook: func(context.Context) (string, bool) {
			return "ignored", false
		}, code: api.CodeAttributionRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Server{hooks: Hooks{Attribution: tc.hook}}
			ctx := context.WithValue(t.Context(), callerKey{}, "authenticated")
			r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/projects", nil)
			got, err := attribution(s.withAttribution(r), "reported")
			if got != tc.want || (tc.code == "" && err != nil) || (tc.code != "" && !api.IsCode(err, tc.code)) {
				t.Fatalf("attribution = %q, %v, want %q, %q", got, err, tc.want, tc.code)
			}
			if tc.code != "" && asAPIError(err).Status != http.StatusForbidden {
				t.Fatalf("missing caller has status %d", asAPIError(err).Status)
			}
		})
	}
}
