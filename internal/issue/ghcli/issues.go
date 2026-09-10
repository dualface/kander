package ghcli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/internal/issue"
)

// The REST schema is pinned: GitHub keeps a version available for at least 24
// months after a successor ships, so the decoded fields cannot shift silently.
const (
	restAPIVersion = "2026-03-10"
	restAccept     = "application/vnd.github+json"
)

// restUser is the shared actor shape of users and assignees.
type restUser struct {
	Login string `json:"login"`
}

type restLabel struct {
	Name string `json:"name"`
}

// restIssue tolerates unknown fields and keeps pull_request raw so a null value
// is distinguishable from an absent one.
type restIssue struct {
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	State       string          `json:"state"`
	HTMLURL     string          `json:"html_url"`
	Body        *string         `json:"body"`
	User        *restUser       `json:"user"`
	Labels      []restLabel     `json:"labels"`
	Assignees   []restUser      `json:"assignees"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
}

type restSearchResult struct {
	TotalCount     int         `json:"total_count"`
	Items          []restIssue `json:"items"`
	IncompleteData bool        `json:"incomplete_results"`
}

// apiArgs builds one direct-argv `gh api` GET request. Parameters go through
// `-f`, which gh sends as query string values for GET, so nothing is hand
// encoded and no shell is involved.
func apiArgs(host string, endpoint string, params [][2]string) []string {
	args := []string{
		"api",
		"--hostname", host,
		"-H", "Accept: " + restAccept,
		"-H", "X-GitHub-Api-Version: " + restAPIVersion,
		"--method", "GET",
	}
	for _, param := range params {
		args = append(args, "-f", param[0]+"="+param[1])
	}
	return append(args, endpoint)
}

// runAPI performs one bounded, UTF-8 validated API call and maps a failure to a
// structured category. The resolved working directory is irrelevant to `gh api`
// because the endpoint is fully qualified.
func (p *Provider) runAPI(ctx context.Context, repository issue.Repository, endpoint string, params [][2]string) ([]byte, error) {
	stdout, stderr, err := p.gh.Run(ctx, ".", apiArgs(repository.Host, endpoint, params), DefaultStdoutLimit)
	if err != nil {
		if kind := issue.KindOf(err); kind == "" || kind == issue.ErrorCommandFailed {
			return nil, classifyFailure(stderr, err, repository.Host, repository.Owner+"/"+repository.Name)
		}
		return nil, err
	}
	if !utf8.Valid(stdout) {
		return nil, issue.NewError(issue.ErrorInvalidResponse, "decode", "response is not valid UTF-8")
	}
	return stdout, nil
}

// ListIssues returns one bounded page set. Pull requests are filtered out even
// though the REST endpoint mixes them in; search uses the search endpoint so
// free-text matches are resolved by GitHub rather than by the client.
func (p *Provider) ListIssues(ctx context.Context, repository issue.Repository, query issue.IssueQuery) (issue.IssuePage, error) {
	normalized, err := query.Normalize()
	if err != nil {
		return issue.IssuePage{}, err
	}
	if err := repository.Validate(); err != nil {
		return issue.IssuePage{}, err
	}
	page := issue.IssuePage{Repository: repository, Limit: normalized.Limit, Issues: []issue.IssueSummary{}}
	searching := normalized.Search != ""
	endpoint := "repos/" + repository.Owner + "/" + repository.Name + "/issues"
	if searching {
		endpoint = "search/issues"
	}
	// perPage is fixed for the whole pagination; see IssueQuery.PageSize.
	perPage := normalized.PageSize()
	for pageNumber := 1; ; pageNumber++ {
		params := [][2]string{
			{"per_page", strconv.Itoa(perPage)},
			{"page", strconv.Itoa(pageNumber)},
		}
		if searching {
			params = append(params, [2]string{"q", searchQuery(repository, normalized)})
		} else {
			params = append(params, [2]string{"state", normalized.State})
			if len(normalized.Labels) > 0 {
				params = append(params, [2]string{"labels", strings.Join(normalized.Labels, ",")})
			}
		}
		raw, err := p.runAPI(ctx, repository, endpoint, params)
		if err != nil {
			return issue.IssuePage{}, err
		}
		items, err := decodeIssueList(raw, searching)
		if err != nil {
			return issue.IssuePage{}, err
		}
		for _, item := range items {
			if isPullRequest(item.PullRequest) {
				continue
			}
			summary, err := summaryFromREST(repository, item)
			if err != nil {
				return issue.IssuePage{}, err
			}
			page.Issues = append(page.Issues, summary)
		}
		if len(page.Issues) > normalized.Limit {
			page.Issues = page.Issues[:normalized.Limit]
			page.More = true
			break
		}
		if len(items) < perPage {
			break
		}
		if pageNumber >= issue.MaxIssuePages {
			return issue.IssuePage{}, &issue.Error{Kind: issue.ErrorLimitExceeded, Op: "pagination", Detail: strconv.Itoa(issue.MaxIssuePages)}
		}
	}
	return page, nil
}

// GetIssue returns one issue and, when requested, up to the comment bound. More
// comments than the bound fail explicitly instead of silently truncating.
func (p *Provider) GetIssue(ctx context.Context, repository issue.Repository, number int, withComments bool) (issue.IssueSnapshot, error) {
	if err := repository.Validate(); err != nil {
		return issue.IssueSnapshot{}, err
	}
	if number <= 0 {
		return issue.IssueSnapshot{}, issue.NewError(issue.ErrorInvalidQuery, "number", strconv.Itoa(number))
	}
	endpoint := "repos/" + repository.Owner + "/" + repository.Name + "/issues/" + strconv.Itoa(number)
	raw, err := p.runAPI(ctx, repository, endpoint, nil)
	if err != nil {
		return issue.IssueSnapshot{}, err
	}
	var item restIssue
	if err := json.Unmarshal(raw, &item); err != nil {
		return issue.IssueSnapshot{}, issue.WrapError(err, issue.ErrorInvalidResponse, "decode", issue.Sanitize(err.Error()))
	}
	if isPullRequest(item.PullRequest) {
		return issue.IssueSnapshot{}, &issue.Error{Kind: issue.ErrorNotAnIssue, Op: "show", Detail: strconv.Itoa(number)}
	}
	snapshot, err := snapshotFromREST(repository, item)
	if err != nil {
		return issue.IssueSnapshot{}, err
	}
	if !withComments {
		return snapshot, nil
	}
	commentsEndpoint := endpoint + "/comments"
	rawComments, err := p.runAPI(ctx, repository, commentsEndpoint, [][2]string{
		{"per_page", strconv.Itoa(issue.MaxIssueComments + 1)},
		{"page", "1"},
	})
	if err != nil {
		return issue.IssueSnapshot{}, err
	}
	var comments []restComment
	if err := json.Unmarshal(rawComments, &comments); err != nil {
		return issue.IssueSnapshot{}, issue.WrapError(err, issue.ErrorInvalidResponse, "decode", issue.Sanitize(err.Error()))
	}
	if len(comments) > issue.MaxIssueComments {
		return issue.IssueSnapshot{}, &issue.Error{Kind: issue.ErrorLimitExceeded, Op: "comments", Detail: strconv.Itoa(issue.MaxIssueComments)}
	}
	snapshot.CommentsLoaded = true
	snapshot.Comments = []issue.IssueComment{}
	for _, comment := range comments {
		normalized, err := commentFromREST(repository, number, comment)
		if err != nil {
			return issue.IssueSnapshot{}, err
		}
		snapshot.Comments = append(snapshot.Comments, normalized)
	}
	return snapshot, nil
}

type restComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	User      *restUser `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

func decodeIssueList(raw []byte, searching bool) ([]restIssue, error) {
	if searching {
		var result restSearchResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, issue.WrapError(err, issue.ErrorInvalidResponse, "decode", issue.Sanitize(err.Error()))
		}
		return result.Items, nil
	}
	var items []restIssue
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, issue.WrapError(err, issue.ErrorInvalidResponse, "decode", issue.Sanitize(err.Error()))
	}
	return items, nil
}

