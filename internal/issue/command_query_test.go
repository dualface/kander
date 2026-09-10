package issue

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func listStub() *stubResolver {
	repository := Repository{
		Host: "github.com", Owner: "dualface", Name: "kander",
		URL: "https://github.com/dualface/kander", Private: false, Remote: "origin",
	}
	return &stubResolver{
		repository: repository,
		page: IssuePage{
			Repository: repository,
			Limit:      30,
			Issues: []IssueSummary{
				{Number: 42, Title: "Fix the widget crash", State: "open", URL: "https://github.com/dualface/kander/issues/42",
					Labels: []string{"bug", "help wanted"}, UpdatedAt: time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC)},
				{Number: 41, Title: "Docs", State: "closed", UpdatedAt: time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)},
			},
		},
	}
}

func showStub() *stubResolver {
	repository := Repository{
		Host: "github.com", Owner: "dualface", Name: "kander",
		URL: "https://github.com/dualface/kander", Remote: "origin",
	}
	return &stubResolver{
		repository: repository,
		snapshot: IssueSnapshot{
			Repository: repository,
			Number:     42, Title: "Fix the widget crash", State: "open",
			Author: "alice", URL: "https://github.com/dualface/kander/issues/42",
			Labels: []string{"bug"}, Assignees: []string{"bob"},
			Body:           "Steps to reproduce:\n\n1. open the board\n",
			CreatedAt:      time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC),
			FetchedAt:      time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC),
			CommentsLoaded: true,
			Comments: []IssueComment{
				{Author: "carol", Body: "Confirmed on Linux.", CreatedAt: time.Date(2026, 9, 11, 2, 10, 0, 0, time.UTC)},
			},
		},
	}
}

func TestIssueListTextOutput(t *testing.T) {
	stub := listStub()
	code, stdout, stderr := runIssue(t, stub, "list")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"#42", "open", "Fix the widget crash", "bug, help wanted", "更新于", "2 个 Issue"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if stub.listCalls != 1 || stub.listQuery.Limit != DefaultIssueLimit || stub.listQuery.State != IssueStateOpen {
		t.Fatalf("query=%+v calls=%d", stub.listQuery, stub.listCalls)
	}
	if stub.explicit != "" {
		t.Fatalf("explicit=%q", stub.explicit)
	}
}

func TestIssueListPassesFilters(t *testing.T) {
	stub := listStub()
	code, _, stderr := runIssue(t, stub, "list", "--repo", "acme/tool", "--state", "closed", "--label", "bug", "--label", "docs", "--search", "crash", "--limit", "5")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if stub.explicit != "acme/tool" {
		t.Fatalf("explicit=%q", stub.explicit)
	}
	query := stub.listQuery
	if query.State != IssueStateClosed || query.Search != "crash" || query.Limit != 5 || len(query.Labels) != 2 {
		t.Fatalf("query=%+v", query)
	}
}

func TestIssueListMoreSummary(t *testing.T) {
	stub := listStub()
	stub.page.More = true
	stub.page.Limit = 1
	code, stdout, stderr := runIssue(t, stub, "list", "--limit", "1")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "上限 1") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestIssueListJSONOutput(t *testing.T) {
	stub := listStub()
	code, stdout, stderr := runIssue(t, stub, "list", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var decoded struct {
		Repository struct {
			Owner string `json:"owner"`
			URL   string `json:"url"`
		} `json:"repository"`
		Issues []struct {
			Number    int      `json:"number"`
			Title     string   `json:"title"`
			State     string   `json:"state"`
			Labels    []string `json:"labels"`
			UpdatedAt string   `json:"updated_at"`
		} `json:"issues"`
		Limit int  `json:"limit"`
		More  bool `json:"more"`
	}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if decoded.Repository.Owner != "dualface" || decoded.Repository.URL != "https://github.com/dualface/kander" {
		t.Fatalf("repository=%+v", decoded.Repository)
	}
	if len(decoded.Issues) != 2 || decoded.Issues[0].Number != 42 || decoded.Issues[0].UpdatedAt != "2026-09-11T02:03:00Z" {
		t.Fatalf("issues=%+v", decoded.Issues)
	}
	if decoded.Limit != 30 || decoded.More {
		t.Fatalf("limit=%d more=%v", decoded.Limit, decoded.More)
	}
}

func TestIssueListUsageErrors(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		match string
	}{
		{"bad state", []string{"list", "--state", "merged"}, "无效的 Issue 查询"},
		{"bad limit", []string{"list", "--limit", "many"}, "--limit"},
		{"limit too big", []string{"list", "--limit", "500"}, "limit"},
		{"unknown flag", []string{"list", "--nope"}, "--nope"},
		{"missing label value", []string{"list", "--label"}, "--label"},
		{"empty repo", []string{"list", "--repo="}, "--repo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := listStub()
			code, _, stderr := runIssue(t, stub, test.args...)
			if code != 2 {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			if !strings.Contains(stderr, test.match) {
				t.Fatalf("stderr missing %q: %s", test.match, stderr)
			}
			if stub.listCalls != 0 || stub.calls != 0 {
				t.Fatalf("provider reached on a usage error: %d/%d", stub.listCalls, stub.calls)
			}
		})
	}
}

