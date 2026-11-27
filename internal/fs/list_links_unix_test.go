//go:build unix

package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// mixedDirectory builds a directory holding one regular file, one
// subdirectory, a symlink to a file outside the root, a dangling symlink, a
// symlink to a directory and a FIFO without a writer. It returns the root, the
// directory, the outside target and the missing target of the dangling link.
func mixedDirectory(t *testing.T) (root, dir, outside, missing string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside = filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "file-link")); err != nil {
		t.Fatal(err)
	}
	missing = filepath.Join(t.TempDir(), "missing")
	if err := os.Symlink(missing, filepath.Join(dir, "dangling-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "sub"), filepath.Join(dir, "dir-link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	return root, dir, outside, missing
}

// ListDirectoryWithLinks reports every member of a directory that holds
// links and a FIFO, never follows or opens a link, and never blocks on the
// FIFO.
func TestListDirectoryWithLinksReportsEveryMemberKind(t *testing.T) {
	root, dir, outside, missing := mixedDirectory(t)
	type result struct {
		entries []DirEntry
		err     error
	}
	done := make(chan result, 1)
	go func() {
		entries, err := ListDirectoryWithLinks(root, dir)
		done <- result{entries, err}
	}()
	var got result
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the listing blocked past the bound")
	}
	if got.err != nil {
		t.Fatalf("listing failed: %v", got.err)
	}
	if len(got.entries) != 6 {
		t.Fatalf("got %d entries, want 6: %v", len(got.entries), got.entries)
	}
	want := map[string]Kind{
		"file":          KindFile,
		"sub":           KindDirectory,
		"file-link":     KindLink,
		"dangling-link": KindLink,
		"dir-link":      KindLink,
		"pipe":          KindOther,
	}
	kinds := map[string]Kind{}
	for _, entry := range got.entries {
		kinds[entry.Name] = entry.Kind
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Fatalf("%s: kind %q, want %q (all: %v)", name, kinds[name], kind, got.entries)
		}
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "keep\n" {
		t.Fatalf("the link target changed: %q %v", data, err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the dangling link target appeared: %v", err)
	}
}

// ListDirectory keeps refusing a directory that holds a link.
func TestListDirectoryStillRejectsLinks(t *testing.T) {
	root, dir, _, _ := mixedDirectory(t)
	if _, err := ListDirectory(root, dir); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("ListDirectory error = %v, want ErrUnsafe", err)
	}
}

// The directory being listed is opened without following links like every
// other path in this package; a missing directory reports not-exist.
func TestListDirectoryWithLinksRejectsALinkedOrMissingDirectory(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := ListDirectoryWithLinks(root, linked); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("linked directory error = %v, want ErrUnsafe", err)
	}
	if _, err := ListDirectoryWithLinks(root, filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing directory error = %v, want not-exist", err)
	}
}
