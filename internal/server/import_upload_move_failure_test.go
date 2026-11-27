package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// assertUploadSettled checks that a request left neither staging behind.
func assertUploadSettled(t *testing.T, s *Server) {
	t.Helper()
	assertUploadClean(t, s.data, "demo")
	assertNoStaging(t, s.data, "demo")
}

// uploadServer serves s with its log kept, reports the context of the latest
// upload request, and says when each request handler returned.
func uploadServer(t *testing.T) (s *Server, url string, logs *syncBuffer, requestContext func() context.Context, handled <-chan struct{}) {
	t.Helper()
	logs = &syncBuffer{}
	s, err := Open(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	request := context.Background()
	done := make(chan struct{}, 4)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		request = r.Context()
		mu.Unlock()
		s.Handler().ServeHTTP(w, r)
		done <- struct{}{}
	}))
	t.Cleanup(func() { h.Close(); s.Close() })
	return s, h.URL, logs, func() context.Context {
		mu.Lock()
		defer mu.Unlock()
		return request
	}, done
}

// changeReceived rewrites rel of the received tree at the "received"
// checkpoint with other bytes of the same length.
func changeReceived(s *Server, rel string) {
	s.importFault = func(step string) error {
		if step != "received" {
			return nil
		}
		matches, _ := filepath.Glob(filepath.Join(projectRoot(s.data, "demo"), uploadPrefix+"*", uploadBoardDir, filepath.FromSlash(rel)))
		if len(matches) != 1 {
			return errors.New("no received " + rel)
		}
		data, err := os.ReadFile(matches[0])
		if err != nil {
			return err
		}
		changed := bytes.Clone(data)
		changed[len(changed)-1] ^= 1
		return os.WriteFile(matches[0], changed, 0o600)
	}
}

func TestUploadRejectsAFileChangedAfterItWasReceived(t *testing.T) {
	source, remnants, _ := remnantBoard(t)
	var leftover string // The remnant whose bytes equal its target's.
	for rel := range remnants {
		if strings.HasPrefix(rel, ".kander/") {
			leftover = rel
		}
	}
	for name, rel := range map[string]string{
		"a file the import keeps": idleCardDir(t, source) + "/spec.md",
		"a leftover it removes":   leftover,
	} {
		t.Run(name, func(t *testing.T) {
			s, h := openTest(t, t.TempDir(), nil)
			defer s.Close()
			defer h.Close()
			changeReceived(s, rel)
			_, err := uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: source})
			if !api.IsCode(err, api.CodeBoardRejected) {
				t.Fatalf("a changed received file: %v", err)
			}
			assertNothingPublished(t, s, "demo")
			assertUploadSettled(t, s)
		})
	}
}

