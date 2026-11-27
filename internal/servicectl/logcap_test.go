package servicectl

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// runCapper runs c until the returned stop is called; stop waits for Run.
func runCapper(t *testing.T, c LogCapper) (stop func()) {
	t.Helper()
	c.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.Run(ctx) }()
	stopped := false
	stop = func() {
		if !stopped {
			stopped = true
			cancel()
			<-done
		}
	}
	t.Cleanup(stop)
	return stop
}

func openAppend(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// A server whose stderr is its server.log trims it once it grows above the
// limit, within one check interval, like a gc trim with the limit kept.
func TestLogCapperTrimsTheLogTheServerWritesTo(t *testing.T) {
	initial := logData(200)
	limit := int64(len(initial) + 1024)
	dir := logDir(t, initial)
	stderr := NewLogWriter(openAppend(t, filepath.Join(dir, LogName)))
	stop := runCapper(t, LogCapper{DataDir: dir, Log: stderr, Limit: limit, Interval: 20 * time.Millisecond})

	// Below the limit nothing changes.
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, LogArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("a log below the limit must stay as it is: %v", err)
	}
	extra := logData(5000)[len(initial):]
	if _, err := stderr.Write(extra); err != nil {
		t.Fatal(err)
	}
	data := append(append([]byte{}, initial...), extra...)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, LogArchiveName)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the log above the limit was not trimmed within the check interval")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	cut := expectedCut(data, limit)
	if cut == int64(len(data))-limit {
		t.Fatal("the test needs a limit that falls inside a line")
	}
	kept, err := os.ReadFile(filepath.Join(dir, LogName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kept, data[cut:]) {
		t.Fatal("server.log must hold the last limit bytes from the first line start")
	}
	if got := gunzip(t, filepath.Join(dir, LogArchiveName)); !bytes.Equal(got, data[:cut]) {
		t.Fatal("server.log.1.gz must hold everything before the kept tail")
	}
}

