//go:build windows

package fs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

func TestWindowsReadOnlyLeafAccess(t *testing.T) {
	read := uint32(windows.GENERIC_READ | windows.FILE_READ_ATTRIBUTES)
	if !readOnlyLeafAccess(read) {
		t.Fatal("GENERIC_READ|FILE_READ_ATTRIBUTES must be read-only")
	}
	if !readOnlyLeafAccess(read | windows.SYNCHRONIZE) {
		t.Fatal("SYNCHRONIZE must not break a read-only access")
	}
	if readOnlyLeafAccess(windows.FILE_READ_ATTRIBUTES) {
		t.Fatal("an attribute-only access has no data to read")
	}
	for name, extra := range map[string]uint32{
		"GENERIC_WRITE":       windows.GENERIC_WRITE,
		"DELETE":              windows.DELETE,
		"WRITE_DAC":           windows.WRITE_DAC,
		"FILE_LIST_DIRECTORY": windows.FILE_LIST_DIRECTORY,
		"FILE_TRAVERSE":       windows.FILE_TRAVERSE,
	} {
		if readOnlyLeafAccess(read | extra) {
			t.Fatalf("%s must not be read-only", name)
		}
	}
}

// TestWindowsReadRejectsReparseLeafSwappedInTheWindow swaps the leaf for a link to a file outside the root at the
// moment the open is about to produce its handle. Whatever the open does, no data of the link target may come back.
func TestWindowsReadRejectsReparseLeafSwappedInTheWindow(t *testing.T) {
	kinds := map[string]func(t *testing.T, outside, link string){
		"symlink": func(t *testing.T, outside, link string) {
			if err := os.Symlink(filepath.Join(outside, "secret.txt"), link); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		},
		"junction": func(t *testing.T, outside, link string) { makeJunction(t, link, outside) },
	}
	for kind, swap := range kinds {
		for _, reader := range leafReaders {
			t.Run(kind+"/"+reader.name, func(t *testing.T) {
				root := t.TempDir()
				outside := t.TempDir()
				if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(root, "target.json")
				if err := WriteBytesAtomicInherited(root, target, []byte("old"), true); err != nil {
					t.Fatal(err)
				}
				calls := setLeafOpenHook(t, target, anyAccess, func() {
					if err := os.Remove(target); err != nil {
						t.Fatalf("remove target: %v", err)
					}
					swap(t, outside, target)
				})
				content, _, err := reader.read(root, target)
				if *calls == 0 {
					t.Fatal("hook did not run for the target")
				}
				if err == nil {
					t.Fatalf("read of a swapped %s succeeded with %q", kind, content)
				}
				if strings.Contains(content, "secret") {
					t.Fatal("data of the link target was returned")
				}
				if !errors.Is(err, ErrUnsafe) {
					t.Fatalf("error is not ErrUnsafe: %v", err)
				}
			})
		}
	}
}

// TestWindowsReadStaysOnTheOriginalParentWhenItIsReplacedByAJunction renames the parent directory and puts a junction
// to a directory outside the root in its place while the leaf is being opened. The read goes through the pinned
// parent handle, so it may return the original content or fail, and never the content behind the junction.
func TestWindowsReadStaysOnTheOriginalParentWhenItIsReplacedByAJunction(t *testing.T) {
	for _, reader := range leafReaders {
		t.Run(reader.name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "target.json"), []byte("other"), 0o600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "d")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "target.json")
			if err := WriteBytesAtomicInherited(root, target, []byte("old"), true); err != nil {
				t.Fatal(err)
			}
			calls := setLeafOpenHook(t, target, anyAccess, func() {
				if err := os.Rename(dir, dir+".moved"); err != nil {
					t.Fatalf("rename parent: %v", err)
				}
				makeJunction(t, dir, outside)
			})
			content, found, err := reader.read(root, target)
			if *calls == 0 {
				t.Fatal("hook did not run for the target")
			}
			if err == nil && found && content != "old" {
				t.Fatalf("content = %q, want old or an error", content)
			}
			t.Logf("%s: found=%v content=%q err=%v", reader.name, found, content, err)
		})
	}
}

// TestWindowsWritableOpenKeepsTheIdentityCheck pins the boundary of the read-only change: an open that asks for write
// access still compares the probed and the reopened object.
func TestWindowsWritableOpenKeepsTheIdentityCheck(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := WriteBytesAtomicInherited(root, target, []byte("old"), true); err != nil {
		t.Fatal(err)
	}
	calls := setLeafOpenHook(t, target, func(access uint32) bool { return access&windows.GENERIC_WRITE != 0 }, func() {
		if err := WriteBytesAtomicInherited(root, target, []byte("new"), true); err != nil {
			t.Errorf("replace: %v", err)
		}
	})
	file, err := OpenWritableRegularFile(root, target)
	if err == nil {
		file.Close()
		t.Fatal("writable open survived a replacement between probe and reopen")
	}
	if *calls == 0 {
		t.Fatal("hook did not run for the target")
	}
	if !errors.Is(err, ErrUnsafe) || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("error = %v, want an unsafe identity-changed error", err)
	}
}
