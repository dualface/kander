package launch

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// A card written before the token rename still keeps its owner, session and
// window fields unique after a start or a takeover: each legacy line is updated
// in place and takes the canonical name, rather than an English duplicate being
// appended next to the Chinese one.
func TestStartAndTakeoverUpdateLegacyFieldsInPlace(t *testing.T) {
	legacy := "# 旧卡\n\n- 负责人:\n- 会话:\n- 窗口:\n- 开始时间:\n- 完成时间:\n"

	started, err := renderStartMetadata(legacy, "claude", "claude abc", "herdr:t1:p1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{board.FieldOwner, board.FieldSession, board.FieldWindow, board.FieldStartedAt} {
		if count := len(board.FieldLineRe(name).FindAllString(started, -1)); count != 1 {
			t.Fatalf("%s appears %d times in %q", name, count, started)
		}
	}
	for _, want := range []string{
		"- " + board.FieldOwner + ": claude\n",
		"- " + board.FieldSession + ": claude abc\n",
		"- " + board.FieldWindow + ": herdr:t1:p1\n",
	} {
		if !strings.Contains(started, want) {
			t.Fatalf("missing %q in %q", want, started)
		}
	}
	for _, legacyName := range []string{"负责人", "会话", "窗口", "开始时间"} {
		if strings.Contains(started, "- "+legacyName+":") {
			t.Fatalf("legacy field %s survived in %q", legacyName, started)
		}
	}

	// A takeover rewrites the same three fields on a card that is still legacy.
	takenOver, err := renderTakeoverMetadata(legacy, "grok", "grok xyz", "tmux:s1:w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{board.FieldOwner, board.FieldSession, board.FieldWindow} {
		if count := len(board.FieldLineRe(name).FindAllString(takenOver, -1)); count != 1 {
			t.Fatalf("takeover %s appears %d times in %q", name, count, takenOver)
		}
	}

	// The start path also has to cope with a legacy card that never had a
	// session or window field at all.
	old := "# 旧卡\n\n- 负责人:\n- 开始时间:\n- 完成时间:\n"
	filled, err := renderStartMetadata(old, "claude", "claude abc", "foreground")
	if err != nil {
		t.Fatal(err)
	}
	want := "- " + board.FieldOwner + ": claude\n- " + board.FieldSession + ": claude abc\n- " + board.FieldWindow + ": foreground\n"
	if !strings.Contains(filled, want) {
		t.Fatalf("insert order: %q", filled)
	}
}