func TestUploadFailuresAroundTheMoveLeaveNoStaging(t *testing.T) {
	t.Run("check rejected after the move", func(t *testing.T) {
		source := v1Board(t)
		view, err := board.InspectImportCards(source)
		if err != nil {
			t.Fatal(err)
		}
		for id, card := range view.Entries {
			if card.State == "working" {
				editSpec(t, source, card.State, id, func(text string) string {
					return strings.Replace(text, "- RESULT:", "- INTEGRATION_TARGET: invalid-target!\n- RESULT:", 1)
				})
			}
		}
		s, h := openTest(t, t.TempDir(), nil)
		defer s.Close()
		defer h.Close()
		_, steps := recordReceived(t, s, "demo")
		if _, err := uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: source}); !api.IsCode(err, api.CodeBoardRejected) {
			t.Fatalf("check refusal: %v", err)
		}
		if !slices.Contains(steps(), "moved") {
			t.Fatalf("refused before the move: %v", steps())
		}
		assertNothingPublished(t, s, "demo")
		assertUploadSettled(t, s)
	})
	t.Run("merge conflict", func(t *testing.T) {
		initial, source, _ := mergeMoveFixture(t)
		s, h := openTest(t, t.TempDir(), nil)
		defer s.Close()
		defer h.Close()
		p, _ := importProject(t, s, initial)
		before := scanImportTestTree(t, p.board)
		if _, err := uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: source}); !api.IsCode(err, api.CodeBoardRejected) {
			t.Fatalf("conflict: %v", err)
		}
		if after := scanImportTestTree(t, p.board); after.digest != before.digest {
			t.Fatal("a refused merge changed the board")
		}
		assertUploadSettled(t, s)
	})
	t.Run("client gone before the move", func(t *testing.T) {
		s, url, logs, requestContext, handled := uploadServer(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s.importFault = func(step string) error {
			if step == "received" {
				cancel()
				<-requestContext().Done()
			}
			return nil
		}
		if _, err := uploadImport(ctx, url, api.ImportRequest{Project: "demo", From: v1Board(t)}, importRun{}); err == nil {
			t.Fatal("a canceled upload answered")
		}
		waitFor(t, handled, "the handler of the canceled upload")
		logs.waitContains(t, `"msg":"board import canceled","project_name":"demo","reason":"`+importCancelCaller+`"`)
		assertNothingPublished(t, s, "demo")
		assertUploadSettled(t, s)
	})
	t.Run("I/O error after the move", func(t *testing.T) {
		s, h := openTest(t, t.TempDir(), nil)
		defer s.Close()
		defer h.Close()
		s.importFault = func(step string) error {
			if step == "moved" {
				return &os.PathError{Op: "write", Path: "injected", Err: syscall.EIO}
			}
			return nil
		}
		_, err := uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: v1Board(t)})
		var e *api.Error
		if !errors.As(err, &e) || e.Code != api.CodeInternal {
			t.Fatalf("I/O failure: %#v", err)
		}
		assertNothingPublished(t, s, "demo")
		assertUploadSettled(t, s)
	})
}

func TestUploadClientGoneDuringVerifyStillPublishes(t *testing.T) {
	s, url, _, requestContext, handled := uploadServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var waited atomic.Bool
	s.importFault = func(step string) error {
		if step == "synced" {
			// The publication waits until the server saw the disconnect.
			select {
			case <-requestContext().Done():
				waited.Store(true)
			case <-time.After(10 * time.Second):
			}
		}
		return nil
	}
	var archive bytes.Buffer
	if err := writeBoardUpload(t.Context(), &archive, api.ImportRequest{Project: "demo", From: v1Board(t)}, nil, nil); err != nil {
		t.Fatal(err)
	}
	peer, _ := client.New(url, "uploader")
	if _, err := peer.UploadImport(ctx, "demo", io.NopCloser(&archive), nil, func(p api.ImportProgress) {
		if p.Phase == api.ImportPhaseVerify {
			cancel()
		}
	}); err == nil {
		t.Fatal("a canceled upload answered")
	}
	waitFor(t, handled, "the handler of the canceled upload")
	if !waited.Load() {
		t.Fatal("the server never saw the disconnect before it published")
	}
	p, err := s.project(mustProject(t, s, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if view, err := board.LoadBoard(p.board); err != nil || len(view.Entries) != 2 {
		t.Fatalf("published board: %d entries, %v", len(view.Entries), err)
	}
	assertUploadSettled(t, s)
}

func TestUploadCrashAfterTheMoveIsDiscardedOnRestart(t *testing.T) {
	data := t.TempDir()
	s, h := openTest(t, data, nil)
	project := projectRoot(data, "demo")
	s.importFault = func(step string) error {
		if step != "moved" {
			return nil
		}
		// The deferred cleanup of the request still runs in this process: a
		// real crash would leave the upload staging behind as well.
		writeImportFixture(t, filepath.Join(project, uploadPrefix+"crashed"), uploadBoardDir+"/backlog/left.md", "left")
		return errImportCrash
	}
	if _, err := uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: v1Board(t)}); err == nil {
		t.Fatal("a crashed import answered")
	}
	h.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(project, "board.import")); err != nil {
		t.Fatalf("the crash did not leave the moved staging: %v", err)
	}
	reopened, h2 := openTest(t, data, nil)
	defer reopened.Close()
	defer h2.Close()
	if _, err := reopened.project(mustProject(t, reopened, "demo")); err != nil {
		t.Fatal(err)
	}
	assertNothingPublished(t, reopened, "demo")
	assertUploadSettled(t, reopened)
}

