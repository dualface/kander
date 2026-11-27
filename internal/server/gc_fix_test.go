package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/servicectl"
)

// stubCompaction replaces the review run compaction for one test.
func stubCompaction(t *testing.T, fn func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error)) {
	t.Helper()
	previous := gcCompact
	t.Cleanup(func() { gcCompact = previous })
	gcCompact = fn
}

func noCompaction(context.Context, string, board.CompactionOptions) (board.CompactionReport, error) {
	return board.CompactionReport{}, nil
}

// receiptFile is the path of a receipt key below a project directory.
func receiptFile(root, key string) string { return filepath.Join(root, receiptPath(key)) }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// A board transaction that wrote a done receipt but stopped before its record
// was committed needs that receipt at recovery. An offline gc keeps every
// receipt until the server recovered the record.
func TestOfflineGCKeepsReceiptsWhileABoardTransactionWaitsForRecovery(t *testing.T) {
	dir, _ := offlineData(t)
	root, boardRoot := projectRoot(dir, "demo"), projectBoardPath(dir, "demo")
	old := time.Now().UTC().Add(-48 * time.Hour)
	key := "observe-crash"
	pending := receipt{Schema: receiptSchema, Key: key, Operation: "observation:20261101-a-task", Digest: "d", Status: receiptPending, CreatedAt: old}
	done := pending
	done.Status, done.HTTP, done.Body = receiptDone, http.StatusOK, json.RawMessage(`{}`)
	if err := writeJSON(root, receiptPath(key), done); err != nil {
		t.Fatal(err)
	}
	before, _ := json.MarshalIndent(pending, "", "  ")
	beforeText := string(before) + "\n"
	after, err := os.ReadFile(receiptFile(root, key))
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(boardRoot, receiptFile(root, key))
	record := board.OperationRecord{Schema: 1, ID: "0123456789abcdef0123456789abcdef", Phase: "prepared", Revisions: map[string]uint64{}, Groups: []string{serviceGroup},
		Files: []board.FileChange{{Path: rel, Before: &beforeText, After: string(after)}}}
	data, _ := json.MarshalIndent(record, "", "  ")
	pendingDir := filepath.Join(boardRoot, ".kander", "operations", "pending")
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir, record.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	offline, err := OpenOfflineGC(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	stubCompaction(t, noCompaction)
	report, err := offline.Run(t.Context(), api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20})
	if closeErr := offline.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	if !fileExists(receiptFile(root, key)) || report.Projects[0].Receipts.Files != 0 || !report.Incomplete() ||
		!slices.ContainsFunc(report.Projects[0].Errors, func(e string) bool { return strings.Contains(e, "board transaction") }) {
		t.Fatalf("a receipt a prepared transaction stages must stay and be reported: %+v", report.Projects[0])
	}

	// The server recovers the record; afterwards the receipt is an ordinary due one.
	s, err := Open(Options{DataDir: dir, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatalf("recovery must find the receipt it staged: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	offline, err = OpenOfflineGC(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	report, err = offline.Run(t.Context(), api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20})
	if err != nil || fileExists(receiptFile(root, key)) || report.Projects[0].Receipts.Files != 1 {
		t.Fatalf("after recovery the receipt goes: %+v %v", report.Projects[0], err)
	}
}

// A gc skips, without waiting, a project whose board an import replaces, and an
// import waits until the gc of that project ended.
func TestServerGCAndImportExcludeEachOther(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	info, _ := f.s.projectByName("demo")
	p, _ := f.s.project(info.ID)

	p.maintenance.Lock()
	calls := 0
	stubCompaction(t, func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error) {
		calls++
		return board.CompactionReport{}, nil
	})
	started := time.Now()
	report, err := f.gc(api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20})
	p.maintenance.Unlock()
	if err != nil || calls != 0 || !report.Incomplete() || len(report.Projects) != 1 ||
		!slices.Contains(report.Projects[0].Errors, errGCProjectBusy.Error()) || time.Since(started) > 5*time.Second {
		t.Fatalf("a busy project must be skipped without waiting: %+v %v (compactions %d)", report, err, calls)
	}

	// The gc holds the project while it compacts; an import that starts then
	// reaches the board only after the gc released it.
	entered, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var order []string
	note := func(event string) {
		mu.Lock()
		order = append(order, event)
		mu.Unlock()
	}
	stubCompaction(t, func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error) {
		close(entered)
		<-release
		note("compaction ended")
		return board.CompactionReport{}, nil
	})
	gcDone := make(chan error, 1)
	go func() {
		_, err := f.gc(api.ServerGCRequest{Project: "demo", OlderThan: "1h", LogKeep: 1 << 20})
		gcDone <- err
	}()
	<-entered
	source := v1Board(t)
	scanned := make(chan struct{})
	var once sync.Once
	importDone := make(chan error, 1)
	go func() {
		_, err := f.s.ImportWithProgress(context.Background(), "demo", "", source, func(progress api.ImportProgress) {
			if progress.Phase == api.ImportPhaseScan {
				once.Do(func() { note("import scanned"); close(scanned) })
			}
		})
		importDone <- err
	}()
	select {
	case <-scanned:
		t.Fatal("an import reached the board while a gc of the project ran")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if err := <-importDone; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(order, []string{"compaction ended", "import scanned"}) {
		t.Fatalf("order = %v", order)
	}
}

// Only claims, takeovers and confirmations are refused by every terminal card;
// any other receipt waits until its card is archived.
func TestReceiptDueWaitsUntilALateRetryCannotWrite(t *testing.T) {
	const doneID, trashID, archivedID = "20261101-done-task", "20261101-trash-task", "20261101-archived-task"
	facts := &bindingFacts{scanned: &board.Board{Entries: map[string]board.Entry{
		doneID: {TaskID: doneID, State: "done"}, trashID: {TaskID: trashID, State: "trash"}, archivedID: {TaskID: archivedID, State: "archived"},
	}}}
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	for operation, want := range map[string]bool{
		"assign:" + doneID: false, "decision-mode:" + doneID: false, "decision-human:" + trashID: false,
		"put:" + doneID + ":spec.md": false, "move:" + doneID: false, "move:" + trashID: false,
		"request:" + doneID: false, "dispatch:" + doneID + ":accept": false, "execution:" + doneID: false,
		"claim:" + doneID: true, "takeover:" + trashID: true, "confirm:" + doneID: true,
		"assign:" + archivedID: true, "move:" + archivedID: true, "put:" + archivedID + ":spec.md": true,
	} {
		r := receipt{Operation: operation, Status: receiptDone, CreatedAt: now.Add(-48 * time.Hour)}
		if got, err := receiptDue(r, now, time.Hour, facts); err != nil || got != want {
			t.Errorf("%s: due %v (%v), want %v", operation, got, err, want)
		}
	}
}

// Requests the administrator check refuses get 403 before the protocol
// handshake and the readiness gate decide anything.
func TestServerGCRouteRefusesBeforeTheHandshake(t *testing.T) {
	f := newGCFixture(t)
	request := api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20}
	plain := func(peer, token string) *http.Request {
		r := gcRequest(nil, peer, token, request)
		r.Header.Del(api.HeaderProtocol)
		return r
	}
	for label, r := range map[string]*http.Request{
		"no protocol, no token":   plain("127.0.0.1:4567", ""),
		"no protocol, remote":     plain("192.0.2.1:4567", f.s.adminToken),
		"no protocol, wrong key":  plain("127.0.0.1:4567", strings.Repeat("b", 64)),
		"with protocol, no token": gcRequest(nil, "127.0.0.1:4567", "", request),
	} {
		out := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(out, r)
		if out.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", label, out.Code, out.Body.String())
		}
	}
	f.s.ready.Store(false)
	defer f.s.ready.Store(true)
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, gcRequest(nil, "192.0.2.1:4567", f.s.adminToken, request))
	if out.Code != http.StatusForbidden {
		t.Fatalf("a remote peer of a server that is not ready: %d", out.Code)
	}
	out = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, gcRequest(nil, "127.0.0.1:4567", f.s.adminToken, request))
	if out.Code != http.StatusServiceUnavailable {
		t.Fatalf("the administrator of a server that is not ready: %d", out.Code)
	}
}

