//go:build windows

package fs

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// leafReader reads one file through a public read entrance and reports the content and whether the file was found.
type leafReader struct {
	name string
	read func(root, path string) (content string, found bool, err error)
}

var leafReaders = []leafReader{
	{"ReadRegularFile", func(root, path string) (string, bool, error) {
		data, err := ReadRegularFile(root, path)
		return string(data), err == nil, err
	}},
	{"ReadRegularFileIfExists", func(root, path string) (string, bool, error) {
		data, found, err := ReadRegularFileIfExists(root, path)
		return string(data), found, err
	}},
	{"OpenRegularFileIfExists", func(root, path string) (string, bool, error) {
		file, err := OpenRegularFileIfExists(root, path)
		if err != nil || file == nil {
			return "", false, err
		}
		defer file.Close()
		data, err := io.ReadAll(file)
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
