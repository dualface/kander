package board

import (
	"errors"
	"maps"
	"slices"
)

// CapturedCard is an immutable read model. Location strings are resource
// identities for diagnostics, never client filesystem paths or write cursors.
type CapturedCard struct {
	TaskID        string           `json:"task_id"`
	State         string           `json:"state"`
	Kind          string           `json:"kind"`
	Text          string           `json:"text"`
	Revision      uint64           `json:"revision"`
	Dispatch      *DispatchSummary `json:"dispatch,omitempty"`
	DocumentError string           `json:"document_error,omitempty"`
	DispatchError string           `json:"dispatch_error,omitempty"`
}
type CapturedView struct {
	Location string            `json:"location"`
	Cards    []CapturedCard    `json:"cards"`
	Blocked  map[string]string `json:"blocked"`
	Problems []Problem         `json:"problems"`
}

func CaptureView(b Board, location string) (CapturedView, error) {
	out := CapturedView{Location: location, Cards: []CapturedCard{}, Blocked: maps.Clone(b.Blocked)}
	for _, problem := range b.Problems {
		// Server filesystem paths are not useful client identities.
		out.Problems = append(out.Problems, Problem{Path: location, Message: problem.Message})
	}
	for _, id := range slices.Sorted(maps.Keys(b.Entries)) {
		entry := b.Entries[id]
		card := CapturedCard{TaskID: id, State: entry.State, Kind: entry.Kind}
		if err := b.documentErrors[id]; err != nil {
			card.DocumentError = err.Error()
		} else {
			card.Text = b.documents[id]
		}
		card.Revision = b.revisions[id]
		if err := b.dispatchErrors[id]; err != nil {
			card.DispatchError = err.Error()
		} else {
			card.Dispatch = copyDispatchSummary(b.dispatches[id])
		}
		out.Cards = append(out.Cards, card)
	}
	return out, nil
}
func copyDispatchSummary(value *DispatchSummary) *DispatchSummary {
	if value == nil {
		return nil
	}
	result := *value
	result.Accepted = copyDispatchReceipt(value.Accepted)
	result.Completed = copyDispatchReceipt(value.Completed)
	return &result
}

// RestoreView builds an in-memory read snapshot only. It performs no I/O and
// supplies no Version cursor, so it cannot authorize a local board mutation.
func RestoreView(view CapturedView) (Board, error) {
	b := Board{Entries: map[string]Entry{}, Blocked: maps.Clone(view.Blocked), Problems: slices.Clone(view.Problems), documents: map[string]string{}, revisions: map[string]uint64{}, documentErrors: map[string]error{}, dispatches: map[string]*DispatchSummary{}, dispatchErrors: map[string]error{}}
	for _, card := range view.Cards {
		id, err := NormalizeTaskID(card.TaskID)
		if err != nil {
			return Board{}, err
		}
		if _, exists := b.Entries[id]; exists || !slices.Contains(States, card.State) {
			return Board{}, kanbanError("board.transaction_invalid", "invalid captured card identity")
		}
		path := view.Location + "/tasks/" + id
		b.Entries[id] = Entry{TaskID: id, State: card.State, Kind: card.Kind, Path: path, Document: path + "/spec.md"}
		b.documents[id], b.revisions[id], b.dispatches[id] = card.Text, card.Revision, copyDispatchSummary(card.Dispatch)
		if card.DocumentError != "" {
			b.documentErrors[id] = errors.New(card.DocumentError)
		}
		if card.DispatchError != "" {
			b.dispatchErrors[id] = errors.New(card.DispatchError)
		}
	}
	return b, nil
}
