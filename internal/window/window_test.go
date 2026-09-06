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
	doc := filepath.Join(root, "card.md")
	if err := os.WriteFile(doc, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := board.Entry{Document: doc, Path: doc, Kind: "small"}
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
