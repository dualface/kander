package issue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

var errPlain = errors.New("plain failure")

type stubResolver struct {
	repository Repository
	err        error
	calls      int
	directory  string
	explicit   string
}

func (s *stubResolver) ResolveRepository(_ context.Context, directory string, explicit string) (Repository, error) {
	s.calls++
	s.directory = directory
	s.explicit = explicit
	return s.repository, s.err
}

// useChinese pins the interface language so message assertions stay stable.
func useChinese(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
}

func runIssue(t *testing.T, resolver RepositoryResolver, args ...string) (int, string, string) {
	t.Helper()
	useChinese(t)
	var stdout, stderr bytes.Buffer
	factory := func() RepositoryResolver {
		if resolver == nil {
			return nil
		}
		return resolver
	}
	code := runWith(factory, args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestIssueRepoTextOutput(t *testing.T) {
	stub := &stubResolver{repository: Repository{
		Host: "github.com", Owner: "dualface", Name: "kander",
		URL: "https://github.com/dualface/kander", Private: true, Remote: "origin",
	}}
	code, stdout, stderr := runIssue(t, stub, "repo")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"github.com", "dualface", "kander", "https://github.com/dualface/kander", "私有", "origin"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if stub.explicit != "" {
		t.Fatalf("explicit=%q want empty", stub.explicit)
	}
	if stub.directory == "" {
		t.Fatal("working directory was not passed")
	}
}

func TestIssueRepoJSONOutput(t *testing.T) {
	stub := &stubResolver{repository: Repository{
		Host: "ghe.example.com", Owner: "acme", Name: "tool",
		URL: "https://ghe.example.com/acme/tool", Private: false, Remote: "upstream",
	}}
	code, stdout, stderr := runIssue(t, stub, "repo", "--repo", "ghe.example.com/acme/tool", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if stub.explicit != "ghe.example.com/acme/tool" {
		t.Fatalf("explicit=%q", stub.explicit)
	}
	var decoded struct {
		Host    string `json:"host"`
		Owner   string `json:"owner"`
		Name    string `json:"name"`
		URL     string `json:"url"`
		Private bool   `json:"private"`
		Remote  string `json:"remote"`
	}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if decoded.Host != "ghe.example.com" || decoded.Owner != "acme" || decoded.Name != "tool" || decoded.Remote != "upstream" || decoded.Private {
		t.Fatalf("decoded=%+v", decoded)
	}
	if strings.Count(strings.TrimSpace(stdout), "\n") != 0 {
		t.Fatalf("JSON output must be one line: %q", stdout)
	}
}

func TestIssueRepoJSONFlagWithEquals(t *testing.T) {
	stub := &stubResolver{repository: Repository{Host: "github.com", Owner: "a", Name: "b", URL: "https://github.com/a/b"}}
	code, _, stderr := runIssue(t, stub, "repo", "--repo=a/b", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if stub.explicit != "a/b" {
		t.Fatalf("explicit=%q", stub.explicit)
	}
}

func TestIssueRepoErrorOutput(t *testing.T) {
	stub := &stubResolver{err: &Error{Kind: ErrorAmbiguousRemotes, Op: "remote", Candidates: []string{"github.com/a/b (origin)", "gitlab.com/c/d (mirror)"}}}
	code, stdout, stderr := runIssue(t, stub, "repo")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	for _, want := range []string{"github.com/a/b (origin)", "gitlab.com/c/d (mirror)", "--repo HOST/OWNER/REPO", "gh repo set-default"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q: %s", want, stderr)
		}
	}
}

func TestIssueUsageErrors(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		code  int
		match string
	}{
		{name: "no arguments", args: nil, code: 2, match: "kander issue"},
		{name: "unknown subcommand", args: []string{"import"}, code: 2, match: "import"},
		{name: "unknown option", args: []string{"repo", "--state", "open"}, code: 2, match: "--state"},
		{name: "missing value", args: []string{"repo", "--repo"}, code: 2, match: "--repo"},
		{name: "empty value", args: []string{"repo", "--repo="}, code: 2, match: "空"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &stubResolver{}
			code, _, stderr := runIssue(t, stub, test.args...)
			if code != test.code {
				t.Fatalf("code=%d want %d (stderr=%q)", code, test.code, stderr)
			}
			if !strings.Contains(stderr, test.match) {
				t.Fatalf("stderr missing %q: %s", test.match, stderr)
			}
			if stub.calls != 0 {
				t.Fatalf("resolver called %d times on a usage error", stub.calls)
			}
		})
	}
}

func TestFormatErrorMessagesAndHints(t *testing.T) {
	useChinese(t)
	tests := []struct {
		name   string
		err    error
		want   []string
		absent []string
	}{
		{
			name:   "not a worktree",
			err:    &Error{Kind: ErrorNotRepository, Op: "git", Detail: "/tmp/dir"},
			want:   []string{"不是 Git 工作树", "/tmp/dir", "--repo"},
			absent: []string{"gh repo set-default"},
		},
		{
			name: "no remote",
			err:  &Error{Kind: ErrorNoRemote},
			want: []string{"未找到 GitHub remote", "--repo", "gh repo set-default"},
		},
		{
			name: "ambiguous remotes",
			err:  &Error{Kind: ErrorAmbiguousRemotes, Candidates: []string{"github.com/a/b (origin)", "github.com/c/d (mirror)"}},
			want: []string{"github.com/a/b (origin)", "github.com/c/d (mirror)", "gh repo set-default"},
		},
		{
			name: "unauthenticated",
			err:  &Error{Kind: ErrorUnauthenticated, Host: "ghe.example.com"},
			want: []string{"gh auth login --hostname ghe.example.com"},
		},
		{
			name: "invalid reference",
			err:  &Error{Kind: ErrorInvalidReference, Detail: "not a reference"},
			want: []string{"无效的仓库引用", "not a reference"},
		},
		{
			name: "not found",
			err:  &Error{Kind: ErrorNotFound, Detail: "github.com/acme/tool"},
			want: []string{"github.com/acme/tool"},
		},
		{
			name: "timeout",
			err:  &Error{Kind: ErrorTimeout},
			want: []string{"超时"},
		},
		{
			name: "plain error",
			err:  errPlain,
			want: []string{"plain failure"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := formatError(test.err)
			if !strings.HasPrefix(message, "kander issue: ") {
				t.Fatalf("message=%q", message)
			}
			for _, want := range test.want {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q missing %q", message, want)
				}
			}
			for _, absent := range test.absent {
				if strings.Contains(message, absent) {
					t.Fatalf("message %q unexpectedly contains %q", message, absent)
				}
			}
		})
	}
}

func TestIssueHelpDoesNotResolve(t *testing.T) {
	stub := &stubResolver{}
	code, stdout, stderr := runIssue(t, stub, "--help")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "kander issue") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runIssue(t, stub, "repo", "--help")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "issue repo") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stub.calls != 0 {
		t.Fatalf("resolver called on help: %d", stub.calls)
	}
}
