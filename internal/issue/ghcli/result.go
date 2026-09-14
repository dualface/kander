package ghcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/internal/issue"
)

type resultRESTIssue struct {
	restIssue
	ID       int64 `json:"id"`
	Comments *int  `json:"comments"`
}

type resultRESTComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

func resultEndpoint(repository issue.Repository, number int) (string, error) {
	if err := repository.Validate(); err != nil {
		return "", err
	}
	if number <= 0 {
		return "", issue.NewError(issue.ErrorInvalidQuery, "number", strconv.Itoa(number))
	}
	return "repos/" + repository.Owner + "/" + repository.Name + "/issues/" + strconv.Itoa(number), nil
}

func resultResponseError(detail string) error {
	return issue.NewError(issue.ErrorInvalidResponse, "result", detail)
}

func (p *Provider) readResultIssue(ctx context.Context, repository issue.Repository, number int, endpoint string) (resultRESTIssue, error) {
	var value resultRESTIssue
	raw, err := p.runAPI(ctx, repository, endpoint, nil)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, resultResponseError("invalid issue JSON")
	}
	url, _ := repository.IssueURL(number)
	if value.ID <= 0 || value.Number != number || value.HTMLURL != url || value.Comments == nil || *value.Comments < 0 || value.UpdatedAt.IsZero() || isPullRequest(value.PullRequest) || (value.State != "open" && value.State != "closed") {
		return value, resultResponseError("issue identity/state/count mismatch")
	}
	return value, nil
}

// resultPages requires an explicit short final page and bounds both page count
// and total bytes. Reaching a bound fails; it never returns partial evidence.
func (p *Provider) resultPages(ctx context.Context, repository issue.Repository, endpoint string) ([]json.RawMessage, error) {
	all := []json.RawMessage{}
	total := 0
	for page := 1; page <= issue.MaxIssuePages; page++ {
		raw, err := p.runAPI(ctx, repository, endpoint, [][2]string{{"per_page", "100"}, {"page", strconv.Itoa(page)}})
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > 4<<20 {
			return nil, issue.NewError(issue.ErrorLimitExceeded, "result", "complete discussion exceeds 4 MiB")
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil || items == nil || len(items) > 100 {
			return nil, resultResponseError("invalid result page")
		}
		all = append(all, items...)
		if len(items) < 100 {
			return all, nil
		}
	}
	return nil, issue.NewError(issue.ErrorLimitExceeded, "result", "complete discussion exceeds pagination limit")
}

func (p *Provider) resultStateVersion(ctx context.Context, repository issue.Repository, endpoint string, issueID int64) (string, error) {
	items, err := p.resultPages(ctx, repository, endpoint+"/events")
	if err != nil {
		return "", err
	}
	var last int64
	seen := map[int64]bool{}
	for _, raw := range items {
		var event struct {
			ID    int64  `json:"id"`
			Event string `json:"event"`
		}
		if err := json.Unmarshal(raw, &event); err != nil || event.ID <= 0 || seen[event.ID] {
			return "", resultResponseError("invalid or duplicate issue event")
		}
		seen[event.ID] = true
		if (event.Event == "closed" || event.Event == "reopened") && event.ID > last {
			last = event.ID
		}
	}
	return fmt.Sprintf("%d:%d", issueID, last), nil
}

// ReadResult brackets complete comment/event pagination with issue metadata and
// event reads. Concurrent changes cause explicit rejection and a fresh inspect.
func (p *Provider) ReadResult(ctx context.Context, repository issue.Repository, number int) (out issue.ResultRemote, err error) {
	endpoint, err := resultEndpoint(repository, number)
	if err != nil {
		return out, err
	}
	before, err := p.readResultIssue(ctx, repository, number, endpoint)
	if err != nil {
		return out, err
	}
	version, err := p.resultStateVersion(ctx, repository, endpoint, before.ID)
	if err != nil {
		return out, err
	}
	raw, err := p.runAPI(ctx, repository, "user", nil)
	if err != nil {
		return out, err
	}
	var actor struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &actor); err != nil || actor.ID <= 0 {
		return out, resultResponseError("authenticated actor identity unavailable")
	}
	items, err := p.resultPages(ctx, repository, endpoint+"/comments")
	if err != nil {
		return out, err
	}
	comments := []issue.ResultComment{}
	seen := map[int64]bool{}
	for _, raw := range items {
		comment, err := decodeResultComment(raw)
		if err != nil {
			return out, err
		}
		if seen[comment.ID] {
			return out, resultResponseError("duplicate comment across pages")
		}
		seen[comment.ID] = true
		comments = append(comments, comment)
	}
	after, err := p.readResultIssue(ctx, repository, number, endpoint)
	if err != nil {
		return out, err
	}
	afterVersion, err := p.resultStateVersion(ctx, repository, endpoint, after.ID)
	if err != nil {
		return out, err
	}
	if before.ID != after.ID || !before.UpdatedAt.Equal(after.UpdatedAt) || before.State != after.State || *before.Comments != len(comments) || *after.Comments != len(comments) || version != afterVersion {
		return out, resultResponseError("issue changed or comments incomplete; inspect again")
	}
	key, _ := repository.IssueSourceKey(number)
	body := ""
	if before.Body != nil {
		body = *before.Body
	}
	encoded, _ := json.Marshal(comments)
	sum := sha256.Sum256(encoded)
	out = issue.ResultRemote{SourceKey: key, IssueID: before.ID, ActorID: actor.ID, State: before.State, StateVersion: version, Revision: before.UpdatedAt.String() + ":" + hex.EncodeToString(sum[:]), Title: issue.Sanitize(before.Title), Body: issue.Sanitize(body), Comments: comments}
	return out, nil
}

