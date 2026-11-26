package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// openLogged serves dir and keeps the server log. requestContext returns the
// context of the latest administration import request.
func openLogged(t *testing.T, dir string) (s *Server, url string, logs *syncBuffer, requestContext func() context.Context) {
	t.Helper()
	logs = &syncBuffer{}
	s, err := Open(Options{DataDir: dir, Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	request := context.Background()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/server/import" {
			mu.Lock()
			request = r.Context()
			mu.Unlock()
		}
		s.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(func() { h.Close(); s.Close() })
	return s, h.URL, logs, func() context.Context {
		mu.Lock()
		defer mu.Unlock()
		return request
	}
}

// offlineServer makes the offline command open its server through setup.
func offlineServer(t *testing.T, setup func(*Server)) {
	t.Helper()
	previous := importOpen
	t.Cleanup(func() { importOpen = previous })
	importOpen = func(o Options) (*Server, error) {
		s, err := Open(o)
		if err == nil {
			setup(s)
		}
		return s, err
	}
}

// holdCopyPastLockWait makes the first copied file wait three lock-wait
// limits, so a fixed import deadline equal to that limit would expire.
func holdCopyPastLockWait(s *Server) {
	s.importLockWait = 50 * time.Millisecond
	fired := false
	s.importFault = func(step string) error {
		if step == "copying" && !fired {
			fired = true
			time.Sleep(3 * s.importLockWait)
		}
		return nil
	}
}

func assertNoStaging(t *testing.T, data, project string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(projectRoot(data, project), "board.import")); !os.IsNotExist(err) {
		t.Fatalf("board.import remains: %v", err)
	}
}

func assertNothingPublished(t *testing.T, s *Server, project string) {
	t.Helper()
	info, err := s.projectByName(project)
	if err != nil {
		return
	}
	p, err := s.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view, err := board.LoadBoard(p.board); err != nil || len(view.Entries) != 0 {
		t.Fatalf("a failed import published: %+v %v", view.Entries, err)
	}
}

func wantImportCanceled(t *testing.T, err error, reason string) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) || e.Code != api.CodeImportCanceled || e.Status != http.StatusServiceUnavailable ||
		!strings.Contains(e.Message, reason) || !strings.Contains(e.Message, "nothing was published; run the same command again") {
		t.Fatalf("want import_canceled (%s), got %#v", reason, err)
	}
}

func TestImportScanAndCopyOutlastTheLockWaitLimit(t *testing.T) {
	t.Run("in process", func(t *testing.T) {
		s, h := openTest(t, t.TempDir(), nil)
		defer h.Close()
		defer s.Close()
		holdCopyPastLockWait(s)
		if result, err := s.Import("demo", "", v1Board(t)); err != nil || result.Tasks != 2 {
			t.Fatalf("import: %+v %v", result, err)
		}
	})
	t.Run("loopback", func(t *testing.T) {
		s, url, _, _ := openLogged(t, t.TempDir())
		holdCopyPastLockWait(s)
		if result, err := onlineImport(t, s, url, api.ImportRequest{Project: "demo", From: v1Board(t)}); err != nil || result.Tasks != 2 {
			t.Fatalf("import: %+v %v", result, err)
		}
	})
	t.Run("offline", func(t *testing.T) {
		offlineServer(t, holdCopyPastLockWait)
		if code, stdout, stderr := runImportCommand(t, t.TempDir(), v1Board(t), make(chan os.Signal, 1)); code != 0 || !strings.Contains(stdout.String(), `"tasks": 2`) {
			t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
		}
	})
}

// holdSourceLock takes the source board lock once the import recovered the
// source, so only the snapshot lock wait can expire.
func holdSourceLock(t *testing.T, s *Server, source string) {
	t.Helper()
	release := make(chan struct{})
	finished := make(chan struct{})
	t.Cleanup(func() {
		close(release)
		<-finished
	})
	s.importLockWait = 100 * time.Millisecond
	s.importFault = func(step string) error {
		if step != "recovered" {
			return nil
		}
		held := make(chan struct{})
		go func() {
			defer close(finished)
			_ = board.WithTransactionContext(context.Background(), source, board.LockScope{ExclusiveBoard: true, ReadOnly: true}, func(*board.Transaction) error {
				close(held)
				<-release
				return nil
			})
		}()
		<-held
		return nil
	}
}

func TestImportLockWaitEndsAsImportCanceled(t *testing.T) {
	t.Run("in process", func(t *testing.T) {
		data := t.TempDir()
		s, _, logs, _ := openLogged(t, data)
		source := v1Board(t)
		holdSourceLock(t, s, source)
		_, err := s.Import("demo", "", source)
		wantImportCanceled(t, err, "waiting for the source board lock took longer than 100ms")
		assertNothingPublished(t, s, "demo")
		assertNoStaging(t, data, "demo")
		logs.waitContains(t, `"msg":"board import canceled","project_name":"demo","reason":"waiting for the source board lock took longer than 100ms"`)
	})
	t.Run("offline", func(t *testing.T) {
		source := v1Board(t)
		offlineServer(t, func(s *Server) { holdSourceLock(t, s, source) })
		code, stdout, stderr := runImportCommand(t, t.TempDir(), source, make(chan os.Signal, 1))
		if code != 1 || stdout.String() != "" || !strings.Contains(stderr.String(), `"reason":"waiting for the source board lock took longer than 100ms"`) ||
			!strings.Contains(stderr.String(), api.CodeImportCanceled) {
			t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
		}
	})
}

