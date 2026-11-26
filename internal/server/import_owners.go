package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/issue"
)

// importRecordOwners resolves v1 producer identities without rewriting the
// shared records. A path key is authoritative for per-task producers. The
// multi-card producers name their owners in their primary metadata.
func importRecordOwners(root string, source treeScan, records map[board.ImportRecordKey][]string, groups map[string][]string, refs map[board.ImportRecordKey][]string) (map[board.ImportRecordKey][]string, error) {
	out := make(map[board.ImportRecordKey][]string, len(records))
	for key, files := range records {
		if key.Kind == "operation" {
			continue
		}
		ids, err := importOwners(root, source, key, files, groups, refs[key])
		if err != nil {
			return nil, err
		}
		out[key] = ids
	}
	for key, files := range records {
		if key.Kind != "operation" {
			continue
		}
		ids, err := importOperationOwners(root, source, key, files[0], groups, out)
		if err != nil {
			return nil, err
		}
		out[key] = ids
	}
	return out, nil
}

func importOwners(root string, source treeScan, key board.ImportRecordKey, files []string, groups map[string][]string, refs []string) ([]string, error) {
	invalid := func() error { return invalidImportOwner(key) }
	switch key.Kind {
	case "version", "execution", "review-task", "prune":
		return []string{key.ID}, nil
	case "group":
		ids := slices.Clone(groups[key.ID])
		for _, path := range files {
			var facts struct {
				TaskID  string                     `json:"task_id"`
				Members map[string]json.RawMessage `json:"members"`
			}
			if err := readImportOwner(root, path, source, &facts, invalid); err != nil {
				return nil, err
			}
			ids = append(ids, slices.Collect(maps.Keys(facts.Members))...)
			if facts.TaskID != "" {
				ids = append(ids, facts.TaskID)
			}
		}
		return ids, nil
	case "issue-result":
		var store issue.ResultStore
		if err := readImportOwner(root, files[0], source, &store, invalid); err != nil {
			return nil, err
		}
		if store.TaskID == "" {
			return nil, invalid()
		}
		ids := []string{store.TaskID}
		for _, item := range store.Records {
			if item != nil {
				ids = append(ids, item.TaskIDs...)
			}
		}
		return ids, nil
	case "dispatch", "review-run", "review-batch", "review-plan":
		var metadata string
		for _, file := range files {
			if importPrimaryRecord(key.Kind, file) {
				metadata = file
				break
			}
		}
		if metadata == "" {
			if len(refs) != 0 {
				return refs, nil
			}
			return nil, invalid()
		}
		var facts struct {
			TaskID  string   `json:"task_id"`
			TaskIDs []string `json:"task_ids"`
		}
		data, err := readScannedImportFile(root, metadata, source)
		if err != nil {
			return nil, err
		}
		if json.Unmarshal(data, &facts) == nil {
			if key.Kind == "dispatch" && facts.TaskID != "" {
				return append(slices.Clone(refs), facts.TaskID), nil
			}
			if len(facts.TaskIDs) > 0 {
				return append(slices.Clone(refs), facts.TaskIDs...), nil
			}
		}
		if len(refs) != 0 {
			// The staged check handles damaged evidence that still has a
			// proven owner by the card's local copies.
			return refs, nil
		}
	}
	return nil, invalid()
}

func importOperationOwners(root string, source treeScan, key board.ImportRecordKey, file string, groups map[string][]string, owners map[board.ImportRecordKey][]string) ([]string, error) {
	var operation board.OperationRecord
	invalid := func() error { return invalidImportOwner(key) }
	if err := readImportOwner(root, file, source, &operation, invalid); err != nil {
		return nil, err
	}
	if operation.Phase != "committed" {
		return nil, invalid()
	}
	ids := slices.Collect(maps.Keys(operation.Revisions))
	for _, group := range operation.Groups {
		ids = append(ids, groups[group]...)
	}
	for _, changed := range operation.Files {
		// Historic operations may have been committed on a different host.
		path := strings.ReplaceAll(changed.Path, "\\", "/")
		record := board.ImportRecord(path)
		ids = append(ids, owners[record]...)
	}
	return ids, nil
}

func invalidImportOwner(key board.ImportRecordKey) error {
	return importRejected(fmt.Sprintf("cannot determine ownership of v1 board record %s %q; nothing was published", key.Kind, key.ID))
}

func importPrimaryRecord(kind, path string) bool {
	parts := strings.Split(path, "/")
	switch kind {
	case "dispatch":
		return true
	case "review-run":
		return len(parts) == 6 && parts[5] == "run.json"
	case "review-batch":
		return parts[3] == "batches"
	case "review-plan":
		return parts[3] == "plans"
	default:
		return false
	}
}

func readImportOwner(root, rel string, source treeScan, out any, invalid func() error) error {
	data, err := readScannedImportFile(root, rel, source)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return invalid()
	}
	return nil
}
