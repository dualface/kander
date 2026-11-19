package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenExistingLockFileNeverCreates(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		root := t.TempDir()
		file, err := OpenExistingLockFile(root, filepath.Join(root, "worker.lock"))
		if file != nil || err != nil {
			t.Fatalf("a missing lock file is (nil, nil): %v, %v", file, err)
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Fatalf("nothing is created: %v", entries)
		}
	})
	t.Run("missing parent", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "absent", "worker.lock")
		file, err := OpenExistingLockFile(root, path)
		if file != nil || err != nil {
			t.Fatalf("a missing parent is (nil, nil): %v, %v", file, err)
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Fatalf("no directory is created: %v", entries)
		}
	})
}

func TestOpenExistingLockFileOpensTheSameFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worker.lock")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := OpenExistingLockFile(root, path)
	if err != nil || file == nil {
		t.Fatalf("open: %v, %v", file, err)
	}
	opened, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatalf("the content is unchanged: %q, %v", data, err)
	}
	again, err := OpenLockFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	info, err := again.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(opened, info) {
		t.Fatal("OpenLockFile reopens the file OpenExistingLockFile opened")
	}
}

func TestOpenExistingLockFileRefusesWhatOpenLockFileRefuses(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	linked := os.Symlink(target, link) == nil
	for name, path := range map[string]string{"directory": directory, "symlink": link} {
		t.Run(name, func(t *testing.T) {
			if name == "symlink" && !linked {
				t.Skip("symlink creation unavailable")
			}
			for open, fn := range map[string]func(string, string) (*os.File, error){
				"OpenLockFile":         OpenLockFile,
				"OpenExistingLockFile": OpenExistingLockFile,
			} {
				file, err := fn(root, path)
				if file != nil {
					_ = file.Close()
				}
				if err == nil {
					t.Fatalf("%s accepted a %s", open, name)
				}
				if name == "symlink" && !errors.Is(err, ErrUnsafe) {
					t.Fatalf("%s did not refuse the symlink as unsafe: %v", open, err)
				}
			}
		})
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "preserve" {
		t.Fatal("opening changed the symlink target")
	}
}

// The handle must see a lock another handle holds: a worker holds worker.lock
// with OpenLockFile, and the probe opens it next to that handle.
func TestOpenExistingLockFileSeesAHeldLock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worker.lock")
	holder, err := OpenLockFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	held, err := LockExclusive(holder)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := OpenExistingLockFile(root, path)
	if err != nil || probe == nil {
		t.Fatalf("a held lock file opens: %v, %v", probe, err)
	}
	defer probe.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if lock, err := LockExclusiveContext(ctx, probe); err == nil {
		_ = lock.Unlock()
		t.Fatal("the probe took a lock another handle holds")
	}
	if err := held.Unlock(); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	lock, err := LockExclusiveContext(ctx2, probe)
	if err != nil {
		t.Fatalf("the probe locks a free file: %v", err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	ctx3, cancel3 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel3()
	again, err := LockExclusiveContext(ctx3, holder)
	if err != nil {
		t.Fatalf("the holder locks at once after the probe is closed: %v", err)
	}
	_ = again.Unlock()
}
