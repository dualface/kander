package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
)

func uploadTestBoard(t *testing.T, url string, req api.ImportRequest) (api.ImportResult, error) {
	t.Helper()
	return uploadImport(t.Context(), url, req, importRun{})
}

func assertUploadClean(t *testing.T, data, project string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(projectRoot(data, project), uploadPrefix+"*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("upload staging remains: %v (%v)", matches, err)
	}
}

func TestRemoteImportCommandUsesOnlyTheBoundServer(t *testing.T) {
	s, httpServer := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer httpServer.Close()
	source := v1Board(t)
	var requests atomic.Int32
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		requests.Add(1)
		var dial net.Dialer
		return dial.DialContext(ctx, network, httpServer.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	client.SetDefaultTransport(transport)
	t.Cleanup(func() { client.SetDefaultTransport(nil) })
	t.Chdir(t.TempDir())
	configPath := filepath.Join(t.TempDir(), "config.json")
	if _, err := config.UpdateAt(configPath, func(cfg *config.Config) error {
		cfg.Server.URL = "http://boards.example.invalid"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CONFIG", configPath)
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	prev := importOpen
	importOpen = func(Options) (*Server, error) {
		t.Error("remote import attempted to open the local data directory")
		return nil, errors.New("unexpected local open")
	}
	t.Cleanup(func() { importOpen = prev })

	run := func(extra ...string) (int, string, string) {
		return commandtest.Capture(func() int {
			return commandtest.Run(NewImportCommand(), append([]string{"--project", "demo", "--from", source}, extra...)...)
		})
	}
	for _, value := range []string{"", filepath.Join(xdg, "explicit")} {
		code, stdout, stderr := run("--data=" + value)
		if code != 2 || stdout != "" || requests.Load() != 0 || !strings.Contains(stderr, "--data") {
			t.Fatalf("--data refusal: code=%d requests=%d stdout=%q stderr=%q", code, requests.Load(), stdout, stderr)
		}
	}
	code, stdout, stderr := run()
	var out api.ImportResult
	if err := json.Unmarshal([]byte(stdout), &out); code != 0 || err != nil || out.Source != source || out.Tasks != 2 || out.Project.BoardPath != "" {
		t.Fatalf("remote import: code=%d, result=%+v, stdout=%q, stderr=%q, err=%v", code, out, stdout, stderr, err)
	}
	if requests.Load() == 0 || !strings.Contains(stderr, config.Text("server.import_progress_upload", "0 B", formatImportBytes(scanImportTestTree(t, source).bytes))) {
		t.Fatalf("no request or progress: %d, %q", requests.Load(), stderr)
	}
	entries, err := os.ReadDir(xdg)
	if err != nil || len(entries) != 0 {
		t.Fatalf("remote import touched the local data directory: %v %v", entries, err)
	}
	assertUploadClean(t, s.data, "demo")
}

func sameImportOutcome(t *testing.T, local, uploaded api.ImportResult) {
	t.Helper()
	local.Project, uploaded.Project = api.Project{}, api.Project{}
	if !reflect.DeepEqual(local, uploaded) {
		t.Fatalf("local result differs from upload:\nlocal  %+v\nupload %+v", local, uploaded)
	}
}

func TestUploadEmptyProjectMatchesLocalImportAndReplays(t *testing.T) {
	source := v1Board(t)
	defaults := stripSize(t, source)
	closed := addClosedCard(t, source, "done", "history", true)
	local, localHTTP := openTest(t, t.TempDir(), nil)
	defer local.Close()
	defer localHTTP.Close()
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	expected, err := local.Import("demo", "", source)
	if err != nil {
		t.Fatal(err)
	}
	req := api.ImportRequest{Project: "demo", From: source}
	got, err := uploadTestBoard(t, h.URL, req)
	if err != nil || got.Project.BoardPath != "" || !slices.Equal(got.DefaultedSize, defaults) ||
		len(got.Warnings) != 1 || !slices.Contains(got.Warnings[0].TaskIDs, closed) {
		t.Fatalf("upload result: %+v %v", got, err)
	}
	sameImportOutcome(t, expected, got)
	again, err := uploadTestBoard(t, h.URL, req)
	if err != nil || !again.Existing {
		t.Fatalf("repeated upload: %+v %v", again, err)
	}
	got.Existing = true
	sameImportOutcome(t, got, again)
	p, err := s.project(got.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	record, err := p.readImportRecord(got.Snapshot)
	if err != nil || record.Source != source || record.Snapshot != got.Snapshot {
		t.Fatalf("import record: %+v %v", record, err)
	}
	assertUploadClean(t, s.data, "demo")
}

func TestUploadMergeMatchesLocalIncludingConflicts(t *testing.T) {
	initial, conflict := newImportFixture(t, "conflict")
	same := addImportTask(t, initial, "same")
	source := filepath.Join(t.TempDir(), "kanban")
	if err := os.CopyFS(source, os.DirFS(initial)); err != nil {
		t.Fatal(err)
	}
	editSpec(t, source, "backlog", conflict, func(text string) string { return text + "\nchanged\n" })
	addImportTask(t, source, "new")
	local, localHTTP := openTest(t, t.TempDir(), nil)
	defer local.Close()
	defer localHTTP.Close()
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	importProject(t, local, initial)
	target, _ := importProject(t, s, initial)
	before := scanImportTestTree(t, target.board)
	req := api.ImportRequest{Project: "demo", From: source}
	_, wantErr := local.Import("demo", "", source)
	_, gotErr := uploadTestBoard(t, h.URL, req)
	if !api.IsCode(gotErr, api.CodeBoardRejected) || wantErr == nil || wantErr.Error() != gotErr.Error() {
		t.Fatalf("conflict: local=%v upload=%v", wantErr, gotErr)
	}
	if after := scanImportTestTree(t, target.board); after.digest != before.digest {
		t.Fatal("rejected upload changed the destination")
	}
	req.SkipConflicts = true
	want, err := local.importV1Board(t.Context(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := uploadTestBoard(t, h.URL, req)
	if err != nil || !slices.Equal(got.ExistingTasks, []string{same}) || !slices.Equal(got.SkippedConflicts, []api.ImportConflict{{Kind: "task", ID: conflict}}) || got.AddedTasks != 1 {
		t.Fatalf("merge upload: %+v %v", got, err)
	}
	sameImportOutcome(t, want, got)
	assertOriginalImportFiles(t, before, scanImportTestTree(t, target.board))
	assertUploadClean(t, s.data, "demo")
}

type uploadResponseRecorder struct{ *httptest.ResponseRecorder }

func (*uploadResponseRecorder) SetReadDeadline(time.Time) error { return nil }

func uploadRequest(t *testing.T, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/api/v1/projects/demo/import", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", api.ImportUploadType)
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	return req
}

func packUpload(t *testing.T, manifest any, rest string) []byte {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	wire := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	wire = append(wire, data...)
	return append(wire, rest...)
}

func uploadFixtureManifest(path, contents string) api.ImportUploadManifest {
	sum := sha256.Sum256([]byte(contents))
	hash := sha256.New()
	hashImportEntry(hash, path, sum)
	return api.ImportUploadManifest{
		ImportRequest: api.ImportRequest{Project: "demo", From: `C:\Users\client\kanban`},
		Snapshot:      hex.EncodeToString(hash.Sum(nil)),
		Directories:   slices.Sorted(slices.Values(board.States)),
		Files:         []api.ImportUploadFile{{Path: path, Size: int64(len(contents)), SHA256: hex.EncodeToString(sum[:])}},
	}
}

func TestUploadRejectsInvalidArchivesWithoutPublishing(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()

	type caseData struct {
		body    []byte
		code    string
		status  int
		limited func(*uploadLimits)
	}
	reject := func(manifest any, suffix string) caseData {
		return caseData{body: packUpload(t, manifest, suffix), code: api.CodeBoardRejected, status: http.StatusUnprocessableEntity}
	}
	const marker = api.ImportUploadComplete
	m := uploadFixtureManifest("backlog/fixture.md", "abc")
	const journal = `{"schema":1,"id":"example","phase":"prepared"}`
	badJournal := uploadFixtureManifest(".kander/operations/committed/example.json", journal)
	badJournal.Directories = append(badJournal.Directories, ".kander", ".kander/operations", ".kander/operations/committed")
	slices.Sort(badJournal.Directories)
	badSnapshot := m
	badSnapshot.Snapshot = strings.Repeat("a", 64)
	badSize := uploadFixtureManifest("backlog/fixture.md", "abc")
	badSize.Files[0].Size--
	repeated := m
	repeated.Files = append(slices.Clone(m.Files), m.Files[0])
	cases := map[string]caseData{
		"unrecovered journal": reject(badJournal, journal+marker),
		"snapshot":            reject(badSnapshot, "abc"+marker),
		"size mismatch":       reject(badSize, "abc"+marker),
		"body digest":         reject(m, "abd"+marker),
		"missing marker":      reject(m, "abc"),
		"truncated payload":   reject(m, "a"),
		"trailing bytes":      reject(m, "abc"+marker+"x"),
		"duplicate":           reject(repeated, "abcabc"+marker),
		"link metadata":       reject(map[string]any{"project": "demo", "files": []map[string]any{{"path": "backlog/regular.md", "type": "symlink"}}}, marker),
		"special entry":       reject(map[string]any{"project": "demo", "files": []map[string]any{{"path": "backlog/regular.md", "type": "directory"}}}, marker),
	}
	for _, rel := range []string{"/absolute", "backlog/../outside", "backlog/./fixture", "backlog//fixture", `backlog\fixture.md`, "backlog/fixture:stream", "backlog/.hidden"} {
		cases[rel] = reject(uploadFixtureManifest(rel, "abc"), "abc"+marker)
	}
	for name, limit := range map[string]func(*uploadLimits){
		"files":       func(l *uploadLimits) { l.files = 0 },
		"total bytes": func(l *uploadLimits) { l.bytes = 2 },
		"file bytes":  func(l *uploadLimits) { l.fileBytes = 2 },
		"manifest":    func(l *uploadLimits) { l.manifest = 3 },
	} {
		cases[name] = caseData{body: packUpload(t, m, "abc"+marker), code: api.CodeInvalidRequest, status: http.StatusRequestEntityTooLarge, limited: limit}
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			request := uploadRequest(t, h.URL, bytes.NewReader(tc.body))
			out := &uploadResponseRecorder{httptest.NewRecorder()}
			if tc.limited != nil {
				limits := defaultUploadLimits()
				tc.limited(&limits)
				s.handleImportUpload(out, request, "demo", limits)
			} else {
				s.Handler().ServeHTTP(out, request)
			}
			if out.Code != tc.status || !containsErrorCode(out.ResponseRecorder, tc.code) || strings.Contains(out.Body.String(), s.data) {
				t.Fatalf("upload refusal = %d %s", out.Code, out.Body)
			}
			if _, err := s.projectByName("demo"); !api.IsCode(err, api.CodeNotFound) {
				t.Fatalf("refused upload registered a project: %v", err)
			}
			assertUploadClean(t, s.data, "demo")
		})
	}
}

func TestUploadRouteIsAReadyAuthorizedDomainRoute(t *testing.T) {
	var log bytes.Buffer
	var scopes []Request
	s, err := Open(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewJSONHandler(&log, nil)), Hooks: Hooks{
		Authorize: func(_ context.Context, scope Request) error { scopes = append(scopes, scope); return nil },
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := uploadFixtureManifest("backlog/fixture.md", "private-text")
	payload := packUpload(t, m, "private-text-bad")
	for _, tc := range []struct {
		protocol string
		ready    bool
		status   int
	}{
		{"", true, http.StatusPreconditionFailed},
		{strconv.Itoa(api.ProtocolVersion), false, http.StatusServiceUnavailable},
		{strconv.Itoa(api.ProtocolVersion), true, http.StatusUnprocessableEntity},
	} {
		s.ready.Store(tc.ready)
		r := uploadRequest(t, "http://server", bytes.NewReader(payload))
		r.Header.Set(api.HeaderProtocol, tc.protocol)
		out := &uploadResponseRecorder{httptest.NewRecorder()}
		s.Handler().ServeHTTP(out, r)
		if out.Code != tc.status || strings.Contains(out.Body.String(), "private-text") {
			t.Fatalf("status=%d body=%s", out.Code, out.Body)
		}
	}
	if len(scopes) != 1 || scopes[0].Project != "demo" || scopes[0].ProjectID != api.ProjectID("demo") {
		t.Fatalf("authorization scopes: %+v", scopes)
	}
	if strings.Count(log.String(), `"path":"/api/v1/projects/demo/import"`) != 3 || strings.Contains(log.String(), "private-text") {
		t.Fatalf("unexpected access log: %s", log.String())
	}
}

func TestUploadValidationRefusalDoesNotExposeBoardContent(t *testing.T) {
	source := v1Board(t)
	view, err := board.InspectImportCards(source)
	if err != nil {
		t.Fatal(err)
	}
	for id, card := range view.Entries {
		if card.State == "working" {
			editSpec(t, source, card.State, id, func(text string) string {
				return strings.Replace(text, "- RESULT:", "- INTEGRATION_TARGET: private-invalid-target!\n- RESULT:", 1)
			})
		}
	}
	var log bytes.Buffer
	s, err := Open(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewJSONHandler(&log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	_, err = uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: source})
	if !api.IsCode(err, api.CodeBoardRejected) || !strings.Contains(err.Error(), quotedPath(source)) ||
		strings.Contains(err.Error(), s.data) || strings.Contains(err.Error(), "private-invalid-target") {
		t.Fatalf("validation refusal: %v", err)
	}
	// Staging is removed before the handler logs and returns, not necessarily
	// before the client receives its final stream line.
	h.Close()
	if strings.Contains(log.String(), "private-invalid-target") {
		t.Fatal("source text in server logs")
	}
	assertUploadClean(t, s.data, "demo")
	if _, err := os.Stat(filepath.Join(projectRoot(s.data, "demo"), "board.import")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("merge staging after refusal: %v", err)
	}
}

func TestUploadDisconnectBeforeEOFDoesNotPublishAndCanRetry(t *testing.T) {
	source := v1Board(t)
	s, err := Open(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan struct{}, 1)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Handler().ServeHTTP(w, r)
		done <- struct{}{}
	}))
	defer h.Close()
	var archive bytes.Buffer
	request := api.ImportRequest{Project: "demo", From: source}
	if err := writeBoardUpload(t.Context(), &archive, request, nil); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	peer, _ := client.New(h.URL, "uploader")
	result := make(chan error, 1)
	go func() {
		_, err := peer.UploadImport(ctx, "demo", reader, nil)
		result <- err
	}()
	if _, err := writer.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	// Even a complete marker is insufficient until the request has ended.
	// Wait until the server starts staging, then cancel without sending EOF.
	deadline := time.Now().Add(5 * time.Second)
	for {
		matches, _ := filepath.Glob(filepath.Join(projectRoot(s.data, "demo"), uploadPrefix+"*"))
		if len(matches) != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("upload never entered staging")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := waitFor(t, result, "upload cancellation"); err == nil {
		t.Fatal("interrupted upload got a result")
	}
	waitFor(t, done, "staging cleanup")
	if _, err := s.projectByName("demo"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("partial upload registered a project: %v", err)
	}
	assertUploadClean(t, s.data, "demo")
	if out, err := uploadTestBoard(t, h.URL, request); err != nil || out.Existing || out.Tasks != 2 {
		t.Fatalf("retry: %+v %v", out, err)
	}
	waitFor(t, done, "successful staging cleanup")
	assertUploadClean(t, s.data, "demo")
}

func TestServerStartupRemovesAbandonedUploads(t *testing.T) {
	data := t.TempDir()
	stage := filepath.Join(projectRoot(data, "abandoned"), uploadPrefix+"crashed")
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	writeImportFixture(t, stage, "backlog/discard.md", "unfinished")
	s, h := openTest(t, data, nil)
	defer s.Close()
	defer h.Close()
	assertUploadClean(t, data, "abandoned")
	if _, err := s.projectByName("abandoned"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("abandoned staging was registered: %v", err)
	}
}

func TestUploadOldServerReportsUpgrade(t *testing.T) {
	source := v1Board(t)
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/projects/demo/import" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
			writeJSONResponse(w, status, api.Error{Code: api.CodeNotFound})
		}))
		_, err := uploadTestBoard(t, h.URL, api.ImportRequest{Project: "demo", From: source})
		h.Close()
		if err == nil || err.Error() != config.Text("server.import_upload_unsupported", h.URL) {
			t.Fatalf("status %d: %v", status, err)
		}
	}
}
