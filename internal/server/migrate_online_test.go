package server

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// onlineImport sends an import through the administration route, as the
// command does when a serving instance holds the data directory lock.
func onlineImport(t *testing.T, s *Server, url string, req api.ImportRequest) (api.ImportResult, error) {
	t.Helper()
	c, err := client.New(url, "host-admin")
	if err != nil {
		t.Fatal(err)
	}
	return c.Import(t.Context(), s.adminToken, req)
}

// pauseImport blocks the import at step until the returned release runs.
func pauseImport(t *testing.T, s *Server, step string) (reached <-chan struct{}, release func()) {
	t.Helper()
	at, gate := make(chan struct{}), make(chan struct{})
	released := false
	release = func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(release)
	fired := false
	s.importFault = func(current string) error {
		if current == step && !fired {
			fired = true
			close(at)
			<-gate
		}
		return nil
	}
	return at, release
}

func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not happen", what)
		panic("unreachable")
	}
}

func TestOnlineImportKeepsOtherProjectsServing(t *testing.T) {
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	c, _ := client.New(h.URL, "host-a")
	other, err := c.Register(t.Context(), "other", "")
	if err != nil {
		t.Fatal(err)
	}
	reached, release := pauseImport(t, s, "copied")
	done := make(chan error, 1)
	var result api.ImportResult
	go func() {
		var err error
		result, err = onlineImport(t, s, h.URL, api.ImportRequest{Project: "demo", From: source})
		done <- err
	}()
	waitFor(t, reached, "import copy")
	served := make(chan error, 1)
	go func() {
		p := c.Project(other.ID)
		if _, err := p.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "side", Title: "Side", Language: "en"}); err != nil {
			served <- err
			return
		}
		list, err := p.List(t.Context(), "")
		if err == nil && len(list.Tasks) != 1 {
			err = errors.New("other project lost its task")
		}
		served <- err
	}()
	if err := waitFor(t, served, "other project requests"); err != nil {
		t.Fatalf("other project during import: %v", err)
	}
	release()
	if err := waitFor(t, done, "online import"); err != nil {
		t.Fatal(err)
	}
	if result.Tasks != 2 || result.Existing || result.Source != source || result.Project.Name != "demo" {
		t.Fatalf("result=%+v", result)
	}
	if !s.ready.Load() {
		t.Fatal("server left the ready state")
	}
	list, err := c.Project(result.Project.ID).List(t.Context(), "")
	if err != nil || len(list.Tasks) != 2 {
		t.Fatalf("imported tasks: %+v %v", list.Tasks, err)
	}
}

