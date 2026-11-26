package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
)

// scanList is the file-scan implementation used before SQLite. It is kept
// independent of the index projection as the differential test's oracle.
func (p *project) scanList(ctx context.Context) (api.TaskList, error) {
	cursor := p.events.cursor()
	view, err := board.BoardPayloadContext(ctx, p.board)
	if err != nil {
		return api.TaskList{}, err
	}
	now := p.server.now()
	out := api.TaskList{ObservedAt: now, Tasks: []api.TaskSummary{}, Cursor: cursor, Warnings: view.Warnings}
	for _, t := range view.Tasks {
		s := api.TaskSummary{
			TaskID: t.TaskID, Title: t.Title, State: t.State, Kind: t.Kind, Type: t.Type, Session: t.Session, Window: t.Window,
			TaskGroup: t.TaskGroup, Owner: t.Owner, Submitter: t.Submitter, Assignee: t.Assignee, DecisionMode: t.DecisionMode, CreatedAt: t.CreatedAt,
			StartedAt: t.StartedAt, CompletedAt: t.CompletedAt, Time: t.Time, Result: t.Result,
		}
		snapshot, err := board.ReadSnapshot(p.board, t.TaskID)
		if err != nil {
			return api.TaskList{}, err
		}
		s.Revision = snapshot.Revision
		if r, found, err := p.readClaim(t.TaskID); err != nil {
			return api.TaskList{}, err
		} else if found && r.Status != claimPending {
			s.Claim = r.view(now)
		}
		out.Tasks = append(out.Tasks, s)
	}
	return out, nil
}

func (p *project) checkIndex(ctx context.Context) error {
	if err := p.lockIndex(); err != nil {
		return err
	}
	defer p.mu.Unlock()
	want, err := p.scanList(ctx)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	rows, _, err := p.index.List(ctx, api.TaskListOptions{}, nil)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	warnings, err := p.index.Warnings(ctx)
	if err != nil {
		return err
	}
	// Render both from the same clock reading to avoid lease boundary flakes.
	got := makeTaskList(rows, warnings, p.events.cursor(), want.ObservedAt)
	got.ObservedAt, want.ObservedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		return fmt.Errorf("index = %s, file scan = %s", gotJSON, wantJSON)
	}
	return nil
}

// ReconcileIndexForSeed brings explicitly seeded historical test evidence into
// the index. Production mutations must go through their real HTTP entrances.
func (s *Server) ReconcileIndexForSeed(projectID string) error {
	p, err := s.project(projectID)
	if err != nil {
		return err
	}
	if err := p.lockBoard(); err != nil {
		return err
	}
	defer p.mu.Unlock()
	return p.reconcileIndex()
}

// IndexDifferentialHandler audits the successful HTTP operations exercised by
// the service integration fixtures before releasing the response to the client.
// Reads and control-only writes are classified separately in the inventory.
func (s *Server) IndexDifferentialHandler(t *testing.T) http.Handler {
	t.Helper()
	handler := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodPost && r.Method != http.MethodPut) || !strings.HasPrefix(r.URL.Path, "/api/v1/projects/") {
			handler.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code >= 200 && rec.Code < 300 {
			if class := indexWriteRoutes()[r.Pattern]; class == "" {
				t.Errorf("unclassified successful route %s", r.Pattern)
			} else if class != "read" {
				p, err := s.project(r.PathValue("project"))
				if err == nil {
					err = p.checkIndex(t.Context())
				}
				if err != nil {
					t.Errorf("%s %s: %v", r.Pattern, r.PathValue("action"), err)
				} else {
					recordIndexCoverage(r.Pattern, r.PathValue("action"))
				}
			}
		}
		for name, values := range rec.Header() {
			w.Header()[name] = values
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	})
}

// The mutation families checked by the HTTP fixtures. Review/dispatch/runtime
// wildcards include all their successful actions. Integration, request, and
// publication control is audited as well, even if its list does not change.
func indexWriteRoutes() map[string]string {
	const project = "/api/v1/projects/{project}"
	const task = project + "/tasks/{task}"
	const group = project + "/groups/{group}"
	const review = project + "/reviews"
	const integration = project + "/integrations/{integration}"
	return map[string]string{
		"POST " + project + "/tasks":                              "card",
		"PUT " + task + "/documents/{path...}":                    "card",
		"POST " + task + "/move":                                  "card and claim",
		"POST " + task + "/assignee":                              "card",
		"POST " + task + "/decisions/human":                       "card",
		"POST " + task + "/decisions/leader":                      "card",
		"POST " + task + "/decision-mode":                         "card",
		"POST " + task + "/claim":                                 "card and claim",
		"POST " + task + "/takeover":                              "card and claim",
		"POST " + task + "/execution":                             "card and claim",
		"POST " + task + "/renew":                                 "claim",
		"POST " + task + "/observation":                           "claim",
		"POST " + task + "/deliveries":                            "control",
		"POST " + task + "/requests":                              "control",
		"POST " + task + "/requests/{request}/answer":             "control",
		"POST " + task + "/dispatches/actions/{action}":           "card",
		"POST " + group + "/claim":                                "group producer",
		"POST " + group + "/renew":                                "control",
		"POST " + group + "/observation":                          "control",
		"POST " + group + "/release":                              "control",
		"POST " + group + "/facts":                                "read",
		"POST " + group + "/reconcile":                            "group producer",
		"POST " + group + "/recover":                              "card and claim",
		"POST " + group + "/remove-member":                        "card",
		"POST " + review + "/members":                             "read",
		"POST " + review + "/plan":                                "card",
		"POST " + review + "/batches/{batch}/aggregate":           "card",
		"POST " + review + "/close":                               "card",
		"POST " + review + "/actions/{action}":                    "card",
		"POST " + review + "/runtime/prepare":                     "review producer",
		"POST " + review + "/runtime/{run}/{action}":              "review producer",
		"POST " + integration + "/claim":                          "control",
		"POST " + integration + "/renew":                          "control",
		"POST " + integration:                                     "control",
		"POST " + project + "/publications":                       "control",
		"POST " + project + "/publications/{publication}/confirm": "control",
		"POST " + project + "/issues/import":                      "card",
		"POST " + project + "/issues/results/view":                "read",
		"POST " + project + "/issues/results/save":                "control",
		"POST " + project + "/snapshot":                           "read",
		"POST " + project + "/check":                              "read",
	}
}

func TestTaskIndexWriteRouteInventory(t *testing.T) {
	want := indexWriteRoutes()
	for _, route := range (&Server{}).routes() {
		method, path, _ := strings.Cut(route.pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/projects/{project}/") || method == http.MethodGet {
			continue
		}
		if _, ok := want[route.pattern]; !ok {
			t.Errorf("classify and add a file-scan differential fixture for %s", route.pattern)
		}
		delete(want, route.pattern)
	}
	if len(want) != 0 {
		t.Errorf("routes not registered: %v", want)
	}
}
