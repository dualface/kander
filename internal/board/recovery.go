package board

import (
	"errors"
	"fmt"
	"github.com/dualface/kander/internal/fs"
	"os"
	"path/filepath"
	"strings"
)

func safeRecordPath(root, path string) (string, error) {
	if filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00") {
		return "", kanbanError("board.transaction_invalid", path)
	}
	rel := filepath.ToSlash(path)
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return "", kanbanError("board.transaction_invalid", path)
	}
	if _, ok := stateSet[parts[0]]; !ok {
		if len(parts) < 3 || parts[0] != ".kander" || parts[1] != "groups" || !taskGroupRe.MatchString(parts[2]) {
			return "", kanbanError("board.transaction_invalid", path)
		}
	}
	for _, p := range parts {
		if p == ".." || p == "." || p == "" || strings.ContainsAny(p, ":\\") {
			return "", kanbanError("board.transaction_invalid", path)
		}
	}
	return filepath.Join(root, path), nil
}
func applyRecord(root, path string, r *OperationRecord, warnings ...*WarningLog) error {
	return applyRecordWithCheckpoint(root, path, r, func(string) error { return nil }, warnings...)
}

// applyRecordWithCheckpoint exposes persisted boundaries for interruption tests;
// the normal publisher uses a no-op checkpoint and the same filesystem steps.
func applyRecordWithCheckpoint(root, path string, r *OperationRecord, checkpoint func(string) error, warnings ...*WarningLog) error {
	if err := checkpoint("prepared"); err != nil {
		return err
	}
	if err := validateRecord(root, r); err != nil {
		return err
	}
	for _, directory := range r.Directories {
		p, err := safeRecordPath(root, directory)
		if err != nil {
			return err
		}
		exists, err := fs.DirectoryExists(root, p)
		if err != nil {
			return err
		}

		if !exists {
			relocated := false
			for _, entry := range r.Entries {
				if entry.From != "" && entry.Kind == "large" && strings.HasPrefix(directory, entry.From+string(os.PathSeparator)) {
					target := filepath.Join(root, entry.To+strings.TrimPrefix(directory, entry.From))
					present, err := fs.DirectoryExists(root, target)
					if err != nil {
						return err
					}
					if present {
						relocated = true
						break
					}
				}
			}
			if relocated {
				continue
			}
		}
		if !exists {
			if err = fs.CreatePrivateDirectory(root, p); err != nil {
				return err
			}
		}
	}

	for _, f := range r.Files {
		p, err := safeRecordPath(root, f.Path)
		if err != nil {
			return err
		}
		b, exists, err := fs.ReadRegularFileIfExists(root, p)
		if err != nil {
			return err
		}
		if exists && string(b) == f.After {
			continue
		}
		// Files precede renames. A completed rename means the matching final file
		// must already contain the staged bytes; never recreate its source path.
		moved := false
		if !exists {
			for _, e := range r.Entries {
				if e.From != "" && (f.Path == e.From || strings.HasPrefix(f.Path, e.From+string(os.PathSeparator))) {
					q := filepath.Join(root, e.To+strings.TrimPrefix(f.Path, e.From))
					data, ok, er := fs.ReadRegularFileIfExists(root, q)
					if er != nil {
						return er
					}
					if ok && string(data) == f.After {
						moved = true
						break
					}
				}
			}
		}
		if moved {
			continue
		}
		if (f.Before == nil && exists) || (f.Before != nil && (!exists || string(b) != *f.Before)) {
			return kanbanError("board.transaction_conflict", f.Path)
		}
		if err = fs.WriteTextAtomic(root, p, f.After, f.Before != nil); err != nil {
			return err
		}
		if r.Purpose == "migration" {
			if err := checkpoint("migration-link-file"); err != nil {
				return err
			}
		}
	}
	if r.Purpose == "migration" {
		if err := checkpoint("migration-links"); err != nil {
			return err
		}
	}
	for _, e := range r.Entries {
		to, err := safeRecordPath(root, e.To)
		if err != nil {
			return err
		}

		if e.From == "" {
			if e.Kind == "large" {
				exists, err := fs.DirectoryExists(root, to)
				if err != nil {
					return err
				}
				if !exists {
					if err = fs.CreatePrivateDirectory(root, to); err != nil {
						return err
					}
				}
				to = filepath.Join(to, "spec.md")
			}
			data, exists, err := fs.ReadRegularFileIfExists(root, to)
			if err != nil {
				return err
			}
			if exists {
				if string(data) != e.Text {
					return kanbanError("board.transaction_conflict", e.To)
				}
			} else if err = fs.WriteTextAtomic(root, to, e.Text, false); err != nil {
				return err
			}
			continue
		}

		from, err := safeRecordPath(root, e.From)
		if err != nil {
			return err
		}
		var source, target bool
		if e.Kind == "large" {
			source, err = fs.DirectoryExists(root, from)
			if err == nil {
				target, err = fs.DirectoryExists(root, to)
			}
		} else {
			source, err = fs.RegularFileExists(root, from)
			if err == nil {
				target, err = fs.RegularFileExists(root, to)
			}
		}
		if err != nil {
			return err
		}
		if !source && target {
			document := to
			if e.Kind == "large" {
				document = filepath.Join(to, "spec.md")
			}
			data, err := fs.ReadRegularFile(root, document)
			if err != nil {
				return err
			}
			if string(data) != e.Text {
				return kanbanError("board.transaction_conflict", e.To)
			}
			continue
		}
		if !source || target {
			return kanbanError("board.transaction_conflict", e.From)
		}
		if err = fs.Rename(root, from, to); err != nil {
			return err
		}
	}
	for _, migration := range r.Migrations {
		if err := applyMigration(root, r, migration, checkpoint); err != nil {
			return err
		}
	}
	for id, v := range r.Revisions {
		if !taskIDRe.MatchString(id) || v == 0 {
			return kanbanError("board.transaction_invalid", id)
		}
		old, err := revision(root, id)
		if err != nil {
			return err
		}
		if old != v && old != v-1 {
			return kanbanError("board.transaction_conflict", id)
		}
		if old != v {
			prior, err := readVersion(root, id)
			if err != nil {
				return err
			}
			prior.Revision = v
			prior.OperationID = r.ID
			for _, entry := range r.Entries {
				if strings.TrimSuffix(filepath.Base(entry.To), ".md") == id && (strings.Split(filepath.ToSlash(entry.To), "/")[0] != "backlog" || entry.From != "") {
					prior.ContractFrozen = true
				}
			}
			if err = writeJSON(root, control(root, "versions", id+".json"), prior, true); err != nil {
				return err
			}
		}
	}
	if err := checkpoint("revision"); err != nil {
		return err
	}
	if err := commitOperation(root, path, r, checkpoint, warnings...); err != nil {
		return err
	}
	return checkpoint("committed")
}

