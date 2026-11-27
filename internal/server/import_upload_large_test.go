package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// overBudgetFiles exceeds the 4,096-entry cleanup budget of a private
// temporary directory.
const overBudgetFiles = 5000

// boardWithAttachments is v1Board with n attachments in its backlog card.
func boardWithAttachments(t *testing.T, n int) string {
	t.Helper()
	source := v1Board(t)
	dir := filepath.Join(source, filepath.FromSlash(idleCardDir(t, source)), "attachments")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("a%06d.txt", i)), []byte(fmt.Sprint(i)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return source
}

// uploadArchiveBytes is the complete archive the client sends for source.
func uploadArchiveBytes(t *testing.T, source string) []byte {
	t.Helper()
	var archive bytes.Buffer
	if err := writeBoardUpload(t.Context(), &archive, api.ImportRequest{Project: "demo", From: source}, nil, nil); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

// limitedUploads serves only the upload route of s with limits, and says when
// each request handler returned.
func limitedUploads(t *testing.T, s *Server, limits uploadLimits) (url string, handled <-chan int) {
	t.Helper()
	done := make(chan int, 4)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done <- s.handleImportUpload(w, r, "demo", limits)
	}))
	t.Cleanup(h.Close)
	return h.URL, done
}

func TestLargeUploadStagingIsRemovedAtEveryEnd(t *testing.T) {
	source := boardWithAttachments(t, overBudgetFiles)
	t.Run("success", func(t *testing.T) {
		s, url, _, _ := openLogged(t, t.TempDir())
		if result, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: source}); err != nil || result.Tasks != 2 {
			t.Fatalf("upload: %+v %v", result, err)
		}
		assertUploadClean(t, s.data, "demo")
	})
	t.Run("merge conflict", func(t *testing.T) {
		s, url, _, _ := openLogged(t, t.TempDir())
		changed := boardWithAttachments(t, overBudgetFiles)
		if _, err := s.Import("demo", "", changed); err != nil {
			t.Fatal(err)
		}
		writeImportFixture(t, changed, idleCardDir(t, changed)+"/attachments/a000000.txt", "changed")
		_, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: changed})
		if !api.IsCode(err, api.CodeBoardRejected) {
			t.Fatalf("conflicting upload: %v", err)
		}
		assertUploadClean(t, s.data, "demo")
	})
	t.Run("import canceled", func(t *testing.T) {
		data := t.TempDir()
		s, url, logs, _ := openLogged(t, data)
		// An upload is moved, not copied: the move is its cancellation boundary.
		shutDownAt(s, "received")
		_, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: source})
		wantImportCanceled(t, err, importCancelShutdown)
		assertUploadClean(t, data, "demo")
		assertNoStaging(t, data, "demo")
		logs.waitContains(t, `"msg":"board import canceled","project_name":"demo","reason":"`+importCancelShutdown+`"`)
	})
	archive := uploadArchiveBytes(t, source)
	// Everything but the end of the last file body and the marker: more
	// than overBudgetFiles files are staged when the body stops.
	partial := archive[:len(archive)-len(api.ImportUploadComplete)-1]
	t.Run("receive idle", func(t *testing.T) {
		s, _, logs, _ := openLogged(t, t.TempDir())
		limits := defaultUploadLimits()
		limits.idle = 200 * time.Millisecond
		url, handled := limitedUploads(t, s, limits)
		reader, writer := io.Pipe()
		defer writer.Close()
		go func() { _, _ = writer.Write(partial) }()
		go func() {
			// The response may arrive while the body is still being sent.
			if resp, err := http.DefaultClient.Do(uploadRequest(t, url, reader)); err == nil {
				resp.Body.Close()
			}
		}()
		if status := waitFor(t, handled, "the idle abort"); status != http.StatusUnprocessableEntity {
			t.Fatalf("idle upload: handler status %d", status)
		}
		logs.waitContains(t, `"msg":"board upload refused","project_name":"demo","stage":"file body `)
		logs.waitContains(t, "i/o timeout")
		assertUploadClean(t, s.data, "demo")
		assertNothingPublished(t, s, "demo")
	})
	t.Run("client disconnect", func(t *testing.T) {
		s, _, logs, _ := openLogged(t, t.TempDir())
		url, handled := limitedUploads(t, s, defaultUploadLimits())
		reader, writer := io.Pipe()
		ctx, cancel := context.WithCancel(t.Context())
		canceled := make(chan struct{})
		go func() {
			_, _ = writer.Write(partial)
			cancel()
			// Without this the transport's write loop can stay in a read of
			// the pipe after the cancel, and Do never returns.
			_ = writer.CloseWithError(context.Canceled)
			close(canceled)
		}()
		req := uploadRequest(t, url, reader).WithContext(ctx)
		answered := make(chan error, 1)
		go func() {
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
			}
			answered <- err
		}()
		// Sending the partial body has no bound: it waits for the server to
		// read it. The bounded wait starts only after the cancel.
		<-canceled
		if err := waitFor(t, answered, "the canceled upload to return"); err == nil {
			t.Fatal("a canceled upload answered")
		}
		waitFor(t, handled, "the handler after the disconnect")
		logs.waitContains(t, `"msg":"board upload refused","project_name":"demo","stage":"file body `)
		assertUploadClean(t, s.data, "demo")
		assertNothingPublished(t, s, "demo")
	})
}