// searchQuery composes the GitHub search expression. Labels are quoted because
// search syntax treats whitespace as a separator.
func searchQuery(repository issue.Repository, query issue.IssueQuery) string {
	parts := []string{"repo:" + repository.Owner + "/" + repository.Name, "is:issue"}
	if query.State != issue.IssueStateAll {
		parts = append(parts, "state:"+query.State)
	}
	for _, label := range query.Labels {
		parts = append(parts, `label:"`+label+`"`)
	}
	if query.Search != "" {
		parts = append(parts, query.Search)
	}
	return strings.Join(parts, " ")
}

func isPullRequest(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null"
}

// canonicalIssueURL rebuilds a link from the validated identity instead of
// trusting a provider field, so a printed URL can never point at another
// repository. An empty result means the identity could not produce one.
func canonicalIssueURL(repository issue.Repository, number int) string {
	url, err := repository.IssueURL(number)
	if err != nil {
		return ""
	}
	return url
}

func summaryFromREST(repository issue.Repository, item restIssue) (issue.IssueSummary, error) {
	labels := make([]string, 0, len(item.Labels))
	for _, label := range item.Labels {
		labels = append(labels, label.Name)
	}
	summary, err := issue.NormalizeSummary(issue.IssueSummary{
		Number:    item.Number,
		Title:     item.Title,
		State:     item.State,
		URL:       canonicalIssueURL(repository, item.Number),
		Labels:    labels,
		UpdatedAt: item.UpdatedAt,
	})
	if err != nil {
		return issue.IssueSummary{}, err
	}
	return summary, nil
}

