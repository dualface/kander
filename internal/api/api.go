// Package api is the /api/v1 wire contract shared by kander-server and the
// kander client: resource types, request bodies, stable error codes, and the
// protocol version handshake. It holds no behavior beyond validation helpers,
// so both sides depend on it one way and never on each other.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/dualface/kander/internal/board"
)

// ProtocolVersion is the wire protocol this build speaks. MinProtocolVersion is
// the oldest peer protocol it still accepts. A peer outside the range is
// refused with CodeProtocolIncompatible before any domain operation runs.
const (
	ProtocolVersion    = 1
	MinProtocolVersion = 1
)

// Header names. The claim token is a bearer secret for one execution; it is
// never logged and never echoed back in a response other than the claim that
// created it.
const (
	HeaderProtocol       = "Kander-Protocol"
	HeaderIdempotencyKey = "Idempotency-Key"
	HeaderClaimToken     = "Kander-Claim-Token"
	HeaderClaimGen       = "Kander-Claim-Generation"
	HeaderClaims         = "Kander-Claims"
	HeaderPublication    = "Kander-Publication"
	HeaderClientID       = "Kander-Client-Id"
	HeaderOperationID    = "Kander-Operation-Id"
)

// Stable error codes. Clients map them to localized text; the server message
// is a diagnostic only.
const (
	CodeInvalidRequest       = "invalid_request"
	CodeNotFound             = "not_found"
	CodeRevisionConflict     = "revision_conflict"
	CodeHeldByOther          = "held_by_other"
	CodeLeaseInvalid         = "lease_invalid"
	CodeLeaseActive          = "lease_active"
	CodeClaimRequired        = "claim_required"
	CodeEvidenceIncomplete   = "evidence_incomplete"
	CodeProtocolIncompatible = "protocol_incompatible"
	CodeIdempotencyMismatch  = "idempotency_mismatch"
	CodeProjectMismatch      = "project_mismatch"
	CodeProjectNotEmpty      = "project_not_empty"
	CodeBoardRejected        = "board_rejected"
	CodeNotReady             = "not_ready"
	CodeInternal             = "internal"
)

// Error is the JSON error body of every non-2xx response. BoardCode carries
// the board package message key when the board rejected the operation, so a
// client can render the same localized text as the v1 local command.
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	BoardCode string `json:"board_code,omitempty"`
	Status    int    `json:"-"`
}

