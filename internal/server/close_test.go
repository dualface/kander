package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
)

func TestServerCloseWithConcurrentTaskRequests(t *testing.T) {
	dir := t.TempDir()
	authorized := make(chan struct{}, 3)
	refreshing, release := make(chan struct{}), make(chan struct{})
	releaseWrite := sync.OnceFunc(func() { close(release) })
	pauseRefresh := sync.OnceFunc(func() {
		close(refreshing)
		<-release
	})
	s, err := Open(Options{
		DataDir: dir,
		Logger:  quietIndexLogger(),
		Hooks: Hooks{Authorize: func(context.Context, Request) error {
			authorized <- struct{}{}
			return nil
		}},
		indexFault: func(_ *project, phase string) error {
			if phase == "update" {
				pauseRefresh()
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	defer releaseWrite()
	info, _, err := s.register(api.RegisterProject{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	request := func(method, key, body string) <-chan *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/projects/"+info.ID+"/tasks", strings.NewReader(body))
		r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
		r.Header.Set(api.HeaderIdempotencyKey, key)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			done <- w
		}()
		return done
	}
	active := request(http.MethodPost, "active-write", `{"type":"chore","slug":"active","title":"Active write"}`)
	waitFor(t, refreshing, "active write in index refresh")
	waitFor(t, authorized, "active write after readiness check")
	queued := request(http.MethodPost, "queued-write", `{"type":"chore","slug":"queued","title":"Queued write"}`)
	list := request(http.MethodGet, "", "")
	for range 2 {
		waitFor(t, authorized, "queued request after readiness check")
	}
	closed := make(chan error, 2)
	go func() { closed <- s.Close() }()
	waitFor(t, s.shutdown.Done(), "Close starting")
	go func() { closed <- s.Close() }()
	// An Open times out on the directory lock while the write is stopped.
	other, err := Open(Options{DataDir: dir, Logger: quietIndexLogger()})
	if err == nil {
		_ = other.Close()
	}
	if !errors.Is(err, ErrDataDirInUse) {
		t.Fatalf("Open during the active write: %v, want ErrDataDirInUse", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before the active write released its project lock: %v", err)
	default:
	}
	releaseWrite()
	for range 2 {
		if err := waitFor(t, closed, "Close after write publication"); err != nil {
			t.Fatal(err)
		}
	}
	if w := waitFor(t, active, "active write response"); w.Code != http.StatusCreated {
		t.Fatalf("active request: %d %s", w.Code, w.Body)
	}
	for _, done := range []<-chan *httptest.ResponseRecorder{queued, list} {
		if w := waitFor(t, done, "queued project response"); w.Code != http.StatusServiceUnavailable || !containsErrorCode(w, api.CodeNotReady) {
			t.Errorf("queued request: %d %s", w.Code, w.Body)
		}
	}
	view, err := board.LoadBoard(info.BoardPath)
	if err != nil || len(view.Entries) != 1 {
		t.Fatalf("only the active request may write the board: %+v, %v", view.Entries, err)
	}
	for id := range view.Entries {
		if !strings.HasSuffix(id, "-active-task") {
			t.Fatalf("unexpected task written during Close: %s", id)
		}
	}
}

func TestServerCloseRejectsDirectRegistration(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{DataDir: dir, Logger: quietIndexLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.register(api.RegisterProject{Name: "late"}); !api.IsCode(err, api.CodeNotReady) {
		t.Fatalf("register after Close: %v", err)
	}
	if _, err := os.Stat(projectRoot(dir, "late")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("register after Close left a project directory: %v", err)
	}
}
