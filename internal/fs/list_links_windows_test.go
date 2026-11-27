//go:build windows

package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ListDirectoryWithLinks reports a junction as a link beside a regular file
// and a subdirectory, while ListDirectory keeps refusing the same directory. A
// junction needs no privilege, so this part never skips.
func TestListDirectoryWithLinksReportsAJunction(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, filepath.Join(dir, "junction"), t.TempDir())
	entries, err := ListDirectoryWithLinks(root, dir)
	if err != nil {
		t.Fatalf("listing failed: %v", err)
	}
	requireListedKinds(t, entries, map[string]Kind{
		"file":     KindFile,
		"sub":      KindDirectory,
		"junction": KindLink,
	})
	if _, err := ListDirectory(root, dir); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("ListDirectory error = %v, want ErrUnsafe", err)
	}

	t.Run("file symlink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "target.txt")
		if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Creating a file symlink needs SeCreateSymbolicLinkPrivilege or
		// developer mode, which a test environment may lack.
		if err := os.Symlink(target, filepath.Join(dir, "file-link")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		entries, err := ListDirectoryWithLinks(root, dir)
		if err != nil {
			t.Fatalf("listing failed: %v", err)
		}
		requireListedKinds(t, entries, map[string]Kind{
			"file":      KindFile,
			"sub":       KindDirectory,
			"junction":  KindLink,
			"file-link": KindLink,
		})
	})
}

func requireListedKinds(t *testing.T, entries []DirEntry, want map[string]Kind) {
	t.Helper()
	kinds := map[string]Kind{}
	for _, entry := range entries {
		kinds[entry.Name] = entry.Kind
	}
	if len(kinds) != len(want) {
		t.Fatalf("got %v, want %v", entries, want)
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Fatalf("%s: kind %q, want %q (all: %v)", name, kinds[name], kind, entries)
		}
	}
}