func TestIssueShowTextOutput(t *testing.T) {
	stub := showStub()
	code, stdout, stderr := runIssue(t, stub, "show", "42", "--comments")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"#42", "Fix the widget crash", "alice", "bug", "bob", "Steps to reproduce", "评论: 1", "@carol", "Confirmed on Linux."} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if stub.issueCalls != 1 || stub.issueNumber != 42 || !stub.withComments {
		t.Fatalf("calls=%d number=%d comments=%v", stub.issueCalls, stub.issueNumber, stub.withComments)
	}
}

func TestIssueShowWithoutComments(t *testing.T) {
	stub := showStub()
	stub.snapshot.Comments = nil
	stub.snapshot.CommentsLoaded = false
	code, stdout, stderr := runIssue(t, stub, "show", "42")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "评论") {
		t.Fatalf("stdout mentions comments without a request: %s", stdout)
	}
	if stub.withComments {
		t.Fatal("comments requested without the flag")
	}
}

func TestIssueShowJSONOutput(t *testing.T) {
	stub := showStub()
	code, stdout, stderr := runIssue(t, stub, "show", "42", "--comments", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var decoded struct {
		Issue struct {
			Number         int      `json:"number"`
			Title          string   `json:"title"`
			Body           string   `json:"body"`
			Labels         []string `json:"labels"`
			Assignees      []string `json:"assignees"`
			CommentsLoaded bool     `json:"comments_loaded"`
			Comments       []struct {
				Author string `json:"author"`
				Body   string `json:"body"`
			} `json:"comments"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if decoded.Issue.Number != 42 || decoded.Issue.Title != "Fix the widget crash" || !decoded.Issue.CommentsLoaded {
		t.Fatalf("issue=%+v", decoded.Issue)
	}
	if len(decoded.Issue.Comments) != 1 || decoded.Issue.Comments[0].Author != "carol" {
		t.Fatalf("comments=%+v", decoded.Issue.Comments)
	}
	if strings.Count(strings.TrimSpace(stdout), "\n") != 0 {
		t.Fatalf("JSON output must be one line: %q", stdout)
	}
}

func TestIssueShowUsageErrors(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		match string
	}{
		{"missing number", []string{"show"}, "NUMBER"},
		{"bad number", []string{"show", "abc"}, "NUMBER"},
		{"zero number", []string{"show", "0"}, "NUMBER"},
		{"two numbers", []string{"show", "1", "2"}, "2"},
		{"unknown flag", []string{"show", "1", "--nope"}, "--nope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := showStub()
			code, _, stderr := runIssue(t, stub, test.args...)
			if code != 2 {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			if !strings.Contains(stderr, test.match) {
				t.Fatalf("stderr missing %q: %s", test.match, stderr)
			}
			if stub.issueCalls != 0 {
				t.Fatalf("provider reached on a usage error: %d", stub.issueCalls)
			}
		})
	}
}

func TestIssueProviderErrorsAreActionable(t *testing.T) {
	stub := showStub()
	stub.issueErr = &Error{Kind: ErrorNotAnIssue, Op: "show", Detail: "42"}
	code, stdout, stderr := runIssue(t, stub, "show", "42")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	for _, want := range []string{"Pull Request", "列表"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q: %s", want, stderr)
		}
	}

	stub = listStub()
	stub.listErr = &Error{Kind: ErrorLimitExceeded, Op: "pagination", Detail: "10"}
	code, _, stderr = runIssue(t, stub, "list")
	if code != 1 || !strings.Contains(stderr, "上限") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}