func TestServerStartRemovesALargeLeftoverUploadStaging(t *testing.T) {
	data := t.TempDir()
	left := filepath.Join(projectRoot(data, "demo"), uploadPrefix+"leftover", "backlog")
	if err := os.MkdirAll(left, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := range overBudgetFiles {
		if err := os.WriteFile(filepath.Join(left, fmt.Sprintf("f%05d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, h := openTest(t, data, nil)
	defer h.Close()
	defer s.Close()
	if !s.ready.Load() {
		t.Fatal("server is not ready")
	}
	assertUploadClean(t, data, "demo")
}

func TestUploadStagingRemovalFailureKeepsTheImportOutcome(t *testing.T) {
	const removal = "injected removal failure 41aa"
	failRemoval := func(s *Server) {
		s.removeUploadStage = func(stage *fs.TempDir) error {
			return errors.Join(removeUploadStaging(stage), errors.New(removal))
		}
	}
	t.Run("success", func(t *testing.T) {
		s, url, logs, _ := openLogged(t, t.TempDir())
		failRemoval(s)
		if result, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: v1Board(t)}); err != nil || result.Tasks != 2 {
			t.Fatalf("upload: %+v %v", result, err)
		}
		logs.waitContains(t, `"msg":"board imported"`)
		logs.waitContains(t, `"msg":"remove board upload staging","project_name":"demo","error":"`+removal)
	})
	t.Run("failure", func(t *testing.T) {
		s, url, logs, _ := openLogged(t, t.TempDir())
		failRemoval(s)
		s.importFault = func(step string) error {
			if step == "copied" {
				return errors.New("injected import failure 9b2e")
			}
			return nil
		}
		_, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: v1Board(t)})
		var e *api.Error
		if !errors.As(err, &e) || e.Code != api.CodeInternal || e.Status != http.StatusInternalServerError {
			t.Fatalf("upload: %#v", err)
		}
		logs.waitContains(t, `"error":"injected import failure 9b2e"`)
		logs.waitContains(t, removal)
	})
}

func TestUploadIdleLimitEndsWithTheLastFileBody(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	limits := defaultUploadLimits()
	limits.idle = 100 * time.Millisecond
	url, _ := limitedUploads(t, s, limits)
	archive := uploadArchiveBytes(t, v1Board(t))
	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write(archive[:len(archive)-len(api.ImportUploadComplete)])
		// The client scans its source again here: no byte for several idle limits.
		time.Sleep(4 * limits.idle)
		_, _ = writer.Write([]byte(api.ImportUploadComplete))
		writer.Close()
	}()
	peer, _ := client.New(url, "uploader")
	if result, err := peer.UploadImport(t.Context(), "demo", reader, nil, nil); err != nil || result.Tasks != 2 {
		t.Fatalf("upload: %+v %v", result, err)
	}
	assertUploadClean(t, s.data, "demo")
}

