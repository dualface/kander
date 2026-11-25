package api

import "github.com/dualface/kander/internal/board"

type (
	DecisionMode          = board.DecisionMode
	DecisionKind          = board.DecisionKind
	DecisionOption        = board.DecisionOption
	DecisionAuthorization = board.DecisionAuthorization
	DecisionInput         = board.DecisionInput
	DecisionRecord        = board.DecisionRecord
)

const (
	DecisionModeLeader   = board.DecisionModeLeader
	DecisionModeAssignee = board.DecisionModeAssignee
	DecisionHuman        = board.DecisionHuman
	DecisionLeader       = board.DecisionLeader
)

// DecisionList is GET /api/v1/projects/{project}/tasks/{task}/decisions.
type DecisionList struct {
	Decisions []DecisionRecord `json:"decisions"`
}

// SetDecisionMode is POST /api/v1/projects/{project}/tasks/{task}/decision-mode.
type SetDecisionMode struct {
	Mode DecisionMode `json:"mode"`
}

// DecisionModeResult reports the revision published with the mode change.
type DecisionModeResult struct {
	TaskID   string       `json:"task_id"`
	Mode     DecisionMode `json:"mode"`
	Revision uint64       `json:"revision"`
}
