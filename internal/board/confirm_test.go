package board

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestMoveEntryRequiresConfirmationBeforeTodoGate(t *testing.T) {
	root := tempBoard(t)
	path, err := NewTaskWithTransaction(root, NewTaskInput{
		Kind: "feature", Slug: "confirm", Title: "Request card", Description: "Original request",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, filepath.Base(path))
	_, err = MoveEntry(s.Entry, root, "todo")
	var rejection *Error
	if !errors.As(err, &rejection) || rejection.Code != "board.confirmation_required" {
		t.Fatalf("MoveEntry returned %v, want board.confirmation_required", err)
	}
	after := transactionSnapshot(t, root, s.Entry.TaskID)
	if after.Entry.State != s.Entry.State || after.Revision != s.Revision || after.Text != s.Text {
		t.Fatal("rejected move changed the card")
	}
}
