//go:build windows

package fs

import (
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsReadingLockedFilesPreservesEmptyAndNonemptySemantics(t *testing.T) {
	for _, content := range []string{"", "locked content\n"} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "source.lock")
			if err := WriteTextAtomic(root, path, content, false); err != nil {
				t.Fatal(err)
			}
			file, err := OpenLockFile(root, path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			lock, err := LockExclusive(file)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Unlock()
			data, err := ReadRegularFile(root, path)
			if content == "" {
				if err != nil || len(data) != 0 {
					t.Fatalf("empty locked file: %q %v", data, err)
				}
			} else if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
				t.Fatalf("nonempty locked file: got %v; byte-range protection must remain", err)
			}
		})
	}
}
