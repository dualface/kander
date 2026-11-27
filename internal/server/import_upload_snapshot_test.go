package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

type afterUploadWrite struct {
	io.Writer
	after func([]byte)
}

func (w afterUploadWrite) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	if err == nil && w.after != nil {
		w.after(b[:n])
	}
	return n, err
}

func TestUploadWithholdsCompletionWhenAnAlreadySentFileChanges(t *testing.T) {
	source, id := newImportFixture(t, "changing")
	const contents = "the original body of a known test attachment"
	rel := "backlog/" + id + "/notes/input.txt"
	writeImportFixture(t, source, rel, contents)
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	peer, _ := client.New(h.URL, "host-a")
	finished := make(chan error, 1)
	go func() {
		_, err := peer.UploadImport(t.Context(), "demo", reader, nil, nil)
		finished <- err
	}()
	changed := false
	out := afterUploadWrite{Writer: writer, after: func(b []byte) {
		if string(b) == contents {
			writeImportFixture(t, source, rel, "changed after these original bytes were sent")
			changed = true
		}
	}}
	err := writeBoardUpload(t.Context(), out, api.ImportRequest{Project: "demo", From: source}, nil, nil)
	_ = writer.CloseWithError(err)
	if !changed || !api.IsCode(err, api.CodeBoardRejected) || !strings.Contains(err.Error(), "changed during upload") {
		t.Fatalf("changed=%v producer refused: %v", changed, err)
	}
	if err := waitFor(t, finished, "upload refusal"); err == nil {
		t.Fatal("client got a success for a changed source")
	}
	h.Close()
	if _, err := s.projectByName("demo"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("changed source registered a project: %v", err)
	}
	assertUploadClean(t, s.data, "demo")
}

func TestUploadManifestUsesSlashRelativePathsAndHasIndependentWindowsSource(t *testing.T) {
	source, id := newImportFixture(t, "paths")
	expected := "backlog/" + id + "/notes/explanation.txt"
	native := filepath.Join("backlog", id, "notes", "explanation.txt")
	writeImportFixture(t, source, native, "hello")

	var archive bytes.Buffer
	if err := writeBoardUpload(t.Context(), &archive, api.ImportRequest{Project: "demo", From: source}, nil, nil); err != nil {
		t.Fatal(err)
	}
	manifest, err := readUploadManifest(&archive, defaultUploadLimits())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.From != source || !slices.Contains(uploadPaths(manifest), expected) {
		t.Fatalf("wrong archived paths: %q %v", manifest.From, uploadPaths(manifest))
	}
	for _, name := range append(uploadPaths(manifest), manifest.Directories...) {
		if strings.ContainsAny(name, `\:`) {
			t.Fatalf("archive contains a native or absolute path: %q", name)
		}
	}

	// The receiving host must not interpret the source path with its own
	// path rules. Reuse the same archive even when it names a Windows source.
	manifest.From = `C:\Users\operator\workspace\kanban`
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	var remote bytes.Buffer
	if err := writeUploadManifest(&remote, manifest, defaultUploadLimits()); err != nil {
		t.Fatal(err)
	}
	remote.Write(archive.Bytes())
	peer, _ := client.New(h.URL, "host-a")
	got, err := peer.UploadImport(t.Context(), "demo", io.NopCloser(&remote), nil, nil)
	if err != nil || got.Source != manifest.From || got.Project.BoardPath != "" {
		t.Fatalf("wrong remote source metadata: %+v %v", got, err)
	}
}

func TestUploadUsesItsOwnBodyLimit(t *testing.T) {
	source, id := newImportFixture(t, "large")
	// A valid single attachment just over the limit of an ordinary domain
	// route must be accepted by the real HTTP upload handler.
	const attachment = maxBody + 1
	writeImportFixture(t, source, "backlog/"+id+"/data.bin", strings.Repeat("x", attachment))
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	result, err := uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: source})
	if err != nil || result.Tasks != 1 {
		t.Fatalf("attachment over ordinary route limit: %+v %v", result, err)
	}
	assertUploadClean(t, s.data, "demo")
}

func TestUploadRefusesRatherThanWaitingWhenSlotsAreFull(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	for range maxImportUploads {
		s.uploadSlots <- struct{}{}
	}
	m := uploadFixtureManifest("backlog/fixture.md", "abc")
	peer, _ := client.New(h.URL, "host-a")
	_, err := peer.UploadImport(t.Context(), "demo", io.NopCloser(bytes.NewReader(packUpload(t, m, "abc"+api.ImportUploadComplete))), nil, nil)
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.Code != api.CodeNotReady || refusal.Status != http.StatusServiceUnavailable ||
		!strings.Contains(refusal.Message, "retry later") {
		t.Fatalf("upload capacity refusal: %v", err)
	}
	assertUploadClean(t, s.data, "demo")
}

func TestUploadBoundsAStalledRequest(t *testing.T) {
	s, endpoint := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer endpoint.Close()

	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limits := defaultUploadLimits()
		limits.timeout = 250 * time.Millisecond
		limits.deadline = func(int64) time.Duration { return 250 * time.Millisecond }
		s.handleImportUpload(w, r, "demo", limits)
	}))
	defer h.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	m := uploadFixtureManifest("backlog/fixture.md", "abc")
	req := uploadRequest(t, h.URL, reader)
	response := make(chan int, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			response <- 0
			return
		}
		resp.Body.Close()
		response <- resp.StatusCode
	}()
	if _, err := writer.Write(packUpload(t, m, "abc")); err != nil {
		t.Fatal(err)
	}
	// The server must return while the client is still waiting to send
	// the completion marker. Closing the writer first would test EOF instead.
	if status := waitFor(t, response, "upload read deadline"); status != http.StatusUnprocessableEntity {
		t.Fatalf("stalled request status %d", status)
	}
	assertUploadClean(t, s.data, "demo")
}
