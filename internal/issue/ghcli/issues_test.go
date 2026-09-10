package ghcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/issue"
)

const listTimestamp = `"2026-09-11T02:03:04Z"`

func testRepository() issue.Repository {
	return issue.Repository{
		Host: "github.com", Owner: "dualface", Name: "kander",
		URL: "https://github.com/dualface/kander", Remote: "origin",
	}
}

func issueJSON(number int, title string, pullRequest bool) string {
	pull := ""
	if pullRequest {
		pull = `,"pull_request":{"url":"https://api.github.com/repos/dualface/kander/pulls/` + fmt.Sprint(number) + `"}`
	}
	return fmt.Sprintf(`{"number":%d,"title":%q,"state":"open","html_url":"https://github.com/dualface/kander/issues/%d","body":"body %d","user":{"login":"alice"},"labels":[{"name":"bug"}],"assignees":[{"login":"bob"}],"created_at":%s,"updated_at":%s%s}`,
		number, title, number, number, listTimestamp, listTimestamp, pull)
}

func TestListIssuesFiltersPullRequests(t *testing.T) {
	repository := testRepository()
	query := issue.IssueQuery{State: issue.IssueStateOpen, Limit: 30}
	key := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues", [][2]string{
		{"per_page", "31"}, {"page", "1"}, {"state", "open"},
	}), " ")
	stdout := "[" + issueJSON(1, "first", false) + "," + issueJSON(2, "a pull request", true) + "," + issueJSON(3, "second", false) + "]"
	provider, ghRunner, _ := newTestProvider(t, map[string]fakeResponse{key: {stdout: stdout}}, nil, nil)

	page, err := provider.ListIssues(context.Background(), repository, query)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Issues) != 2 || page.Issues[0].Number != 1 || page.Issues[1].Number != 3 {
		t.Fatalf("issues=%+v", page.Issues)
	}
	if page.More || page.Limit != 30 {
		t.Fatalf("more=%v limit=%d", page.More, page.Limit)
	}
	if page.Issues[0].Title != "first" || page.Issues[0].Labels[0] != "bug" {
		t.Fatalf("summary=%+v", page.Issues[0])
	}
	if len(ghRunner.calls) != 1 {
		t.Fatalf("calls=%v", ghRunner.calls)
	}
}

func TestListIssuesFollowsPagesUntilLimit(t *testing.T) {
	repository := testRepository()
	query := issue.IssueQuery{State: issue.IssueStateAll, Limit: 2}
	first := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues", [][2]string{
		{"per_page", "3"}, {"page", "1"}, {"state", "all"},
	}), " ")
	second := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues", [][2]string{
		{"per_page", "1"}, {"page", "2"}, {"state", "all"},
	}), " ")
	provider, ghRunner, _ := newTestProvider(t, map[string]fakeResponse{
		first:  {stdout: "[" + issueJSON(1, "one", false) + "," + issueJSON(2, "pr", true) + "," + issueJSON(3, "three", false) + "]"},
		second: {stdout: "[" + issueJSON(4, "four", false) + "]"},
	}, nil, nil)

	page, err := provider.ListIssues(context.Background(), repository, query)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Issues) != 2 || !page.More {
		t.Fatalf("issues=%d more=%v", len(page.Issues), page.More)
	}
	if len(ghRunner.calls) != 2 {
		t.Fatalf("calls=%v", ghRunner.calls)
	}
}

func TestListIssuesSearchUsesSearchEndpoint(t *testing.T) {
	repository := testRepository()
	query := issue.IssueQuery{State: issue.IssueStateClosed, Labels: []string{"help wanted"}, Search: "crash", Limit: 10}
	perPage := (issue.IssueQuery{Limit: 10}).PageSize(0)
	key := strings.Join(apiArgs(repository.Host, "search/issues", [][2]string{
		{"per_page", fmt.Sprint(perPage)}, {"page", "1"}, {"q", `repo:dualface/kander is:issue state:closed label:"help wanted" crash`},
	}), " ")
	provider, ghRunner, _ := newTestProvider(t, map[string]fakeResponse{
		key: {stdout: `{"total_count":1,"incomplete_results":false,"items":[` + issueJSON(7, "crash", false) + `]}`},
	}, nil, nil)

	page, err := provider.ListIssues(context.Background(), repository, query)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Issues) != 1 || page.Issues[0].Number != 7 {
		t.Fatalf("issues=%+v", page.Issues)
	}
	if len(ghRunner.calls) != 1 {
		t.Fatalf("calls=%v", ghRunner.calls)
	}
}

