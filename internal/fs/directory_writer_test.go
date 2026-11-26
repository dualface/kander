package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirectoryWriterCreatesNewFilesOnly(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "stage", "cards")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := OpenDirectoryWriter(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.CreateInherited("spec.md", []byte("spec")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "spec.md")); err != nil || string(data) != "spec" {
		t.Fatalf("written file: %q %v", data, err)
	}
	if err := w.CreateInherited("spec.md", []byte("other")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("replaced an existing file: %v", err)
	}
	for _, name := range []string{"", ".", "..", "a/b", `a\b`} {
		if err := w.CreateInherited(name, nil); err == nil {
			t.Fatalf("created %q", name)
		}
	}
	if err := SyncTree(root, filepath.Join(root, "stage"), []string{"cards/spec.md"}, []string{"cards"}); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryWriterRefusesALinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows; reparse points are covered by the shared path checks")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if w, err := OpenDirectoryWriter(root, filepath.Join(root, "linked")); err == nil {
		w.Close()
		t.Fatal("opened a directory through a link")
	}
}