func TestOnlineImportReadsWaitForPublicationAndEventsReset(t *testing.T) {
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	c, _ := client.New(h.URL, "host-a")
	info, err := c.Register(t.Context(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	before, err := p.List(t.Context(), "")
	if err != nil || len(before.Tasks) != 0 {
		t.Fatalf("empty project: %+v %v", before, err)
	}
	polled := make(chan api.EventPage, 1)
	go func() {
		page, _ := p.Events(t.Context(), before.Cursor, 50*time.Second)
		polled <- page
	}()
	// The long poll must be waiting before the publication wakes it.
	time.Sleep(200 * time.Millisecond)
	reached, release := pauseImport(t, s, "backup-renamed")
	done := make(chan error, 1)
	go func() {
		_, err := onlineImport(t, s, h.URL, api.ImportRequest{Project: "demo", From: source})
		done <- err
	}()
	waitFor(t, reached, "publication window")
	project, err := s.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(project.board); !os.IsNotExist(err) {
		t.Fatalf("the window must have no board: %v", err)
	}
	read := make(chan api.TaskList, 1)
	go func() {
		list, _ := p.List(t.Context(), "")
		read <- list
	}()
	select {
	case list := <-read:
		t.Fatalf("a read returned inside the publication window: %+v", list)
	case <-time.After(200 * time.Millisecond):
	}
	published := time.Now()
	release()
	if err := waitFor(t, done, "online import"); err != nil {
		t.Fatal(err)
	}
	if list := waitFor(t, read, "queued read"); len(list.Tasks) != 2 {
		t.Fatalf("queued read: %+v", list)
	}
	if page := waitFor(t, polled, "long poll"); !page.Reset || time.Since(published) > 5*time.Second {
		t.Fatalf("long poll must answer reset at publication: %+v after %s", page, time.Since(published))
	}
	if page, err := p.Events(t.Context(), before.Cursor, 0); err != nil || !page.Reset {
		t.Fatalf("an old cursor must read reset: %+v %v", page, err)
	}
}

func TestOnlineImportRecoversAFailedPublicationInProcess(t *testing.T) {
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	failed := false
	s.importFault = func(step string) error {
		if step == "published" && !failed {
			failed = true
			return errors.New("publication step failed")
		}
		return nil
	}
	if _, err := onlineImport(t, s, h.URL, api.ImportRequest{Project: "demo", From: source}); err == nil {
		t.Fatal("the failure must still be reported")
	}
	again, err := onlineImport(t, s, h.URL, api.ImportRequest{Project: "demo", From: source})
	if err != nil || !again.Existing || again.Tasks != 2 {
		t.Fatalf("recovered publication: %+v %v", again, err)
	}
	p, err := s.project(again.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(p.root, "control", "import-pending.json")); !os.IsNotExist(err) {
		t.Fatalf("intent left after in-process recovery: %v", err)
	}
}

func TestOnlineImportFailureClosesTheProjectWithoutTouchingItsBoard(t *testing.T) {
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	c, _ := client.New(h.URL, "host-a")
	c.Retries = 0
	info, err := c.Register(t.Context(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	project, err := s.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	queuedRead, queuedWrite := make(chan error, 1), make(chan error, 1)
	s.importFault = func(step string) error {
		if step != "backup-renamed" {
			return nil
		}
		// Requests queue behind the import, which holds the project lock.
		go func() { _, err := p.List(t.Context(), ""); queuedRead <- err }()
		go func() {
			_, err := p.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "late", Title: "Late", Language: "en"})
			queuedWrite <- err
		}()
		time.Sleep(200 * time.Millisecond)
		// A changed staging tree makes in-process recovery refuse too.
		if err := os.WriteFile(filepath.Join(project.root, "board.import", "tampered.md"), []byte("x\n"), 0o600); err != nil {
			t.Error(err)
		}
		return errors.New("publication step failed")
	}
	if _, err := onlineImport(t, s, h.URL, api.ImportRequest{Project: "demo", From: source}); err == nil {
		t.Fatal("failed publication reported success")
	}
	for name, ch := range map[string]chan error{"queued read": queuedRead, "queued write": queuedWrite} {
		if err := waitFor(t, ch, name); !api.IsCode(err, api.CodeNotReady) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := p.List(t.Context(), ""); !api.IsCode(err, api.CodeNotReady) {
		t.Fatalf("later read: %v", err)
	}
	if _, err := onlineImport(t, s, h.URL, api.ImportRequest{Project: "demo", From: source}); !api.IsCode(err, api.CodeNotReady) {
		t.Fatalf("later import: %v", err)
	}
	if _, err := os.Lstat(project.board); !os.IsNotExist(err) {
		t.Fatalf("a request created the board path: %v", err)
	}
}

func TestImportIntoAProjectWithTasksDigestsTheSourceWithoutTheProjectLock(t *testing.T) {
	source := v1Board(t)
	s, err := Open(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.Import("demo", "", source)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.project(first.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	digests := 0
	s.importFault = func(step string) error {
		if step == "precheck-digest" {
			digests++
			if !p.mu.TryLock() {
				return errors.New("source digested under the project lock")
			}
			p.mu.Unlock()
		}
		return nil
	}
	moved := filepath.Join(t.TempDir(), "kanban")
	if err := os.CopyFS(moved, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	again, err := s.Import("demo", "", moved)
	if err != nil || !again.Existing || again.Source != moved || again.Snapshot != first.Snapshot {
		t.Fatalf("same snapshot elsewhere: %+v %v", again, err)
	}
	if _, err := board.NewTask(moved, "chore", "later", "Later", "en", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import("demo", "", moved); !api.IsCode(err, api.CodeProjectNotEmpty) {
		t.Fatalf("changed snapshot: %v", err)
	}
	if digests != 2 {
		t.Fatalf("digests=%d", digests)
	}
	other, err := s.Import("plain", "", v1Board(t))
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.project(other.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(q.root, "control", "import.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import("plain", "", source); !api.IsCode(err, api.CodeProjectNotEmpty) || digests != 2 {
		t.Fatalf("a project with tasks and no import record: %v digests=%d", err, digests)
	}
}

func TestImportAfterAnEmptyBoardImportKeepsTheFullFlow(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "kanban")
	if err := board.InitStorage(empty); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if result, err := s.Import("demo", "", empty); err != nil || result.Tasks != 0 {
		t.Fatalf("empty import: %+v %v", result, err)
	}
	source := v1Board(t)
	result, err := s.Import("demo", "", source)
	if err != nil || result.Existing || result.Tasks != 2 || result.Source != source {
		t.Fatalf("second import: %+v %v", result, err)
	}
}

func TestImportRouteAuthenticationAndHandshake(t *testing.T) {
	var log bytes.Buffer
	s, err := Open(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewJSONHandler(&log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	handler := s.Handler()
	send := func(peer, token, protocol, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/server/import", strings.NewReader(body))
		req.RemoteAddr = peer
		req.Header.Set(api.HeaderAdminToken, token)
		if protocol != "" {
			req.Header.Set(api.HeaderProtocol, protocol)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	protocol := strconv.Itoa(api.ProtocolVersion)
	body := `{"project":"demo","from":"` + filepath.ToSlash(t.TempDir()) + `"}`
	for _, tc := range []struct{ peer, token string }{{"192.0.2.1:4567", s.adminToken}, {"127.0.0.1:4567", ""}, {"127.0.0.1:4567", strings.Repeat("a", 64)}} {
		out := send(tc.peer, tc.token, protocol, body)
		if out.Code != http.StatusForbidden || strings.Contains(out.Body.String(), s.adminToken) {
			t.Fatalf("refusal %s=%d %s", tc.peer, out.Code, out.Body.String())
		}
	}
	if out := send("127.0.0.1:4567", s.adminToken, "", body); out.Code != http.StatusPreconditionFailed || !strings.Contains(out.Body.String(), api.CodeProtocolIncompatible) {
		t.Fatalf("handshake=%d %s", out.Code, out.Body.String())
	}
	if out := send("127.0.0.1:4567", s.adminToken, protocol, `{"project":"demo","from":"kanban"}`); out.Code != http.StatusBadRequest {
		t.Fatalf("relative source=%d %s", out.Code, out.Body.String())
	}
	if strings.Contains(log.String(), s.adminToken) {
		t.Fatal("token logged")
	}
	if _, err := s.projectByName("demo"); err == nil {
		t.Fatal("a refused import registered a project")
	}
}

func TestImportCommandUsesTheServingInstance(t *testing.T) {
	source := v1Board(t)
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	defer h.Close()
	defer s.Close()
	useImportTarget(t, h.URL)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := importBoard(dir, "demo", "", source, logger)
	if err != nil || result.Tasks != 2 || result.Source != source {
		t.Fatalf("online command: %+v %v", result, err)
	}
	if _, err := s.projectByName("demo"); err != nil {
		t.Fatalf("the serving instance did not import: %v", err)
	}
}

func TestImportCommandExplainsWhyItCannotImportOnline(t *testing.T) {
	source := v1Board(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	locked := t.TempDir()
	holder, err := Open(Options{DataDir: locked})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	useImportTarget(t, closed.URL)
	if _, err := importBoard(locked, "demo", "", source, logger); err == nil || !strings.Contains(err.Error(), locked) || !strings.Contains(err.Error(), closed.URL) {
		t.Fatalf("no server: %v", err)
	}

	useImportTarget(t, "http://example.invalid:7878")
	if _, err := importBoard(locked, "demo", "", source, logger); err == nil || !strings.Contains(err.Error(), "http://example.invalid:7878") {
		t.Fatalf("non-local: %v", err)
	}

	serving, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer serving.Close()
	useImportTarget(t, h.URL)
	if _, err := importBoard(locked, "demo", "", source, logger); err == nil || !strings.Contains(err.Error(), locked) || !strings.Contains(err.Error(), serving.data) {
		t.Fatalf("other data directory: %v", err)
	}
	if _, err := serving.projectByName("demo"); err == nil {
		t.Fatal("a mismatched server imported")
	}

	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/server" {
			writeJSONResponse(w, http.StatusOK, api.ServerInfo{DataDir: locked})
			return
		}
		http.NotFound(w, r)
	}))
	defer old.Close()
	useImportTarget(t, old.URL)
	if _, err := importBoard(locked, "demo", "", source, logger); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("old server: %v", err)
	}
}

func useImportTarget(t *testing.T, url string) {
	t.Helper()
	previous := importTarget
	importTarget = func() (string, error) { return url, nil }
	t.Cleanup(func() { importTarget = previous })
}

func TestImportSourceLocation(t *testing.T) {
	t.Setenv(board.EnvBoardDir, "")
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v %s", err, out)
	}
	kanban := filepath.Join(repo, "kanban")
	if err := board.InitStorage(kanban); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(kanban)
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{repo, sub} {
		t.Chdir(cwd)
		got, err := importSource("")
		if err != nil {
			t.Fatalf("%s: %v", cwd, err)
		}
		if resolved, _ := filepath.EvalSymlinks(got); resolved != want {
			t.Fatalf("%s: located %s, want %s", cwd, got, want)
		}
	}
	t.Chdir(sub)
	if got, err := importSource("../../kanban"); err != nil || !filepath.IsAbs(got) {
		t.Fatalf("relative --from: %s %v", got, err)
	}

	explicit := filepath.Join(t.TempDir(), "kanban")
	if err := board.InitStorage(explicit); err != nil {
		t.Fatal(err)
	}
	t.Setenv(board.EnvBoardDir, explicit)
	if got, err := importSource(""); err != nil || got != explicit {
		t.Fatalf("KANBAN_DIR: %s %v", got, err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv(board.EnvBoardDir, missing)
	if _, err := importSource(""); err == nil || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("missing KANBAN_DIR: %v", err)
	}

	t.Setenv(board.EnvBoardDir, "")
	t.Chdir(t.TempDir())
	if _, err := importSource(""); err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("no board anywhere: %v", err)
	}
}
