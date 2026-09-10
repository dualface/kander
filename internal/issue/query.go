package issue

import (
	"strconv"
	"strings"
	"unicode"
)

// Issue query states and bounds. The limit and page size are fixed here so the
// command layer, the provider and the TUI share one bounded contract.
const (
	IssueStateOpen   = "open"
	IssueStateClosed = "closed"
	IssueStateAll    = "all"

	DefaultIssueLimit  = 30
	MaxIssueLimit      = 200
	MaxIssuePageSize   = 100
	maxIssueLabels     = 20
	maxIssueLabelRunes = 100
	maxIssueSearchRune = 256
)

// IssueQuery is one validated list request. Limit is the total number of
// issues the caller is willing to receive, never a page size.
type IssueQuery struct {
	State  string
	Labels []string
	Search string
	Limit  int
}

// Normalize validates user input and fills the defaults. Labels are trimmed
// and deduplicated; an empty label is rejected instead of silently ignored.
func (q IssueQuery) Normalize() (IssueQuery, error) {
	out := IssueQuery{
		State:  strings.ToLower(strings.TrimSpace(q.State)),
		Search: strings.TrimSpace(q.Search),
		Limit:  q.Limit,
	}
	switch out.State {
	case "":
		out.State = IssueStateOpen
	case IssueStateOpen, IssueStateClosed, IssueStateAll:
	default:
		return IssueQuery{}, &Error{Kind: ErrorInvalidQuery, Op: "state", Detail: Sanitize(q.State)}
	}
	if err := validateSearch(out.Search); err != nil {
		return IssueQuery{}, err
	}
	seen := map[string]struct{}{}
	for _, raw := range q.Labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			return IssueQuery{}, &Error{Kind: ErrorInvalidQuery, Op: "label", Detail: "empty label"}
		}
		if len([]rune(label)) > maxIssueLabelRunes || strings.ContainsFunc(label, unicode.IsControl) || strings.ContainsAny(label, "\"") {
			return IssueQuery{}, &Error{Kind: ErrorInvalidQuery, Op: "label", Detail: Sanitize(label)}
		}
		key := strings.ToLower(label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.Labels = append(out.Labels, label)
	}
	if len(out.Labels) > maxIssueLabels {
		return IssueQuery{}, &Error{Kind: ErrorInvalidQuery, Op: "labels", Detail: strconv.Itoa(len(out.Labels))}
	}
	switch {
	case out.Limit == 0:
		out.Limit = DefaultIssueLimit
	case out.Limit < 1 || out.Limit > MaxIssueLimit:
		return IssueQuery{}, &Error{Kind: ErrorInvalidQuery, Op: "limit", Detail: strconv.Itoa(out.Limit)}
	}
	return out, nil
}

func validateSearch(value string) error {
	if len([]rune(value)) > maxIssueSearchRune {
		return &Error{Kind: ErrorInvalidQuery, Op: "search", Detail: strconv.Itoa(len([]rune(value)))}
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return &Error{Kind: ErrorInvalidQuery, Op: "search", Detail: Sanitize(value)}
	}
	return nil
}

// PageSize returns the request size used for every page of one list call. It
// must not depend on how much was already collected: GitHub derives the offset
// of a page as (page-1)*per_page, so changing the size between pages would
// re-read items that were already seen and skip items that were never seen.
func (q IssueQuery) PageSize() int {
	remaining := q.Limit + 1
	if remaining < 1 {
		remaining = 1
	}
	if remaining > MaxIssuePageSize {
		remaining = MaxIssuePageSize
	}
	return remaining
}

// MaxIssuePages bounds provider pagination; a list that would need more pages
// fails with ErrorLimitExceeded instead of scanning without a bound.
const MaxIssuePages = 10
