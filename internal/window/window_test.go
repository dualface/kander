package window

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func TestRenderWindowMetadataInsertAndReplace(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
	withWindow := "- OWNER: claude\n- SESSION: claude abc\n- WINDOW: old\n"
	updated, err := RenderWindowMetadata(withWindow, "herdr:w1:t9:w1:p9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "- WINDOW: herdr:w1:t9:w1:p9\n") {
		t.Fatalf("replace: %q", updated)
	}
	without := "- OWNER: claude\n- SESSION: claude abc\n- STARTED_AT: 2026-09-04 12:00\n"
	inserted, err := RenderWindowMetadata(without, "foreground")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inserted, "- SESSION: claude abc\n- WINDOW: foreground\n") {
		t.Fatalf("insert: %q", inserted)
	}
}

func TestRestoreWindowTextAndFailureMessage(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	id := "20260907-window-task"
	doc := filepath.Join(root, "working", id+".md")
	if err := os.WriteFile(doc, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	entry := snapshot.Entry
	if err := WriteDocument(root, entry, "changed\n"); err != nil {
		t.Fatal(err)
	}
	if err := RestoreWindowText(root, entry, "original\n"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(doc)
	if string(body) != "original\n" {
		t.Fatalf("got %q", body)
	}
	msg := ResumeFailureMessage(errors.New("liveness failed"), errors.New("cleanup boom"), errors.New("rollback boom"))
	if !strings.Contains(msg, "liveness failed") || !strings.Contains(msg, "清理=") || !strings.Contains(msg, "窗口回滚=") {
		t.Fatalf("message=%q", msg)
	}
	if ResumeFailureMessage(errors.New("only"), nil, nil) != "" {
		t.Fatal("expected empty when no extra failures")
	}
}

// A card still using the legacy Chinese fields is updated in place: the window
// line keeps its single occurrence and switches to the canonical name, instead
// of the card gaining a second window field.
func TestRenderWindowMetadataUpdatesLegacyFieldsInPlace(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})

	withWindow := "- 负责人: claude\n- 会话: claude abc\n- 窗口: old\n"
	updated, err := RenderWindowMetadata(withWindow, "herdr:t1:p1")
	if err != nil {
		t.Fatal(err)
	}
	if count := len(board.FieldLineRe(WindowField).FindAllString(updated, -1)); count != 1 {
		t.Fatalf("window fields: %d in %q", count, updated)
	}
	if !strings.Contains(updated, "- "+board.FieldWindow+": herdr:t1:p1\n") || strings.Contains(updated, "窗口") {
		t.Fatalf("replace: %q", updated)
	}

	without := "- 负责人: claude\n- 会话: claude abc\n- 开始时间: 2026-09-04 12:00\n"
	inserted, err := RenderWindowMetadata(without, "foreground")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inserted, "- 会话: claude abc\n- "+board.FieldWindow+": foreground\n") {
		t.Fatalf("insert: %q", inserted)
	}
	if count := len(board.FieldLineRe(SessionField).FindAllString(inserted, -1)); count != 1 {
		t.Fatalf("session fields: %d in %q", count, inserted)
	}
}

func TestRollbackNeverResurrectsMovedSmallCard(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	id := "20260907-moved-window-task"
	old := filepath.Join(root, "working", id+".md")
	text := "# Card\n- OWNER: codex\n- SESSION: codex\n- WINDOW: old\n- TASK_BRANCH: task\n"
	if err := os.WriteFile(old, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := RenderWindowMetadata(text, "new")
	if err = WriteDocument(root, snapshot.Entry, updated); err != nil {
		t.Fatal(err)
	}
	mover, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.MoveEntry(mover.Entry, root, "review")
	if err != nil {
		t.Fatal(err)
	}
	if err = RestoreWindowText(root, snapshot.Entry, text); err == nil {
		t.Fatal("stale rollback succeeded")
	}
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old card resurrected: %v", err)
	}
	body, err := board.ReadDocument(moved)
	if err != nil || body != updated {
		t.Fatalf("moved body changed: %q %v", body, err)
	}
	loaded, err := board.Scan(root)
	if err != nil || len(loaded.Entries) != 1 || len(loaded.Problems) != 0 {
		t.Fatalf("duplicate: %+v %v", loaded, err)
	}
}

func TestStaleRollbackPreservesNewBodyAndRejectsSecondRollback(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	id := "20260907-stale-window-task"
	path := filepath.Join(root, "working", id+".md")
	if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteDocument(root, first.Entry, "window changed\n"); err != nil {
		t.Fatal(err)
	}
	if err = RestoreWindowText(root, second.Entry, "original\n"); err == nil {
		t.Fatal("second operation overwrote first")
	}
	next, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.UpdateDocument(root, id, board.UpdateOptions{Document: "spec.md", Text: "new body\n", ExpectedRevision: next.Revision}); err != nil {
		t.Fatal(err)
	}
	if err = RestoreWindowText(root, first.Entry, "original\n"); err == nil {
		t.Fatal("stale full rollback succeeded")
	}
	body, _ := os.ReadFile(path)
	if string(body) != "new body\n" {
		t.Fatalf("new body lost: %q", body)
	}
}
