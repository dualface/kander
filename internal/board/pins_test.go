package board

import (
	"os"
	"strings"
	"testing"
)

const pinHeader = "# Card\n\n- TYPE: Chore\n- SIZE: small\n- OWNER:\n- RESULT:\n"

func TestParseCardPinsReadsOnlyTheHeader(t *testing.T) {
	text := pinHeader + "- EXEC_AGENT: my_agent\n- EXEC_MODEL: org_model.v2[1m]\n- EXEC_EFFORT: auto\n- REVIEW_SECURITY_AGENT: pi\n\n## GOAL\n\n- REVIEW_PMQA_AGENT: grok\n- EXEC_AGENT: codex\n"
	pins, err := ParseCardPins(text)
	if err != nil {
		t.Fatal(err)
	}
	if pins.Exec != (StagePin{Agent: "my_agent", Model: "org_model.v2[1m]"}) {
		t.Fatalf("exec=%+v", pins.Exec)
	}
	if pins.Review["PMQA"].Forced() || pins.Review["Security"] != (StagePin{Agent: "pi"}) || !pins.Any() {
		t.Fatalf("review=%+v", pins.Review)
	}
	if body, err := ParseCardPins(pinHeader + "\n## GOAL\n\n- EXEC_AGENT: codex\n"); err != nil || body.Any() {
		t.Fatalf("body line took effect: %+v %v", body, err)
	}
}

func TestParseCardPinsRejectsInvalidHeaders(t *testing.T) {
	for name, lines := range map[string]string{
		"duplicate":        "- EXEC_AGENT: claude\n- EXEC_AGENT: codex\n",
		"duplicate record": "- EXEC_AGENT: claude\n- EXEC_RESOLVED: a\n- EXEC_RESOLVED: b\n",
		"model only":       "- EXEC_MODEL: m1\n",
		"effort only":      "- REVIEW_PMQA_EFFORT: high\n",
		"agent pattern":    "- EXEC_AGENT: Claude\n",
		"option model":     "- EXEC_AGENT: claude\n- EXEC_MODEL: -x\n",
		"spaced effort":    "- EXEC_AGENT: claude\n- EXEC_EFFORT: very high\n",
		"non ascii":        "- EXEC_AGENT: claude\n- EXEC_MODEL: 模型\n",
	} {
		if _, err := ParseCardPins(pinHeader + lines + "\n## GOAL\n"); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestResolvedRecordsKeepUnpinnedCardsUnchanged(t *testing.T) {
	plain := pinHeader + "\n## GOAL\n\n- EXEC_AGENT: codex\n"
	if got, err := WithResolvedRecord(plain, FieldExecResolved, "agent=x(cli)"); err != nil || got != plain {
		t.Fatalf("unpinned card changed: %q %v", got, err)
	}
	if WithoutResolvedRecords(plain) != plain {
		t.Fatal("stripping changed an unpinned card")
	}

	pinned := pinHeader + "- EXEC_AGENT: claude\n\n## GOAL\n\n- EXEC_RESOLVED: body stays\n"
	written, err := WithResolvedRecord(pinned, FieldExecResolved, "agent=claude(forced)")
	if err != nil {
		t.Fatal(err)
	}
	want := pinHeader + "- EXEC_AGENT: claude\n- EXEC_RESOLVED: agent=claude(forced)\n\n## GOAL\n\n- EXEC_RESOLVED: body stays\n"
	if written != want {
		t.Fatalf("insert=%q", written)
	}
	rewritten, err := WithResolvedRecord(written, FieldExecResolved, "agent=claude(cli)")
	if err != nil || strings.Count(rewritten, "- EXEC_RESOLVED: agent=") != 1 || !strings.Contains(rewritten, "agent=claude(cli)") {
		t.Fatalf("replace=%q %v", rewritten, err)
	}
	review, err := WithResolvedRecord(rewritten, ReviewResolvedField("Security"), "agent=pi(forced)")
	if err != nil || !strings.Contains(review, "- REVIEW_SECURITY_RESOLVED: agent=pi(forced)\n") {
		t.Fatalf("review record=%q %v", review, err)
	}
	if got := WithoutResolvedRecords(review); got != pinned {
		t.Fatalf("strip=%q", got)
	}
}

func TestUpdateProtectsPinsAndRecords(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "pin-update", false)
	pinned := strings.Replace(readyText(s), "- RESULT:\n", "- RESULT:\n- EXEC_AGENT: claude\n", 1)
	updateSnapshot(t, root, s, pinned)
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, todo.TaskID)
	update := func(text, decision string) error {
		return UpdateDocument(root, todo.TaskID, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, ContractDecision: decision})
	}

	repinned := strings.Replace(s.Text, "- EXEC_AGENT: claude\n", "- EXEC_AGENT: codex\n", 1)
	if err := update(repinned, ""); err == nil {
		t.Fatal("a frozen pin changed without a contract decision")
	}
	if err := update(strings.Replace(s.Text, "- RESULT:\n", "- RESULT:\n- EXEC_RESOLVED: agent=claude(forced)\n", 1), ""); err == nil || !strings.Contains(err.Error(), FieldExecResolved) {
		t.Fatalf("an update wrote a managed record: %v", err)
	}
	if err := update(strings.Replace(s.Text, "- EXEC_AGENT: claude\n", "- EXEC_AGENT: claude\n- EXEC_AGENT: claude\n", 1), "dup"); err == nil {
		t.Fatal("a repeated pin line was accepted")
	}
	// Pin-like lines in the body are ordinary text: no decision is needed.
	if err := update(strings.Replace(s.Text, "## DISCUSSION\n", "## DISCUSSION\n\n- EXEC_AGENT: grok\n- EXEC_RESOLVED: example\n", 1), ""); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, todo.TaskID)
	if err := update(strings.Replace(s.Text, "- EXEC_AGENT: claude\n", "- EXEC_AGENT: codex\n", 1), "user approved codex"); err != nil {
		t.Fatal(err)
	}
	if s = transactionSnapshot(t, root, todo.TaskID); !strings.Contains(s.Text, `"EXEC_AGENT": "- EXEC_AGENT: codex"`) {
		t.Fatalf("decision record=%s", s.Text)
	}
}

