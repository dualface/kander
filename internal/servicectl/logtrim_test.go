package servicectl

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// logData returns n numbered lines of varying length.
func logData(n int) []byte {
	var b bytes.Buffer
	for i := range n {
		fmt.Fprintf(&b, "{\"line\":%d,\"pad\":%q}\n", i, strings.Repeat("x", i%37))
	}
	return b.Bytes()
}

func logDir(t *testing.T, content []byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if content != nil {
		if err := os.WriteFile(filepath.Join(dir, LogName), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func gunzip(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// expectedCut is the first line start at or after size-keep.
func expectedCut(data []byte, keep int64) int64 {
	from := int64(len(data)) - keep
	if from <= 0 || data[from-1] == '\n' {
		return max(from, 0)
	}
	i := bytes.IndexByte(data[from:], '\n')
	if i < 0 {
		return int64(len(data))
	}
	return from + int64(i) + 1
}

func TestTrimLogKeepsTheTailAndArchivesTheHead(t *testing.T) {
	data := logData(5000)
	keep := int64(len(data) / 3)
	dir := logDir(t, data)
	if err := os.WriteFile(filepath.Join(dir, LogArchiveName), []byte("older generation"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := TrimLog(dir, keep, false)
	if err != nil {
		t.Fatal(err)
	}
	cut := expectedCut(data, keep)
	if cut == int64(len(data))-keep {
		t.Fatal("the test needs a limit that falls inside a line")
	}
	if result.Size != int64(len(data)) || result.Bytes != cut || result.Recovered != 0 {
		t.Fatalf("result = %+v, want size %d and bytes %d", result, len(data), cut)
	}
	kept, err := os.ReadFile(filepath.Join(dir, LogName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kept, data[cut:]) {
		t.Fatal("server.log must hold exactly the tail from the first line start")
	}
	if got := gunzip(t, filepath.Join(dir, LogArchiveName)); !bytes.Equal(got, data[:cut]) {
		t.Fatal("server.log.1.gz must hold exactly the head and replace the older generation")
	}
	for _, name := range []string{logArchiveTemp, logArchiveOld, logTailName, logTailTemp} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s left behind: %v", name, err)
		}
	}
}

func TestTrimLogLeavesASmallOrMissingLogAlone(t *testing.T) {
	data := logData(100)
	dir := logDir(t, data)
	result, err := TrimLog(dir, int64(len(data)), false)
	if err != nil || result.Bytes != 0 || result.Size != int64(len(data)) {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, LogName)); !bytes.Equal(kept, data) {
		t.Fatal("a log not larger than the limit must stay as it is")
	}
	if _, err := os.Stat(filepath.Join(dir, LogArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("no archive may be written: %v", err)
	}
	missing := logDir(t, nil)
	result, err = TrimLog(missing, 1, false)
	if err != nil || result.Size != 0 || result.Bytes != 0 {
		t.Fatalf("missing log: %+v, %v", result, err)
	}
	if entries, _ := os.ReadDir(missing); len(entries) != 0 {
		t.Fatalf("a missing log must not create files: %v", entries)
	}
}

func TestTrimLogDryRunChangesNothing(t *testing.T) {
	data := logData(3000)
	dir := logDir(t, data)
	if err := os.WriteFile(filepath.Join(dir, logTailName), []byte("saved tail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := TrimLog(dir, 1024, true)
	if err != nil || result.Bytes != expectedCut(data, 1024) {
		t.Fatalf("dry run = %+v, %v", result, err)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, LogName)); !bytes.Equal(kept, data) {
		t.Fatal("a dry run must not change server.log")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("a dry run must not create or remove files: %v", entries)
	}
}

func TestTrimLogKeepsLinesTheServerWritesAfterwards(t *testing.T) {
	data := logData(4000)
	dir := logDir(t, data)
	// The server holds the log in append mode for its whole life.
	server, err := fs.OpenAppendFile(dir, filepath.Join(dir, LogName))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := TrimLog(dir, 2048, false); err != nil {
		t.Fatal(err)
	}
	line := "{\"after\":\"trim\"}\n"
	if _, err := server.WriteString(line); err != nil {
		t.Fatal(err)
	}
	kept, _ := os.ReadFile(filepath.Join(dir, LogName))
	want := append(append([]byte{}, data[expectedCut(data, 2048):]...), line...)
	if !bytes.Equal(kept, want) {
		t.Fatalf("a line written after the trim must follow the kept tail:\n%q", kept[max(0, len(kept)-200):])
	}
}

func TestTrimLogRecoversAnInterruptedTail(t *testing.T) {
	for _, step := range []string{"tail", "truncate"} {
		t.Run(step, func(t *testing.T) {
			data := logData(4000)
			dir := logDir(t, data)
			cut := expectedCut(data, 4096)
			stop := errors.New("interrupted")
			logTrimStep = func(s string) error {
				if s == step {
					return stop
				}
				return nil
			}
			_, err := TrimLog(dir, 4096, false)
			logTrimStep = nil
			if !errors.Is(err, stop) {
				t.Fatalf("interrupt: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, logTailName)); err != nil {
				t.Fatalf("the saved tail must survive the interruption: %v", err)
			}
			plan, err := TrimLog(dir, 1<<30, true)
			if err != nil || plan.Recovered != int64(len(data))-cut {
				t.Fatalf("a dry run must report the tail to append back: %+v, %v", plan, err)
			}
			result, err := TrimLog(dir, 1<<30, false)
			if err != nil || result.Recovered != int64(len(data))-cut {
				t.Fatalf("recovery = %+v, %v", result, err)
			}
			kept, _ := os.ReadFile(filepath.Join(dir, LogName))
			if !bytes.HasSuffix(kept, data[cut:]) {
				t.Fatal("the next trim must append the saved tail back")
			}
			if step == "truncate" && !bytes.Equal(kept, data[cut:]) {
				t.Fatal("after the truncation the log must hold the tail exactly once")
			}
			if _, err := os.Stat(filepath.Join(dir, logTailName)); !os.IsNotExist(err) {
				t.Fatalf("the saved tail must be removed after recovery: %v", err)
			}
		})
	}
}

// An archive replacement that stops after the previous archive went aside,
// or whose final rename fails, never loses the previous generation.
func TestTrimLogKeepsThePreviousArchiveUntilTheNewOneIsInPlace(t *testing.T) {
	data := logData(4000)
	dir := logDir(t, data)
	older := []byte("older generation")
	if err := os.WriteFile(filepath.Join(dir, LogArchiveName), older, 0o600); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("interrupted")
	logTrimStep = func(s string) error {
		if s == "archive-aside" {
			return stop
		}
		return nil
	}
	_, err := TrimLog(dir, 4096, false)
	logTrimStep = nil
	if !errors.Is(err, stop) {
		t.Fatalf("interrupt: %v", err)
	}
	if aside, err := os.ReadFile(filepath.Join(dir, logArchiveOld)); err != nil || !bytes.Equal(aside, older) {
		t.Fatalf("the previous archive must wait aside: %v", err)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, LogName)); !bytes.Equal(kept, data) {
		t.Fatal("the log must not change before the archive is in place")
	}
	if _, err := TrimLog(dir, 1<<30, false); err != nil {
		t.Fatal(err)
	}
	if back, err := os.ReadFile(filepath.Join(dir, LogArchiveName)); err != nil || !bytes.Equal(back, older) {
		t.Fatalf("the next trim must put the previous archive back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, logArchiveOld)); !os.IsNotExist(err) {
		t.Fatalf("nothing may stay aside after recovery: %v", err)
	}

	// The final rename fails: the temporary archive disappears after the
	// previous one went aside, so the rename cannot complete.
	logTrimStep = func(s string) error {
		if s == "archive-aside" {
			if err := os.Remove(filepath.Join(dir, logArchiveTemp)); err != nil {
				return err
			}
		}
		return nil
	}
	_, err = TrimLog(dir, 4096, false)
	logTrimStep = nil
	if err == nil {
		t.Fatal("a failed archive rename must fail the trim")
	}
	if back, err := os.ReadFile(filepath.Join(dir, LogArchiveName)); err != nil || !bytes.Equal(back, older) {
		t.Fatalf("a failed rename must put the previous archive back: %v", err)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, LogName)); !bytes.Equal(kept, data) {
		t.Fatal("a failed archive must leave the log alone")
	}
}

func TestTrimLogRemovesAPartialTailCopy(t *testing.T) {
	data := logData(200)
	dir := logDir(t, data)
	if err := os.WriteFile(filepath.Join(dir, logTailTemp), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := TrimLog(dir, int64(len(data)), false)
	if err != nil || result.Recovered != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, LogName)); !bytes.Equal(kept, data) {
		t.Fatal("a partial tail copy means the log was never truncated, so it must stay as it is")
	}
	if _, err := os.Stat(filepath.Join(dir, logTailTemp)); !os.IsNotExist(err) {
		t.Fatalf("the partial copy must be removed: %v", err)
	}
}

func TestTrimLogRunsOneTrimAtATime(t *testing.T) {
	dir := logDir(t, logData(2000))
	held := false
	logTrimStep = func(string) error {
		if logTrimMu.TryLock() {
			logTrimMu.Unlock()
		} else {
			held = true
		}
		return nil
	}
	defer func() { logTrimStep = nil }()
	if _, err := TrimLog(dir, 1024, false); err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("a trim must hold the process-wide trim lock through its steps")
	}
}

// Two trims that really overlap: the second starts while the first is inside
// its steps, takes none of its own until the first ended, and then finds the
// log already trimmed.
func TestTrimLogSerializesOverlappingTrims(t *testing.T) {
	data := logData(4000)
	dir := logDir(t, data)
	entered, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var steps []string
	first := true
	logTrimStep = func(step string) error {
		mu.Lock()
		steps = append(steps, step)
		blocking := first && step == "tail"
		if blocking {
			first = false
		}
		mu.Unlock()
		if blocking {
			close(entered)
			<-release
		}
		return nil
	}
	defer func() { logTrimStep = nil }()
	results := make(chan LogTrim, 2)
	go func() {
		r, err := TrimLog(dir, 4096, false)
		if err != nil {
			t.Error(err)
		}
		results <- r
	}()
	<-entered
	go func() {
		r, err := TrimLog(dir, 4096, false)
		if err != nil {
			t.Error(err)
		}
		results <- r
	}()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	during := slices.Clone(steps)
	mu.Unlock()
	close(release)
	a, b := <-results, <-results
	if !slices.Equal(during, []string{"archive", "tail"}) {
		t.Fatalf("the second trim took a step while the first was inside its steps: %v", during)
	}
	cut := expectedCut(data, 4096)
	if a.Bytes+b.Bytes != cut || min(a.Bytes, b.Bytes) != 0 {
		t.Fatalf("one trim moves the head, the other finds nothing to do: %+v %+v", a, b)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, LogName)); !bytes.Equal(kept, data[cut:]) {
		t.Fatal("overlapping trims must leave exactly the kept tail")
	}
}

func TestAppendLinesWritesWholeLines(t *testing.T) {
	long := strings.Repeat("y", lineFlush+10) + "\n"
	src := "a\n" + strings.Repeat("b", 100) + "\n" + long + "tail without newline"
	var writes []string
	w := writerFunc(func(p []byte) (int, error) { writes = append(writes, string(p)); return len(p), nil })
	if err := appendLines(w, strings.NewReader(src)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(writes, "") != src {
		t.Fatal("appendLines must copy every byte")
	}
	for _, write := range writes[:len(writes)-1] {
		if len(write) <= lineFlush && !strings.HasSuffix(write, "\n") {
			t.Fatalf("a write that fits the buffer must end at a line end: %q...", write[:min(20, len(write))])
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
