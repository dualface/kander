package board

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// TransactionExtension lets a domain service validate its authority and stage
// its control records in the same redo transaction as a board mutation. Both
// staging callbacks run under the declared board, group and task locks. They
// must use tx methods and must never call a locking board API.
type TransactionExtension struct {
	Groups []string
	Tasks  []string
	Before func(*Transaction) error
	After  func(*Transaction) error
	// Published runs after the publication attempt and release of board locks.
	// It cannot roll back the journal. An error means the outcome is uncertain.
	Published func(TransactionChanges, error)
}

// TransactionChanges identifies card revisions and board-relative files in a
// publication without exposing document or control-record contents.
type TransactionChanges struct {
	Tasks []string
	Files []string
}

func (tx *Transaction) notifyPublication(extensions []TransactionExtension, err error) {
	var changes TransactionChanges
	for _, extension := range extensions {
		if extension.Published == nil {
			continue
		}
		if changes.Files == nil {
			changes.Tasks = tx.ChangedTaskIDs()
			changes.Files = make([]string, 0, len(tx.record.Files))
			for _, file := range tx.record.Files {
				changes.Files = append(changes.Files, file.Path)
			}
		}
		extension.Published(changes, err)
	}
}

// StagedSnapshot returns the document, location and version this transaction
// will publish. A service can bind a durable operation receipt to that exact
// result without reading the board again after releasing its locks.
func (tx *Transaction) StagedSnapshot(id string) (Snapshot, error) {
	s, err := tx.Snapshot(id)
	if err != nil {
		return Snapshot{}, err
	}
	for _, file := range tx.record.Files {
		if filepath.Join(tx.root, file.Path) == s.Entry.Document {
			s.Text = file.After
		}
	}
	for _, entry := range tx.record.Entries {
		if filepath.Join(tx.root, entry.From) == s.Entry.Path {
			s.Entry.Path = filepath.Join(tx.root, entry.To)
			s.Entry.State = filepath.Dir(entry.To)
			s.Entry.Document = s.Entry.Path
			if s.Entry.IsDirectory() {
				s.Entry.Document = filepath.Join(s.Entry.Path, "spec.md")
			}
		}
	}
	if revision, ok := tx.record.Revisions[id]; ok {
		s.Revision, s.OperationID = revision, tx.record.ID
	}
	return s, nil
}

// ChangedTaskIDs names the cards staged by the producer. A service uses it to
// verify authority for every written card, including copies outside the direct
// command member set. The set is stable after the producer callback returns.
func (tx *Transaction) ChangedTaskIDs() []string {
	return slices.Sorted(maps.Keys(tx.record.Revisions))
}

// SetOperationResult makes the producer's typed result available to a service
// extension before the redo intent is persisted. It is not a stored field.
func (tx *Transaction) SetOperationResult(value any) { tx.result = value }

// OperationResult returns the result staged by a domain producer.
func (tx *Transaction) OperationResult() any { return tx.result }

func combineExtensions(extensions []TransactionExtension) TransactionExtension {
	var combined TransactionExtension
	for _, extension := range extensions {
		combined.Groups = append(combined.Groups, extension.Groups...)
		combined.Tasks = append(combined.Tasks, extension.Tasks...)
	}
	combined.Before = func(tx *Transaction) error {
		for _, extension := range extensions {
			if extension.Before != nil {
				if err := extension.Before(tx); err != nil {
					return err
				}
			}
		}
		return nil
	}
	combined.After = func(tx *Transaction) error {
		for _, extension := range extensions {
			if extension.After != nil {
				if err := extension.After(tx); err != nil {
					return err
				}
			}
		}
		return nil
	}
	combined.Published = func(changes TransactionChanges, err error) {
		for _, extension := range extensions {
			if extension.Published != nil {
				extension.Published(changes, err)
			}
		}
	}
	return combined
}

func extendTransaction(scope LockScope, fn func(*Transaction) error, extensions []TransactionExtension) (LockScope, func(*Transaction) error) {
	for _, extension := range extensions {
		scope.Groups = append(scope.Groups, extension.Groups...)
		scope.Tasks = append(scope.Tasks, extension.Tasks...)
	}
	return scope, func(tx *Transaction) error {
		for _, extension := range extensions {
			if extension.Before != nil {
				if err := extension.Before(tx); err != nil {
					return err
				}
			}
		}
		if err := fn(tx); err != nil {
			return err
		}
		for _, extension := range extensions {
			if extension.After != nil {
				if err := extension.After(tx); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// StagedGroup reads a producer's pending bytes before falling back to committed
// control data. It lets finalization hash the exact upload in the same redo.
func (tx *Transaction) StagedGroup(group, name string) ([]byte, bool, error) {
	path, err := tx.groupPath(group, name)
	if err != nil {
		return nil, false, err
	}
	rel, err := filepath.Rel(tx.root, path)
	if err != nil {
		return nil, false, err
	}
	for _, file := range tx.record.Files {
		if strings.EqualFold(file.Path, rel) {
			return []byte(file.After), true, nil
		}
	}
	return tx.ReadGroup(group, name)
}