// A receipt that cannot be read or decoded is a kept candidate: it makes the
// gc exit 1, and the receipts after it are still handled.
func TestServerGCKeepsUnreadableReceiptsAndGoesOn(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a file the current user cannot read")
	}
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	info, _ := f.s.projectByName("demo")
	p, _ := f.s.project(info.ID)
	old := f.clock.now().UTC().Add(-48 * time.Hour)
	var due []string
	for _, key := range []string{"a-due", "z-due"} {
		writeReceipt(t, f.s, "demo", receipt{Key: key, Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: old})
		due = append(due, receiptFile(p.root, key))
	}
	unreadable := writeReceipt(t, f.s, "demo", receipt{Key: "m-unreadable", Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: old})
	if err := os.Chmod(filepath.Join(p.root, servicePath("receipts", unreadable)), 0); err != nil {
		t.Fatal(err)
	}
	damaged := filepath.Join(p.root, servicePath("receipts", strings.Repeat("e", 64)+".json"))
	if err := os.WriteFile(damaged, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := f.gc(api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	receipts := report.Projects[0].Receipts
	var kept []string
	for _, k := range receipts.Kept {
		kept = append(kept, k.ID)
	}
	slices.Sort(kept)
	if receipts.Files != 2 || !slices.Equal(kept, []string{strings.TrimSuffix(unreadable, ".json"), strings.Repeat("e", 64)}) || !report.Incomplete() {
		t.Fatalf("receipts = %+v", receipts)
	}
	for _, path := range due {
		if fileExists(path) {
			t.Fatalf("%s must go although an earlier receipt could not be read", path)
		}
	}
}

// A batch an interrupted gc recorded is finished first, with its recorded
// retention: a member still as recorded goes, one already gone counts, and one
// whose content changed is decided anew.
func TestServerGCFinishesTheReceiptBatchAnInterruptedGCRecorded(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	info, _ := f.s.projectByName("demo")
	p, _ := f.s.project(info.ID)
	old := f.clock.now().UTC().Add(-48 * time.Hour)
	recorded := func(key string) receiptIntentMember {
		name := writeReceipt(t, f.s, "demo", receipt{Key: key, Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: old})
		data, err := os.ReadFile(receiptFile(p.root, key))
		if err != nil {
			t.Fatal(err)
		}
		return receiptIntentMember{Name: name, SHA256: contentHash(data), Bytes: int64(len(data))}
	}
	same, gone, replaced := recorded("same"), recorded("gone"), recorded("replaced")
	if err := os.Remove(receiptFile(p.root, "gone")); err != nil {
		t.Fatal(err)
	}
	// A fresh receipt now holds the name: it is not due, so it stays.
	writeReceipt(t, f.s, "demo", receipt{Key: "replaced", Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: f.clock.now().UTC()})
	intent := receiptIntent{Schema: 1, OlderThan: "1h", Receipts: []receiptIntentMember{same, gone, replaced}}
	if err := writeReceiptIntent(p.root, intent); err != nil {
		t.Fatal(err)
	}
	request := api.ServerGCRequest{DryRun: true, OlderThan: "1h", LogKeep: 1 << 20}
	plan, err := f.gc(request)
	if err != nil || plan.Projects[0].Receipts.Files != 1 || !fileExists(receiptIntentPath(p.root)) {
		t.Fatalf("plan: %+v %v", plan.Projects[0], err)
	}
	request.DryRun = false
	report, err := f.gc(request)
	if err != nil || report.Incomplete() {
		t.Fatal(err, report)
	}
	if got := report.Projects[0].Receipts; got.Files != 2 || got.Bytes != same.Bytes+gone.Bytes {
		t.Fatalf("receipts = %+v, want the recorded member and the one already gone", got)
	}
	if fileExists(receiptFile(p.root, "same")) || !fileExists(receiptFile(p.root, "replaced")) || fileExists(receiptIntentPath(p.root)) {
		t.Fatal("the recorded batch must end, and only with members still as recorded")
	}
}

// A server started with another --listen than server.url is found through the
// address it recorded in its data directory.
func TestServerGCCommandFindsAServerThroughItsRecordedAddress(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	gcCommandEnv(t, unusedTarget, "")
	calls := 0
	stubCompaction(t, func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error) {
		calls++
		return board.CompactionReport{}, nil
	})
	code, stdout, stderr := runGCCommand("--data", f.s.data, "--yes", "--json")
	if code != 1 || calls != 0 {
		t.Fatalf("without a record no server is found: exit %d, compactions %d\n%s\n%s", code, calls, stdout, stderr)
	}
	if err := writeJSON(f.s.data, servicectl.ListenRecordName, servicectl.ListenRecord{Listen: f.http.Listener.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runGCCommand("--data", f.s.data, "--yes", "--json")
	if code != 0 || calls != 2 {
		t.Fatalf("the recorded address (a plan and a run): exit %d, compactions %d\n%s\n%s", code, calls, stdout, stderr)
	}
}

// RunForeground records the address it listens on and removes the record when
// it stops.
func TestRunForegroundRecordsItsListenAddress(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunForeground(ctx, Options{DataDir: dir, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Ready: func(listen string) { ready <- listen }}, "127.0.0.1:0")
	}()
	listen := <-ready
	recorded, err := servicectl.RecordedURL(dir)
	if want, _ := servicectl.ProbeURL(listen); err != nil || recorded != want {
		t.Fatalf("recorded %q %v, want %q", recorded, err, want)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dir, servicectl.ListenRecordName)) {
		t.Fatal("a stopped server must remove its record")
	}
}

