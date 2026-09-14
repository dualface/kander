package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/issue/ghcli/ghclitest"
)

func resultIssueJSON(count int) string {
	return strings.TrimSuffix(issueJSON(42, "result", false), "}") + fmt.Sprintf(`,"id":42,"comments":%d}`, count)
}
func resultRoute(endpoint string, params [][2]string) string {
	return strings.Join(apiArgs("github.com", endpoint, params), " ")
}
func resultReadRoutes(count int, comments string) map[string]ghclitest.Options {
	base := "repos/dualface/kander/issues/42"
	page := [][2]string{{"per_page", "100"}, {"page", "1"}}
	return map[string]ghclitest.Options{
		resultRoute(base, nil):              {Stdout: resultIssueJSON(count)},
		resultRoute("user", nil):            {Stdout: `{"id":7}`},
		resultRoute(base+"/events", page):   {Stdout: `[{"id":10,"event":"reopened"}]`},
		resultRoute(base+"/comments", page): {Stdout: comments},
	}
}

func TestResultProviderCompleteReadAndWriteArgv(t *testing.T) {
	ghclitest.Install(t)
	// Route the issue metadata by a trailing boundary to avoid matching its
	// comments/events endpoints in the fake's substring routing.
	routes := resultReadRoutes(1, `[{"id":55,"body":"human result","user":{"id":8}}]`)
	base := "repos/dualface/kander/issues/42"
	// Metadata request has no -f page fields; unlike a suffix-only route this
	// exact argv prefix is disjoint from paginated paths.
	ghclitest.SetRoutes(t, "gh", routes)
	provider := NewProvider(Options{})
	out, err := provider.ReadResult(context.Background(), testRepository(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if out.StateVersion != "42:10" || out.ActorID != 7 || len(out.Comments) != 1 || out.Comments[0].AuthorID != 8 {
		t.Fatalf("out=%+v", out)
	}
	body := "Verified `go test ./...`; $(never execute).\nNo remaining work."
	comment := fmt.Sprintf(`{"id":56,"body":%q,"user":{"id":7}}`, body)
	log := t.TempDir() + "/calls.jsonl"
	t.Setenv(ghclitest.RecordEnv, log)
	ghclitest.SetRoutes(t, "gh", nil)
	ghclitest.Set(t, "gh", ghclitest.Options{Stdout: comment})
	posted, err := provider.PostResult(context.Background(), testRepository(), 42, body)
	if err != nil || posted.Body != body {
		t.Fatalf("post=%+v err=%v", posted, err)
	}
	data, _ := os.ReadFile(log)
	var call struct {
		Argv []string `json:"argv"`
	}
	if err := json.Unmarshal(data, &call); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(call.Argv, "|"), "--method|POST|-f|body="+body+"|"+base+"/comments") {
		t.Fatalf("argv=%q", call.Argv)
	}
	ghclitest.Set(t, "gh", ghclitest.Options{Stdout: strings.Replace(resultIssueJSON(1), `"state":"open"`, `"state":"closed"`, 1)})
	if err := provider.CloseResult(context.Background(), testRepository(), 42); err != nil {
		t.Fatal(err)
	}
}

func TestResultProviderRejectsIncompleteUnreadableAndForeignReplies(t *testing.T) {
	ghclitest.Install(t)
	base := "repos/dualface/kander/issues/42"
	for _, mode := range []string{"count-mismatch", "bad-author", "auth", "foreign", "bad-events", "too-many-pages"} {
		t.Run(mode, func(t *testing.T) {
			routes := resultReadRoutes(1, `[{"id":55,"body":"result","user":{"id":8}}]`)
			switch mode {
			case "count-mismatch":
				routes[resultRoute(base, nil)] = ghclitest.Options{Stdout: resultIssueJSON(2)}
			case "bad-author":
				routes[resultRoute(base+"/comments", [][2]string{{"per_page", "100"}, {"page", "1"}})] = ghclitest.Options{Stdout: `[{"id":55,"body":"result"}]`}
			case "auth":
				routes[resultRoute("user", nil)] = ghclitest.Options{Stderr: "HTTP 401: Bad credentials", Exit: 1}
			case "foreign":
				routes[resultRoute(base, nil)] = ghclitest.Options{Stdout: strings.Replace(resultIssueJSON(1), "dualface/kander/issues/42", "other/repo/issues/42", 1)}
			case "bad-events":
				routes[resultRoute(base+"/events", [][2]string{{"per_page", "100"}, {"page", "1"}})] = ghclitest.Options{Stdout: `[{"event":"reopened"}]`}
			case "too-many-pages":
				full := []string{}
				for i := 0; i < 100; i++ {
					full = append(full, fmt.Sprintf(`{"id":%d,"event":"labeled"}`, i+1))
				}
				for page := 1; page <= 10; page++ {
					routes[resultRoute(base+"/events", [][2]string{{"per_page", "100"}, {"page", fmt.Sprint(page)}})] = ghclitest.Options{Stdout: "[" + strings.Join(full, ",") + "]"}
				}
			}
			ghclitest.SetRoutes(t, "gh", routes)
			if _, err := NewProvider(Options{}).ReadResult(context.Background(), testRepository(), 42); err == nil {
				t.Fatal("unsafe result observation accepted")
			}
		})
	}
}

func TestResultWriteDistinguishesDefiniteRejectionFromUncertainty(t *testing.T) {
	ghclitest.Install(t)
	cases := []struct {
		name   string
		stderr string
		want   bool
	}{}
	for _, code := range []int{400, 401, 403, 404, 410, 413, 422, 429, 408, 500, 503} {
		cases = append(cases, struct {
			name   string
			stderr string
			want   bool
		}{fmt.Sprint(code), fmt.Sprintf("gh: request failed (HTTP %d)\n", code), code < 500 && code != 408})
	}
	cases = append(cases, []struct {
		name   string
		stderr string
		want   bool
	}{
		{"scope", "gh: Not Found (HTTP 404)\ngh: This API operation needs the \"repo\" scope. To request it, run:  gh auth refresh -h github.com -s repo\n", true},
		{"SSO", "gh: Resource protected by organization SAML enforcement (HTTP 403)\nAuthorize in your web browser: https://github.com/orgs/example/sso\n", true},
		{"plain status", "gh: HTTP 403\n", true},
		{"unknown trailing failure", "gh: request failed (HTTP 403)\nunexpected transport failure\n", false},
		{"server error with embedded status", "gh: upstream reported (HTTP 403) (HTTP 503)\n", false},
		{"server error with later status", "gh: failed (HTTP 503)\ngh: request failed (HTTP 403)\n", false},
	}...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ghclitest.Set(t, "gh", ghclitest.Options{Stderr: tc.stderr, Exit: 1})
			_, err := NewProvider(Options{}).PostResult(context.Background(), testRepository(), 42, "result")
			if err == nil {
				t.Fatal("failure reported success")
			}
			var rejected *issue.ResultWriteRejection
			if errors.As(err, &rejected) != tc.want {
				t.Fatalf("classification for %s: %v", tc.name, err)
			}
		})
	}
}
