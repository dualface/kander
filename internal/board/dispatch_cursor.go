package board

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// StagedDispatch reads the validated dispatch and the producer's staged state
// under the existing task lock, so the move receipt needs no post-unlock read.
func (tx *Transaction) StagedDispatch(task, id string) (Dispatch, error) {
	value, err := readDispatch(tx, task, id)
	if err != nil {
		return value, err
	}
	snapshot, err := tx.Snapshot(task)
	if err != nil {
		return value, err
	}
	path, err := documentPath(snapshot.Entry, dispatchPath(id, "state"))
	if err != nil {
		return value, err
	}
	relative, err := filepath.Rel(tx.root, path)
	if err != nil {
		return value, err
	}
	for _, file := range tx.record.Files {
		if strings.EqualFold(file.Path, relative) {
			err = json.Unmarshal([]byte(file.After), &value)
			break
		}
	}
	return value, err
}
