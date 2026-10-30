package takeover

import (
	"testing"

	"github.com/dualface/kander/internal/board"
)

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