// A server that logs anywhere else, a terminal or a file the user chose,
// trims no file.
func TestLogCapperLeavesALogItDoesNotWriteAlone(t *testing.T) {
	data := logData(5000)
	dir := logDir(t, data)
	other := NewLogWriter(openAppend(t, filepath.Join(t.TempDir(), "other.log")))
	stop := runCapper(t, LogCapper{DataDir: dir, Log: other, Limit: 1024, Interval: 10 * time.Millisecond})
	time.Sleep(100 * time.Millisecond)
	stop()
	kept, err := os.ReadFile(filepath.Join(dir, LogName))
	if err != nil || !bytes.Equal(kept, data) {
		t.Fatalf("server.log must stay as it is: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, LogArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("no archive may appear: %v", err)
	}
}

func TestWritesLogComparesFileIdentity(t *testing.T) {
	dir := logDir(t, []byte("x\n"))
	own := openAppend(t, filepath.Join(dir, LogName))
	if ok, err := WritesLog(dir, own); err != nil || !ok {
		t.Fatalf("the data directory log itself: %v %v", ok, err)
	}
	other := openAppend(t, filepath.Join(t.TempDir(), LogName))
	if ok, err := WritesLog(dir, other); err != nil || ok {
		t.Fatalf("a log of the same name elsewhere: %v %v", ok, err)
	}
	if ok, err := WritesLog(logDir(t, nil), own); err != nil || ok {
		t.Fatalf("a data directory without a log: %v %v", ok, err)
	}
	if ok, err := WritesLog(dir, nil); err != nil || ok {
		t.Fatalf("no log file: %v %v", ok, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	linked := logDir(t, nil)
	if err := os.Symlink(filepath.Join(dir, LogName), filepath.Join(linked, LogName)); err != nil {
		t.Fatal(err)
	}
	if ok, err := WritesLog(linked, own); err != nil || ok {
		t.Fatalf("a symbolic link named server.log is not the log: %v %v", ok, err)
	}
}

// A shell's plain 2> gives the server a descriptor without O_APPEND. After the
// trim the server's next lines follow the kept tail: no hole of zero bytes, no
// regrowth.
func TestLogCapperKeepsWritingAtTheEndThroughADescriptorWithoutAppend(t *testing.T) {
	dir := logDir(t, nil)
	file, err := os.OpenFile(filepath.Join(dir, LogName), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	stderr := NewLogWriter(file)
	data := logData(5000)
	if _, err := stderr.Write(data); err != nil {
		t.Fatal(err)
	}
	limit := int64(len(data) / 4)
	c := LogCapper{DataDir: dir, Log: stderr, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.check(limit)
	more := []byte("{\"after\":\"trim\"}\n{\"after\":\"again\"}\n")
	if _, err := stderr.Write(more); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(filepath.Join(dir, LogName))
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte{}, data[expectedCut(data, limit):]...), more...)
	if bytes.IndexByte(kept, 0) >= 0 || !bytes.Equal(kept, want) {
		t.Fatalf("the lines after the trim must follow the kept tail: %d bytes, want %d", len(kept), len(want))
	}
}

// A line the server logs while the trim copies the tail, truncates and appends
// it back is neither lost nor written before the kept tail: it waits and
// follows the tail.
func TestLogCapperHoldsTheServersLinesWhileItMovesTheTail(t *testing.T) {
	data := logData(5000)
	dir := logDir(t, data)
	stderr := NewLogWriter(openAppend(t, filepath.Join(dir, LogName)))
	limit := int64(len(data) / 4)
	lines := map[string]string{"tail": "{\"during\":\"tail\"}\n", "truncate": "{\"during\":\"truncate\"}\n"}
	written := make(chan error, len(lines))
	var order []string
	logTrimStep = func(step string) error {
		line, ok := lines[step]
		if !ok {
			return nil
		}
		order = append(order, line)
		// The server's request goroutine logs while the trim is in this step.
		go func() { _, err := stderr.Write([]byte(line)); written <- err }()
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	t.Cleanup(func() { logTrimStep = nil })
	c := LogCapper{DataDir: dir, Log: stderr, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.check(limit)
	logTrimStep = nil
	for range lines {
		if err := <-written; err != nil {
			t.Fatal(err)
		}
	}
	kept, err := os.ReadFile(filepath.Join(dir, LogName))
	if err != nil {
		t.Fatal(err)
	}
	tail := data[expectedCut(data, limit):]
	if !bytes.HasPrefix(kept, tail) {
		t.Fatal("server.log must start with the kept tail")
	}
	rest := string(kept[len(tail):])
	if rest != order[0]+order[1] && rest != order[1]+order[0] {
		t.Fatalf("the lines logged during the trim must follow the tail: %q", rest)
	}
}

// The background start passes server.log to the server as its stderr, an open
// append handle for the server's whole life. The trim opens the log beside it;
// on Windows that needs handles that share write access.
func TestTrimLogWorksBesideTheHandleTheServerAppendsThrough(t *testing.T) {
	data := logData(5000)
	dir := logDir(t, data)
	// The same open as the background start in Start.
	held, err := fs.OpenAppendFile(dir, filepath.Join(dir, LogName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { held.Close() })
	stderr := NewLogWriter(held.File)
	limit := int64(len(data) / 4)
	c := LogCapper{DataDir: dir, Log: stderr, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.check(limit)
	if _, err := os.Stat(filepath.Join(dir, LogArchiveName)); err != nil {
		t.Fatalf("the trim beside an open append handle: %v", err)
	}
	// A dry run of the gc reads the log beside it too.
	if plan, err := TrimLog(dir, 1024, true); err != nil || plan.Size == 0 {
		t.Fatalf("a dry run beside an open append handle: %+v %v", plan, err)
	}
	kept, _ := os.ReadFile(filepath.Join(dir, LogName))
	if !bytes.Equal(kept, data[expectedCut(data, limit):]) {
		t.Fatal("server.log must hold the kept tail")
	}
}
