package issue

import (
	"context"
	"time"
)

// IssueSummary is the provider-neutral view of one issue in a list result.
// Remote text is already sanitized and bounded by the provider.
type IssueSummary struct {
	Number    int
	Title     string
	State     string
	URL       string
	Labels    []string
	UpdatedAt time.Time
}

// IssueComment is one comment of an issue snapshot.
type IssueComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
	URL       string
}

// IssueSnapshot is the provider-neutral view of one issue, optionally with its
// comments. Body is Markdown kept lossless apart from sanitizing and bounds.
type IssueSnapshot struct {
	Repository     Repository
	Number         int
	Title          string
	Body           string
	State          string
	Author         string
	URL            string
	Labels         []string
	Assignees      []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CommentsLoaded bool
	Comments       []IssueComment
	FetchedAt      time.Time
}

// IssuePage is one bounded list result. More reports that the provider stopped
// at Limit while further matches existed.
type IssuePage struct {
	Repository Repository
	Issues     []IssueSummary
	Limit      int
	More       bool
}

// IssueProvider is the read-only query contract of the issue integration.
// internal/issue/ghcli implements it on top of the GitHub CLI; the board, TUI
// and command layers never import that concrete provider.
type IssueProvider interface {
	RepositoryResolver
	ListIssues(ctx context.Context, repository Repository, query IssueQuery) (IssuePage, error)
	GetIssue(ctx context.Context, repository Repository, number int, withComments bool) (IssueSnapshot, error)
}
