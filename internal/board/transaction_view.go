package board

// ReadOnly returns a view with the same declared lock scope and no write
// capability. Service control producers can inspect legacy cards without
// pretending to update them or bypassing any task ownership declaration.
func (tx *Transaction) ReadOnly() *Transaction {
	view := *tx
	view.scope.ReadOnly = true
	view.scope.validateMutable = false
	return &view
}

// CommittedView captures all documents under the caller's existing exclusive
// board lock. It never acquires another lock or exposes a write cursor.
func (tx *Transaction) CommittedView() (Board, error) {
	if !tx.scope.ExclusiveBoard {
		return Board{}, kanbanError("board.transaction_invalid", "exclusive board view required")
	}
	view, err := scan(tx.root)
	if err != nil {
		return view, err
	}
	view.documents = make(map[string]string, len(view.Entries))
	view.documentErrors = make(map[string]error)
	view.revisions = make(map[string]uint64, len(view.Entries))
	for id, entry := range view.Entries {
		text, err := readDocument(entry)
		if err != nil {
			view.documentErrors[id] = err
		} else {
			view.documents[id] = text
			entry = attachSize(entry, text)
		}
		value, err := revision(tx.root, id)
		if err != nil {
			return view, err
		}
		view.revisions[id] = value
		entry.Version = nil
		view.Entries[id] = entry
	}
	return view, nil
}
