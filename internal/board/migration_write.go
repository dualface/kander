package board

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// Schema-1 records without explicit names use these operation-bound defaults.
// Arbitrary atomic-write leftovers are never adopted based on their filename.
func migrationWriteNames(id string, m FormMigration) (string, string) {
	rewrite, original := m.Rewrite, m.Original
	if rewrite == "" {
		rewrite = "spec.write-" + id
	}
	if original == "" {
		original = "spec.original-" + id
	}
	return rewrite, original
}

// rewriteMigrationDocument publishes through named redo steps, not an atomic
// writer with an unrecorded scratch path. The original remains intact until the
// full replacement is synced. Partial writes are resumed only when they match
// an exact prefix of the journal's after image, under exclusive board access.
func rewriteMigrationDocument(root, stage, id string, m FormMigration, checkpoint func(string) error) error {
	rewriteName, originalName := migrationWriteNames(id, m)
	spec := filepath.Join(stage, "spec.md")
	rewrite := filepath.Join(stage, rewriteName)
	original := filepath.Join(stage, originalName)
	current, currentExists, err := fs.ReadRegularFileIfExists(root, spec)
	if err != nil {
		return err
	}
	before, beforeExists, err := fs.ReadRegularFileIfExists(root, original)
	if err != nil {
		return err
	}
	output, outputExists, err := fs.ReadRegularFileIfExists(root, rewrite)
	if err != nil {
		return err
	}
	if beforeExists && string(before) != m.Before {
		return kanbanError("board.transaction_conflict", original)
	}
	if currentExists && string(current) == m.After {
		if outputExists {
			return kanbanError("board.transaction_conflict", rewrite)
		}
		if beforeExists {
			if _, err := fs.RemoveRegularFileIfExists(root, original); err != nil {
				return err
			}
		}
		return nil
	}
	if currentExists {
		if string(current) != m.Before || beforeExists {
			return kanbanError("board.transaction_conflict", spec)
		}
	} else if !beforeExists {
		return kanbanError("board.transaction_conflict", spec)
	}
	if outputExists && !strings.HasPrefix(m.After, string(output)) {
		return kanbanError("board.transaction_conflict", rewrite)
	}
	file, err := fs.OpenAppendFile(root, rewrite)
	if err != nil {
		return err
	}
	if err := checkpoint("migration-write-created"); err != nil {
		return errors.Join(err, file.Close())
	}
	// Appending the remainder also handles a process killed during WriteString.
	if _, err := file.WriteString(m.After[len(output):]); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := checkpoint("migration-write-synced"); err != nil {
		return err
	}
	if currentExists {
		if err := fs.Rename(root, spec, original); err != nil {
			return err
		}
	}
	if err := checkpoint("migration-original-saved"); err != nil {
		return err
	}
	if err := fs.Rename(root, rewrite, spec); err != nil {
		return err
	}
	if err := checkpoint("migration-write-published"); err != nil {
		return err
	}
	if _, err := fs.RemoveRegularFileIfExists(root, original); err != nil {
		return err
	}
	return checkpoint("migration-original-removed")
}
