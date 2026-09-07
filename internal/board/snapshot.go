package board

import "errors"

// Scan obtains a stable directory view and shared task locks before attaching
// versions. Mutations of unrelated cards may run concurrently.
func Scan(root string) (b Board, err error) {
	locks, err := acquire(root, LockScope{ReadOnly: true})
	if err != nil {
		return b, err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	b, err = scan(root)
	if err != nil {
		return b, err
	}
	ids := make([]string, 0, len(b.Entries))
	for id := range b.Entries {
		ids = append(ids, id)
	}
	ids, err = orderedIDs(ids, false)
	if err != nil {
		return b, err
	}
	for _, id := range ids {
		if err = locks.take(root, control(root, "locks", id+".lock"), true); err != nil {
			return b, err
		}
	}
	if err = pending(root, ids); err != nil {
		return b, err
	}
	for id, e := range b.Entries {
		v, er := revision(root, id)
		if er != nil {
			return b, er
		}
		e.Version = &Version{revision: v}
		b.Entries[id] = e
	}
	// Also identify interrupted entry creation, whose task is not visible yet.
	records, err := operationRecords(root)
	if err != nil {
		return b, err
	}
	for _, rec := range records {
		if rec.Phase == "prepared" && len(rec.Entries) > 0 {
			return b, kanbanError("board.transaction_pending", rec.ID)
		}
	}
	return b, nil
}

// ScanTargets reads selected identities with the same visibility guarantees as Scan.
func ScanTargets(root string, values []string) (b Board, err error) {
	ids := make([]string, len(values))
	for i, v := range values {
		ids[i], err = NormalizeTaskID(v)
		if err != nil {
			return b, err
		}
	}
	err = WithTransaction(root, LockScope{Tasks: ids, ReadOnly: true}, func(tx *Transaction) error {
		var e error
		b, e = scanTargets(root, ids)
		if e != nil {
			return e
		}
		for id, entry := range b.Entries {
			v, er := revision(root, id)
			if er != nil {
				return er
			}
			entry.Version = &Version{revision: v}
			b.Entries[id] = entry
		}
		return nil
	})
	return b, err
}

// ReadDocument reads a committed revision and rejects an Entry invalidated by a
// concurrent mutation. Relocate through ReadSnapshot when retrying a conflict.
func ReadDocument(entry Entry) (string, error) {
	if entry.Version != nil {
		entry.Version.mu.Lock()
		defer entry.Version.mu.Unlock()
	}
	s, err := ReadSnapshot(boardRootFromEntry(entry), entry.TaskID)
	if err != nil {
		return "", err
	}
	if s.Entry.Path != entry.Path || (entry.Version != nil && entry.Version.revision != s.Revision) {
		return "", kanbanError("board.transaction_conflict", entry.TaskID)
	}
	return s.Text, nil
}

// WriteManagedDocument commits a lifecycle operation's existing document and
// advances only its own cursor. Other operation cursors remain stale.
func WriteManagedDocument(root string, entry Entry, text string) error {
	return managedMutation(root, entry, text, "")
}

// RollbackDocument restores the original document and optional state only while
// this operation still owns the current revision; it cannot erase newer work.
func RollbackDocument(root string, entry Entry, text, state string) error {
	return managedMutation(root, entry, text, state)
}
func managedMutation(root string, entry Entry, text, state string) error {
	if entry.Version == nil {
		return kanbanError("board.transaction_conflict", entry.TaskID)
	}
	entry.Version.mu.Lock()
	defer entry.Version.mu.Unlock()
	err := WithTransaction(root, LockScope{Tasks: []string{entry.TaskID}, ExclusiveBoard: state != "" && state != entry.State}, func(tx *Transaction) error {
		s, err := tx.Expect(entry.TaskID, entry.State, entry.Version.revision)
		if err != nil {
			return err
		}
		if s.Entry.Path != entry.Path {
			return kanbanError("board.transaction_conflict", entry.TaskID)
		}
		if err = tx.Put(entry.TaskID, "spec.md", text); err != nil {
			return err
		}
		if state != "" && state != entry.State {
			return tx.Relocate(entry.TaskID, state)
		}
		return nil
	})
	if err == nil {
		entry.Version.revision++
	}
	return err
}

// MoveOptions are the dedicated managed-field entrances. A decision reference
// records the user's authorization basis; the tool cannot verify user intent.
type MoveOptions struct {
	Owner            string
	Result           string
	Reason           string
	Decision         string
	DuplicateOf      string
	ExpectedRevision *uint64
}

// MoveEntry preserves the existing API for callers already holding a snapshot.
func MoveEntry(entry Entry, root, target string) (Entry, error) {
	return MoveWithOptions(entry, root, target, MoveOptions{})
}

// MoveWithOptions validates and publishes state, result and timestamps together.
func MoveWithOptions(entry Entry, root, target string, options MoveOptions) (moved Entry, err error) {
	if entry.Version == nil {
		return moved, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	if entry.Version != nil {
		entry.Version.mu.Lock()
		defer entry.Version.mu.Unlock()
	}
	err = WithTransaction(root, LockScope{Tasks: []string{entry.TaskID}, ExclusiveBoard: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(entry.TaskID)
		if e != nil {
			return e
		}
		if s.Entry.State != entry.State || s.Entry.Path != entry.Path || (entry.Version != nil && s.Revision != entry.Version.revision) || (options.ExpectedRevision != nil && s.Revision != *options.ExpectedRevision) {
			return kanbanError("board.transaction_conflict", entry.TaskID)
		}
		if !allowedMove(entry.State, target) {
			return kanbanError("board.move_not_allowed", entry.State, target)
		}
		updated, e := moveMetadata(s.Text, entry.State, target, options)
		if e != nil {
			return e
		}
		if e = validateTarget(s.Entry, target, updated); e != nil {
			return e
		}
		if target == "done" {
			updated, e = completionMetadata(updated)
			if e != nil {
				return e
			}
		}
		if updated != s.Text {
			if e = tx.Put(entry.TaskID, "spec.md", updated); e != nil {
				return e
			}
		}
		if e = tx.Relocate(entry.TaskID, target); e != nil {
			return e
		}
		moved = s.Entry
		moved.State = target
		moved.Path = joinBoard(root, target, baseEntryName(entry))
		moved.Document = moved.Path
		if moved.Kind == "large" {
			moved.Document = joinBoard(moved.Path, "spec.md")
		}
		moved.Version = &Version{revision: s.Revision + 1}
		return nil
	})
	return moved, err
}