func snapshotFromREST(repository issue.Repository, item restIssue) (issue.IssueSnapshot, error) {
	body := ""
	if item.Body != nil {
		body = *item.Body
	}
	author := ""
	if item.User != nil {
		author = item.User.Login
	}
	labels := make([]string, 0, len(item.Labels))
	for _, label := range item.Labels {
		labels = append(labels, label.Name)
	}
	assignees := make([]string, 0, len(item.Assignees))
	for _, assignee := range item.Assignees {
		assignees = append(assignees, assignee.Login)
	}
	snapshot, err := issue.NormalizeSnapshot(issue.IssueSnapshot{
		Repository: repository,
		Number:     item.Number,
		Title:      item.Title,
		Body:       body,
		State:      item.State,
		Author:     author,
		URL:        canonicalIssueURL(repository, item.Number),
		Labels:     labels,
		Assignees:  assignees,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
		FetchedAt:  time.Now().UTC(),
	})
	if err != nil {
		return issue.IssueSnapshot{}, err
	}
	return snapshot, nil
}

func commentFromREST(repository issue.Repository, issueNumber int, comment restComment) (issue.IssueComment, error) {
	author := ""
	if comment.User != nil {
		author = comment.User.Login
	}
	// GitHub anchors one comment as #issuecomment-<id> on the issue URL; the
	// provider's own html_url is ignored like every other provider URL.
	url := canonicalIssueURL(repository, issueNumber)
	if url != "" && comment.ID > 0 {
		url += "#issuecomment-" + strconv.FormatInt(comment.ID, 10)
	}
	normalized, err := issue.NormalizeComment(issue.IssueComment{
		Author:    author,
		Body:      comment.Body,
		CreatedAt: comment.CreatedAt,
		URL:       url,
	})
	if err != nil {
		return issue.IssueComment{}, err
	}
	return normalized, nil
}
