package board

// fixtureWrite publishes card text through a cursor without touching the
// execution ledger. It reproduces a write only tests may make: production code
// changes OWNER, SESSION and WINDOW through the transition entrances.
func fixtureWrite(root string, entry Entry, text string) error {
	if entry.Version == nil {
		return kanbanError("board.transaction_conflict", entry.TaskID)
	}
	entry.Version.mu.Lock()
	defer entry.Version.mu.Unlock()
	err := WithTransaction(root, LockScope{Tasks: []string{entry.TaskID}}, func(tx *Transaction) error {
		s, err := tx.Expect(entry.TaskID, entry.State, entry.Version.revision)
		if err != nil {
			return err
		}
		if err = tx.requireExecution(s, entry.Version.authorization, false); err != nil {
			return err
		}
		if err = tx.requireFullExecution(s); err != nil {
			return err
		}
		if authFrom(text) != authFrom(s.Text) {
			return dispatchError(entry.TaskID)
		}
		return tx.Put(entry.TaskID, "spec.md", text)
	})
	if err == nil {
		entry.Version.revision++
	}
	return err
}
