package board

import (
	"context"
)

// Scan reads a coordinated committed view with blocking lock acquisition.
func Scan(root string) (Board, error) { return ScanContext(context.Background(), root) }

// ScanWithWarnings captures a board and routes journal advisories to the log.
// Entries retain this operation-local log through their version cursors.
func ScanWithWarnings(root string, warnings *WarningLog) (Board, error) {
	return scanContext(context.Background(), root, nil, false, warnings)
}

// ScanTargets reads only selected identities with blocking lock acquisition.
func ScanTargets(root string, values []string) (Board, error) {
	return ScanTargetsContext(context.Background(), root, values)
}

// ReadDocument reads a committed revision and rejects an Entry invalidated by a
// concurrent mutation. Relocate through ReadSnapshot when retrying a conflict.
func ReadDocument(entry Entry) (string, error) {
	if entry.Version != nil {
		entry.Version.mu.Lock()
		defer entry.Version.mu.Unlock()
	}
	s, err := ReadSnapshotWithWarnings(boardRootFromEntry(entry), entry.TaskID, entryWarningLog(entry))
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
	err := WithTransaction(root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{entry.TaskID}, ExclusiveBoard: state != "" && state != entry.State, warnings: entryWarningLog(entry)}, func(tx *Transaction) error {
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
		if s.Entry.Path != entry.Path {
			return kanbanError("board.transaction_conflict", entry.TaskID)
		}
		if err = stageTaskStart(tx, s, text, state); err != nil {
			return err
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
	Authorization    ExecutionAuthorization
	DeliveryCommit   string
	Disposition      *ArtifactReference
	Replayed         *bool
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
	scope := LockScope{Tasks: []string{entry.TaskID}, ExclusiveBoard: true}
	if target == "done" {
		scope, err = reviewGateScope(root, entry.TaskID, true, entryWarningLog(entry))
		if err != nil {
			return moved, err
		}
	}
	scope.warnings = entryWarningLog(entry)
	err = WithTransaction(root, scope, func(tx *Transaction) error {
		s, e := tx.Snapshot(entry.TaskID)
		if e != nil {
			return e
		}
		if s.Entry.State != entry.State || s.Entry.Path != entry.Path || (entry.Version != nil && s.Revision != entry.Version.revision) || (options.ExpectedRevision != nil && s.Revision != *options.ExpectedRevision) {
			return kanbanError("board.transaction_conflict", entry.TaskID)
		}
		replayed, e := stageDispatchMove(tx, s, target, options)
		if e != nil {
			return e
		}
		if options.Replayed != nil {
			*options.Replayed = replayed
		}
		if replayed {
			moved = s.Entry
			return nil
		}
		if !allowedMove(entry.State, target) && !(options.Authorization.DispatchID != "" && target == "working" && entry.State == "working") {
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
			if _, e = validateTaskReview(tx, entry.TaskID, true); e != nil {
				return e
			}
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
		if moved.IsDirectory() {
			moved.Document = joinBoard(moved.Path, "spec.md")
		}
		moved.Version = &Version{revision: s.Revision + 1, authorization: authFrom(updated), warnings: entryWarningLog(entry)}
		return nil
	})
	return moved, err
}

// Document returns the immutable body captured by Scan/ScanTargets under the
// same shared locks as Entries. Display and subscription consumers can finish
// reading that committed snapshot even if a later mutation moves the card.
// Mutations still use ReadSnapshot/Expect and never treat this text as current.
func (b Board) Document(id string) (string, error) {
	entry, err := Locate(b, id)
	if err != nil {
		return "", err
	}
	if err := b.documentErrors[entry.TaskID]; err != nil {
		return "", err
	}
	text, ok := b.documents[entry.TaskID]
	if !ok {
		return "", kanbanError("board.transaction_invalid", "missing document snapshot")
	}
	return text, nil
}
