package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// isolateTestOverlay keeps explicit standalone fixture directories, including
// their overlays. An inherited overlay location (the repository's main worktree
// during go test) is replaced with a temporary working directory before loading
// configuration. t.Chdir restores the package directory after each test.
func isolateTestOverlay(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	location, err := config.ResolveOverlayLocation(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if location.ProjectRoot != cwd {
		t.Chdir(t.TempDir())
	}
}

func TestOptionsSessionIsolatesInheritedOverlay(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	path, original := writeTempOverlay(t, root, map[string]any{
		"models": map[string]any{"kanban": map[string]any{
			"codex": map[string]any{"large_model": "inherited-overlay-model"},
		}},
	})
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	session := newTestSession(t)
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loaded.Models.Kanban["codex"]["large_model"], session.Config.Models.Kanban["codex"]["large_model"]; got != want {
		t.Fatalf("inherited overlay changed model: got %q want %q", got, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("inherited overlay changed")
	}
}