func TestListIssuesRejectsMalformedJSON(t *testing.T) {
	repository := testRepository()
	key := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues", [][2]string{
		{"per_page", "31"}, {"page", "1"}, {"state", "open"},
	}), " ")
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{key: {stdout: "not json"}}, nil, nil)
	_, err := provider.ListIssues(context.Background(), repository, issue.IssueQuery{Limit: 30})
	if issue.KindOf(err) != issue.ErrorInvalidResponse {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestListIssuesRejectsInvalidQuery(t *testing.T) {
	provider, ghRunner, _ := newTestProvider(t, nil, nil, nil)
	if _, err := provider.ListIssues(context.Background(), testRepository(), issue.IssueQuery{Limit: issue.MaxIssueLimit + 1}); issue.KindOf(err) != issue.ErrorInvalidQuery {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if len(ghRunner.calls) != 0 {
		t.Fatalf("invalid query reached the provider: %v", ghRunner.calls)
	}
}

func TestListIssuesRejectsUntrustedIdentity(t *testing.T) {
	provider, ghRunner, _ := newTestProvider(t, nil, nil, nil)
	bad := testRepository()
	bad.URL = "https://evil.example/dualface/kander"
	if _, err := provider.ListIssues(context.Background(), bad, issue.IssueQuery{}); issue.KindOf(err) != issue.ErrorInvalidResponse {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if len(ghRunner.calls) != 0 {
		t.Fatalf("untrusted identity reached the provider: %v", ghRunner.calls)
	}
}

func TestListIssuesClassifiesHTTPFailures(t *testing.T) {
	repository := testRepository()
	key := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues", [][2]string{
		{"per_page", "31"}, {"page", "1"}, {"state", "open"},
	}), " ")
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{
		key: {stdout: "", stderr: "gh: Not Found (HTTP 404)", err: errors.New("exit status 1")},
	}, nil, nil)
	_, err := provider.ListIssues(context.Background(), repository, issue.IssueQuery{Limit: 30})
	if issue.KindOf(err) != issue.ErrorNotFound {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	structured := err.(*issue.Error)
	if structured.Host != "github.com" || !strings.Contains(structured.Detail, "dualface/kander") {
		t.Fatalf("error=%+v", structured)
	}
}

func TestGetIssueWithComments(t *testing.T) {
	repository := testRepository()
	issueKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/5", nil), " ")
	commentsKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/5/comments", [][2]string{
		{"per_page", "51"}, {"page", "1"},
	}), " ")
	hostile := `body \u001b[31mred\u001b[0m \u202eevil\u202c\ttab`
	comments := fmt.Sprintf(`[{"body":%q,"html_url":"https://github.com/dualface/kander/issues/5#issuecomment-1","user":{"login":"carol"},"created_at":%s}]`, hostile, listTimestamp)
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{
		issueKey:    {stdout: issueJSON(5, "hostile", false)},
		commentsKey: {stdout: comments},
	}, nil, nil)

	snapshot, err := provider.GetIssue(context.Background(), repository, 5, true)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if snapshot.Number != 5 || !snapshot.CommentsLoaded || len(snapshot.Comments) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if strings.ContainsAny(snapshot.Comments[0].Body, "\x1b") || strings.Contains(snapshot.Comments[0].Body, "\u202e") {
		t.Fatalf("comment body kept control characters: %q", snapshot.Comments[0].Body)
	}
	if snapshot.FetchedAt.IsZero() {
		t.Fatal("snapshot has no fetch time")
	}
}

