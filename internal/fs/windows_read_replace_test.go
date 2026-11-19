//go:build windows

package fs

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// leafReader reads one file through a public read entrance. found reports whether the entrance handed anything back,
// data or a file, whatever its error; an entrance that rejects a path must hand back nothing.
type leafReader struct {
	name string
	read func(root, path string) (content string, found bool, err error)
}

var leafReaders = []leafReader{
	{"ReadRegularFile", func(root, path string) (string, bool, error) {
		data, err := ReadRegularFile(root, path)
		return string(data), data != nil, err
	}},
	{"ReadRegularFileIfExists", func(root, path string) (string, bool, error) {
		data, found, err := ReadRegularFileIfExists(root, path)
		return string(data), found, err
	}},
	{"OpenRegularFileIfExists", func(root, path string) (string, bool, error) {
		file, err := OpenRegularFileIfExists(root, path)
		if file == nil {
			return "", false, err
		}
		defer file.Close()
		data, readErr := io.ReadAll(file)
		if err == nil {
			err = readErr
		}
		return string(data), true, err
	}},
}

// sameLeafPath compares two paths the way the hook has to: cleaned, and without case.
func sameLeafPath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// setLeafOpenHook installs leafOpenHook for one test and returns the number of times it saw target. The hook is
// a package-level variable, so tests that call it never run in parallel. act runs on the first call for target
// whose access mask is accepted by match; calls for other paths return at once.
func setLeafOpenHook(t *testing.T, target string, match func(access uint32) bool, act func()) *int {
	t.Helper()
	calls := new(int)
	acted := false
	leafOpenHook = func(path string, access uint32) {
		if !sameLeafPath(path, target) {
			return
		}
		*calls++
		if acted || !match(access) {
			return
		}
		acted = true
		act()
	}
	t.Cleanup(func() { leafOpenHook = nil })
	return calls
}

func anyAccess(uint32) bool { return true }

func TestWindowsReadSurvivesAtomicReplacementBetweenProbeAndReopen(t *testing.T) {
	for _, reader := range leafReaders {
		t.Run(reader.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target.json")
			if err := WriteBytesAtomicInherited(root, target, []byte("old"), true); err != nil {
				t.Fatal(err)
			}
			calls := setLeafOpenHook(t, target, anyAccess, func() {
				if err := WriteBytesAtomicInherited(root, target, []byte("new"), true); err != nil {
					t.Errorf("replace: %v", err)
				}
			})
			content, found, err := reader.read(root, target)
			if *calls == 0 {
				t.Fatal("hook did not run for the target")
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !found {
				t.Fatal("file reported missing")
			}
			if content != "old" && content != "new" {
				t.Fatalf("content = %q, want old or new", content)
			}
		})
	}
}

// TestWindowsReadUnderContinuousAtomicReplacement runs lock-free readers against a writer that keeps replacing one
// file. A read returns one complete version or a sharing violation; an identity error is a failure.
func TestWindowsReadUnderContinuousAtomicReplacement(t *testing.T) {
	const (
		replacements = 200
		readers      = 16
		deadline     = 2 * time.Minute
	)
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	version := func(n int) []byte { return []byte(fmt.Sprintf("version %08d %s end", n, strings.Repeat("x", 4096))) }
	complete := func(data []byte) bool {
		var n int
		_, err := fmt.Sscanf(string(data), "version %d", &n)
		return err == nil && string(data) == string(version(n))
	}
	if err := WriteBytesAtomicInherited(root, target, version(0), true); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	var okReads, sharingReads, otherReads, partialReads, busyWrites atomic.Int64
	var wg sync.WaitGroup
	for range readers {
		wg.Go(func() {
			for !stop.Load() {
				data, err := ReadRegularFile(root, target)
				switch {
				case err == nil && complete(data):
					okReads.Add(1)
				case err == nil:
					partialReads.Add(1)
				case IsSharingViolation(err):
					sharingReads.Add(1)
				default:
					otherReads.Add(1)
				}
			}
		})
	}
	replaced := 0
	end := time.Now().Add(deadline)
	var writeFailure error
	for replaced < replacements && time.Now().Before(end) {
		err := WriteBytesAtomicInherited(root, target, version(replaced+1), true)
		switch {
		case err == nil:
			replaced++
		case IsBusyFile(err):
			busyWrites.Add(1)
		default:
			writeFailure = err
		}
		if writeFailure != nil {
			break
		}
	}
	stop.Store(true)
	wg.Wait()
	t.Logf("replacements=%d busy_writes=%d reads_ok=%d reads_sharing=%d reads_other=%d reads_partial=%d",
		replaced, busyWrites.Load(), okReads.Load(), sharingReads.Load(), otherReads.Load(), partialReads.Load())
	if writeFailure != nil {
		t.Fatal("writer failed with an error that is not a busy file")
	}
	if replaced < replacements {
		t.Fatalf("only %d replacements succeeded, want %d", replaced, replacements)
	}
	if otherReads.Load() != 0 || partialReads.Load() != 0 {
		t.Fatalf("reads with other errors = %d, partial content = %d", otherReads.Load(), partialReads.Load())
	}
	if okReads.Load() == 0 {
		t.Fatal("no read succeeded")
	}
}
