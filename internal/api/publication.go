package api

import (
	"encoding/json"
	"time"
)

// PublicationMember binds a joint operation to one exact task version and
// claim generation. Approval never follows a takeover to the next execution.
type PublicationMember struct {
	TaskID     string `json:"task_id"`
	Revision   uint64 `json:"revision"`
	Generation uint64 `json:"generation"`
}

// PublicationPrepare saves immutable material before any holder confirms it.
type PublicationPrepare struct {
	ID        string              `json:"id"`
	Operation string              `json:"operation"`
	Payload   json.RawMessage     `json:"payload"`
	Members   []PublicationMember `json:"members"`
}

// PublicationApproval is a holder's confirmation of the material and version.
type PublicationApproval struct {
	TaskID      string    `json:"task_id"`
	Digest      string    `json:"digest"`
	Revision    uint64    `json:"revision"`
	Generation  uint64    `json:"generation"`
	ClientID    string    `json:"client_id"`
	ExecutionID string    `json:"execution_id"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

// Publication is a recoverable joint-publication barrier. Result is written in
// the same redo record as the published domain evidence.
type Publication struct {
	PublicationPrepare
	Digest      string                         `json:"digest"`
	Status      string                         `json:"status"`
	Approvals   map[string]PublicationApproval `json:"approvals"`
	CreatedAt   time.Time                      `json:"created_at"`
	PublishedAt time.Time                      `json:"published_at,omitzero"`
	Result      json.RawMessage                `json:"result,omitempty"`
}
