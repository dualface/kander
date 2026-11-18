package notify

import (
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
