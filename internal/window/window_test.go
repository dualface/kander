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
	withWindow := "- 负责人: claude\n- 会话: claude abc\n- 窗口: old\n"
	updated, err := RenderWindowMetadata(withWindow, "herdr:w1:t9:w1:p9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "- 窗口: herdr:w1:t9:w1:p9\n") {
		t.Fatalf("replace: %q", updated)
	}
	without := "- 负责人: claude\n- 会话: claude abc\n- 开始时间: 2026-09-04 12:00\n"
	inserted, err := RenderWindowMetadata(without, "foreground")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inserted, "- 会话: claude abc\n- 窗口: foreground\n") {
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
