package board

import (
	"strings"
	"testing"
)

// A card written before the token rename still keeps its owner, session and
// window fields unique after a start or a takeover: each legacy line is updated
// in place and takes the canonical name, rather than an English duplicate being
// appended next to the Chinese one.
func TestSetExecutionFieldInsertsBelowTheAnchor(t *testing.T) {
	updated, err := setExecutionField("- OWNER: claude\n- SESSION: claude abc\n- WINDOW: old\n", FieldWindow, FieldSession, "herdr:w1:t9:w1:p9")
	if err != nil || !strings.Contains(updated, "- WINDOW: herdr:w1:t9:w1:p9\n") {
		t.Fatalf("replace: %q %v", updated, err)
	}
	inserted, err := setExecutionField("- 负责人: claude\n- 会话: claude abc\n- 开始时间: 2026-09-04 12:00\n", FieldWindow, FieldSession, "foreground")
	if err != nil || !strings.Contains(inserted, "- 会话: claude abc\n- "+FieldWindow+": foreground\n") {
		t.Fatalf("insert: %q %v", inserted, err)
	}
	legacy, err := setExecutionField("- 负责人: claude\n- 会话: claude abc\n- 窗口: old\n", FieldWindow, FieldSession, "herdr:t1:p1")
	if err != nil || strings.Contains(legacy, "窗口") || len(FieldLineRe(FieldWindow).FindAllString(legacy, -1)) != 1 {
		t.Fatalf("legacy: %q %v", legacy, err)
	}
}

func TestStartAndTakeoverUpdateLegacyFieldsInPlace(t *testing.T) {
	legacy := "# 旧卡\n\n- 负责人:\n- 会话:\n- 窗口:\n- 开始时间:\n- 完成时间:\n"

	started, err := renderStart(legacy, "claude", TaskStartInput{Session: "claude abc", Window: "herdr:t1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{FieldOwner, FieldSession, FieldWindow, FieldStartedAt} {
		if count := len(FieldLineRe(name).FindAllString(started, -1)); count != 1 {
			t.Fatalf("%s appears %d times in %q", name, count, started)
		}
	}
	for _, want := range []string{
		"- " + FieldOwner + ": claude\n",
		"- " + FieldSession + ": claude abc\n",
		"- " + FieldWindow + ": herdr:t1:p1\n",
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
	takenOver, err := renderTakeover(legacy, TakeoverInput{Owner: "grok", Session: "grok xyz", Window: "tmux:s1:w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{FieldOwner, FieldSession, FieldWindow} {
		if count := len(FieldLineRe(name).FindAllString(takenOver, -1)); count != 1 {
			t.Fatalf("takeover %s appears %d times in %q", name, count, takenOver)
		}
	}

	// The start path also has to cope with a legacy card that never had a
	// session or window field at all.
	old := "# 旧卡\n\n- 负责人:\n- 开始时间:\n- 完成时间:\n"
	filled, err := renderStart(old, "claude", TaskStartInput{Session: "claude abc", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	want := "- " + FieldOwner + ": claude\n- " + FieldSession + ": claude abc\n- " + FieldWindow + ": foreground\n"
	if !strings.Contains(filled, want) {
		t.Fatalf("insert order: %q", filled)
	}
}

func TestStartRetryCreatesNewClaimAndTakeoverPreservesIt(t *testing.T) {
	original := "# Card\n\n- OWNER:\n- SESSION:\n- WINDOW:\n- STARTED_AT:\n\n## GOAL\n\nFixture.\n"
	first, err := renderStart(original, "codex", TaskStartInput{Session: "codex", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	// A failed start restores this original body before the next launch renders it.
	second, err := renderStart(original, "codex", TaskStartInput{Session: "codex", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	firstClaim, _ := SectionBody(first, "LIFECYCLE_DECISION")
	secondClaim, _ := SectionBody(second, "LIFECYCLE_DECISION")
	if firstClaim == "" || firstClaim == secondClaim {
		t.Fatal("retry reused claim identity")
	}
	takeover, err := renderTakeover(second, TakeoverInput{Owner: "claude", Session: "claude next", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	takeoverClaim, _ := SectionBody(takeover, "LIFECYCLE_DECISION")
	if takeoverClaim != secondClaim || MetadataFrom(takeover, FieldStartedAt) != MetadataFrom(second, FieldStartedAt) {
		t.Fatal("takeover changed execution cycle")
	}
}
