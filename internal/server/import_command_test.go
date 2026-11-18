package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// syncBuffer is a writer the command and the test goroutine share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitContains waits until the buffer holds text.
func (b *syncBuffer) waitContains(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(b.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("stderr never held %q:\n%s", text, b.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestImportReporterWritesPlainLinesWithoutATerminal(t *testing.T) {
	var out bytes.Buffer
	r := newImportReporter(&out, false)
	if r.Seen() {
		t.Fatal("nothing was reported yet")
	}
	r.Report(api.ImportProgress{Phase: api.ImportPhaseLock})
	if !r.Seen() || r.PastCopy() {
		t.Fatal("a report was seen and the copy has not finished")
	}
	r.Report(api.ImportProgress{Phase: api.ImportPhaseCopy, Total: 10, TotalBytes: 2048})
	r.Report(api.ImportProgress{Phase: api.ImportPhaseCopy, Done: 1, Total: 10, Bytes: 100, TotalBytes: 2048})
	r.Report(api.ImportProgress{Phase: api.ImportPhaseVerify})
	r.Notice("done")
	r.Finish()
	if strings.ContainsAny(out.String(), "\r\x1b") {
		t.Fatalf("control characters in %q", out.String())
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	want := []string{
		config.Text("server.import_progress_lock"),
		config.Text("server.import_progress_copy", 0, 10, "0 B", "2.0 KB"),
		config.Text("server.import_progress_verify"),
		"done",
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if !r.PastCopy() {
		t.Fatal("verify is past the copy")
	}
}

func TestImportReporterRewritesOneLineOnATerminal(t *testing.T) {
	var out bytes.Buffer
	r := newImportReporter(&out, true)
	r.Report(api.ImportProgress{Phase: api.ImportPhaseScan, Done: 1000, Total: 20000})
	r.Report(api.ImportProgress{Phase: api.ImportPhaseCheck})
	if _, err := r.Write([]byte("log line\n")); err != nil {
		t.Fatal(err)
	}
	r.Notice("cancelled")
	r.Finish()
	got := out.String()
	if strings.Count(got, "\n") != 3 || strings.Count(got, "\r") != 2 || !strings.Contains(got, "\nlog line\n") {
		t.Fatalf("two status rewrites, then a log line and a notice on their own lines: %q", got)
	}
	short := config.Text("server.import_progress_check")
	long := config.Text("server.import_progress_scan_count", 1000, 20000)
	if !strings.Contains(got, "\r"+short+strings.Repeat(" ", len([]rune(long))-len([]rune(short)))) {
		t.Fatalf("the shorter text must cover the longer one: %q", got)
	}
	if r.PastCopy() != true {
		t.Fatal("check is past the copy")
	}
}

func TestFormatImportBytes(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 3 << 20: "3.0 MB", 5 << 30: "5.0 GB"} {
		if got := formatImportBytes(in); got != want {
			t.Fatalf("%d: %q want %q", in, got, want)
		}
	}
}

// runImportCommand runs `kander server import` offline or online against dir.
func runImportCommand(t *testing.T, dir, source string, signals chan os.Signal) (code int, stdout, stderr *syncBuffer) {
	t.Helper()
	stdout, stderr = &syncBuffer{}, &syncBuffer{}
	code = importCommand(importOptions{data: dir, name: "demo", from: source}, stdout, stderr, false, signals)
	return code, stdout, stderr
}

func TestImportCommandPrintsProgressOnStderrAndOnlyTheResultOnStdout(t *testing.T) {
	source := v1Board(t)
	stripSize(t, source)
	code, stdout, stderr := runImportCommand(t, t.TempDir(), source, make(chan os.Signal, 1))
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr.String())
	}
	var result api.ImportResult
	decoder := json.NewDecoder(strings.NewReader(stdout.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || decoder.More() || result.Tasks != 2 || len(result.DefaultedSize) != 2 {
		t.Fatalf("stdout is one JSON result: %q %v", stdout.String(), err)
	}
	if strings.Contains(stderr.String(), "\r") {
		t.Fatalf("carriage return on a stream that is not a terminal: %q", stderr.String())
	}
	copyLine := strings.SplitN(config.Text("server.import_progress_copy", 0, 0, "", ""), "0", 2)[0]
	last := -1
	for _, text := range []string{config.Text("server.import_progress_lock"), config.Text("server.import_progress_scan"), copyLine, config.Text("server.import_progress_verify"), config.Text("server.import_progress_check"), config.Text("server.import_progress_publish")} {
		at := strings.Index(stderr.String(), text)
		if at <= last {
			t.Fatalf("%q is missing or out of order in:\n%s", text, stderr.String())
		}
		last = at
	}
}

// pauseOffline makes the offline command stop at step until release is called,
// and says when it got there.
func pauseOffline(t *testing.T, step string, onReach func()) {
	t.Helper()
	previous := importOpen
	t.Cleanup(func() { importOpen = previous })
	importOpen = func(o Options) (*Server, error) {
		s, err := Open(o)
		if err != nil {
			return nil, err
		}
		fired := false
		s.importFault = func(current string) error {
			if current == step && !fired {
				fired = true
				onReach()
			}
			return nil
		}
		return s, nil
	}
}

func TestImportCommandCancelsAnOfflineImportBeforeTheCopyEnds(t *testing.T) {
	source := v1Board(t)
	dir := t.TempDir()
	signals := make(chan os.Signal, 1)
	stderr := &syncBuffer{}
	pauseOffline(t, "copying", func() {
		signals <- os.Interrupt
		stderr.waitContains(t, config.Text("server.import_interrupt_cancelling"))
	})
	stdout := &syncBuffer{}
	code := importCommand(importOptions{data: dir, name: "demo", from: source}, stdout, stderr, false, signals)
	if code != 130 || stdout.String() != "" {
		t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), config.Text("server.import_cancelled")) {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
	s, err := Open(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if info, err := s.projectByName("demo"); err == nil {
		p, _ := s.project(info.ID)
		if view, err := board.LoadBoard(p.board); err != nil || len(view.Entries) != 0 {
			t.Fatalf("a cancelled import published: %+v %v", view.Entries, err)
		}
	}
}

func TestImportCommandFinishesAnOfflineImportInterruptedAfterTheCopy(t *testing.T) {
	source := v1Board(t)
	signals := make(chan os.Signal, 1)
	stderr := &syncBuffer{}
	pauseOffline(t, "prepared", func() {
		signals <- os.Interrupt
		stderr.waitContains(t, config.Text("server.import_interrupt_finishing"))
	})
	stdout := &syncBuffer{}
	code := importCommand(importOptions{data: t.TempDir(), name: "demo", from: source}, stdout, stderr, false, signals)
	var result api.ImportResult
	if code != 0 || json.Unmarshal([]byte(stdout.String()), &result) != nil || result.Tasks != 2 {
		t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), config.Text("server.import_cancelled")) {
		t.Fatalf("the import was not cancelled:\n%s", stderr.String())
	}
}

// onlineServer serves dir and records the context of the import request.
func onlineServer(t *testing.T, dir string) (*Server, *httptest.Server, func() context.Context) {
	t.Helper()
	s, err := Open(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var request context.Context
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/server/import" {
			mu.Lock()
			request = r.Context()
			mu.Unlock()
		}
		s.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(func() { h.Close(); s.Close() })
	useImportTarget(t, h.URL)
	return s, h, func() context.Context {
		mu.Lock()
		defer mu.Unlock()
		return request
	}
}

func TestImportCommandCancelsAnOnlineImportBeforeTheCopyEnds(t *testing.T) {
	source := v1Board(t)
	dir := t.TempDir()
	s, _, requestContext := onlineServer(t, dir)
	reached := make(chan struct{})
	fired := false
	s.importFault = func(step string) error {
		if step == "copying" && !fired {
			fired = true
			close(reached)
			// Hold the copy until the client has gone, so the next file sees the cancellation.
			<-requestContext().Done()
		}
		return nil
	}
	signals := make(chan os.Signal, 1)
	go func() {
		<-reached
		signals <- os.Interrupt
	}()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	code := importCommand(importOptions{data: dir, name: "demo", from: source}, stdout, stderr, false, signals)
	if code != 130 || stdout.String() != "" || !strings.Contains(stderr.String(), config.Text("server.import_cancelled")) || strings.Contains(stderr.String(), config.Text("server.import_cancelled_late")) {
		t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
	}
	p, _ := s.project(mustProject(t, s, "demo"))
	waitFor(t, importSettled(p), "the server to end the cancelled import")
	if view, err := board.LoadBoard(p.board); err != nil || len(view.Entries) != 0 {
		t.Fatalf("a cancelled import published: %+v %v", view.Entries, err)
	}
}

// importSettled is closed once the project lock is free again.
func importSettled(p *project) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		p.mu.Lock()
		p.mu.Unlock()
		close(done)
	}()
	return done
}

func TestImportCommandTellsAnInterruptedOnlineImportToCheckTheResult(t *testing.T) {
	source := v1Board(t)
	dir := t.TempDir()
	s, _, _ := onlineServer(t, dir)
	reached, release := pauseImport(t, s, "prepared")
	defer release()
	signals := make(chan os.Signal, 1)
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	go func() {
		<-reached
		stderr.waitContains(t, config.Text("server.import_progress_publish"))
		signals <- os.Interrupt
	}()
	code := importCommand(importOptions{data: dir, name: "demo", from: source}, stdout, stderr, false, signals)
	if code != 130 || stdout.String() != "" || !strings.Contains(stderr.String(), config.Text("server.import_cancelled_late")) {
		t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
	}
	release()
	p, _ := s.project(mustProject(t, s, "demo"))
	waitFor(t, importSettled(p), "the server to finish the import")
	// The server finishes what it was past: the same command now finds it imported.
	again, err := s.Import("demo", "", source)
	if err != nil || !again.Existing || again.Tasks != 2 {
		t.Fatalf("rerun: %+v %v", again, err)
	}
}

func TestImportCommandPrintsOnlineProgress(t *testing.T) {
	source := v1Board(t)
	dir := t.TempDir()
	onlineServer(t, dir)
	code, stdout, stderr := runImportCommand(t, dir, source, make(chan os.Signal, 1))
	var result api.ImportResult
	if code != 0 || json.Unmarshal([]byte(stdout.String()), &result) != nil || result.Tasks != 2 {
		t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
	}
	for _, text := range []string{config.Text("server.import_progress_lock"), config.Text("server.import_progress_check"), config.Text("server.import_progress_publish")} {
		if !strings.Contains(stderr.String(), text) {
			t.Fatalf("stderr lacks the server's %q:\n%s", text, stderr.String())
		}
	}
}

func TestImportCommandTreatsAnOnlineCancelWithoutProgressAsUnknown(t *testing.T) {
	source := v1Board(t)
	dir := t.TempDir()
	holder, err := Open(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	reached := make(chan struct{})
	var once sync.Once
	// A server that predates the stream answers once, at the end, and never reports progress.
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/server/import" {
			writeJSONResponse(w, http.StatusOK, api.ServerInfo{DataDir: dir})
			return
		}
		// The server notices a closed connection only after it read the request.
		_, _ = io.Copy(io.Discard, r.Body)
		once.Do(func() { close(reached) })
		<-r.Context().Done()
	}))
	defer old.Close()
	useImportTarget(t, old.URL)
	signals := make(chan os.Signal, 1)
	go func() {
		<-reached
		signals <- os.Interrupt
	}()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	code := importCommand(importOptions{data: dir, name: "demo", from: source}, stdout, stderr, false, signals)
	if code != 130 || stdout.String() != "" || !strings.Contains(stderr.String(), config.Text("server.import_cancelled_late")) || strings.Contains(stderr.String(), config.Text("server.import_cancelled")+"\n") {
		t.Fatalf("exit %d stdout %q\n%s", code, stdout.String(), stderr.String())
	}
}
