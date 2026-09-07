package board

import (
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/internal/fs"
)

// Windows paths compare case-insensitively, including URI spellings of states.
func migrationPathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func migrationPathMap(root string, record *OperationRecord) map[string]string {
	mapping := map[string]string{}
	if !record.LinkRelocation {
		return mapping
	}
	for _, migration := range record.Migrations {
		mapping[migrationPathKey(filepath.Join(root, migration.From))] = filepath.Join(root, migration.To, "spec.md")
	}
	return mapping
}

// Plan the complete dependency set before publishing anything. A single journal
// preserves the mapping even if a referenced card publishes before its reader.
func planMigration(root string, b Board) (OperationRecord, error) {
	id, err := operationID()
	if err != nil {
		return OperationRecord{}, err
	}
	r := OperationRecord{Schema: 1, Purpose: "migration", LinkRelocation: true, ID: id, Phase: "prepared", Revisions: map[string]uint64{}}
	entries := selectedEntries(b.Entries, "")
	for _, e := range entries {
		if !e.IsDirectory() {
			from, _ := filepath.Rel(root, e.Path)
			r.Migrations = append(r.Migrations, FormMigration{From: from, To: strings.TrimSuffix(from, ".md"), Rewrite: "spec.write-" + id, Original: "spec.original-" + id})
		}
	}
	mapping := migrationPathMap(root, &r)
	migrationIndex := 0
	for _, e := range entries {
		before, err := readDocument(e)
		if err != nil {
			return r, err
		}
		size, err := taskSize(e, before)
		if err != nil {
			return r, err
		}
		after := addSize(before, size)
		target := e.Document
		if !e.IsDirectory() {
			target = mapping[migrationPathKey(e.Document)]
		}
		after, err = relocateMarkdown(after, e.Document, target, mapping)
		if err != nil {
			return r, err
		}
		changed := false
		if !e.IsDirectory() {
			r.Migrations[migrationIndex].Before = before
			r.Migrations[migrationIndex].After = after
			migrationIndex++
			changed = true
		} else {
			if after != before {
				path, _ := filepath.Rel(root, e.Document)
				r.Files = append(r.Files, FileChange{Path: path, Before: &before, After: after})
				changed = true
			}
			if len(mapping) > 0 {
				documents, err := migrationMarkdownFiles(root, e.Path)
				if err != nil {
					return r, err
				}
				for _, document := range documents {
					if document == e.Document {
						continue
					}
					bytes, err := fs.ReadRegularFile(root, document)
					if err != nil {
						return r, err
					}
					if !utf8.Valid(bytes) {
						return r, kanbanError("board.task_document_is_not_valid_utf_8", document)
					}
					old := string(bytes)
					next, err := relocateMarkdown(old, document, document, mapping)
					if err != nil {
						return r, err
					}
					if old == next {
						continue
					}
					path, _ := filepath.Rel(root, document)
					r.Files = append(r.Files, FileChange{Path: path, Before: &old, After: next})
					changed = true
				}
			}
		}
		if changed {
			v, err := revision(root, e.TaskID)
			if err != nil {
				return r, err
			}
			if v == ^uint64(0) {
				return r, kanbanError("board.transaction_invalid", "revision overflow")
			}
			r.Revisions[e.TaskID] = v + 1
		}
	}
	return r, nil
}

func migrationMarkdownFiles(root, directory string) ([]string, error) {
	items, err := fs.ListDirectory(root, directory)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, item := range items {
		path := filepath.Join(directory, item.Name)
		switch item.Kind {
		case fs.KindDirectory:
			nested, err := migrationMarkdownFiles(root, path)
			if err != nil {
				return nil, err
			}
			files = append(files, nested...)
		case fs.KindFile:
			if strings.EqualFold(filepath.Ext(path), ".md") {
				files = append(files, path)
			}
		default:
			return nil, kanbanError("board.transaction_conflict", path)
		}
	}
	return files, nil
}

func validateMigrationFiles(root string, r *OperationRecord) error {
	mapping := migrationPathMap(root, r)
	seen := map[string]bool{}
	for _, m := range r.Migrations {
		if seen[m.To] {
			return kanbanError("board.transaction_invalid", m.To)
		}
		seen[m.To] = true
	}
	for _, f := range r.Files {
		if f.Before == nil || seen[f.Path] {
			return kanbanError("board.transaction_invalid", f.Path)
		}
		seen[f.Path] = true
		parts := strings.Split(filepath.ToSlash(f.Path), "/")
		if len(parts) < 3 || !taskIDRe.MatchString(parts[1]) || !strings.EqualFold(filepath.Ext(f.Path), ".md") {
			return kanbanError("board.transaction_invalid", f.Path)
		}
		for _, m := range r.Migrations {
			if parts[1] == filepath.Base(m.To) {
				return kanbanError("board.transaction_invalid", f.Path)
			}
		}
		before := *f.Before
		if len(parts) == 3 && parts[2] == "spec.md" {
			size, err := taskSize(Entry{Path: filepath.Join(root, parts[0], parts[1]), TaskID: parts[1]}, before)
			if err != nil {
				return err
			}
			before = addSize(before, size)
		}
		path := filepath.Join(root, f.Path)
		expected, err := relocateMarkdown(before, path, path, mapping)
		if err != nil {
			return err
		}
		if expected != f.After {
			return kanbanError("board.transaction_invalid", f.Path)
		}
	}
	return nil
}
