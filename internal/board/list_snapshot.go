package board

import (
	"context"
)

// ListSnapshotSchemaVersion is the schema_version of `kander list --json`.
// See docs/board-snapshot.md for the contract.
const ListSnapshotSchemaVersion = 1

// ListSnapshot is the machine-readable board snapshot of `kander list --json`.
type ListSnapshot struct {
	SchemaVersion int        `json:"schema_version"`
	ChangeToken   string     `json:"change_token"`
	GeneratedAt   string     `json:"generated_at"`
	Root          string     `json:"root"`
	Tasks         []ListTask `json:"tasks"`
}

// ListTask is one card of a ListSnapshot. Every string field is always
// present; a missing value is the empty string.
type ListTask struct {
	TaskID     string     `json:"task_id"`
	Title      string     `json:"title"`
	State      string     `json:"state"`
	Size       string     `json:"size"`
	Type       string     `json:"type"`
	TaskGroup  string     `json:"task_group"`
	Owner      string     `json:"owner"`
	Session    string     `json:"session"`
	Window     string     `json:"window"`
	Launcher   string     `json:"launcher"`
	CreatedAt  string     `json:"created_at"`
	StartedAt  string     `json:"started_at"`
	FinishedAt string     `json:"finished_at"`
	Result     string     `json:"result"`
	Revision   uint64     `json:"revision"`
	Agent      *ListAgent `json:"agent"`
}

// ListAgent is the observed executing agent of one card.
type ListAgent struct {
	Status       string `json:"status"`
	RuntimeState string `json:"runtime_state"`
	Reason       string `json:"reason"`
	ObservedAt   string `json:"observed_at"`
	Valid        bool   `json:"valid"`
}

// AgentStatusRequest names one card whose executing agent a snapshot observes.
type AgentStatusRequest struct {
	TaskID  string
	Session string
	Window  string
}

// AgentStatusFunc observes the requested agents and returns one ListAgent per
// request, in request order. internal/liveness supplies it; board never probes
// terminals itself.
type AgentStatusFunc func(ctx context.Context, requests []AgentStatusRequest) []ListAgent
