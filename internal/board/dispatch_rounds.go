package board

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/dualface/kander/internal/fs"
)

// dispatchRounds returns every dispatch of a card, oldest first. A schema 2
// record names its predecessor. A schema 1 record does not, so its place comes
// from the card revisions of its receipts: they are unique, because only one
// round owns a card at a time. Epoch cannot order rounds, since a release resets
// it. An order the facts do not fix is an error, never a guess.
func dispatchRounds(tx *Transaction, s Snapshot) ([]Dispatch, error) {
	entries, err := fs.ListDirectory(tx.root, filepath.Join(s.Entry.Path, "dispatches"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	task := s.Entry.TaskID
	var legacy, linked []Dispatch
	for _, entry := range entries {
		d, err := readDispatch(tx, task, entry.Name)
		if err != nil {
			return nil, err
		}
		if d.Schema == 1 {
			legacy = append(legacy, d)
		} else {
			linked = append(linked, d)
		}
	}
	ordered, err := orderLegacyDispatches(task, legacy)
	if err != nil {
		return nil, err
	}
	tail := ""
	if len(ordered) > 0 {
		tail = ordered[len(ordered)-1].Input.ID
	}
	for len(linked) > 0 {
		at := slices.IndexFunc(linked, func(d Dispatch) bool { return d.PreviousID == tail })
		if at < 0 {
			return nil, coordinatorError("dispatch chain broken: " + task)
		}
		ordered = append(ordered, linked[at])
		tail = linked[at].Input.ID
		linked = slices.Delete(linked, at, at+1)
	}
	return ordered, nil
}

// orderLegacyDispatches sorts records by their first receipt. Only a cancelled
// round without receipts can lack one, and only the newest round can be that.
func orderLegacyDispatches(task string, records []Dispatch) ([]Dispatch, error) {
	first := func(d Dispatch) uint64 {
		for _, r := range []*DispatchReceipt{d.Accepted, d.Completed} {
			if r != nil {
				return r.CardRevision
			}
		}
		if d.Release != nil {
			return d.Release.CardRevision
		}
		return 0
	}
	var receipted, silent []Dispatch
	for _, d := range records {
		if first(d) == 0 {
			silent = append(silent, d)
		} else {
			receipted = append(receipted, d)
		}
	}
	if len(silent) > 1 {
		return nil, coordinatorError("legacy dispatch order unknown: " + task)
	}
	slices.SortFunc(receipted, func(a, b Dispatch) int { return cmp.Compare(first(a), first(b)) })
	for i := 1; i < len(receipted); i++ {
		if first(receipted[i-1]) == first(receipted[i]) {
			return nil, coordinatorError("legacy dispatch order unknown: " + task)
		}
	}
	return append(receipted, silent...), nil
}
