package issue

import (
	"errors"
	"os"

	"github.com/dualface/kander/internal/board"
)

// ReadResultWorkInTransaction repeats work-set identity, readiness and evidence
// checks under one board-exclusive publication scope. It acquires no locks.
func ReadResultWorkInTransaction(tx *board.Transaction, repository Repository, number int, cardID string) (ResultWork, error) {
	key, err := repository.IssueSourceKey(number)
	if err != nil {
		return ResultWork{}, err
	}
	reader := tx.ReadOnly()
	view, err := reader.CommittedView()
	if err != nil {
		return ResultWork{}, err
	}
	return readResultWorkView(view, key, cardID, func(id string, anchor bool) (ResultCard, error) {
		snapshot, err := reader.Snapshot(id)
		if err != nil {
			return ResultCard{}, err
		}
		return resultEvidence(snapshot, key, repository, number, id, anchor, func(name string) ([]byte, bool, error) {
			if !snapshot.Entry.IsDirectory() && name != "spec.md" {
				return nil, false, nil
			}
			text, err := reader.Read(id, name)
			if errors.Is(err, os.ErrNotExist) {
				return nil, false, nil
			}
			return []byte(text), err == nil, err
		})
	})
}
