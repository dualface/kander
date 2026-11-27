package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceRegularFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	source, target := filepath.Join(dir, "new.tmp"), filepath.Join(dir, "current")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A missing target is created.
	write(source, "first")
	if err := ReplaceRegularFile(root, source, target); err != nil {
		t.Fatal(err)
	}
	// An existing regular file is replaced in one step.
	write(source, "second")
	if err := ReplaceRegularFile(root, source, target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "second" {
		t.Fatalf("target = %q, %v", data, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("the source must be gone: %v", err)
	}
	// A directory at the target, or a source that is not a regular file, is refused.
	write(source, "third")
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceRegularFile(root, source, blocked); err == nil {
		t.Fatal("a directory target must be refused")
	}
	if err := ReplaceRegularFile(root, blocked, target); err == nil {
		t.Fatal("a directory source must be refused")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := ReplaceRegularFile(root, source, link); err == nil {
			t.Fatal("a link target must be refused")
		}
	}
	if data, _ := os.ReadFile(target); string(data) != "second" {
		t.Fatalf("a refused replacement must leave the target alone: %q", data)
	}
}
