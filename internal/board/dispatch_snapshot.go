package board

import (
	"context"
	"time"
)

// DispatchSummary exposes durable receipts without publishing the message body.
// Revision belongs to the dispatch; receipt revisions belong to the card.
type DispatchSummary struct {
	ID        string           `json:"dispatch_id"`
	TaskID    string           `json:"task_id"`
	Kind      string           `json:"kind"`
	State     DispatchState    `json:"state"`
	Revision  uint64           `json:"revision"`
	Epoch     uint64           `json:"epoch"`
	CreatedAt time.Time        `json:"created_at"`
	ConfirmBy time.Time        `json:"confirm_by"`
	Accepted  *DispatchReceipt `json:"accepted,omitempty"`
	Completed *DispatchReceipt `json:"completed,omitempty"`
}

// ScanDispatchesContext adds current dispatch facts to the coordinated scan.
// Nil values scans all cards for group expansion; an empty slice scans none.
// Individual dispatch failures remain attached to their card, so a subscriber
// can expand groups before requiring facts only for its monitored identities.
func ScanDispatchesContext(ctx context.Context, root string, values []string) (Board, error) {
	var ids []string
	if values != nil {
		ids = make([]string, len(values))
		for i, value := range values {
			id, err := NormalizeTaskID(value)
			if err != nil {
				return Board{}, err
			}
			ids[i] = id
		}
	}
	return scanContext(ctx, root, ids, true)
}

func snapshotDispatch(tx *Transaction, id, text string, revision uint64) (*DispatchSummary, error) {
	authorization := authFrom(text)
	if authorization == (ExecutionAuthorization{}) {
		return nil, nil
	}
	d, err := readDispatch(tx, id, authorization.DispatchID)
	if err != nil {
		return nil, err
	}
	if d.Authorization != authorization {
		return nil, dispatchError(authorization.DispatchID)
	}
	for _, receipt := range []*DispatchReceipt{d.Accepted, d.Completed} {
		if receipt != nil && (receipt.CardRevision == 0 || receipt.CardRevision > revision) {
			return nil, dispatchError(authorization.DispatchID)
		}
	}
	return &DispatchSummary{ID: d.Input.ID, TaskID: id, Kind: d.Input.Kind,
		State: d.State, Revision: d.Revision, Epoch: d.Authorization.Epoch,
		CreatedAt: d.Input.CreatedAt, ConfirmBy: dispatchAcceptBefore(d),
		Accepted: d.Accepted, Completed: d.Completed}, nil
}

// CurrentDispatch returns the current grant captured with Document and Revision.
// Nil means legacy/unbound, never an invented historical receipt. A later grant
// supersedes this summary; older IDs remain readable through ReadDispatch.
func (b Board) CurrentDispatch(id string) (*DispatchSummary, error) {
	entry, err := Locate(b, id)
	if err != nil {
		return nil, err
	}
	if _, err = b.Document(entry.TaskID); err != nil {
		return nil, err
	}
	if b.dispatches == nil {
		return nil, kanbanError("board.transaction_invalid", "dispatch facts not captured")
	}
	if err = b.dispatchErrors[entry.TaskID]; err != nil {
		return nil, err
	}
	original := b.dispatches[entry.TaskID]
	if original == nil {
		return nil, nil
	}
	result := *original
	result.Accepted = copyDispatchReceipt(original.Accepted)
	result.Completed = copyDispatchReceipt(original.Completed)
	return &result, nil
}

func copyDispatchReceipt(original *DispatchReceipt) *DispatchReceipt {
	if original == nil {
		return nil
	}
	result := *original
	if original.Disposition != nil {
		reference := *original.Disposition
		result.Disposition = &reference
	}
	return &result
}
