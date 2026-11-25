package api

import "github.com/dualface/kander/internal/board"

// DispatchCommand names one controlled producer. Ownership lease generation
// and business dispatch epoch are independent fences.
type DispatchCommand struct {
	Input        *board.DispatchInput      `json:"input,omitempty"`
	DispatchID   string                    `json:"dispatch_id,omitempty"`
	Revision     uint64                    `json:"revision,omitzero"`
	CardRevision uint64                    `json:"card_revision,omitzero"`
	State        board.DispatchState       `json:"state,omitempty"`
	Reason       string                    `json:"reason,omitempty"`
	Decision     string                    `json:"decision,omitempty"`
	Author       string                    `json:"author,omitempty"`
	Exit         *board.WrapUpExitEvidence `json:"exit,omitempty"`
}