// RecoverTransactions is the compatibility entrance for recovery without new
// migrations. Init uses MigrateCards; both share the same locked recovery core.
func RecoverTransactions(root string) (err error) {
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	_, err = recoverMigrationRecords(root, InitOptions{})
	return err
}

// The caller holds exclusive board access. Recovery precedes structural scan;
// unrelated board problems cannot prevent replay of a valid pending operation.
func recoverMigrationRecords(root string, options InitOptions) (int, error) {
	records, err := operationRecords(root)
	if err != nil {
		return 0, err
	}
	if err := validateMigrationStaging(root, records); err != nil {
		return 0, err
	}
	if err := requireMigrationWindow(root, records, options.Maintenance); err != nil {
		return 0, err
	}
	if err := partitionJournal(root, records, options); err != nil {
		return 0, err
	}
	count := 0
	for _, record := range records {
		if record.Phase != "prepared" {
			continue
		}
		if err := applyRecord(root, control(root, "operations", "pending", record.ID+".json"), &record); err != nil {
			return count, fmt.Errorf("%s: %w", record.ID, err)
		}
		if record.Purpose == "migration" || len(record.Migrations) > 0 {
			count += len(record.Revisions)
		}
	}
	pruneJournal(root)
	return count, nil
}

func validateRecord(root string, r *OperationRecord) error {
	if r.LinkRelocation && r.Purpose != "migration" || r.Purpose != "" && r.Purpose != "migration" {
		return kanbanError("board.transaction_invalid", r.Purpose)
	}
	if len(r.Migrations) > 0 {
		if r.Purpose != "migration" {
			return kanbanError("board.transaction_invalid", r.ID)
		}
	}

	if r.Purpose == "migration" {
		if len(r.Entries) > 0 || len(r.Directories) > 0 || len(r.Groups) > 0 {
			return kanbanError("board.transaction_invalid", r.ID)
		}
		if err := validateMigrationFiles(root, r); err != nil {
			return err
		}
	}

	if r.Schema != 1 || r.ID == "" || len(r.Revisions) == 0 && len(r.Groups) == 0 {
		return kanbanError("board.transaction_invalid", r.ID)
	}
	for _, migration := range r.Migrations {
		if err := validateMigration(root, r, migration); err != nil {
			return err
		}
	}
	for id, v := range r.Revisions {
		if !taskIDRe.MatchString(id) || v == 0 {
			return kanbanError("board.transaction_invalid", id)
		}
		current, err := readVersion(root, id)
		if err != nil {
			return err
		}
		if current.Revision != v-1 && (current.Revision != v || current.OperationID != r.ID) {
			return kanbanError("board.transaction_conflict", id)
		}
		// A true duplicate blocks recovery before any bytes are changed.
		matches := 0
		for _, state := range States {
			file := filepath.Join(root, state, id+".md")
			dir := filepath.Join(root, state, id)
			exists, err := fs.RegularFileExists(root, file)
			if err != nil {
				return err
			}
			if exists {
				matches++
			}
			exists, err = fs.DirectoryExists(root, dir)
			if err != nil {
				return err
			}
			if exists {
				matches++
			}
		}
		if matches > 1 {
			return kanbanError("board.transaction_conflict", id)
		}
	}
	check := func(path string) error {
		if _, err := safeRecordPath(root, path); err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(path), "/")
		if parts[0] == ".kander" {
			if !containsID(r.Groups, parts[2]) {
				return kanbanError("board.transaction_invalid", path)
			}
		} else {
			if _, ok := r.Revisions[strings.TrimSuffix(parts[1], ".md")]; !ok {
				return kanbanError("board.transaction_invalid", path)
			}
		}
		return nil
	}
	for _, path := range r.Directories {
		if err := check(path); err != nil {
			return err
		}
	}
	for _, f := range r.Files {
		if err := check(f.Path); err != nil {
			return err
		}
	}
	for _, e := range r.Entries {
		if e.Kind != "small" && e.Kind != "large" {
			return kanbanError("board.transaction_invalid", e.Kind)
		}
		if err := check(e.To); err != nil {
			return err
		}
		if e.From != "" {
			if err := check(e.From); err != nil {
				return err
			}
		}
	}
	return nil
}
