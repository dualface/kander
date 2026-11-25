package api

import "github.com/dualface/kander/internal/board"

// GroupClaim binds the public checkpoint identity to a private coordinator
// lease. The checkpoint token itself remains a public session label.
type GroupClaim struct {
	Request            board.CoordinatorClaim `json:"request"`
	ExecutionID        string                 `json:"execution_id"`
	ExpectedGeneration uint64                 `json:"expected_generation"`
	Decision           string                 `json:"decision,omitempty"`
}
type GroupView struct {
	GroupID    string                       `json:"group_id"`
	Checkpoint *board.CoordinatorCheckpoint `json:"checkpoint,omitempty"`
	Claim      *ClaimView                   `json:"claim,omitempty"`
}
type GroupGrant struct {
	GroupID       string                      `json:"group_id"`
	Checkpoint    board.CoordinatorCheckpoint `json:"checkpoint"`
	Claim         ClaimView                   `json:"claim"`
	Token         string                      `json:"token,omitempty"`
	RenewInterval Duration                    `json:"renew_interval"`
	TTL           Duration                    `json:"ttl"`
}
type CoordinatorGitCheck struct {
	Facts board.CoordinatorGitFacts `json:"facts"`
	Error string                    `json:"error,omitempty"`
}
type GroupReconcile struct {
	Request board.CoordinatorReconcile `json:"request"`
	Checks  []CoordinatorGitCheck      `json:"checks"`
}

// RecoveryLease explicitly fences a lost member without launching an agent.
// Its audit generation is released in the same recovery transaction.
type RecoveryLease struct {
	ExecutionID        string `json:"execution_id"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}

type GroupRecover struct {
	Lost              *RecoveryLease           `json:"lost,omitempty"`
	ObservationTicket string                   `json:"observation_ticket,omitempty"`
	Request           board.CoordinatorRecover `json:"request"`
	ObservedRevision  uint64                   `json:"observed_revision"`
	Observation       AgentObservation         `json:"observation"`
}

type GroupRelease struct {
	GroupID  string `json:"group_id"`
	Reason   string `json:"reason"`
	Decision string `json:"decision"`
}

// CoordinatorSummary is the public group state captured with a subscription
// snapshot. Renewal timestamps do not represent a checkpoint change.
type CoordinatorSummary struct {
	GroupID     string     `json:"group_id"`
	Revision    uint64     `json:"revision"`
	Epoch       uint64     `json:"epoch"`
	ContentHash string     `json:"content_hash,omitempty"`
	Claim       *ClaimView `json:"claim,omitempty"`
}
