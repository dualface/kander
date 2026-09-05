//go:build windows

package fs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsReadWriteRename(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "board")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"todo", "working"} {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(root, "todo", "task.md")
	if err := os.WriteFile(source, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteTextAtomic(root, source, "new\n", true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "working", "task.md")
	if err := Rename(root, source, target); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegularFile(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("got %q", data)
	}
}

func TestWindowsNeutralAppendDedup(t *testing.T) {
	base := t.TempDir()
	gitDir := filepath.Join(base, "project", ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exclude := filepath.Join(gitDir, "info", "exclude")
	if err := EnsureInheritedDirectoryPath(filepath.Dir(exclude)); err != nil {
		t.Fatal(err)
	}
	if err := AppendUniqueLine(filepath.VolumeName(exclude)+`\`, exclude, "/kanban/"); err != nil {
		t.Fatal(err)
	}
	if err := AppendUniqueLine(filepath.VolumeName(exclude)+`\`, exclude, "/kanban/"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "/kanban/\n" {
		t.Fatalf("exclude=%q", data)
	}
}

func TestWindowsRejectsJunctionComponent(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "board")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(root, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "todo")
	link := filepath.Join(parent, "trap")
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, outside)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create junction: %s %v", out, err)
	}
	defer func() { _ = os.Remove(link) }()
	err := WriteTextAtomic(root, filepath.Join(link, "secret.txt"), "owned\n", true)
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("expected unsafe, got %v", err)
	}
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep\n" {
		t.Fatalf("outside mutated: %q", data)
	}
}

func TestWindowsPrivateDirectoryCollision(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := CreatePrivateDirectory(root, private); err != nil {
		t.Fatal(err)
	}
	if err := CreatePrivateDirectory(root, private); err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected exist, got %v", err)
	}
}
