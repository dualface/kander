package board

import (
	"context"
	"errors"
	"unicode/utf8"
)

// WithImportSnapshot holds the exclusive source lock for a recovered board.
// The context bounds lock acquisition. The importer separately checks it
// during the source copy, but must finish verification after the copy.
func WithImportSnapshot(ctx context.Context, root string, fn func() error) (err error) {
	if err := ensureLayout(root); err != nil {
		return err
	}
	locks, err := acquireContext(ctx, root, LockScope{ExclusiveBoard: true, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	// A writer may have started an operation between recovery and this lock.
	var journal lockSet
	if err := journal.takeSharedContext(ctx, root, control(root, "locks", "journal.lock")); err != nil {
		return err
	}
	records, readErr := readPendingRecords(root)
	if err := errors.Join(readErr, journal.close()); err != nil {
		return err
	}
	for _, record := range records {
		if record.Phase == "prepared" {
			return kanbanError("board.transaction_pending", record.ID)
		}
	}
	return fn()
}

// InspectImportCards reads card identities and structural problems without
// opening locks or creating runtime records. The caller must serialize the
// board or inspect it inside WithImportSnapshot.
func InspectImportCards(root string) (Board, error) { return scan(root) }

// ImportCardDetails extracts the legacy-aware group and returns a replacement
// spec only when the same SIZE default as DefaultMissingSize applies.
func ImportCardDetails(entry Entry, raw []byte) (group, defaulted string) {
	if !utf8.Valid(raw) {
		// The full staged check reports the encoding problem for this card.
		return "", ""
	}
	text := string(raw)
	normalized, _ := normalizeLegacyCard(text)
	if entry.IsDirectory() && len(fieldLines(text, FieldSize)) == 0 {
		defaulted = DefaultedSizeText(text)
	}
	return TaskGroupFrom(normalized), defaulted
}
