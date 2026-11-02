package board

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// lineEndingCard returns a working card whose body holds managed fields, the
// LIFECYCLE_DECISION section of its claim, and a multi-line CONTRACT_DECISIONS
// section.
func lineEndingCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	updateSnapshot(t, root, s, readyText(s))
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	working, err := MoveWithOptions(todo, root, "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{
		Document:         "spec.md",
		Text:             strings.Replace(s.Text, "## GOAL\n\n已确定的任务范围", "## GOAL\n\n调整后的任务范围", 1),
		ExpectedRevision: s.Revision,
		ContractDecision: "用户同意调整目标",
	})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	for _, section := range []string{"\n## LIFECYCLE_DECISION\n", "\n## CONTRACT_DECISIONS\n"} {
		if !strings.Contains(s.Text, section) {
			t.Fatalf("fixture lacks %q:\n%s", section, s.Text)
		}
	}
	return s
}

func crlf(text string) string { return strings.ReplaceAll(text, "\n", "\r\n") }

// writeCardBytes replaces the stored body the way a CRLF build or a Windows
// editor left it, and returns the snapshot of that body.
func writeCardBytes(t *testing.T, root string, s Snapshot, text string) Snapshot {
	t.Helper()
	if err := os.WriteFile(s.Entry.Document, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if s.Text != text {
		t.Fatal("the snapshot does not show the written body")
	}
	return s
}

func storedCardBytes(t *testing.T, root, id string) string {
	t.Helper()
	data, err := os.ReadFile(transactionSnapshot(t, root, id).Entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func withSummary(text string) string {
	return strings.Replace(text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\n已实现。", 1)
}

func TestUpdateAcceptsLFSubmissionOnCRLFCard(t *testing.T) {
	root := tempBoard(t)
	s := lineEndingCard(t, root, "crlf-card")
	lf := withSummary(s.Text)
	s = writeCardBytes(t, root, s, crlf(s.Text))
	if err := tryUpdate(root, s, lf); err != nil {
		t.Fatalf("LF submission on a CRLF card: %v", err)
	}
	if got := storedCardBytes(t, root, s.Entry.TaskID); got != lf {
		t.Fatalf("stored body is not the LF submission:\n%q", got)
	}
}

func TestUpdateAcceptsCRLFSubmissionOnLFCard(t *testing.T) {
	root := tempBoard(t)
	s := lineEndingCard(t, root, "lf-card")
	lf := withSummary(s.Text)
	if err := tryUpdate(root, s, crlf(lf)); err != nil {
		t.Fatalf("CRLF submission on an LF card: %v", err)
	}
	if got := storedCardBytes(t, root, s.Entry.TaskID); got != lf {
		t.Fatalf("stored body is not the submission with LF line endings:\n%q", got)
	}
}

func TestUpdateKeepsAnLFSubmissionOnAnLFCardByteForByte(t *testing.T) {
	root := tempBoard(t)
	s := lineEndingCard(t, root, "lf-lf-card")
	lf := withSummary(s.Text)
	if err := tryUpdate(root, s, lf); err != nil {
		t.Fatal(err)
	}
	if got := storedCardBytes(t, root, s.Entry.TaskID); got != lf {
		t.Fatalf("stored body differs from the submission:\n%q", got)
	}
}

func TestUpdateRejectsProtectedChangesWhateverTheLineEndings(t *testing.T) {
	changes := map[string]struct {
		edit func(string) string
		code string
	}{
		"managed field": {func(text string) string {
			return strings.Replace(text, "- OWNER: codex", "- OWNER: claude", 1)
		}, "board.transaction_managed"},
		"managed section": {func(text string) string {
			return strings.Replace(text, "用户同意调整目标", "用户同意其他目标", 1)
		}, "board.transaction_managed"},
		"frozen section": {func(text string) string {
			return strings.Replace(text, "调整后的任务范围", "未经决策的目标", 1)
		}, "board.transaction_decision_required"},
	}
	endings := map[string]func(string) string{"LF": func(text string) string { return text }, "CRLF": crlf}
	root := tempBoard(t)
	base := lineEndingCard(t, root, "protected-card")
	for cardName, card := range endings {
		s := writeCardBytes(t, root, base, card(base.Text))
		for submissionName, submission := range endings {
			for changeName, change := range changes {
				label := cardName + " card, " + submissionName + " submission, " + changeName
				next := change.edit(base.Text)
				if next == base.Text {
					t.Fatalf("%s: fixture did not change the card", label)
				}
				var ke *Error
				err := tryUpdate(root, s, submission(next))
				if !errors.As(err, &ke) || ke.Code != change.code {
					t.Fatalf("%s: want %s, got %v", label, change.code, err)
				}
			}
		}
	}
}