func TestTodoGateAndCheckValidatePins(t *testing.T) {
	root := tempBoard(t)
	bad := transactionCard(t, root, "pin-gate", false)
	updateSnapshot(t, root, bad, strings.Replace(readyText(bad), "- RESULT:\n", "- RESULT:\n- EXEC_AGENT: cursor\n- EXEC_EFFORT: high\n", 1))
	bad = transactionSnapshot(t, root, bad.Entry.TaskID)
	if _, err := MoveEntry(bad.Entry, root, "todo"); err == nil || !strings.Contains(err.Error(), "EXEC_EFFORT") {
		t.Fatalf("todo gate err=%v", err)
	}

	plain := transactionCard(t, root, "pin-plain", false)
	updateSnapshot(t, root, plain, readyText(plain))
	plain = transactionSnapshot(t, root, plain.Entry.TaskID)
	moved, err := MoveEntry(plain.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	if code, _, stderr, err := CheckBoard(root, nil, false); err != nil || strings.Contains(strings.Join(stderr, "\n"), "pin-plain") {
		t.Fatalf("unpinned card reported: code=%d %v %v", code, stderr, err)
	}
	// A card edited outside the controlled path is still caught by check.
	text := strings.Replace(transactionSnapshot(t, root, moved.TaskID).Text, "- RESULT:\n", "- RESULT:\n- EXEC_MODEL: m1\n", 1)
	if err := os.WriteFile(moved.Document, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr, err := CheckBoard(root, nil, false)
	if err != nil || code == 0 || !strings.Contains(strings.Join(stderr, "\n"), "EXEC_AGENT") {
		t.Fatalf("check code=%d stderr=%v err=%v", code, stderr, err)
	}
}
