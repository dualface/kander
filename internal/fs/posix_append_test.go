//go:build unix

package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	appendCreateRounds = 2000
	appendCreateRacers = 8
)

// Eight goroutines open one new file at once in each round. Every open must succeed and
// every descriptor of a round must name the same file.
func TestOpenAppendFileConcurrentCreate(t *testing.T) {
	root := t.TempDir()
	for round := 0; round < appendCreateRounds; round++ {
		dir := filepath.Join(root, fmt.Sprint(round))
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		target := filepath.Join(dir, "server.log")

		start := make(chan struct{})
		files := make([]*AppendFile, appendCreateRacers)
		errs := make([]error, appendCreateRacers)
		var wg sync.WaitGroup
		for i := range files {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				files[i], errs[i] = OpenAppendFile(root, target)
			}()
		}
		close(start)
		wg.Wait()

		var first Identity
		var haveFirst bool
		var failure error
		for i, file := range files {
			if errs[i] != nil {
				failure = errs[i]
				continue
			}
			id, err := posixIdentity(int(file.Fd()))
			if err != nil {
				failure = err
				continue
			}
			if !haveFirst {
				first, haveFirst = id, true
			} else if id != first {
				failure = fmt.Errorf("descriptors name different files: %v and %v", first, id)
			}
		}
		for _, file := range files {
			if file != nil {
				// Skip the fsync of AppendFile.Close: 16000 of them would dominate the run.
				_ = file.File.Close()
			}
		}
		if failure != nil {
			t.Fatalf("round %d: %v", round, failure)
		}
	}
}

// stubAppendOpen replaces the append open and the backoff for one test. The stub gets the call number, counted from
// one; a non-nil result fails that call, nil runs the real open. The result lists the directory descriptor of every call.
func stubAppendOpen(t *testing.T, backoff []time.Duration, stub func(call int) error) *[]int {
	t.Helper()
	oldOpen, oldBackoff := appendOpenat, appendCreateBackoff
	t.Cleanup(func() { appendOpenat, appendCreateBackoff = oldOpen, oldBackoff })
	appendCreateBackoff = backoff
	var dirfds []int
	appendOpenat = func(dirfd int, name string, flags int, mode uint32) (int, error) {
		dirfds = append(dirfds, dirfd)
		if err := stub(len(dirfds)); err != nil {
			return -1, err
		}
		return openat(dirfd, name, flags, mode)
	}
	return &dirfds
}

func threeMillisecondWaits() []time.Duration {
	return []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
}

func TestOpenAppendFileRetriesAVanishedCreate(t *testing.T) {
	dir := t.TempDir()
	dirfds := stubAppendOpen(t, threeMillisecondWaits(), func(call int) error {
		if call < 3 {
			return unix.ENOENT
		}
		return nil
	})
	file, err := OpenAppendFile(dir, filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	if file == nil {
		t.Fatal("OpenAppendFile returned no file and no error")
	}
	defer file.Close()
	if len(*dirfds) != 3 {
		t.Fatalf("opens=%d, want 3", len(*dirfds))
	}
	if (*dirfds)[0] != (*dirfds)[1] || (*dirfds)[1] != (*dirfds)[2] {
		t.Fatalf("directory descriptors differ across the retries: %v", *dirfds)
	}
}

func TestOpenAppendFileGivesUpOnAPersistentENOENT(t *testing.T) {
	dir := t.TempDir()
	dirfds := stubAppendOpen(t, threeMillisecondWaits(), func(int) error { return unix.ENOENT })
	_, err := OpenAppendFile(dir, filepath.Join(dir, "server.log"))
	if err == nil {
		t.Fatal("OpenAppendFile succeeded, want the missing-path error")
	}
	if len(*dirfds) != len(threeMillisecondWaits())+1 {
		t.Fatalf("opens=%d, want %d", len(*dirfds), len(threeMillisecondWaits())+1)
	}
	if text := err.Error(); !strings.Contains(text, "append") || !strings.Contains(text, "protected path does not exist") {
		t.Fatalf("error %q lacks the operation or the cause", text)
	}
	if !isNotExist(err) {
		t.Fatalf("error %v is not a not-exist error", err)
	}
}

func TestOpenAppendFileDoesNotRetryOtherErrors(t *testing.T) {
	dir := t.TempDir()
	dirfds := stubAppendOpen(t, threeMillisecondWaits(), func(int) error { return unix.ELOOP })
	_, err := OpenAppendFile(dir, filepath.Join(dir, "server.log"))
	if err == nil || !strings.Contains(err.Error(), "symlink is not allowed") {
		t.Fatalf("error %v, want the symlink refusal", err)
	}
	if len(*dirfds) != 1 {
		t.Fatalf("opens=%d, want 1", len(*dirfds))
	}
}

func TestAppendCreateBackoffIsDarwinOnly(t *testing.T) {
	var total time.Duration
	for _, wait := range appendCreateBackoff {
		total += wait
	}
	if runtime.GOOS != "darwin" {
		if len(appendCreateBackoff) != 0 {
			t.Fatalf("backoff %v on %s, want none", appendCreateBackoff, runtime.GOOS)
		}
		return
	}
	if len(appendCreateBackoff) == 0 || total > 100*time.Millisecond {
		t.Fatalf("backoff %v adds up to %v, want a non-empty list of at most 100ms", appendCreateBackoff, total)
	}
}

func TestOpenAppendFileRejectsASymlinkLeaf(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	leaf := filepath.Join(dir, "server.log")
	if err := os.Symlink(target, leaf); err != nil {
		t.Skip(err)
	}
	file, err := OpenAppendFile(dir, leaf)
	if file != nil {
		_ = file.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "symlink is not allowed") {
		t.Fatalf("error %v, want the symlink refusal", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("the link target was created: %v", err)
	}
}

func TestOpenAppendFileRejectsAFIFOLeaf(t *testing.T) {
	dir := t.TempDir()
	leaf := filepath.Join(dir, "server.log")
	if err := unix.Mkfifo(leaf, 0o600); err != nil {
		t.Skip(err)
	}
	type result struct {
		file *AppendFile
		err  error
	}
	done := make(chan result, 1)
	go func() {
		file, err := OpenAppendFile(dir, leaf)
		done <- result{file, err}
	}()
	select {
	case got := <-done:
		if got.file != nil {
			_ = got.file.Close()
			t.Fatal("a FIFO opened as an append file")
		}
		if got.err == nil || !strings.Contains(got.err.Error(), "task document is not a regular file") {
			t.Fatalf("error %v, want the regular-file refusal", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OpenAppendFile blocked on a FIFO")
	}
}