func TestUploadCrashAfterTheIntentPublishesOnRestart(t *testing.T) {
	source, _, _ := moveFixture(t)
	localData := t.TempDir()
	local, localHTTP := openTest(t, localData, nil)
	localHTTP.Close()
	want, err := local.Import("demo", "", source)
	if err != nil {
		t.Fatal(err)
	}
	// Opening a server adds its runtime directories to every board: restart
	// the local one too, as the crashed one is restarted below.
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}
	local, localHTTP = openTest(t, localData, nil)
	defer local.Close()
	defer localHTTP.Close()
	wantProject, err := local.project(want.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	s, h := openTest(t, data, nil)
	s.importFault = func(step string) error {
		if step == "prepared" {
			return errImportCrash
		}
		return nil
	}
	req := api.ImportRequest{Project: "demo", From: source}
	if _, err := uploadTestBoard(t, h.URL, req); err == nil {
		t.Fatal("a crashed import answered")
	}
	h.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, h2 := openTest(t, data, nil)
	defer reopened.Close()
	defer h2.Close()
	p, err := reopened.project(mustProject(t, reopened, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	assertSameImportedTree(t, wantProject.board, p.board)
	again, err := uploadTestBoard(t, h2.URL, req)
	if err != nil || !again.Existing || again.Snapshot != want.Snapshot {
		t.Fatalf("repeated upload after recovery: %+v %v", again, err)
	}
	assertUploadSettled(t, reopened)
}

func TestUploadOfARecordedSnapshotMovesNothing(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	req := api.ImportRequest{Project: "demo", From: v1Board(t)}
	first, err := uploadTestBoard(t, h.URL, req)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.project(first.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := scanImportTestTree(t, p.board)
	_, steps := recordReceived(t, s, "demo")
	again, err := uploadTestBoard(t, h.URL, req)
	if err != nil || !again.Existing || slices.Contains(steps(), "received") || slices.Contains(steps(), "moved") {
		t.Fatalf("repeated upload: %+v %v, steps %v", again, err, steps())
	}
	if after := scanImportTestTree(t, p.board); after.digest != before.digest {
		t.Fatal("a repeated upload changed the board")
	}
	assertUploadSettled(t, s)
}

func TestUploadCanceledWhileWaitingForTheProjectLockIsCanceled(t *testing.T) {
	s, url, logs, requestContext, handled := uploadServer(t)
	source := v1Board(t)
	if _, err := uploadImport(t.Context(), url, api.ImportRequest{Project: "demo", From: source}, importRun{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, handled, "the first upload")
	p, err := s.project(mustProject(t, s, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	// The same snapshot again, while another import holds the project lock:
	// the client leaves before the lock is free.
	if err := p.lockBoard(); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			p.unlockBoard()
		}
	}()
	var archive bytes.Buffer
	if err := writeBoardUpload(t.Context(), &archive, api.ImportRequest{Project: "demo", From: source}, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	peer, _ := client.New(url, "uploader")
	answered := make(chan error, 1)
	go func() {
		_, err := peer.UploadImport(ctx, "demo", io.NopCloser(&archive), nil, func(p api.ImportProgress) {
			if p.Phase == api.ImportPhaseLock {
				cancel()
			}
		})
		answered <- err
	}()
	if err := waitFor(t, answered, "the canceled upload"); err == nil {
		t.Fatal("a canceled upload answered")
	}
	select {
	case <-requestContext().Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the server never saw the disconnect")
	}
	locked = false
	p.unlockBoard()
	waitFor(t, handled, "the handler of the canceled upload")
	logs.waitContains(t, `"msg":"board import canceled","project_name":"demo","reason":"`+importCancelCaller+`"`)
	assertUploadSettled(t, s)
}