func TestLoopbackImportEndsAsImportCanceledWhenTheClientLeaves(t *testing.T) {
	data := t.TempDir()
	s, url, logs, requestContext := openLogged(t, data)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.importFault = func(step string) error {
		if step == "copying" {
			cancel()
			<-requestContext().Done()
		}
		return nil
	}
	c, err := client.New(url, "host-admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ImportWithProgress(ctx, s.adminToken, api.ImportRequest{Project: "demo", From: v1Board(t)}, func(api.ImportProgress) {}); err == nil {
		t.Fatal("a canceled request answered")
	}
	p, _ := s.project(mustProject(t, s, "demo"))
	waitFor(t, importSettled(p), "the server to end the canceled import")
	logs.waitContains(t, `"msg":"board import canceled","project_name":"demo","reason":"`+importCancelCaller+`"`)
	if strings.Contains(logs.String(), "request failed") || strings.Contains(logs.String(), `"msg":"board import failed"`) {
		t.Fatalf("a canceled import was logged as internal:\n%s", logs.String())
	}
	assertNothingPublished(t, s, "demo")
	assertNoStaging(t, data, "demo")
}

// shutDownWhileCopying starts the server shutdown at the first copied file
// and gives the cancellation time to reach the import.
func shutDownWhileCopying(s *Server) {
	fired := false
	s.importFault = func(step string) error {
		if step == "copying" && !fired {
			fired = true
			s.shutdownCancel()
			time.Sleep(200 * time.Millisecond)
		}
		return nil
	}
}

func TestLoopbackImportEndsAsImportCanceledWhenTheServerShutsDown(t *testing.T) {
	data := t.TempDir()
	s, url, logs, _ := openLogged(t, data)
	shutDownWhileCopying(s)
	_, err := onlineImport(t, s, url, api.ImportRequest{Project: "demo", From: v1Board(t)})
	wantImportCanceled(t, err, importCancelShutdown)
	logs.waitContains(t, `"reason":"`+importCancelShutdown+`"`)
	assertNothingPublished(t, s, "demo")
	assertNoStaging(t, data, "demo")
}

func TestFailedImportRemovesItsStagingOnEveryPath(t *testing.T) {
	const failure = "injected copy failure 7f3c"
	failCopy := func(s *Server) {
		s.importFault = func(step string) error {
			if step == "copied" {
				return errors.New(failure)
			}
			return nil
		}
	}
	t.Run("in process", func(t *testing.T) {
		data := t.TempDir()
		s, _, logs, _ := openLogged(t, data)
		failCopy(s)
		if _, err := s.Import("demo", "", v1Board(t)); err == nil || !strings.Contains(err.Error(), failure) {
			t.Fatalf("import: %v", err)
		}
		assertNoStaging(t, data, "demo")
		logs.waitContains(t, failure)
	})
	t.Run("loopback", func(t *testing.T) {
		data := t.TempDir()
		s, url, logs, _ := openLogged(t, data)
		failCopy(s)
		if _, err := onlineImport(t, s, url, api.ImportRequest{Project: "demo", From: v1Board(t)}); err == nil {
			t.Fatal("a failed import answered")
		}
		assertNoStaging(t, data, "demo")
		logs.waitContains(t, failure)
	})
	t.Run("offline", func(t *testing.T) {
		data := t.TempDir()
		offlineServer(t, failCopy)
		code, _, stderr := runImportCommand(t, data, v1Board(t), make(chan os.Signal, 1))
		if code != 1 || !strings.Contains(stderr.String(), `"msg":"board import failed","project_name":"demo","error":"`+failure) {
			t.Fatalf("exit %d\n%s", code, stderr.String())
		}
		assertNoStaging(t, data, "demo")
	})
	t.Run("upload", func(t *testing.T) {
		data := t.TempDir()
		s, url, logs, _ := openLogged(t, data)
		failCopy(s)
		_, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: v1Board(t)})
		var e *api.Error
		if !errors.As(err, &e) || e.Code != api.CodeInternal || strings.Contains(e.Message, failure) {
			t.Fatalf("upload: %#v", err)
		}
		assertNoStaging(t, data, "demo")
		assertUploadClean(t, data, "demo")
		logs.waitContains(t, `"msg":"board import failed","project_name":"demo","error":"`+failure)
	})
}

func TestServerStartRemovesStagingWithoutAPublicationIntent(t *testing.T) {
	data := t.TempDir()
	s, h := openTest(t, data, nil)
	if _, err := s.Import("demo", "", v1Board(t)); err != nil {
		t.Fatal(err)
	}
	h.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(projectRoot(data, "demo"), "board.import")
	if err := os.MkdirAll(filepath.Join(stale, "backlog"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "backlog", "left.md"), []byte("left behind"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, h := openTest(t, data, nil)
	defer h.Close()
	defer reopened.Close()
	assertNoStaging(t, data, "demo")
	if _, err := reopened.projectByName("demo"); err != nil {
		t.Fatal(err)
	}
}
