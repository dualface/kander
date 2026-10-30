package notify

import (
	"cmp"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// writeCardFixture publishes card text directly. Production code changes OWNER,
// SESSION and WINDOW through the board transition entrances; a test uses this to
// set up a card or to stand in for a concurrent writer.
func writeCardFixture(root string, entry board.Entry, text string) error {
	return board.WithTransaction(root, board.LockScope{Tasks: []string{entry.TaskID}}, func(tx *board.Transaction) error {
		return tx.Put(entry.TaskID, "spec.md", text)
	})
}

// executionFixture records the card's own OWNER, SESSION and WINDOW as a launched
// execution, so a test card is not read as a self-claimed one.
func executionFixture(t *testing.T, root string, entry board.Entry) {
	t.Helper()
	s, err := board.ReadSnapshot(root, entry.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.BindExecutionCursor(root, entry); err != nil {
		t.Fatal(err)
	}
	in := board.TakeoverInput{Owner: board.MetadataFrom(s.Text, board.FieldOwner), Session: board.MetadataFrom(s.Text, board.FieldSession), Window: board.MetadataFrom(s.Text, board.FieldWindow)}
	if _, err = board.BeginTakeover(root, entry, in); err != nil {
		t.Fatal(err)
	}
	if err = board.ConfirmTaskStart(root, entry); err != nil {
		t.Fatal(err)
	}
}

// setExecutionFields changes SESSION or WINDOW of the card at path through the
// takeover entrance; an empty value keeps the field.
func setExecutionFields(t *testing.T, path, session, window string) {
	t.Helper()
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	s, err := board.ReadSnapshot(root, filepath.Base(filepath.Dir(path)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	in := board.TakeoverInput{Owner: board.MetadataFrom(s.Text, board.FieldOwner), Session: cmp.Or(session, board.MetadataFrom(s.Text, board.FieldSession)), Window: cmp.Or(window, board.MetadataFrom(s.Text, board.FieldWindow))}
	if _, err = board.BeginTakeover(root, s.Entry, in); err != nil {
		t.Fatal(err)
	}
	if err = board.ConfirmTaskStart(root, s.Entry); err != nil {
		t.Fatal(err)
	}
}
