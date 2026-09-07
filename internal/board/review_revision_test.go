package board

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReviewPublicationAfterControlledHeaderUpdate(t *testing.T) {
	for _, tc := range []struct{ name, newline, header string }{
		{"spaces", "\n", "## REVIEWS  \n"},
		{"header-crlf", "\n", "## REVIEWS\r\n"},
		{"crlf-card", "\r\n", "## REVIEWS \t\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tempBoard(t)
			id := archiveCard(t, root, "header")
			if tc.newline == "\r\n" {
				s := transactionSnapshot(t, root, id)
				// Existing CRLF cards are supported; fixture creation is producer-owned.
				if err := WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
					return tx.Put(id, "spec.md", strings.ReplaceAll(s.Text, "\n", "\r\n"))
				}); err != nil {
					t.Fatal(err)
				}
			}
			first := finalizedRun(t, root, archiveInput([]string{id}, "first", "PM"))
			publishRun(t, root, first.RunID)
			s := transactionSnapshot(t, root, id)
			text := strings.Replace(s.Text, "## REVIEWS\r\n", tc.header, 1)
			if text == s.Text {
				text = strings.Replace(s.Text, "## REVIEWS\n", tc.header, 1)
			}
			text += tc.newline + "## OWNER_NOTE" + tc.newline + "preserve me" + tc.newline
			updateSnapshot(t, root, s, text)
			second := finalizedRun(t, root, archiveInput([]string{id}, "second", "QA"))
			publishRun(t, root, second.RunID)
			publishRun(t, root, second.RunID)
			for _, run := range []ReviewRun{first, second} {
				if err := ReviewPublicationComplete(root, run.RunID); err != nil {
					t.Fatal(err)
				}
			}
			s = transactionSnapshot(t, root, id)
			indexes, err := ParseReviewIndexes(s.Text)
			if err != nil || len(indexes) != 2 || !strings.HasSuffix(s.Text, "## OWNER_NOTE"+tc.newline+"preserve me"+tc.newline) {
				t.Fatalf("%+v %v %q", indexes, err, s.Text)
			}
			code, _, stderr, err := CheckBoard(root, []string{id}, false)
			if code != 0 || err != nil {
				t.Fatalf("%d %v %v", code, stderr, err)
			}
		})
	}
}

func TestReviewCheckPublicDiagnosticsAndOrder(t *testing.T) {
	root := tempBoard(t)
	ids := []string{archiveCard(t, root, "diagnostic-a"), archiveCard(t, root, "diagnostic-b")}
	for _, runID := range []string{"pm-z", "pm-a"} {
		run := finalizedRun(t, root, archiveInput(ids, runID, "PM"))
		publishRun(t, root, run.RunID)
	}
	for i, id := range ids {
		s := transactionSnapshot(t, root, id)
		name := []string{"report.md", "manifest.json"}[i]
		for _, runID := range []string{"pm-a", "pm-z"} {
			if err := os.WriteFile(filepath.Join(s.Entry.Path, "reviews", runID, name), []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var previous []string
	for i := 0; i < 20; i++ {
		code, _, stderr, err := CheckBoard(root, nil, false)
		if code == 0 || err != nil || len(stderr) != 4 {
			t.Fatalf("%d %v %v", code, stderr, err)
		}
		for j, id := range ids {
			for k, runID := range []string{"pm-a", "pm-z"} {
				line := stderr[j*2+k]
				if !strings.Contains(line, id) || !strings.Contains(line, runID) {
					t.Fatalf("missing attribution/order: %s", line)
				}
			}
		}
		if i > 0 && !reflect.DeepEqual(previous, stderr) {
			t.Fatalf("unstable: %v / %v", previous, stderr)
		}
		previous = stderr
	}
}

func TestReviewCheckContinuesAfterInvalidRecords(t *testing.T) {
	for _, corruption := range []string{"entry", "json", "schema", "card"} {
		t.Run(corruption, func(t *testing.T) {
			root := tempBoard(t)
			bad := archiveCard(t, root, "bad-record")
			good := archiveCard(t, root, "other-record")
			run := finalizedRun(t, root, archiveInput([]string{good}, "other-run", "PM"))
			publishRun(t, root, run.RunID)
			s := transactionSnapshot(t, root, good)
			if err := os.WriteFile(filepath.Join(s.Entry.Path, "reviews", run.RunID, "report.md"), []byte("tamper"), 0600); err != nil {
				t.Fatal(err)
			}
			dir := control(root, "groups", reviewControlGroup, "runs", "broken-run")
			switch corruption {
			case "entry":
				if err := os.WriteFile(dir, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "json", "schema":
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				data := "{"
				if corruption == "schema" {
					data = "{}"
				}
				if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			case "card":
				s := transactionSnapshot(t, root, bad)
				if err := os.WriteFile(s.Entry.Document, []byte{0xff}, 0600); err != nil {
					t.Fatal(err)
				}
			}
			problems, err := CheckReviewEvidence(root, []string{bad, good})
			if err != nil || len(problems) < 2 {
				t.Fatalf("%+v %v", problems, err)
			}
			found := false
			for _, p := range problems {
				if strings.Contains(p.Message, good) && strings.Contains(p.Message, run.RunID) {
					found = true
				}
			}
			if !found {
				t.Fatalf("remaining card skipped: %+v", problems)
			}
		})
	}
}

func TestReviewPublicationRejectsTerminalMove(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "terminal")
	run := finalizedRun(t, root, archiveInput([]string{id}, "pending", "PM"))
	s := transactionSnapshot(t, root, id)
	if err := UpdateDocument(root, id, UpdateOptions{Document: "report.md", Text: "Completed fixture", ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	updateSnapshot(t, root, s, strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\nCompleted fixture", 1))
	s = transactionSnapshot(t, root, id)
	entry, err := MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	result, failed, err := PublishReviewRun(root, run.RunID)
	if err != nil || len(failed) != 1 || result.Published[id] {
		t.Fatalf("%+v %v %v", result, failed, err)
	}
	if _, err = ReadReviewOriginal(root, run.RunID, "report.md"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr, err := CheckBoard(root, nil, false)
	if code != 0 || err != nil {
		t.Fatalf("default scope: %d %v %v", code, stderr, err)
	}
	code, _, stderr, err = CheckBoard(root, []string{id}, false)
	if code == 0 || err != nil || !strings.Contains(strings.Join(stderr, "\n"), run.RunID) {
		t.Fatalf("explicit scope: %d %v %v", code, stderr, err)
	}
	if _, err = MoveEntry(entry, root, "working"); err == nil {
		t.Fatal("terminal card reused")
	}
}