// An interrupt while the prompt waits ends the command: nothing is removed and
// the data directory lock is free again.
func TestServerGCCommandStopsAtAnInterruptDuringThePrompt(t *testing.T) {
	dir, log := offlineData(t)
	gcCommandEnv(t, unusedTarget, "tty:")
	in, writer := io.Pipe()
	t.Cleanup(func() { writer.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	previousIn, previousOut, previousInterrupt := gcPromptIn, gcPromptOut, gcInterrupt
	t.Cleanup(func() { gcPromptIn, gcPromptOut, gcInterrupt = previousIn, previousOut, previousInterrupt })
	gcPromptIn = in
	gcPromptOut = writerFunc(func(p []byte) (int, error) {
		if bytes.Contains(p, []byte("[y/N]")) {
			cancel()
		}
		return len(p), nil
	})
	gcInterrupt = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	finished := make(chan int, 1)
	go func() {
		code, _, _ := runGCCommand("--data", dir, "--log-keep", "1MiB")
		finished <- code
	}()
	select {
	case code := <-finished:
		if code != 1 {
			t.Fatalf("an interrupted prompt: exit %d, want 1", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command kept waiting for an answer after the interrupt")
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, "server.log")); !bytes.Equal(kept, log) {
		t.Fatal("an interrupted prompt must not trim the log")
	}
	offline, err := OpenOfflineGC(dir, "test")
	if err != nil {
		t.Fatalf("the lock must be free after the command ended: %v", err)
	}
	offline.Close()
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// With --yes the text output shows the plan before the result.
func TestServerGCCommandPrintsThePlanBeforeAYesRun(t *testing.T) {
	dir, _ := offlineData(t)
	gcCommandEnv(t, unusedTarget, "")
	code, stdout, stderr := runGCCommand("--data", dir, "--yes", "--log-keep", "1MiB")
	plan, result := strings.Index(stdout, "gc plan"), strings.Index(stdout, "gc result")
	if code != 0 || plan < 0 || result < plan {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
}

// The tail an interrupted trim saved is a plan item of its own: the command
// asks for it and appends it back, also when nothing else is due.
func TestServerGCCommandOffersTheRecoveryOfASavedLogTail(t *testing.T) {
	dir, _ := offlineData(t)
	if err := os.WriteFile(filepath.Join(dir, "server.log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tail := []byte("{\"kept\":\"tail\"}\n")
	if err := os.WriteFile(filepath.Join(dir, "server.log.tail"), tail, 0o600); err != nil {
		t.Fatal(err)
	}
	stubCompaction(t, noCompaction)
	gcCommandEnv(t, unusedTarget, "")
	code, stdout, _ := runGCCommand("--data", dir, "--dry-run", "--json")
	var plan api.ServerGCReport
	if code != 0 || json.Unmarshal([]byte(stdout), &plan) != nil || plan.Log == nil || plan.Log.Recovered != int64(len(tail)) {
		t.Fatalf("the plan must report the saved tail: exit %d %s", code, stdout)
	}
	prompt := gcCommandEnv(t, unusedTarget, "tty:y\n")
	code, stdout, stderr := runGCCommand("--data", dir)
	if code != 0 || !strings.Contains(prompt.String(), "[y/N]") {
		t.Fatalf("a pending recovery must be confirmed and run: exit %d, prompt %q\n%s\n%s", code, prompt.String(), stdout, stderr)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, "server.log")); !bytes.Equal(kept, tail) || fileExists(filepath.Join(dir, "server.log.tail")) {
		t.Fatalf("the saved tail must be back in server.log: %q", kept)
	}
}

// Two gc requests that really overlap: the second is refused while the first
// is inside its compaction, and the first still ends normally.
func TestServerGCRefusesAnOverlappingRequest(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	entered, release := make(chan struct{}), make(chan struct{})
	stubCompaction(t, func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error) {
		close(entered)
		<-release
		return board.CompactionReport{}, nil
	})
	first := make(chan error, 1)
	go func() {
		_, err := f.gc(api.ServerGCRequest{Project: "demo", OlderThan: "1h", LogKeep: 1 << 20})
		first <- err
	}()
	<-entered
	gcCommandEnv(t, f.http.URL, "")
	code, stdout, stderr := runGCCommand("--data", f.s.data, "--yes", "--json", "--project", "demo")
	close(release)
	if code != 2 || !strings.Contains(stdout+stderr, api.CodeGCRunning) {
		t.Fatalf("an overlapping gc: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

// A server that does not answer the first probe but holds the data directory
// lock is asked again and does the gc.
func TestServerGCCommandTurnsOnlineWhenTheLockIsHeld(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	gcCommandEnv(t, f.http.URL, "")
	stubCompaction(t, noCompaction)
	probes := 0
	previous := gcProbe
	t.Cleanup(func() { gcProbe = previous })
	gcProbe = func(ctx context.Context, target, dir string) (api.ServerInfo, error) {
		probes++
		if probes == 1 {
			return api.ServerInfo{}, servicectl.ErrNoServer
		}
		return previous(ctx, target, dir)
	}
	code, stdout, stderr := runGCCommand("--data", f.s.data, "--yes", "--json")
	var result api.ServerGCReport
	if code != 0 || probes != 2 || json.Unmarshal([]byte(stdout), &result) != nil || len(result.Projects) != 1 {
		t.Fatalf("exit %d, probes %d\n%s\n%s", code, probes, stdout, stderr)
	}
}
