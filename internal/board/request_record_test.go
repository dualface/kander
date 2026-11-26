package board

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTaskWithoutDescriptionKeepsTemplate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		submitter string
		large     bool
	}{
		{name: "small", submitter: "Alice"},
		{name: "large", large: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tempBoard(t)
			target, err := NewTaskWithTransaction(root, NewTaskInput{
				Kind: "feature", Slug: tc.name, Title: "An ordinary card", Language: "zh-CN",
				Submitter: tc.submitter, Large: tc.large,
			})
			if err != nil {
				t.Fatal(err)
			}
			snapshot := transactionSnapshot(t, root, filepath.Base(target))
			// Captured from NewTaskWithTransaction at the delivery base.
			golden, err := os.ReadFile(filepath.Join("testdata", "request-record", tc.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var want string
			if err := json.Unmarshal(golden, &want); err != nil {
				t.Fatal(err)
			}
			stamp := MetadataFrom(snapshot.Text, FieldCreatedAt)
			got := strings.Replace(snapshot.Text, "- CREATED_AT: "+stamp, "- CREATED_AT: 2026-11-26 12:00", 1)
			if got != want {
				t.Fatalf("ordinary spec changed: got %q, want %q", got, want)
			}
			if _, found, err := ReadCardFile(snapshot.Entry, requestDocument); found || err != nil {
				t.Fatalf("ordinary card has a request record or read failed: %t, %v", found, err)
			}
		})
	}
}

func TestNewTaskRejectsInvalidUTF8Description(t *testing.T) {
	root := tempBoard(t)
	_, err := NewTaskWithTransaction(root, NewTaskInput{
		Kind: "feature", Slug: "invalid", Title: "Invalid description", Language: "en", Description: "text\xff",
	})
	var boardErr *Error
	if !errors.As(err, &boardErr) || boardErr.Code != "board.request_description_invalid" {
		t.Fatalf("creation error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "backlog"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid description changed backlog: %v, %v", entries, err)
	}
}

func TestRequestCreationRecoversRecordAndSpecTogether(t *testing.T) {
	for _, point := range []string{"prepared", "files", "revision"} {
		t.Run(point, func(t *testing.T) {
			root := tempBoard(t)
			input := NewTaskInput{Kind: "feature", Slug: "interrupted", Title: "Interrupted creation", Language: "en", Description: "First line\r\nSecond line"}
			id := todayPrefix() + "-" + input.Slug + "-task"
			interrupted := errors.New("stop publication")
			err := withTransactionCheckpoint(t.Context(), root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(stage string) error {
				if stage == point {
					return interrupted
				}
				return nil
			}, func(tx *Transaction) error {
				_, err := newTask(tx, root, input)
				return err
			})
			if !errors.Is(err, interrupted) {
				t.Fatalf("expected interruption at %s: %v", point, err)
			}
			if point == "files" {
				// The attachment is durable before the marker can exist.
				card := filepath.Join(root, "backlog", id)
				data, err := os.ReadFile(filepath.Join(card, requestDocument))
				if err != nil || string(data) != "First line\nSecond line" {
					t.Fatalf("record at checkpoint: %q, %v", data, err)
				}
				if _, err := os.Stat(filepath.Join(card, "spec.md")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("spec exists before entry publication: %v", err)
				}
			}
			if err := RecoverTransactions(root); err != nil {
				t.Fatal(err)
			}
			s, record, err := ReadDocumentSnapshot(root, id, requestDocument)
			if err != nil || s.Entry.State != "backlog" || s.Revision != 1 || MetadataFrom(s.Text, FieldRequest) != requestDocument || record != "First line\nSecond line" {
				t.Fatalf("recovered revision=%d state=%q record=%q err=%v\nspec=%s", s.Revision, s.Entry.State, record, err, s.Text)
			}
		})
	}
}
