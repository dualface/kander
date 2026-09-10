package issue

import (
	"strings"
	"testing"
	"time"
)

func TestIssueQueryNormalizeDefaults(t *testing.T) {
	query, err := IssueQuery{}.Normalize()
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if query.State != IssueStateOpen || query.Limit != DefaultIssueLimit || len(query.Labels) != 0 || query.Search != "" {
		t.Fatalf("query=%+v", query)
	}
}

func TestIssueQueryNormalizeFilters(t *testing.T) {
	query, err := (IssueQuery{
		State:  " ALL ",
		Labels: []string{" bug ", "bug", "help wanted"},
		Search: " crash on start ",
		Limit:  60,
	}).Normalize()
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if query.State != IssueStateAll {
		t.Fatalf("state=%q", query.State)
	}
	if len(query.Labels) != 2 || query.Labels[0] != "bug" || query.Labels[1] != "help wanted" {
		t.Fatalf("labels=%v", query.Labels)
	}
	if query.Search != "crash on start" {
		t.Fatalf("search=%q", query.Search)
	}
}

func TestIssueQueryNormalizeRejects(t *testing.T) {
	tests := []struct {
		name  string
		query IssueQuery
	}{
		{"unknown state", IssueQuery{State: "merged"}},
		{"empty label", IssueQuery{Labels: []string{" "}}},
		{"quoted label", IssueQuery{Labels: []string{`a"b`}}},
		{"label control", IssueQuery{Labels: []string{"a\nb"}}},
		{"many labels", IssueQuery{Labels: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u"}}},
		{"search control", IssueQuery{Search: "a\nb"}},
		{"search long", IssueQuery{Search: strings.Repeat("x", maxIssueSearchRune+1)}},
		{"limit zero-ish", IssueQuery{Limit: -1}},
		{"limit too big", IssueQuery{Limit: MaxIssueLimit + 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.query.Normalize(); KindOf(err) != ErrorInvalidQuery {
				t.Fatalf("kind=%q err=%v", KindOf(err), err)
			}
		})
	}
}

func TestIssueQueryPageSize(t *testing.T) {
	query := IssueQuery{Limit: MaxIssueLimit}
	if got := query.PageSize(0); got != MaxIssuePageSize {
		t.Fatalf("page size=%d", got)
	}
	small := IssueQuery{Limit: 30}
	if got := small.PageSize(0); got != 31 {
		t.Fatalf("page size=%d", got)
	}
	if got := small.PageSize(30); got != 1 {
		t.Fatalf("page size=%d", got)
	}
}

func TestSanitizeRemoteTextRemovesTerminalControl(t *testing.T) {
	input := "a\x1b[31mb\x1b]0;title\x07c\x1b]8;;https://evil.example\x1b\\d\x07e\u202eg\u202ch\tok\n第二行"
	got := SanitizeRemoteText(input)
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("escape survived: %q", got)
	}
	for _, absent := range []string{"\u202e", "\u202c", "31m", "evil.example"} {
		if strings.Contains(got, absent) {
			t.Fatalf("got %q still contains %q", got, absent)
		}
	}
	if !strings.Contains(got, "abcde") || !strings.Contains(got, "第二行") || !strings.Contains(got, "\tok") {
		t.Fatalf("got=%q", got)
	}
}

func TestSanitizeRemoteTextNormalizesNewlines(t *testing.T) {
	if got := SanitizeRemoteText("a\r\nb\rc"); got != "a\nb\nc" {
		t.Fatalf("got=%q", got)
	}
}

func TestNormalizeSummaryTruncatesTitle(t *testing.T) {
	summary, err := NormalizeSummary(IssueSummary{
		Number:    1,
		Title:     strings.Repeat("x", MaxIssueTitleRunes+10),
		State:     "open",
		URL:       "https://github.com/o/r/issues/1",
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !strings.HasSuffix(summary.Title, "...") || len([]rune(summary.Title)) != MaxIssueTitleRunes {
		t.Fatalf("title=%q len=%d", summary.Title, len([]rune(summary.Title)))
	}
}

func TestNormalizeSnapshotBounds(t *testing.T) {
	now := time.Now()
	valid := IssueSnapshot{Number: 1, CreatedAt: now, UpdatedAt: now, Body: "hello"}
	if _, err := NormalizeSnapshot(valid); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	tooBig := valid
	tooBig.Body = strings.Repeat("x", MaxIssueBodyBytes+1)
	if _, err := NormalizeSnapshot(tooBig); KindOf(err) != ErrorLimitExceeded {
		t.Fatal("oversized body must exceed the limit")
	}
	tooManyComments := valid
	for i := 0; i <= MaxIssueComments; i++ {
		tooManyComments.Comments = append(tooManyComments.Comments, IssueComment{Body: "x", CreatedAt: now})
	}
	if _, err := NormalizeSnapshot(tooManyComments); KindOf(err) != ErrorLimitExceeded {
		t.Fatal("comment count must exceed the limit")
	}
	comment := IssueComment{Body: strings.Repeat("x", MaxCommentBytes+1), CreatedAt: now}
	if _, err := NormalizeComment(comment); KindOf(err) != ErrorLimitExceeded {
		t.Fatal("oversized comment must exceed the limit")
	}
	invalid := valid
	invalid.Number = 0
	if _, err := NormalizeSnapshot(invalid); KindOf(err) != ErrorInvalidResponse {
		t.Fatal("missing number must be an invalid response")
	}
	labels := valid
	for i := 0; i <= MaxIssueLabels; i++ {
		labels.Labels = append(labels.Labels, "label")
	}
	if _, err := NormalizeSnapshot(labels); KindOf(err) != ErrorLimitExceeded {
		t.Fatal("label count must exceed the limit")
	}
}

func TestRepositoryValidateAndIssueURL(t *testing.T) {
	repository := Repository{Host: "github.com", Owner: "o", Name: "r", URL: "https://github.com/o/r"}
	if err := repository.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	target, err := repository.IssueURL(42)
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if target != "https://github.com/o/r/issues/42" {
		t.Fatalf("url=%q", target)
	}
	if _, err := repository.IssueURL(0); KindOf(err) != ErrorInvalidQuery {
		t.Fatalf("kind=%q", KindOf(err))
	}
	badHost := repository
	badHost.URL = "https://evil.example/o/r"
	if err := badHost.Validate(); KindOf(err) != ErrorInvalidResponse {
		t.Fatalf("kind=%q", KindOf(err))
	}
	insecure := repository
	insecure.URL = "http://github.com/o/r"
	if err := insecure.Validate(); KindOf(err) != ErrorInvalidResponse {
		t.Fatalf("kind=%q", KindOf(err))
	}
	credentials := repository
	credentials.URL = "https://user@github.com/o/r"
	if err := credentials.Validate(); KindOf(err) != ErrorInvalidResponse {
		t.Fatalf("kind=%q", KindOf(err))
	}
	wrongPath := repository
	wrongPath.URL = "https://github.com/o/r/../../etc"
	if err := wrongPath.Validate(); KindOf(err) != ErrorInvalidResponse {
		t.Fatalf("kind=%q", KindOf(err))
	}
}