func TestGetIssueWithoutComments(t *testing.T) {
	repository := testRepository()
	issueKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/5", nil), " ")
	provider, ghRunner, _ := newTestProvider(t, map[string]fakeResponse{issueKey: {stdout: issueJSON(5, "plain", false)}}, nil, nil)
	snapshot, err := provider.GetIssue(context.Background(), repository, 5, false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if snapshot.CommentsLoaded || len(snapshot.Comments) != 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if len(ghRunner.calls) != 1 {
		t.Fatalf("comments were fetched without request: %v", ghRunner.calls)
	}
}

func TestGetIssueRejectsPullRequest(t *testing.T) {
	repository := testRepository()
	issueKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/9", nil), " ")
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{issueKey: {stdout: issueJSON(9, "pr", true)}}, nil, nil)
	_, err := provider.GetIssue(context.Background(), repository, 9, false)
	if issue.KindOf(err) != issue.ErrorNotAnIssue {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestGetIssueBoundsComments(t *testing.T) {
	repository := testRepository()
	issueKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/5", nil), " ")
	commentsKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/5/comments", [][2]string{
		{"per_page", "51"}, {"page", "1"},
	}), " ")
	items := make([]string, 0, issue.MaxIssueComments+1)
	for i := 0; i <= issue.MaxIssueComments; i++ {
		items = append(items, fmt.Sprintf(`{"body":"c%d","user":{"login":"u"},"created_at":%s}`, i, listTimestamp))
	}
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{
		issueKey:    {stdout: issueJSON(5, "many", false)},
		commentsKey: {stdout: "[" + strings.Join(items, ",") + "]"},
	}, nil, nil)
	_, err := provider.GetIssue(context.Background(), repository, 5, true)
	if issue.KindOf(err) != issue.ErrorLimitExceeded {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestGetIssueBoundsBody(t *testing.T) {
	repository := testRepository()
	issueKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/5", nil), " ")
	bigBody := strings.Repeat("x", issue.MaxIssueBodyBytes+1)
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{
		issueKey: {stdout: fmt.Sprintf(`{"number":5,"title":"big","state":"open","html_url":"https://github.com/dualface/kander/issues/5","body":%q,"user":{"login":"a"},"created_at":%s,"updated_at":%s}`, bigBody, listTimestamp, listTimestamp)},
	}, nil, nil)
	_, err := provider.GetIssue(context.Background(), repository, 5, false)
	if issue.KindOf(err) != issue.ErrorLimitExceeded {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestGetIssueRejectsInvalidNumber(t *testing.T) {
	provider, ghRunner, _ := newTestProvider(t, nil, nil, nil)
	if _, err := provider.GetIssue(context.Background(), testRepository(), 0, false); issue.KindOf(err) != issue.ErrorInvalidQuery {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if len(ghRunner.calls) != 0 {
		t.Fatalf("invalid number reached the provider: %v", ghRunner.calls)
	}
}

func TestGetIssueRejectsMalformedTimestamps(t *testing.T) {
	repository := testRepository()
	issueKey := strings.Join(apiArgs(repository.Host, "repos/dualface/kander/issues/5", nil), " ")
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{
		issueKey: {stdout: `{"number":5,"title":"bad","state":"open","html_url":"https://github.com/dualface/kander/issues/5","updated_at":"yesterday"}`},
	}, nil, nil)
	_, err := provider.GetIssue(context.Background(), repository, 5, false)
	if issue.KindOf(err) != issue.ErrorInvalidResponse {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestAPIRequestShape(t *testing.T) {
	args := apiArgs("ghe.example.com", "repos/a/b/issues", [][2]string{{"state", "open"}})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"api",
		"--hostname ghe.example.com",
		"-H Accept: application/vnd.github+json",
		"-H X-GitHub-Api-Version: " + restAPIVersion,
		"--method GET",
		"-f state=open",
		"repos/a/b/issues",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %v missing %q", args, want)
		}
	}
}
