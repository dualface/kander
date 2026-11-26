package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// gcClock is a test clock the server consults for receipts and the gc.
type gcClock struct{ at atomic.Int64 }

func (c *gcClock) now() time.Time          { return time.Unix(0, c.at.Load()) }
func (c *gcClock) advance(d time.Duration) { c.at.Add(int64(d)) }

type gcFixture struct {
	t     *testing.T
	s     *Server
	http  *httptest.Server
	clock *gcClock
}

func newGCFixture(t *testing.T) *gcFixture {
	t.Helper()
	c := &gcClock{}
	c.at.Store(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC).UnixNano())
	s, err := Open(Options{DataDir: t.TempDir(), Now: c.now, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	t.Cleanup(func() { h.Close(); s.Close() })
	return &gcFixture{t: t, s: s, http: h, clock: c}
}

func (f *gcFixture) project(name string) *client.Project {
	f.t.Helper()
	c, err := client.New(f.http.URL, "gc-host")
	if err != nil {
		f.t.Fatal(err)
	}
	c.Backoff = time.Millisecond
	p, err := c.Register(f.t.Context(), name, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return c.Project(p.ID)
}

// gc sends one gc request through the client with the administration token.
func (f *gcFixture) gc(req api.ServerGCRequest) (api.ServerGCReport, error) {
	c, err := client.New(f.http.URL, "gc-host")
	if err != nil {
		f.t.Fatal(err)
	}
	return c.ServerGC(f.t.Context(), f.s.adminToken, req)
}

func gcTodoTask(t *testing.T, p *client.Project, slug, group string) api.Task {
	t.Helper()
	ctx := t.Context()
	task, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: slug, Title: "Demo " + slug, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if group != "" {
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nCARD_REVIEW: PASS\n", 1)
	}
	if task, err = p.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if task, err = p.Move(ctx, "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	return task
}

// receiptFiles maps each receipt file of the project to its operation and status.
func receiptFiles(t *testing.T, s *Server, name string) map[string]string {
	t.Helper()
	info, err := s.projectByName(name)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.project(info.ID)
	dir := filepath.Join(p.root, servicePath("receipts"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var r receipt
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = r.Status + " " + r.Operation
	}
	return out
}

func notServerError(t *testing.T, label string, err error) {
	t.Helper()
	var e *api.Error
	if errors.As(err, &e) && e.Status >= 500 {
		t.Fatalf("%s: a retry after the receipt was removed must not fail with a 5xx: %v", label, err)
	}
	if client.IsUnreachable(err) {
		t.Fatalf("%s: %v", label, err)
	}
}

func dataFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func gcRequest(r *http.Request, peer, token string, body api.ServerGCRequest) *http.Request {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/server/gc", bytes.NewReader(data))
	req.RemoteAddr = peer
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	req.Header.Set(api.HeaderAdminToken, token)
	if r != nil {
		req = req.WithContext(r.Context())
	}
	return req
}

func TestServerGCRouteRequiresTheAdministrator(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	log := bytes.Repeat([]byte("{\"line\":\"x\"}\n"), (2<<20)/13+1)
	if err := os.WriteFile(filepath.Join(f.s.data, "server.log"), log, 0o600); err != nil {
		t.Fatal(err)
	}
	before := dataFiles(t, f.s.data)
	request := api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20}
	for _, tc := range []struct{ peer, token string }{{"192.0.2.1:4567", f.s.adminToken}, {"127.0.0.1:4567", ""}, {"127.0.0.1:4567", strings.Repeat("a", 64)}} {
		out := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(out, gcRequest(nil, tc.peer, tc.token, request))
		if out.Code != http.StatusForbidden || strings.Contains(out.Body.String(), f.s.adminToken) {
			t.Fatalf("%s %q: refusal = %d %s", tc.peer, tc.token, out.Code, out.Body.String())
		}
	}
	if after := dataFiles(t, f.s.data); !mapsEqual(before, after) {
		t.Fatal("a refused gc must not change any file")
	}
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, gcRequest(nil, "127.0.0.1:4567", f.s.adminToken, request))
	var report api.ServerGCReport
	if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &report) != nil || report.Log == nil || report.Log.Bytes == 0 {
		t.Fatalf("authenticated gc = %d %s", out.Code, out.Body.String())
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func TestServerGCRefusesASecondGCAndInvalidRequests(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	before := dataFiles(t, f.s.data)
	f.s.gcBusy.Store(true)
	_, err := f.gc(api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20})
	if !api.IsCode(err, api.CodeGCRunning) {
		t.Fatalf("a second gc must be refused with gc_running, got %v", err)
	}
	f.s.gcBusy.Store(false)
	for _, tc := range []struct {
		request api.ServerGCRequest
		code    string
	}{
		{api.ServerGCRequest{OlderThan: "59m", LogKeep: 1 << 20}, api.CodeInvalidRequest},
		{api.ServerGCRequest{OlderThan: "7 days", LogKeep: 1 << 20}, api.CodeInvalidRequest},
		{api.ServerGCRequest{OlderThan: "1h", LogKeep: 1<<20 - 1}, api.CodeInvalidRequest},
		{api.ServerGCRequest{Project: "bad name", OlderThan: "1h", LogKeep: 1 << 20}, api.CodeInvalidRequest},
		{api.ServerGCRequest{Project: "unknown", OlderThan: "1h", LogKeep: 1 << 20}, api.CodeNotFound},
	} {
		if _, err := f.gc(tc.request); !api.IsCode(err, tc.code) {
			t.Fatalf("%+v: want %s, got %v", tc.request, tc.code, err)
		}
	}
	if after := dataFiles(t, f.s.data); !mapsEqual(before, after) {
		t.Fatal("a refused gc must not change any file")
	}
}

func TestServerGCCompactsWithTheRetentionAndReportsKeptRuns(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	f.project("other")
	var calls []board.CompactionOptions
	defer func(previous func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error)) {
		gcCompact = previous
	}(gcCompact)
	gcCompact = func(_ context.Context, _ string, o board.CompactionOptions) (board.CompactionReport, error) {
		calls = append(calls, o)
		return board.CompactionReport{
			Runs: []board.CompactedRun{{RunID: "r1", Files: 3, Bytes: 30}}, Files: 3, Bytes: 30,
			Skipped: []board.SkippedRun{
				{RunID: "unfinished", Conditions: []board.CompactionCondition{board.CompactionRunFinal}, Reason: "not final"},
				{RunID: "busy", Conditions: []board.CompactionCondition{board.CompactionRunLock}, Reason: "lock held"},
				{RunID: "open", Conditions: []board.CompactionCondition{board.CompactionRunFinal, board.CompactionMembersFinal}, Reason: "members open"},
				{RunID: "recent", Conditions: []board.CompactionCondition{board.CompactionRetention}, Reason: "too recent"},
			},
			Warnings: []string{"runs/x: invalid intent"},
		}, nil
	}
	report, err := f.gc(api.ServerGCRequest{Project: "demo", DryRun: true, OlderThan: "36h", LogKeep: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].OlderThan != 36*time.Hour || !calls[0].DryRun || calls[0].KanderVersion != "test" {
		t.Fatalf("compaction calls = %+v", calls)
	}
	if len(report.Projects) != 1 || report.Projects[0].Name != "demo" || report.Log != nil {
		t.Fatalf("--project must cover only that project and leave the log alone: %+v", report)
	}
	compaction := report.Projects[0].Compaction
	kept := []string{}
	for _, k := range compaction.Kept {
		kept = append(kept, k.ID)
	}
	if compaction.Runs != 1 || compaction.Files != 3 || compaction.Bytes != 30 || !slices.Equal(kept, []string{"unfinished", "busy"}) || len(report.Projects[0].Warnings) != 1 {
		t.Fatalf("compaction = %+v, warnings %v", compaction, report.Projects[0].Warnings)
	}
	if !report.Incomplete() {
		t.Fatal("kept runs must make the gc incomplete")
	}
}

func TestReceiptBindings(t *testing.T) {
	for operation, want := range map[string]receiptBinding{
		"move:20261101-a-task":                 {"task", "20261101-a-task"},
		"put:20261101-a-task:spec.md":          {"task", "20261101-a-task"},
		"dispatch:20261101-a-task:accept":      {"task", "20261101-a-task"},
		"answer:20261101-a-task:req-1":         {"task", "20261101-a-task"},
		"decision-leader:20261101-a-task":      {"task", "20261101-a-task"},
		"group-claim:20261101-g-group":         {"group", "20261101-g-group"},
		"group-remove-member:20261101-g-group": {"group", "20261101-g-group"},
		"review-runtime:run-1:finalize":        {"run", "run-1"},
		"review-publish:run-1":                 {"run", "run-1"},
		"new":                                  {},
		"issue-import":                         {},
		"issue-result-save:github.com/a/b#1":   {},
		"integration-claim:x":                  {},
		"review-plan":                          {},
		"review-close:batch-1":                 {},
		"move:":                                {},
		"observation:20261101-a-task":          {},
		"group-observation:20261101-g-group":   {},
	} {
		if got := bindingOf(operation); got != want {
			t.Errorf("%s: %+v, want %+v", operation, got, want)
		}
	}
	if !livenessReceipt("observation:x") || !livenessReceipt("group-observation:g") || livenessReceipt("group-claim:g") {
		t.Fatal("liveness operations")
	}
}

// writeReceipt stores a receipt the way the server does.
func writeReceipt(t *testing.T, s *Server, name string, r receipt) string {
	t.Helper()
	info, _ := s.projectByName(name)
	p, _ := s.project(info.ID)
	if err := writeJSON(p.root, receiptPath(r.Key), r); err != nil {
		t.Fatal(err)
	}
	return filepath.Base(receiptPath(r.Key))
}

func TestServerGCExpiresReceipts(t *testing.T) {
	f := newGCFixture(t)
	p := f.project("demo")
	ctx := t.Context()
	// A: claimed, observed, then archived. C: claimed and observed, still working. B: open in todo.
	a := gcTodoTask(t, p, "alpha", "20261101-closed-group")
	grantA, err := p.Claim(ctx, "", a.TaskID, api.ClaimRequest{ClientID: "gc-host", ExecutionID: "exec-a", Owner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	proofA := client.Claim{Token: grantA.Token, Generation: grantA.Claim.Generation}
	observeA := api.AgentObservation{Status: "alive"}
	if _, err := p.ObserveAgent(ctx, "observe-a", a.TaskID, observeA, proofA); err != nil {
		t.Fatal(err)
	}
	archive := api.Move{State: "archived", Result: "cancelled", Reason: "test", Decision: "user"}
	if _, err := p.Move(ctx, "archive-a", a.TaskID, archive, &proofA); err != nil {
		t.Fatal(err)
	}
	c := gcTodoTask(t, p, "gamma", "")
	grantC, err := p.Claim(ctx, "", c.TaskID, api.ClaimRequest{ClientID: "gc-host", ExecutionID: "exec-c", Owner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	proofC := client.Claim{Token: grantC.Token, Generation: grantC.Claim.Generation}
	observeC := api.AgentObservation{Status: "alive"}
	if _, err := p.ObserveAgent(ctx, "observe-c", c.TaskID, observeC, proofC); err != nil {
		t.Fatal(err)
	}
	b := gcTodoTask(t, p, "beta", "20261101-open-group")
	old := f.clock.now().UTC()
	closedGroup := writeReceipt(t, f.s, "demo", receipt{Key: "group-closed", Operation: "group-claim:20261101-closed-group", Digest: "d", Status: receiptDone, CreatedAt: old})
	for key, operation := range map[string]string{"group-open": "group-claim:20261101-open-group", "group-empty": "group-claim:20261101-empty-group", "run-gone": "review-publish:run-gone"} {
		writeReceipt(t, f.s, "demo", receipt{Key: key, Operation: operation, Digest: "d", Status: receiptDone, CreatedAt: old})
	}
	pending := writeReceipt(t, f.s, "demo", receipt{Schema: receiptSchema, Key: "pending-a", Operation: "move:" + a.TaskID, Digest: "d", Status: receiptPending, CreatedAt: f.clock.now().UTC()})
	initial := receiptFiles(t, f.s, "demo")
	count := func(match func(string) bool) int {
		n := 0
		for _, v := range initial {
			if match(v) {
				n++
			}
		}
		return n
	}
	boundA := count(func(v string) bool {
		return strings.HasPrefix(v, "done ") && !strings.Contains(v, "observation:") && (strings.HasSuffix(v, ":"+a.TaskID) || strings.Contains(v, ":"+a.TaskID+":"))
	})
	if boundA < 3 || count(func(v string) bool { return strings.Contains(v, "observation:") }) != 2 {
		t.Fatalf("unexpected receipts: %v", initial)
	}
	request := api.ServerGCRequest{OlderThan: "48h", LogKeep: 1 << 20}

	// 23 hours: no liveness receipt is due yet, and nothing else is.
	f.clock.advance(23 * time.Hour)
	report, err := f.gc(request)
	if err != nil || report.Projects[0].Receipts.Files != 0 || !mapsEqual(initial, receiptFiles(t, f.s, "demo")) {
		t.Fatalf("23h: %+v %v", report, err)
	}

	// 25 hours: both liveness receipts go; the dry run plans exactly what the run removes.
	f.clock.advance(2 * time.Hour)
	request.DryRun = true
	plan, err := f.gc(request)
	if err != nil || plan.Projects[0].Receipts.Files != 2 || !mapsEqual(initial, receiptFiles(t, f.s, "demo")) {
		t.Fatalf("25h plan: %+v %v", plan, err)
	}
	request.DryRun = false
	report, err = f.gc(request)
	if err != nil || report.Projects[0].Receipts.Files != plan.Projects[0].Receipts.Files || report.Projects[0].Receipts.Bytes != plan.Projects[0].Receipts.Bytes || report.Incomplete() {
		t.Fatalf("25h run: %+v %v (plan %+v)", report, err, plan)
	}
	for _, v := range receiptFiles(t, f.s, "demo") {
		if strings.Contains(v, "observation:") {
			t.Fatalf("a liveness receipt older than 24 hours must go: %v", v)
		}
	}
	_, err = p.ObserveAgent(ctx, "observe-c", c.TaskID, observeC, proofC)
	notServerError(t, "replayed liveness receipt", err)

	// 49 hours: the receipts bound to the archived card go; the open card, the pending
	// receipt, and unbound creation receipts stay.
	f.clock.advance(24 * time.Hour)
	report, err = f.gc(request)
	// The replayed observation wrote a new liveness receipt at 25 hours; it is due as
	// well, and so is the receipt of the group whose only member is archived.
	if err != nil || report.Projects[0].Receipts.Files != boundA+2 || report.Incomplete() {
		t.Fatalf("49h: %+v %v (want %d)", report, err, boundA+2)
	}
	left := receiptFiles(t, f.s, "demo")
	if left[pending] != "pending move:"+a.TaskID {
		t.Fatal("a pending receipt must stay")
	}
	if _, ok := left[closedGroup]; ok {
		t.Fatal("the receipt of a group whose members are all terminal must go")
	}
	for _, operation := range []string{"group-claim:20261101-open-group", "group-claim:20261101-empty-group", "review-publish:run-gone"} {
		if !slices.Contains(slices.Collect(mapValues(left)), "done "+operation) {
			t.Fatalf("%s must stay: an open group, a group without members, or a run that is gone binds nothing terminal", operation)
		}
	}
	for _, v := range left {
		if strings.HasPrefix(v, "done ") && (strings.HasSuffix(v, ":"+a.TaskID) || strings.Contains(v, ":"+a.TaskID+":")) {
			t.Fatalf("a done receipt bound to the archived card must go: %v", v)
		}
	}
	if !slices.ContainsFunc(slices.Collect(mapValues(left)), func(v string) bool { return v == "done move:"+b.TaskID }) {
		t.Fatalf("receipts of an open card must stay: %v", left)
	}
	if !slices.Contains(slices.Collect(mapValues(left)), "done new") {
		t.Fatalf("receipts bound to nothing must stay: %v", left)
	}
	_, err = p.Move(ctx, "archive-a", a.TaskID, archive, &proofA)
	notServerError(t, "replayed move receipt", err)

	info, _ := f.s.projectByName("demo")
	project, _ := f.s.project(info.ID)
	if code, _, problems, err := board.CheckBoardForImport(project.board); err != nil || code != 0 {
		t.Fatalf("check after gc: %d %v %v", code, problems, err)
	}
}

func mapValues(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, v := range m {
			if !yield(v) {
				return
			}
		}
	}
}

func TestOfflineGCHoldsTheDataDirectoryLock(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{DataDir: dir, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.register(api.RegisterProject{Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenOfflineGC(dir, "test"); !errors.Is(err, ErrDataDirInUse) {
		t.Fatalf("a held lock must refuse an offline gc, got %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := dataFiles(t, dir)
	offline, err := OpenOfflineGC(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{DataDir: dir, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}); !errors.Is(err, ErrDataDirInUse) {
		t.Fatalf("an offline gc must keep a server from starting, got %v", err)
	}
	report, err := offline.Run(t.Context(), api.ServerGCRequest{DryRun: true, OlderThan: "1h", LogKeep: 1 << 20})
	if err != nil || len(report.Projects) != 1 || report.Projects[0].Name != "demo" || report.Log == nil {
		t.Fatalf("offline dry run: %+v %v", report, err)
	}
	if _, err := offline.Run(t.Context(), api.ServerGCRequest{Project: "unknown", OlderThan: "1h", LogKeep: 1 << 20}); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("an unknown project must be not_found, got %v", err)
	}
	if err := offline.Close(); err != nil {
		t.Fatal(err)
	}
	after := dataFiles(t, dir)
	delete(after, filepath.Join(dir, "server.lock"))
	delete(before, filepath.Join(dir, "server.lock"))
	if !mapsEqual(before, after) {
		t.Fatal("an offline dry run must not change any data file")
	}
}
