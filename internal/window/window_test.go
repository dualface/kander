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

const windowCard = "# Card\n- OWNER: codex\n- SESSION: codex\n- WINDOW: old\n- STARTED_AT: 2026-09-07 10:00\n- TASK_BRANCH: task\n"

func recordWindow(t *testing.T, root string, entry board.Entry, value string) {
	t.Helper()
	if _, err := board.BindExecutionCursor(root, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := board.RecordExecutionWindow(root, entry, value, ""); err != nil {
		t.Fatal(err)
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
	doc := filepath.Join(root, "working", id, "spec.md")
	if err := os.Mkdir(filepath.Dir(doc), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc, []byte(windowCard), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	entry := snapshot.Entry
	recordWindow(t, root, entry, "changed")
	if err := RestoreWindowText(root, entry, windowCard); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(doc)
	if string(body) != windowCard {
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

func TestRollbackNeverResurrectsMovedSmallCard(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	id := "20260907-moved-window-task"
	old := filepath.Join(root, "working", id, "spec.md")
	if err := os.Mkdir(filepath.Dir(old), 0700); err != nil {
		t.Fatal(err)
	}
	text := "# Card\n- OWNER: codex\n- SESSION: codex\n- WINDOW: old\n- TASK_BRANCH: task\n"
	if err := os.WriteFile(old, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	recordWindow(t, root, snapshot.Entry, "new")
	updated := strings.Replace(text, "- WINDOW: old", "- WINDOW: new", 1)
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
	path := filepath.Join(root, "working", id, "spec.md")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(windowCard), 0600); err != nil {
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
	recordWindow(t, root, first.Entry, "changed")
	if err = RestoreWindowText(root, second.Entry, windowCard); err == nil {
		t.Fatal("second operation overwrote first")
	}
	next, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.UpdateDocument(root, id, board.UpdateOptions{Document: "spec.md", Text: next.Text + "new body\n", ExpectedRevision: next.Revision}); err != nil {
		t.Fatal(err)
	}
	if err = RestoreWindowText(root, first.Entry, windowCard); err == nil {
		t.Fatal("stale full rollback succeeded")
	}
	body, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(body), "new body\n") {
		t.Fatalf("new body lost: %q", body)
	}
}