func decodeResultComment(raw []byte) (issue.ResultComment, error) {
	var comment resultRESTComment
	if err := json.Unmarshal(raw, &comment); err != nil || comment.ID <= 0 || comment.User.ID <= 0 || !utf8.ValidString(comment.Body) || len(comment.Body) > 65536 {
		return issue.ResultComment{}, resultResponseError("invalid comment identity/body")
	}
	// Retain exact body for nonce recovery; JSON output escapes control bytes.
	return issue.ResultComment{ID: comment.ID, AuthorID: comment.User.ID, Body: comment.Body}, nil
}

// Accept gh's HTTP rejection line and its known scope/SSO suggestions only.
// Extra unknown diagnostics cannot turn an ambiguous failure into retry consent.
var resultRejectionStatus = regexp.MustCompile(`^gh: (?:[^\r\n]*\(HTTP (?:400|401|403|404|410|413|422|429)\)|HTTP (?:400|401|403|404|410|413|422|429))[ \t]*$`)
var resultRejectionHint = regexp.MustCompile(`^(?:gh: This API operation needs the "[^"\r\n]+" scope\. To request it, run:[ \t]+gh auth refresh -h \S+ -s \S+|Authorize in your web browser:[ \t]+https?://\S+)$`)

func definiteResultRejection(stderr []byte) bool {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	if !resultRejectionStatus.MatchString(strings.TrimSpace(lines[0])) {
		return false
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line != "" && !resultRejectionHint.MatchString(line) {
			return false
		}
	}
	return true
}

func (p *Provider) resultWrite(ctx context.Context, repository issue.Repository, endpoint, method string, fields [][2]string) ([]byte, error) {
	args := apiArgs(repository.Host, endpoint, fields)
	for i := range args {
		if args[i] == "GET" {
			args[i] = method
			break
		}
	}
	raw, stderr, err := p.gh.Run(ctx, ".", args, DefaultStdoutLimit)
	if err != nil {
		failure := classifyFailure(stderr, err, repository.Host, repository.Owner+"/"+repository.Name)
		kind := issue.KindOf(err)
		if (kind == "" || kind == issue.ErrorCommandFailed) && definiteResultRejection(stderr) {
			return nil, &issue.ResultWriteRejection{Err: failure}
		}
		return nil, failure
	}
	if !utf8.Valid(raw) {
		return nil, resultResponseError("write response is not UTF-8")
	}
	return raw, nil
}

func (p *Provider) PostResult(ctx context.Context, repository issue.Repository, number int, body string) (issue.ResultComment, error) {
	endpoint, err := resultEndpoint(repository, number)
	if err != nil {
		return issue.ResultComment{}, err
	}
	if strings.TrimSpace(body) == "" || len(body) > 17000 {
		return issue.ResultComment{}, resultResponseError("invalid result body")
	}
	raw, err := p.resultWrite(ctx, repository, endpoint+"/comments", "POST", [][2]string{{"body", body}})
	if err != nil {
		return issue.ResultComment{}, err
	}
	return decodeResultComment(raw)
}

func (p *Provider) CloseResult(ctx context.Context, repository issue.Repository, number int) error {
	endpoint, err := resultEndpoint(repository, number)
	if err != nil {
		return err
	}
	raw, err := p.resultWrite(ctx, repository, endpoint, "PATCH", [][2]string{{"state", "closed"}, {"state_reason", "completed"}})
	if err != nil {
		return err
	}
	var value resultRESTIssue
	url, _ := repository.IssueURL(number)
	if err := json.Unmarshal(raw, &value); err != nil || value.ID <= 0 || value.Number != number || value.HTMLURL != url || value.State != "closed" || isPullRequest(value.PullRequest) {
		return resultResponseError("close response mismatch; outcome uncertain")
	}
	return nil
}