func TestUploadImportOutlastsTheReceiveDeadline(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	limits := defaultUploadLimits()
	limits.deadline = func(int64) time.Duration { return 300 * time.Millisecond }
	url, _ := limitedUploads(t, s, limits)
	var fired atomic.Bool
	s.importFault = func(step string) error {
		// The last step of an upload import that observes cancellation.
		if step == "received" && !fired.Swap(true) {
			time.Sleep(time.Second)
		}
		return nil
	}
	peer, _ := client.New(url, "uploader")
	archive := io.NopCloser(bytes.NewReader(uploadArchiveBytes(t, v1Board(t))))
	if result, err := peer.UploadImport(t.Context(), "demo", archive, nil, func(api.ImportProgress) {}); err != nil || result.Tasks != 2 || !fired.Load() {
		t.Fatalf("upload: %+v %v (fired %v)", result, err, fired.Load())
	}
}

func TestUploadClientLeavesTheLimitsToTheServer(t *testing.T) {
	s, url, _, _ := openLogged(t, t.TempDir())
	s.upload.files = 3
	_, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: v1Board(t)})
	var e *api.Error
	if !errors.As(err, &e) || e.Status != http.StatusRequestEntityTooLarge ||
		!strings.Contains(e.Message, "board upload exceeds the files limit: declared ") ||
		!strings.HasSuffix(e.Message, "; "+config.Text("server.import_upload_limit_hint")) {
		t.Fatalf("refusal: %#v", err)
	}
	assertNothingPublished(t, s, "demo")
	assertUploadClean(t, s.data, "demo")
}

func TestUploadOfMoreFilesThanTheOldDefaultLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("creates more than 100,000 files")
	}
	const oldLimit = 100_000
	source := boardWithAttachments(t, oldLimit+1)
	local, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer local.Close()
	expected, err := local.Import("demo", "", source)
	if err != nil {
		t.Fatal(err)
	}
	s, url, _, _ := openLogged(t, t.TempDir())
	uploaded, err := uploadTestBoard(t, url, api.ImportRequest{Project: "demo", From: source})
	if err != nil || uploaded.Files <= oldLimit {
		t.Fatalf("upload: %d files, %v", uploaded.Files, err)
	}
	sameImportOutcome(t, expected, uploaded)
	assertUploadClean(t, s.data, "demo")
}

// stallAfter returns data, then err.
type stallAfter struct {
	data []byte
	err  error
}

func (r *stallAfter) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestUploadManifestReadFailuresKeepTheirCause(t *testing.T) {
	m := uploadFixtureManifest("backlog/fixture.md", "abc")
	whole := packUpload(t, m, "")
	stalled := errors.New("read deadline 5d1c")
	// The JSON is complete, but its frame declares one more byte of trailing
	// white space that never arrives.
	padded := append([]byte(nil), whole...)
	padded[3]++
	for name, tc := range map[string]struct {
		body []byte
		err  error
		want error
	}{
		"stall inside the JSON":     {whole[:len(whole)/2], stalled, stalled},
		"truncated inside the JSON": {whole[:len(whole)/2], io.EOF, io.ErrUnexpectedEOF},
		"stall after the JSON":      {padded, stalled, stalled},
		"truncated after the JSON":  {padded, io.EOF, io.ErrUnexpectedEOF},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readUploadManifest(&stallAfter{data: tc.body, err: tc.err}, defaultUploadLimits())
			var refused *uploadReceiveError
			if !errors.As(err, &refused) || refused.stage != "manifest" || !errors.Is(err, tc.want) || !api.IsCode(err, api.CodeBoardRejected) {
				t.Fatalf("manifest failure: %#v", err)
			}
		})
	}
}
