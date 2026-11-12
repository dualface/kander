package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenLockFileConcurrentCreatorsKeepOneInode(t *testing.T) {
	root := t.TempDir()
	for round := range 8 {
		path := filepath.Join(root, fmt.Sprintf("lock-%d", round))
		type result struct {
			info os.FileInfo
			err  error
		}
		const contenders = 32
		start := make(chan struct{})
		results := make(chan result, contenders)
		var wait sync.WaitGroup
		for range contenders {
			wait.Go(func() {
				<-start
				file, err := OpenLockFile(root, path)
				var info os.FileInfo
				if err == nil {
					info, err = file.Stat()
					err = errors.Join(err, file.Close())
				}
				results <- result{info: info, err: err}
			})
		}
		close(start)
		wait.Wait()
		close(results)
		var first os.FileInfo
		for got := range results {
			if got.err != nil {
				t.Fatalf("round %d: concurrent lock open: %v", round, got.err)
			}
			if first == nil {
				first = got.info
			} else if !os.SameFile(first, got.info) {
				t.Fatalf("round %d: lock contenders acquired different inodes", round)
			}
		}
	}
}

func TestOpenLockFileRejectsSymlinkAndDirectory(t *testing.T) {
	root := t.TempDir()
	t.Run("directory", func(t *testing.T) {
		path := filepath.Join(root, "directory")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		file, err := OpenLockFile(root, path)
		if file != nil {
			_ = file.Close()
		}
		if err == nil {
			t.Fatal("directory accepted as a lock")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		target := filepath.Join(root, "target")
		if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "link")
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		file, err := OpenLockFile(root, path)
		if file != nil {
			_ = file.Close()
		}
		if !errors.Is(err, ErrUnsafe) {
			t.Fatalf("symlink lock was not refused: %v", err)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "preserve" {
			t.Fatal("lock open changed the symlink target")
		}
	})
}
