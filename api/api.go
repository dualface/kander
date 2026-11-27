// Package api is the /api/v1 wire contract shared by the server and the
// kander client: resource types, request bodies, stable error codes, and the
// protocol version handshake. It holds no behavior beyond validation helpers,
// so both sides depend on it one way and never on each other.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/board"
)

// ProtocolVersion is the wire protocol this build speaks. MinProtocolVersion is
// the oldest peer protocol it still accepts. A peer outside the range is
// refused with CodeProtocolIncompatible before any domain operation runs.
// Protocol 3 moves the executing agent to "owner"; "assignee" now names the
// person assigned to the task. Older peers would misread the same key.
// Protocol 4 adds the policy revision to claims, group claims, and requests
// that create review batches, which the server checks against the project
// policy. A protocol 3 client sends no revision and would pass that check, so
// it is refused at the handshake.
const (
	ProtocolVersion    = 4
	MinProtocolVersion = 4
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
	CodeInvalidRequest = "invalid_request"
	// CodeForbidden means the embedding host denied the request.
	CodeForbidden = "forbidden"
	// CodeAttributionRequired means the embedding host did not identify a caller for an attributed operation.
	CodeAttributionRequired  = "attribution_required"
	CodeNotFound             = "not_found"
	CodeRevisionConflict     = "revision_conflict"
	CodeHeldByOther          = "held_by_other"
	CodeLeaseInvalid         = "lease_invalid"
	CodeLeaseActive          = "lease_active"
	CodeTakenOver            = "taken_over"
	CodeClaimRequired        = "claim_required"
	CodeEvidenceIncomplete   = "evidence_incomplete"
	CodeProtocolIncompatible = "protocol_incompatible"
	CodeIdempotencyMismatch  = "idempotency_mismatch"
	CodeProjectMismatch      = "project_mismatch"
	CodeProjectNotEmpty      = "project_not_empty"
	CodeBoardRejected        = "board_rejected"
	CodeNotReady             = "not_ready"
	CodeIndexRebuilding      = "index_rebuilding"
	CodeDecisionReserved     = "decision_reserved"
	CodeConfirmationRequired = "confirmation_required"
	// CodeReviewCompacted means a settled review run was compacted, so the
	// evidence file or the replay the request needs was removed on purpose.
	CodeReviewCompacted = "review_compacted"
	// CodeGCRunning means the server already runs a gc; one runs at a time.
	CodeGCRunning = "gc_running"
	// CodeImportCanceled means a board import stopped before publication:
	// the source lock wait expired, the caller went away, or the server is
	// shutting down. Nothing was published, and the same import can run again.
	CodeImportCanceled = "import_canceled"
	CodeInternal       = "internal"
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

// Project is one registered project. ID is ProjectID(Name), so one name has
// the same ID on every server; Name is the value of the client's project.name.
// Remote is the Git remote URL the first registration declared; a later
// registration with another remote is refused, so two different repositories
// cannot share one board by accident. BoardPath is the absolute board
// directory on the server host; it appears only in responses and is never
// stored in the server registry. Policy is the project policy; a server that
// keeps project policies sets it in every response (revision 0 when the
// project has none), and it is never stored in the registry either. A client
// reads a missing Policy as a server without project policies.
type Project struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Remote    string         `json:"remote,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	BoardPath string         `json:"board_path,omitempty"`
	Policy    *ProjectPolicy `json:"policy,omitempty"`
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
	// BranchSnapshot is the branch model of the card: its own snapshot, or
	// for a task group member the snapshot of its group. Absent when neither
	// has one, a card or group claimed before snapshots existed among them.
	BranchSnapshot *BranchSnapshot `json:"branch_snapshot,omitempty"`
}

// Claim status values. A claim is active while its lease is unexpired and
// lapsed once the lease expired: its holder keeps its authority and may renew,
// and another execution may take it over with a decision. A claim is lost once
// its holder released it or its agent was observed stopped, and released when
// the card left working/review through done, archived, or trash. Lapsed is a
// view value; a stored record is never lapsed.
const (
	ClaimActive   = "active"
	ClaimLapsed   = "lapsed"
	ClaimLost     = "lost"
	ClaimReleased = "released"
)

// ClaimStands reports whether a claim view status still gives its generation
// an execution: active, or lapsed (expired but not taken over). A client
// treats both alike wherever it asks whether an execution still holds a task.
func ClaimStands(status string) bool { return status == ClaimActive || status == ClaimLapsed }

// Task is one card as the server publishes it.
// MoveReceipt is the exact producer result saved with the move transaction.
type MoveReceipt struct {
	Dispatch *board.Dispatch `json:"dispatch,omitempty"`
	Replayed bool            `json:"replayed"`
}

type Task struct {
	MoveReceipt *MoveReceipt      `json:"move_receipt,omitempty"`
	Delivery    *DeliveryEvidence `json:"delivery,omitempty"`
	ObservedAt  time.Time         `json:"observed_at"`
	TaskID      string            `json:"task_id"`
	State       string            `json:"state"`
	Revision    uint64            `json:"revision"`
	OperationID string            `json:"operation_id,omitempty"`
	Text        string            `json:"text"`
	Claim       *ClaimView        `json:"claim,omitempty"`
}

// TaskSummary is one list row. Its fields follow the v1 board summary.
type TaskSummary struct {
	Revision     uint64       `json:"revision"`
	TaskID       string       `json:"task_id"`
	Title        string       `json:"title"`
	State        string       `json:"state"`
	Kind         string       `json:"kind"`
	Type         string       `json:"type"`
	TaskGroup    string       `json:"task_group"`
	Owner        string       `json:"owner"`
	Submitter    string       `json:"submitter"`
	Request      bool         `json:"request"`
	Assignee     string       `json:"assignee"`
	DecisionMode DecisionMode `json:"decision_mode"`
	CreatedAt    string       `json:"created_at"`
	StartedAt    string       `json:"started_at"`
	CompletedAt  string       `json:"completed_at"`
	Time         string       `json:"time"`
	Result       string       `json:"result"`
	Claim        *ClaimView   `json:"claim,omitempty"`
	Session      string       `json:"session,omitempty"`
	Window       string       `json:"window,omitempty"`
}

// TaskList is GET /api/v1/projects/{project}/tasks.
type TaskList struct {
	ObservedAt time.Time     `json:"observed_at"`
	Tasks      []TaskSummary `json:"tasks"`
	// Cursor is the event cursor the list is consistent with; a subscriber
	// continues from it without missing a change.
	Cursor     string   `json:"cursor"`
	Warnings   []string `json:"warnings,omitempty"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// TaskListOptions selects tasks for List. Zero values return the entire board.
// Cursor continues a query with the same filters and requires a positive Limit.
type TaskListOptions struct {
	State, Assignee, Submitter, Group, Search, Cursor string
	DecisionMode                                      DecisionMode
	Limit                                             int
}

// MaxTaskListLimit bounds one paginated list response.
const MaxTaskListLimit = 1000

// TaskAccess is the current task identity, workflow state and revision that a
// host authorization policy can read from its embedded server.
type TaskAccess struct {
	Submitter    string       `json:"submitter"`
	Request      bool         `json:"request"`
	Assignee     string       `json:"assignee"`
	DecisionMode DecisionMode `json:"decision_mode"`
	State        string       `json:"state"`
	Revision     uint64       `json:"revision"`
}

// NewTask is POST /api/v1/projects/{project}/tasks.
type NewTask struct {
	Type      string `json:"type"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Language  string `json:"language"`
	Large     bool   `json:"large,omitzero"`
	Submitter string `json:"submitter,omitempty"`
	// Description is the submitter's text. A non-empty value creates a protected request.md record.
	Description string `json:"description,omitempty"`
}

// Assign is POST /api/v1/projects/{project}/tasks/{task}/assignee.
// An empty Assignee clears the assignment.
type Assign struct {
	Assignee         string  `json:"assignee"`
	ExpectedRevision *uint64 `json:"expected_revision,omitempty"`
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

// Confirm is POST /api/v1/projects/{project}/tasks/{task}/confirm. It admits a
// request card from backlog to todo at the revision the submitter read.
type Confirm struct {
	ExpectedRevision uint64 `json:"expected_revision"`
	// ConfirmedBy is reported by the client and replaced by an Attribution hook when set.
	ConfirmedBy string `json:"confirmed_by,omitempty"`
}

// ClaimRequest is POST .../tasks/{task}/claim: an atomic todo -> working
// claim. Takeover is POST .../tasks/{task}/takeover with the generation the
// caller observed as lost, and a decision reference for the handoff record.
type ClaimRequest struct {
	ExpectedRevision   uint64                  `json:"expected_revision,omitzero"`
	ObservationTicket  string                  `json:"observation_ticket,omitempty"`
	PreviousPresence   *AgentObservation       `json:"previous_presence,omitempty"`
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
	// PolicyRevision is the project policy revision the client works with,
	// and BranchMode the git.branch_mode in effect for it. A claim that
	// creates the branch snapshot of the card is refused with
	// CodePolicyRevisionStale when the revision is not current; BranchMode
	// becomes the snapshot while the project has no policy. Neither is part
	// of the idempotency digest of the claim.
	PolicyRevision uint64 `json:"policy_revision,omitzero"`
	BranchMode     string `json:"branch_mode,omitempty"`
}

// StartInput is the launcher metadata of a start claim.
type StartInput struct {
	Session      string `json:"session,omitempty"`
	Window       string `json:"window,omitempty"`
	ExecResolved string `json:"exec_resolved,omitempty"`
}

// Execution actions of POST .../tasks/{task}/execution, all by the holder.
const (
	ExecutionWindow    = "window"
	ExecutionSession   = "session"
	ExecutionConfirm   = "confirm"
	ExecutionRollback  = "rollback"
	ExecutionUnresolve = "unresolve"
	ExecutionAdopt     = "adopt"
	ExecutionWorkspace = "workspace"
	ExecutionRelease   = "release"
)

// ExecutionUpdate records launcher facts of the current execution. window
// writes WINDOW; session replaces SessionBefore with SessionAfter; confirm
// records start success (with a session replacement when both are set);
// rollback returns a failed start to todo and releases the claim.
type ExecutionState struct {
	ObservationTicket string                  `json:"observation_ticket"`
	Task              Task                    `json:"task"`
	Record            board.ExecutionRecord   `json:"record"`
	Standing          board.ExecutionStanding `json:"standing"`
}

type ExecutionUpdate struct {
	Branch            string            `json:"branch,omitempty"`
	ObservationTicket string            `json:"observation_ticket,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	Decision          string            `json:"decision,omitempty"`
	ExpectedRevision  uint64            `json:"expected_revision,omitzero"`
	Presence          *AgentObservation `json:"presence,omitempty"`
	Action            string            `json:"action"`
	Window            string            `json:"window,omitempty"`
	SessionBefore     string            `json:"session_before,omitempty"`
	SessionAfter      string            `json:"session_after,omitempty"`
	ExecResolved      string            `json:"exec_resolved,omitempty"`
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

// Collaboration request kinds and states. Clients created by notify, by a
// resume that turns into a notify, and by a non-holder dispatch prepare give a
// request the kind of its dispatch (fix, sync or wrap-up), or note without one.
// The server also keeps the other combinations older clients and kander request
// produce (see checkRequestShape in internal/server).
const (
	RequestFix    = "fix"
	RequestSync   = "sync"
	RequestWrapUp = "wrap-up"
	RequestReview = "review"
	RequestStop   = "stop"
	RequestNote   = "note"

	// MaxRequestMessage bounds the message of one request in bytes; the server
	// refuses a longer one and clients check it before they send.
	MaxRequestMessage = 64 << 10

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
	AcceptWithin     Duration             `json:"accept_within_ms,omitzero"`
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

// DispatchRequestKind is the request kind that a client gives a new dispatch
// proposal: the same word as the dispatch kind. Any dispatch kind other than
// fix, sync and wrap-up is refused, so a proposal never shows a kind such as
// stop for a dispatch that cannot be prepared. It says nothing about requests
// without a dispatch: notify makes those notes, and kander request also makes
// fix, sync, review and stop requests without one.
func DispatchRequestKind(dispatchKind string) (string, error) {
	switch dispatchKind {
	case RequestFix, RequestSync, RequestWrapUp:
		return dispatchKind, nil
	}
	return "", &Error{Code: CodeInvalidRequest, Message: fmt.Sprintf("dispatch kind %q is not fix, sync or wrap-up", dispatchKind)}
}

// NewCollabRequest is POST .../tasks/{task}/requests.
type NewCollabRequest struct {
	AcceptWithin     Duration             `json:"accept_within_ms,omitzero"`
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
	projectNameRe = regexp.MustCompile(ProjectNamePattern)
	identityRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// ProjectNamePattern is the project name rule: an ASCII letter, then up to
// 127 ASCII letters, digits, underscores, or hyphens. internal/config keeps a
// copy because it cannot import this package; a test pins the two together.
const ProjectNamePattern = `^[A-Za-z][A-Za-z0-9_-]{0,127}$`

// ProjectNameRule describes ProjectNamePattern in refusal messages.
const ProjectNameRule = "a project name starts with an ASCII letter and contains only ASCII letters, digits, '_' and '-', at most 128 characters"

// ValidProjectName reports whether name can name a project. Names are
// identifiers, never paths: the server derives the ID and directory from them.
func ValidProjectName(name string) bool { return projectNameRe.MatchString(name) }

// projectDirPrefixLimit bounds the readable part of a project directory name.
const projectDirPrefixLimit = 48

// projectHash is the first 16 hex digits of the SHA-256 of the exact name.
func projectHash(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:8])
}

// ProjectID derives the stable ID of a valid project name.
func ProjectID(name string) string { return "p-" + projectHash(name) }

// ProjectDirName derives the directory name of a valid project name: the
// lowercase name, cut to projectDirPrefixLimit bytes, then the name hash. The
// hash of the exact name keeps names that differ only in case apart on
// case-insensitive file systems.
func ProjectDirName(name string) string {
	prefix := strings.ToLower(name)
	return prefix[:min(len(prefix), projectDirPrefixLimit)] + "-" + projectHash(name)
}

// ValidIdentity validates client IDs, execution IDs, request IDs, and
// idempotency keys.
func ValidIdentity(value string) bool { return identityRe.MatchString(value) }

// IssueImport publishes one issue snapshot. Contract sections are generated by
// the server; remote body and comments remain untrusted source attachments.
type IssueImport struct {
	Snapshot  json.RawMessage `json:"snapshot"`
	Type      string          `json:"type,omitempty"`
	Language  string          `json:"language"`
	Large     bool            `json:"large,omitzero"`
	Submitter string          `json:"submitter,omitempty"`
}

// ClaimProof is one execution's private authority over one task. A multi-task
// operation supplies a separate proof for each task; it never reuses one token
// as authority for another task.
type ClaimProof struct {
	Token      string `json:"token"`
	Generation uint64 `json:"generation"`
}
