package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

type indexTestClock struct{ seconds atomic.Int64 }

func (c *indexTestClock) now() time.Time { return time.Unix(c.seconds.Load(), 0).UTC() }

func TestTaskIndexFaultRefusesQueriesUntilReconciled(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	c, _ := client.New(h.URL, "host-a")
	info, err := c.Register(t.Context(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	task, err := p.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "fault", Title: "Fault recovery", Submitter: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	access, err := s.TaskAccess(t.Context(), info.ID, task.TaskID)
	if access != (api.TaskAccess{Submitter: "alice", DecisionMode: api.DecisionModeLeader, State: "backlog", Revision: task.Revision}) || err != nil {
		t.Fatalf("access before fault: %+v %v", access, err)
	}
	if _, err := s.TaskAccess(t.Context(), info.ID, "20261125-missing-task"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("missing task: %v", err)
	}
	reached, release := make(chan struct{}), make(chan struct{})
	releaseRebuild := sync.OnceFunc(func() { close(release) })
	defer releaseRebuild()
	var once sync.Once
	var failed atomic.Bool
	var attempts atomic.Int32
	s.indexRetryDelay = func(int) time.Duration { return 5 * time.Millisecond }
	s.indexFault = func(_ *project, phase string) error {
		switch phase {
		case "update":
			if !failed.Swap(true) {
				return errors.New("injected SQLite write failure")
			}
		case "rebuild":
			if attempts.Add(1) <= 2 {
				return errors.New("injected rebuild failure")
			}
		case "reconcile":
			// Hold the project lock to prove queries fail without waiting for it.
			once.Do(func() { close(reached); <-release })
		}
		return nil
	}
	next, err := p.Assign(t.Context(), "fault-assignment", task.TaskID, api.Assign{Assignee: "bob", ExpectedRevision: &task.Revision})
	if err != nil || next.Revision != task.Revision+1 {
		t.Fatalf("committed assignment: %+v, %v", next, err)
	}
	waitFor(t, reached, "automatic rebuild after two failures")
	listErr := make(chan error, 1)
	go func() {
		_, err := p.List(t.Context(), "")
		listErr <- err
	}()
	if err := waitFor(t, listErr, "list during reconciliation"); !api.IsCode(err, api.CodeIndexRebuilding) {
		t.Fatalf("list must fail closed: %v", err)
	}
	type accessResult struct {
		access api.TaskAccess
		err    error
	}
	accessDone := make(chan accessResult, 1)
	go func() {
		access, err := s.TaskAccess(t.Context(), info.ID, task.TaskID)
		accessDone <- accessResult{access, err}
	}()
	if result := waitFor(t, accessDone, "task access during reconciliation"); result.access != (api.TaskAccess{}) || !api.IsCode(result.err, api.CodeIndexRebuilding) {
		t.Fatalf("access must fail closed: %+v, %v", result.access, result.err)
	}
	releaseRebuild()
	s.indexWorkers.Wait()
	if listed, err := p.List(t.Context(), ""); err != nil || len(listed.Tasks) != 1 || listed.Tasks[0].Assignee != "bob" {
		t.Fatalf("list after rebuild: %+v, %v", listed, err)
	}
	access, err = s.TaskAccess(t.Context(), info.ID, task.TaskID)
	if err != nil || access.Assignee != "bob" || access.Revision != next.Revision {
		t.Fatalf("access after rebuild: %+v, %v", access, err)
	}
	proj, _ := s.project(info.ID)
	if err := proj.checkIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestTaskIndexStartupRepairsDerivedRowsAndFiles(t *testing.T) {
	for name, damage := range map[string]func(*testing.T, string, string){
		"missing row": func(t *testing.T, path, task string) {
			editTestIndex(t, path, "DELETE FROM tasks WHERE task_id = ?", task)
		},
		"changed row": func(t *testing.T, path, task string) {
			editTestIndex(t, path, "UPDATE tasks SET title='wrong title',assignee='wrong assignee' WHERE task_id = ?", task)
		},
		"orphan row": func(t *testing.T, path, task string) {
			editTestIndex(t, path, "UPDATE tasks SET task_id='20261125-orphan-task' WHERE task_id = ?", task)
		},
		"missing file": func(t *testing.T, path, _ string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		},
		"corrupt file": func(t *testing.T, path, _ string) {
			if err := os.WriteFile(path, []byte("not a SQLite file"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"format mismatch": func(t *testing.T, path, _ string) {
			editTestIndex(t, path, "PRAGMA user_version=999")
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var clock indexTestClock
			clock.seconds.Store(time.Now().Unix())
			s, h := openTest(t, dir, clock.now)
			t.Cleanup(func() { h.Close(); _ = s.Close() })
			p, id := prepareTodo(t, h.URL)
			grant, err := p.Claim(t.Context(), "", id, api.ClaimRequest{BranchMode: "main-develop", ClientID: "host-a", ExecutionID: "exec-a", Owner: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Assign(t.Context(), "", id, api.Assign{Assignee: "alice"}); err != nil {
				t.Fatal(err)
			}
			proj, _ := s.project(p.ID())
			h.Close()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			damage(t, filepath.Join(proj.root, "index.sqlite"), id)
			clock.seconds.Add(int64((2 * DefaultTTL).Seconds()))
			checkpoint := false
			opened, err := Open(Options{DataDir: dir, Now: clock.now, Logger: quietIndexLogger(), indexFault: func(project *project, step string) error {
				if step != "reconcile" {
					return nil
				}
				checkpoint = true
				claim, ok, err := project.readClaim(id)
				if err != nil || !ok || !claim.ExpiresAt.Equal(clock.now().Add(DefaultTTL)) || !claim.RenewedAt.Equal(grant.Claim.RenewedAt) {
					t.Fatalf("reconciliation ran before startup leases settled: %+v %v", claim, err)
				}
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/projects/"+p.ID()+"/tasks", nil)
				req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
				w := httptest.NewRecorder()
				project.server.Handler().ServeHTTP(w, req)
				if w.Code != http.StatusServiceUnavailable || !containsErrorCode(w, api.CodeNotReady) {
					t.Fatalf("domain request during startup: %d %s", w.Code, w.Body)
				}
				return nil
			}})
			if err != nil || !checkpoint {
				t.Fatalf("startup failed to reconcile: %v", err)
			}
			t.Cleanup(func() { _ = opened.Close() })
			project, err := opened.project(p.ID())
			if err != nil {
				t.Fatal(err)
			}
			if err := project.checkIndex(t.Context()); err != nil {
				t.Fatal(err)
			}
			list, err := project.list(t.Context(), api.TaskListOptions{Search: "rash"})
			if err != nil || len(list.Tasks) != 1 || list.Tasks[0].Assignee != "alice" || list.Tasks[0].Claim.Status != api.ClaimActive {
				t.Fatalf("recovered list: %+v %v", list, err)
			}
		})
	}
}

func quietIndexLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func containsErrorCode(w *httptest.ResponseRecorder, code string) bool {
	var out api.Error
	return json.Unmarshal(w.Body.Bytes(), &out) == nil && out.Code == code
}

func editTestIndex(t *testing.T, path, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestTaskIndexClaimOnlyWritesAndClock(t *testing.T) {
	var clock indexTestClock
	clock.seconds.Store(time.Now().Unix())
	s, h := openTest(t, t.TempDir(), clock.now)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, id := prepareTodo(t, h.URL)
	grant, err := p.Claim(t.Context(), "", id, api.ClaimRequest{BranchMode: "main-develop", ClientID: "host-a", ExecutionID: "exec-a", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	project, _ := s.project(p.ID())
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	clock.seconds.Add(10)
	renewal, err := p.Renew(t.Context(), id, proof)
	if err != nil || !renewal.Claim.ExpiresAt.After(grant.Claim.ExpiresAt) {
		t.Fatalf("renewal: %+v %v", renewal, err)
	}
	if err := project.checkIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ObserveAgent(t.Context(), "", id, api.AgentObservation{Status: "alive"}, proof); err != nil {
		t.Fatal(err)
	}
	if err := project.checkIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := p.List(t.Context(), "working")
	if err != nil || len(before.Tasks) != 1 || before.Tasks[0].Claim == nil || before.Tasks[0].Claim.Status != api.ClaimActive {
		t.Fatalf("before expiry: %+v %v", before, err)
	}
	clock.seconds.Store(renewal.Claim.ExpiresAt.Unix())
	after, err := p.List(t.Context(), "working")
	if err != nil || len(after.Tasks) != 1 || after.Tasks[0].Claim == nil || after.Tasks[0].Claim.Status != api.ClaimLapsed {
		t.Fatalf("after expiry: %+v %v", after, err)
	}
	after.Tasks[0].Claim.Status = api.ClaimActive
	after.ObservedAt = before.ObservedAt
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("the clock changed more than claim standing: %+v -> %+v", before, after)
	}
}