func (e *Error) Error() string {
	if e.BoardCode != "" {
		return fmt.Sprintf("%s (%s): %s", e.Code, e.BoardCode, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// IsCode reports whether err is an *Error with the given code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// VersionInfo is GET /api/v1/version and the body of the protocol handshake.
type VersionInfo struct {
	Protocol      int    `json:"protocol"`
	MinProtocol   int    `json:"min_protocol"`
	ServerVersion string `json:"server_version"`
}

// Health is GET /api/v1/health (liveness) and GET /api/v1/ready (readiness).
type Health struct {
	Status string `json:"status"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

// Project is one registered project. ID is server-assigned and stable; Name is
// the value of the client's project.name. Remote is the Git remote URL the
// first registration declared; a later registration with another remote is
// refused, so two different repositories cannot share one board by accident.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Remote    string    `json:"remote,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// RegisterProject is POST /api/v1/projects. Registering an existing name with
// the same remote returns the existing project.
type RegisterProject struct {
	Name   string `json:"name"`
	Remote string `json:"remote,omitempty"`
}

// ClaimView is the public part of a claim; the token never appears here.
type ClaimView struct {
	Observation *AgentObservation `json:"observation,omitempty"`
	ClientID    string            `json:"client_id"`
	ExecutionID string            `json:"execution_id"`
	Owner       string            `json:"owner"`
	Generation  uint64            `json:"generation"`
	Status      string            `json:"status"`
	ExpiresAt   time.Time         `json:"expires_at"`
	RenewedAt   time.Time         `json:"renewed_at"`
}

// Claim status values. A claim is active while its lease is unexpired, lost
// once the lease expired (only an explicit takeover moves on), and released
// when the card left working/review through done, archived, or trash.
const (
	ClaimActive   = "active"
	ClaimLost     = "lost"
	ClaimReleased = "released"
)

// Task is one card as the server publishes it.
type Task struct {
	ObservedAt  time.Time  `json:"observed_at"`
	TaskID      string     `json:"task_id"`
	State       string     `json:"state"`
	Revision    uint64     `json:"revision"`
	OperationID string     `json:"operation_id,omitempty"`
	Text        string     `json:"text"`
	Claim       *ClaimView `json:"claim,omitempty"`
}

// TaskSummary is one list row. Its fields follow the v1 board summary.
type TaskSummary struct {
	Revision    uint64     `json:"revision"`
	TaskID      string     `json:"task_id"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	Kind        string     `json:"kind"`
	Type        string     `json:"type"`
	TaskGroup   string     `json:"task_group"`
	Assignee    string     `json:"assignee"`
	CreatedAt   string     `json:"created_at"`
	StartedAt   string     `json:"started_at"`
	CompletedAt string     `json:"completed_at"`
	Time        string     `json:"time"`
	Result      string     `json:"result"`
	Claim       *ClaimView `json:"claim,omitempty"`
	Session     string     `json:"session,omitempty"`
	Window      string     `json:"window,omitempty"`
}

// TaskList is GET /api/v1/projects/{project}/tasks.
type TaskList struct {
	ObservedAt time.Time     `json:"observed_at"`
	Tasks      []TaskSummary `json:"tasks"`
	// Cursor is the event cursor the list is consistent with; a subscriber
	// continues from it without missing a change.
	Cursor   string   `json:"cursor"`
	Warnings []string `json:"warnings,omitempty"`
}

// NewTask is POST /api/v1/projects/{project}/tasks.
type NewTask struct {
	Type     string `json:"type"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Language string `json:"language"`
	Large    bool   `json:"large,omitzero"`
}

// Document is GET and PUT /api/v1/projects/{project}/tasks/{task}/documents/{path}.
// On PUT, SHA256 is the hex digest of Text; the server publishes the document
// only after the received bytes match it, so an interrupted upload never
// becomes a visible partial document.
type Document struct {
	Authorization    board.ExecutionAuthorization `json:"authorization,omitzero"`
	Path             string                       `json:"path"`
	Text             string                       `json:"text"`
	SHA256           string                       `json:"sha256"`
	ExpectedRevision uint64                       `json:"expected_revision"`
	ContractDecision string                       `json:"contract_decision,omitempty"`
}

// Move is POST /api/v1/projects/{project}/tasks/{task}/move. A move into
// working is refused here; it is a claim.
type Move struct {
	Authorization    board.ExecutionAuthorization `json:"authorization,omitzero"`
	DeliveryCommit   string                       `json:"delivery_commit,omitempty"`
	Disposition      *board.ArtifactReference     `json:"disposition,omitempty"`
	State            string                       `json:"state"`
	Result           string                       `json:"result,omitempty"`
	Reason           string                       `json:"reason,omitempty"`
	Decision         string                       `json:"decision,omitempty"`
	DuplicateOf      string                       `json:"duplicate_of,omitempty"`
	ExpectedRevision *uint64                      `json:"expected_revision,omitempty"`
}

// ClaimRequest is POST .../tasks/{task}/claim: an atomic todo -> working
// claim. Takeover is POST .../tasks/{task}/takeover with the generation the
// caller observed as lost, and a decision reference for the handoff record.
type ClaimRequest struct {
	Dispatch           *board.DispatchTakeover `json:"dispatch,omitempty"`
	ClientID           string                  `json:"client_id"`
	ExecutionID        string                  `json:"execution_id"`
	Owner              string                  `json:"owner"`
	Decision           string                  `json:"decision,omitempty"`
	ExpectedGeneration uint64                  `json:"expected_generation,omitzero"`
	// Start makes the claim a launcher start: the card records the session,
	// window, and resolved pins in the same transaction, and the execution
	// stays unconfirmed until POST .../execution confirms or rolls it back.
	Start *StartInput `json:"start,omitempty"`
}

// StartInput is the launcher metadata of a start claim.
type StartInput struct {
	Session      string `json:"session,omitempty"`
	Window       string `json:"window,omitempty"`
	ExecResolved string `json:"exec_resolved,omitempty"`
}

// Execution actions of POST .../tasks/{task}/execution, all by the holder.
const (
	ExecutionWindow   = "window"
	ExecutionSession  = "session"
	ExecutionConfirm  = "confirm"
	ExecutionRollback = "rollback"
)

// ExecutionUpdate records launcher facts of the current execution. window
// writes WINDOW; session replaces SessionBefore with SessionAfter; confirm
// records start success (with a session replacement when both are set);
// rollback returns a failed start to todo and releases the claim.
type ExecutionUpdate struct {
	Action        string `json:"action"`
	Window        string `json:"window,omitempty"`
	SessionBefore string `json:"session_before,omitempty"`
	SessionAfter  string `json:"session_after,omitempty"`
	ExecResolved  string `json:"exec_resolved,omitempty"`
}

// ClaimGrant answers a claim or takeover. Token is returned exactly once.
type ClaimGrant struct {
	Claim         ClaimView `json:"claim"`
	Token         string    `json:"token"`
	Task          Task      `json:"task"`
	RenewInterval Duration  `json:"renew_interval"`
	TTL           Duration  `json:"ttl"`
}

// Renewal is POST .../tasks/{task}/renew. The claim headers carry the token
// and generation. A renewal never changes the card revision.
type Renewal struct {
	Claim ClaimView `json:"claim"`
}

// Handoff is one takeover record kept with the claim history.
type Handoff struct {
	At             time.Time `json:"at"`
	FromGeneration uint64    `json:"from_generation"`
	ToGeneration   uint64    `json:"to_generation"`
	FromClient     string    `json:"from_client"`
	ToClient       string    `json:"to_client"`
	Owner          string    `json:"owner"`
	Decision       string    `json:"decision"`
}

// Collaboration request kinds and states.
const (
	RequestFix    = "fix"
	RequestSync   = "sync"
	RequestReview = "review"
	RequestStop   = "stop"

	RequestOpen       = "open"
	RequestAccepted   = "accepted"
	RequestDone       = "done"
	RequestRejected   = "rejected"
	RequestSuperseded = "superseded"
)

// CollabRequest is a request from a non-holder to the holder of a claimed
// task. It targets one generation; a takeover supersedes the open requests of
// the old generation instead of handing them to the new execution.
type CollabRequest struct {
	Dispatch         *board.DispatchInput `json:"dispatch,omitempty"`
	CardRevision     uint64               `json:"card_revision,omitzero"`
	Revision         uint64               `json:"revision"`
	ID               string               `json:"id"`
	TaskID           string               `json:"task_id"`
	Kind             string               `json:"kind"`
	Message          string               `json:"message"`
	FromClient       string               `json:"from_client"`
	TargetGeneration uint64               `json:"target_generation"`
	Status           string               `json:"status"`
	Note             string               `json:"note,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

// NewCollabRequest is POST .../tasks/{task}/requests.
type NewCollabRequest struct {
	Dispatch         *board.DispatchInput `json:"dispatch,omitempty"`
	CardRevision     uint64               `json:"card_revision,omitzero"`
	Kind             string               `json:"kind"`
	Message          string               `json:"message"`
	FromClient       string               `json:"from_client"`
	TargetGeneration uint64               `json:"target_generation"`
}

// CollabAnswer is POST .../tasks/{task}/requests/{id}/answer by the holder.
type CollabAnswer struct {
	ExpectedRevision uint64 `json:"expected_revision,omitzero"`
	Status           string `json:"status"`
	Note             string `json:"note,omitempty"`
}

// Event kinds.
const (
	EventGroupChanged = "group_changed"
	EventTaskChanged  = "task_changed"
	EventClaimChanged = "claim_changed"
	EventRequest      = "request_changed"
)

// Event is one change notification. Events carry identities, not bodies; a
// subscriber reads the current resource.
type Event struct {
	GroupID    string    `json:"group_id,omitempty"`
	Seq        uint64    `json:"seq"`
	Kind       string    `json:"kind"`
	TaskID     string    `json:"task_id,omitempty"`
	State      string    `json:"state,omitempty"`
	Revision   uint64    `json:"revision,omitzero"`
	Generation uint64    `json:"generation,omitzero"`
	RequestID  string    `json:"request_id,omitempty"`
	At         time.Time `json:"at"`
}

// EventPage is GET .../events?cursor=C&wait=S. Reset means the cursor is no
// longer valid (unknown epoch or pruned); the client re-reads the full task
// list and continues from its cursor.
type EventPage struct {
	Cursor string  `json:"cursor"`
	Reset  bool    `json:"reset,omitzero"`
	Events []Event `json:"events"`
}

// Duration marshals as integer milliseconds.
type Duration int64

func (d Duration) Std() time.Duration { return time.Duration(d) * time.Millisecond }

func DurationOf(d time.Duration) Duration { return Duration(d / time.Millisecond) }

var (
	projectNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	identityRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// ValidProjectName reports whether name can name a project. Names are
// identifiers, never paths: the server maps them to internal IDs.
func ValidProjectName(name string) bool { return projectNameRe.MatchString(name) }

// ValidIdentity validates client IDs, execution IDs, request IDs, and
// idempotency keys.
func ValidIdentity(value string) bool { return identityRe.MatchString(value) }

// IssueImport publishes one issue snapshot. Contract sections are generated by
// the server; remote body and comments remain untrusted source attachments.
type IssueImport struct {
	Snapshot json.RawMessage `json:"snapshot"`
	Type     string          `json:"type,omitempty"`
	Language string          `json:"language"`
	Large    bool            `json:"large,omitzero"`
}

// ClaimProof is one execution's private authority over one task. A multi-task
// operation supplies a separate proof for each task; it never reuses one token
// as authority for another task.
type ClaimProof struct {
	Token      string `json:"token"`
	Generation uint64 `json:"generation"`
}
