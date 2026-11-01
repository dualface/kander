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
// round owns a card at a time. A round without a receipt takes its place from its
// creation time. Epoch cannot order rounds, since a release resets it. An order the
// facts do not fix is an error, never a guess.
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

// orderLegacyDispatches sorts records by their first receipt. A cancelled round
// without receipts can sit anywhere, so its creation time places it among the
// others. That time counts only when it is known, differs from both neighbors, and
// agrees with the order the receipts give.
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
	unknown := coordinatorError("legacy dispatch order unknown: " + task)
	var receipted, silent []Dispatch
	for _, d := range records {
		if first(d) != 0 {
			receipted = append(receipted, d)
		} else if d.Input.CreatedAt.IsZero() {
			return nil, unknown
		} else {
			silent = append(silent, d)
		}
	}
	slices.SortFunc(receipted, func(a, b Dispatch) int { return cmp.Compare(first(a), first(b)) })
	for i := 1; i < len(receipted); i++ {
		if first(receipted[i-1]) == first(receipted[i]) {
			return nil, unknown
		}
	}
	if len(silent) == 0 {
		return receipted, nil
	}
	created := func(a, b Dispatch) int { return a.Input.CreatedAt.Compare(b.Input.CreatedAt) }
	slices.SortFunc(silent, created)
	merged := make([]Dispatch, 0, len(records))
	for _, d := range silent {
		for len(receipted) > 0 && created(receipted[0], d) < 0 {
			merged, receipted = append(merged, receipted[0]), receipted[1:]
		}
		merged = append(merged, d)
	}
	merged = append(merged, receipted...)
	for i := 1; i < len(merged); i++ {
		a, b := merged[i-1], merged[i]
		if order := created(a, b); order > 0 || order == 0 && (first(a) == 0 || first(b) == 0) {
			return nil, unknown
		}
	}
	return merged, nil
}
