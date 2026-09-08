package board

import (
	"context"
	"errors"

	"github.com/dualface/kander/internal/fs"
)

// ScanContext captures all states, documents and revisions under shared locks.
// The context bounds lock contention, not OS file operations.
func ScanContext(ctx context.Context, root string) (Board, error) {
	return scanContext(ctx, root, nil, false)
}

// ScanTargetsContext captures selected identities without reading unrelated cards.
func ScanTargetsContext(ctx context.Context, root string, values []string) (Board, error) {
	ids := make([]string, len(values))
	for i, value := range values {
		id, err := NormalizeTaskID(value)
		if err != nil {
			return Board{}, err
		}
		ids[i] = id
	}
	if ids == nil {
		ids = []string{}
	}
	return scanContext(ctx, root, ids, false)
}

func (locks *lockSet) takeSharedContext(ctx context.Context, root, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := fs.OpenLockFile(root, path)
	if err != nil {
		return err
	}
	lock, err := fs.LockSharedContext(ctx, file)
	if err != nil {
		return errors.Join(err, file.Close())
	}
	*locks = append(*locks, heldLock{file, lock})
	return nil
}

func scanContext(ctx context.Context, root string, ids []string, dispatches bool, warnings ...*WarningLog) (b Board, err error) {
	if err = ctx.Err(); err != nil {
		return b, err
	}
	if err = ensureControl(root); err != nil {
		return b, err
	}
	var locks lockSet
	defer func() { err = errors.Join(err, locks.close()) }()
	if err = locks.takeSharedContext(ctx, root, control(root, "locks", "board.lock")); err != nil {
		return b, err
	}
	if ids == nil {
		b, err = scan(root)
	} else {
		b, err = scanTargets(root, ids)
	}
	if err != nil {
		return b, err
	}
	selected := append([]string{}, ids...)
	for id := range b.Entries {
		selected = append(selected, id)
	}
	selected, err = orderedIDs(selected, false)
	if err != nil {
		return b, err
	}
	for _, id := range selected {
		if err = locks.takeSharedContext(ctx, root, control(root, "locks", id+".lock")); err != nil {
			return b, err
		}
	}
	// Journal locking is terminal: release it before reading card files.
	var journal lockSet
	if err = journal.takeSharedContext(ctx, root, control(root, "locks", "journal.lock")); err != nil {
		return b, err
	}
	records, readErr := readPendingRecords(root, warnings...)
	if err = errors.Join(readErr, journal.close()); err != nil {
		return b, err
	}
	for _, rec := range records {
		if rec.Phase != "prepared" {
			continue
		}
		affected := ids == nil || len(rec.Entries) > 0 || len(rec.Migrations) > 0
		for _, id := range selected {
			if _, ok := rec.Revisions[id]; ok {
				affected = true
			}
		}
		if affected {
			return b, kanbanError("board.transaction_pending", rec.ID)
		}
	}
	b.documents = make(map[string]string, len(b.Entries))
	b.documentErrors = make(map[string]error)
	b.revisions = make(map[string]uint64)
	if dispatches {
		b.dispatches = make(map[string]*DispatchSummary)
		b.dispatchErrors = make(map[string]error)
	}
	for _, id := range selected {
		if err = ctx.Err(); err != nil {
			return b, err
		}
		entry, ok := b.Entries[id]
		if !ok {
			continue
		}
		version, e := revision(root, id)
		if e != nil {
			return b, e
		}
		text, e := readDocument(entry)
		if e != nil {
			b.documentErrors[id] = e
		} else {
			b.documents[id] = text
			entry = attachSize(entry, text)
		}
		b.revisions[id] = version
		entry.Version = &Version{revision: version, authorization: authFrom(text), warnings: journalWarningLog(warnings)}
		b.Entries[id] = entry
		if dispatches && e == nil {
			tx := &Transaction{root: root, scope: LockScope{Tasks: selected, ReadOnly: true}}
			b.dispatches[id], b.dispatchErrors[id] = snapshotDispatch(tx, id, text, version)
			if err = ctx.Err(); err != nil {
				return b, err
			}
		}
	}
	return b, nil
}

// Revision returns the committed revision captured with the immutable document.
func (b Board) Revision(id string) (uint64, error) {
	entry, err := Locate(b, id)
	if err != nil {
		return 0, err
	}
	value, ok := b.revisions[entry.TaskID]
	if !ok {
		return 0, kanbanError("board.transaction_invalid", "missing version")
	}
	return value, nil
}

// SnapshotReadStatus classifies unavailable facts without matching translated text.
func SnapshotReadStatus(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "maintenance"
	}
	var failure *Error
	if errors.As(err, &failure) && failure.Code == "board.transaction_pending" {
		return "recoverable"
	}
	return "invalid"
}
