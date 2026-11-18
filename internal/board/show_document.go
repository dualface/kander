package board

import (
	"errors"
	"os"
	"unicode/utf8"
)

// afterDocumentSnapshot lets a test act between the snapshot and the document
// read of ReadDocumentSnapshot; it is nil in production.
var afterDocumentSnapshot func()

// ReadDocumentSnapshot reads one document of a card together with the snapshot
// it belongs to. The snapshot and the document come from one read-only
// transaction, so the revision always describes the returned text. The path
// rules are those of the document update entrance; the read never follows a
// symlink.
func ReadDocumentSnapshot(root, id, name string) (Snapshot, string, error) {
	id, err := NormalizeTaskID(id)
	if err != nil {
		return Snapshot{}, "", err
	}
	var (
		s    Snapshot
		text string
	)
	err = WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		var e error
		if s, e = tx.Snapshot(id); e != nil {
			return e
		}
		if afterDocumentSnapshot != nil {
			afterDocumentSnapshot()
		}
		if text, e = tx.Read(id, name); e != nil {
			if errors.Is(e, os.ErrNotExist) {
				return kanbanError("board.document_not_found", name)
			}
			return e
		}
		if !utf8.ValidString(text) {
			return kanbanError("board.task_document_is_not_valid_utf_8", name)
		}
		if name == "spec.md" {
			// The body shows the same text as the snapshot, English schema names included.
			text, _ = normalizeLegacyCard(text)
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, "", err
	}
	return s, text, nil
}
