package issue

import "context"

// ResultComment retains immutable numeric identity and exact text for recovery.
// These are untrusted evidence, never authorization.
type ResultComment struct {
	ID       int64  `json:"id"`
	AuthorID int64  `json:"author_id"`
	Body     string `json:"body"`
}

// ResultRemote is a complete, bounded observation. StateVersion includes the
// issue's identity and latest open/close event, so reopening invalidates consent.
type ResultRemote struct {
	SourceKey    string          `json:"source_key"`
	IssueID      int64           `json:"issue_id"`
	ActorID      int64           `json:"actor_id"`
	State        string          `json:"state"`
	StateVersion string          `json:"state_version"`
	Revision     string          `json:"revision"`
	Title        string          `json:"title"`
	Body         string          `json:"body"`
	Comments     []ResultComment `json:"comments"`
}

// ResultProvider is separate from the read-only UI/import provider. Reads must
// reject incomplete comments, events, mismatched identities and unstable reads.
// Writes are attempted once; an error never means that nothing was published.
type ResultProvider interface {
	ReadResult(context.Context, Repository, int) (ResultRemote, error)
	PostResult(context.Context, Repository, int, string) (ResultComment, error)
	CloseResult(context.Context, Repository, int) error
}

// ResultCard exposes current local evidence without modifying the completed card.
type ResultCard struct {
	TaskID          string   `json:"task_id"`
	Spec            string   `json:"spec"`
	Report          string   `json:"report"`
	Digest          string   `json:"digest"`
	EvidenceVersion string   `json:"evidence_version"`
	Commits         []string `json:"commits"`
}

type ResultInspection struct {
	Schema    int                      `json:"schema_version"`
	SourceKey string                   `json:"source_key"`
	Card      ResultCard               `json:"card"`
	Remote    ResultRemote             `json:"remote"`
	Token     string                   `json:"token"`
	Records   map[string]*ResultRecord `json:"records"`
}

// ResultCheck records a documented verification command and canonical outcome.
type ResultCheck struct {
	Command string `json:"command"`
	Status  string `json:"status"`
}

// ResultProposal contains the agent's semantic assessment. Criterion outcomes
// are ordered by the card's acceptance criteria, using met/unmet/unknown only.
// They and the recorded delivery SHAs form the language-independent result key.
type ResultProposal struct {
	Checks              []ResultCheck `json:"checks"`
	Token               string        `json:"token"`
	Outcomes            []string      `json:"outcomes"`
	Body                string        `json:"body"`
	EquivalentCommentID int64         `json:"equivalent_comment_id"`
	FullyResolved       bool          `json:"fully_resolved"`
	ResolutionEvidence  string        `json:"resolution_evidence"`
}

type ResultDecision struct {
	Token         string `json:"token"`
	Version       string `json:"version"`
	Decision      string `json:"decision"`
	UserReference string `json:"user_reference"`
	Reconsider    bool   `json:"reconsider"`
}

// ResultRecord is durable recovery evidence; never prune it as a cache.
type ResultRecord struct {
	EvidenceVersion    string              `json:"evidence_version"`
	Failures           []ResultFailure     `json:"failures,omitempty"`
	Commits            []string            `json:"commits"`
	Outcomes           []string            `json:"outcomes"`
	Checks             []ResultCheck       `json:"checks"`
	Version            string              `json:"version"`
	CardDigest         string              `json:"card_digest"`
	Status             string              `json:"status"`
	Body               string              `json:"body"`
	ActorID            int64               `json:"actor_id"`
	CommentID          int64               `json:"comment_id"`
	FullyResolved      bool                `json:"fully_resolved"`
	ResolutionEvidence string              `json:"resolution_evidence"`
	Decisions          []ResultCloseRecord `json:"decisions,omitempty"`
}

type ResultCloseRecord struct {
	StateVersion  string `json:"state_version"`
	Decision      string `json:"decision"`
	UserReference string `json:"user_reference"`
	Status        string `json:"status"`
}

// ResultWriteRejection means the provider received a definite no-effect response.
// Transport failures, deadlines and ambiguous responses must never use it.
type ResultWriteRejection struct{ Err error }

func (e *ResultWriteRejection) Error() string { return e.Err.Error() }
func (e *ResultWriteRejection) Unwrap() error { return e.Err }

type ResultFailure struct {
	Action string `json:"action"`
	Detail string `json:"detail"`
}
