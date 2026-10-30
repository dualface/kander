package launch

import "github.com/dualface/kander/internal/board"

// writeCardFixture publishes card text directly. Production code changes OWNER,
// SESSION and WINDOW through the board transition entrances; a test uses this to
// set up a card or to stand in for a concurrent writer.
func writeCardFixture(root string, entry board.Entry, text string) error {
	return board.WithTransaction(root, board.LockScope{Tasks: []string{entry.TaskID}}, func(tx *board.Transaction) error {
		return tx.Put(entry.TaskID, "spec.md", text)
	})
}

// executionFixture gives a card an executor through the takeover entrance, the
// way a card reaches a session and a window without a launcher in the test.
func executionFixture(root string, entry board.Entry, owner, session, window string) error {
	if _, err := board.BindExecutionCursor(root, entry); err != nil {
		return err
	}
	if _, err := board.BeginTakeover(root, entry, board.TakeoverInput{Owner: owner, Session: session, Window: window}); err != nil {
		return err
	}
	return board.ConfirmTaskStart(root, entry)
}
