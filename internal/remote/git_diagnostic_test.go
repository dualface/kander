package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dualface/kander/internal/config"
)

// failingGit runs a real Git command that exits non-zero and writes the
// message of a missing revision to stderr.
func failingGit(t *testing.T) error {
	t.Helper()
	dir := t.TempDir()
	if _, err := gitCommand(context.Background(), dir, "init", "-q"); err != nil {
		t.Skipf("git init: %v", err)
	}
	_, err := gitCommand(context.Background(), dir, "rev-parse", "--verify", "refs/heads/no-such-branch")
	if err == nil {
		t.Fatal("rev-parse of a missing ref succeeded")
	}
	return err
}

func TestGitCommandErrorStartsWithThePlainTextAndCarriesTheDiagnostic(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		t.Run(lang, func(t *testing.T) {
			useLanguage(t, lang)
			err := failingGit(t)
			var git *gitError
			if !errors.As(err, &git) {
				t.Fatalf("error %T is not a Git error", err)
			}
			plain := config.Text("remote.git_unverified")
			if git.plain != plain || git.diagnostic == "" {
				t.Fatalf("plain %q, diagnostic %q", git.plain, git.diagnostic)
			}
			if text := err.Error(); !strings.HasPrefix(text, plain) || !strings.HasSuffix(text, git.diagnostic) || len(text) <= len(plain) {
				t.Fatalf("error text %q does not carry the diagnostic %q after %q", text, git.diagnostic, plain)
			}
			if got := ServerText(err); got != plain {
				t.Fatalf("server text = %q, want %q", got, plain)
			}
		})
	}
	useLanguage(t, "en")
	if got := config.Text("remote.git_unverified"); got != "Git transport could not verify the requested commit or remote reference" {
		t.Fatalf("plain text changed: %q", got)
	}
}

func TestGitCommandErrorWithoutStderrIsTheOriginalText(t *testing.T) {
	useLanguage(t, "en")
	dir := t.TempDir()
	for name, body := range map[string]string{"a": "one\n", "b": "two\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A quiet diff of different files exits 1 and writes nothing.
	_, err := gitCommand(context.Background(), dir, "diff", "--no-index", "--quiet", "a", "b")
	if err == nil {
		t.Fatal("a differing quiet diff succeeded")
	}
	if want := config.Text("remote.git_unverified"); err.Error() != want {
		t.Fatalf("error text = %q, want %q", err.Error(), want)
	}
	if got := gitDiagnostic(" \t\r\n \n"); got != "" {
		t.Fatalf("blank stderr gave diagnostic %q", got)
	}
}

func TestGitDiagnosticRedactsCredentials(t *testing.T) {
	for _, tc := range []struct{ name, stderr, secret string }{
		{"url user and password", "fatal: unable to access 'https://user:secret@example.invalid/r.git': 403", "secret"},
		{"url user", "fatal: unable to access 'https://user:secret@example.invalid/r.git': 403", "user:"},
		{"password with an at sign", "fatal: repository 'https://user:p@ss@example.invalid/r.git' not found", "p@ss"},
		{"token as user", "fatal: repository 'https://ghp_0123456789abcdefABCD@example.invalid/r.git' not found", "0123456789abcdef"},
		{"authorization header", "> Authorization: Basic dXNlcjpwYXNz\nfatal: failed", "dXNlcjpwYXNz"},
		{"lower case header", "authorization: basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"extraheader config", "http.extraheader=AUTHORIZATION: basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"extraheader with a leading line", "warning: x\nhttp.https://example.invalid/.extraheader=AUTHORIZATION: basic dXNlcjpwYXNz\nfatal: y", "dXNlcjpwYXNz"},
		{"github token", "bad credential ghp_0123456789abcdefghij", "ghp_0123456789abcdefghij"},
		{"github pat", "bad credential github_pat_0123456789abcdefghij0123", "github_pat_0123456789abcdefghij0123"},
		{"gitlab pat", "bad credential glpat-0123456789abcdefghij", "glpat-0123456789abcdefghij"},
		{"bearer", "got Bearer 0123456789abcdef from the proxy", "0123456789abcdef"},
		{"query token", "fatal: unable to access 'https://example.invalid/r.git?access_token=abc123def456'", "abc123def456"},
		{"query token among others", "see https://example.invalid/r.git?a=1&token=abc123def456&b=2", "abc123def456"},
		{"assignment", "GITHUB_TOKEN=abc123def456 git push", "abc123def456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := gitDiagnostic(tc.stderr)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("diagnostic %q still holds %q", got, tc.secret)
			}
			if !strings.Contains(got, gitRedacted) {
				t.Fatalf("diagnostic %q has no redaction marker", got)
			}
		})
	}
	// Text without a credential is kept as written.
	const plain = "fatal: not a git repository (or any of the parent directories): .git"
	if got := gitDiagnostic(plain + "\n"); got != plain {
		t.Fatalf("diagnostic = %q, want %q", got, plain)
	}
	if got := gitDiagnostic("fatal: could not read from 'https://example.invalid/r.git'"); strings.Contains(got, gitRedacted) {
		t.Fatalf("a URL without user information was redacted: %q", got)
	}
}

func TestGitDiagnosticIsOneBoundedLineOfValidText(t *testing.T) {
	got := gitDiagnostic("\x1b[31mfatal:\x1b[0m first\r\nsecond\x00\x07\tthird\n\n\x1b]0;title\x07fourth")
	if want := "fatal: first second third fourth"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
	if got := gitDiagnostic("fatal: bad \xff\xfe byte"); !utf8.ValidString(got) || !strings.Contains(got, "fatal: bad") {
		t.Fatalf("diagnostic = %q", got)
	}

	long := strings.Repeat("noise ", 200) + "fatal: the end"
	got = gitDiagnostic(long)
	if n := len([]rune(got)); n != gitDiagnosticLimit || !strings.HasPrefix(got, "...") || !strings.HasSuffix(got, "fatal: the end") {
		t.Fatalf("%d runes: %q", n, got)
	}
	// A multi-byte stderr is cut on a rune boundary and stays within the bound.
	for _, unit := range []string{"路径过长", "ファイル名が長すぎます", "é"} {
		got = gitDiagnostic("fatal: " + strings.Repeat(unit, 400) + "。終")
		if n := len([]rune(got)); !utf8.ValidString(got) || n > gitDiagnosticLimit || !strings.HasSuffix(got, "。終") {
			t.Fatalf("unit %q: %d runes, valid %v", unit, n, utf8.ValidString(got))
		}
	}
	// Output far beyond the input bound still yields a bounded diagnostic.
	if got := gitDiagnostic(strings.Repeat("x", 3*gitDiagnosticInput) + " fatal: tail"); len([]rune(got)) > gitDiagnosticLimit || !strings.HasSuffix(got, "fatal: tail") {
		t.Fatalf("%d runes", len([]rune(got)))
	}
}

func TestGitDiagnosticRedactsBeforeItTruncates(t *testing.T) {
	// A token cut in half by truncation would escape its pattern, so the
	// credential must be replaced while it is still whole.
	stderr := strings.Repeat("x ", 140) + "token ghp_0123456789abcdefghij tail"
	got := gitDiagnostic(stderr)
	if strings.Contains(got, "ghp_") || strings.Contains(got, "abcdefghij") {
		t.Fatalf("diagnostic %q holds a token fragment", got)
	}
}

func TestServerTextDropsEveryGitDiagnostic(t *testing.T) {
	useLanguage(t, "en")
	plain := config.Text("remote.git_unverified")
	git := &gitError{plain: plain, diagnostic: "fatal: secret-path-detail"}
	other := &gitError{plain: plain, diagnostic: "fatal: another-detail"}

	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"nil":                        {nil, ""},
		"single":                     {git, plain},
		"without cause":              {&gitError{plain: plain}, plain},
		"join":                       {errors.Join(git, errors.New("recovery required")), plain + "\nrecovery required"},
		"wrapped":                    {fmt.Errorf("fetch delivery: %w", git), "fetch delivery: " + plain},
		"two errors":                 {errors.Join(git, other), plain + "\n" + plain},
		"plain error":                {errors.New("no git here"), "no git here"},
		"%v wrapper":                 {verbWrapper{"fetch delivery", git}, "fetch delivery: " + plain},
		"reworded":                   {fmt.Errorf("delivery unusable: %w", reworded{git}), plain},
		"join of reworded and plain": {errors.Join(reworded{git}, errors.New("recovery required")), plain},
	} {
		t.Run(name, func(t *testing.T) {
			got := ServerText(tc.err)
			if got != tc.want {
				t.Fatalf("server text = %q, want %q", got, tc.want)
			}
			for _, leak := range []string{"secret-path-detail", "another-detail"} {
				if strings.Contains(got, leak) {
					t.Fatalf("server text %q holds %q", got, leak)
				}
			}
		})
	}
}

// verbWrapper formats its cause with %v, as a caller that wanted no %w would,
// and still exposes it through Unwrap.
type verbWrapper struct {
	op  string
	err error
}

func (w verbWrapper) Error() string { return fmt.Sprintf("%s: %v", w.op, w.err) }
func (w verbWrapper) Unwrap() error { return w.err }

// reworded wraps a Git error but renders its own text, so the Error() of the
// Git error is not a substring of the outer text.
type reworded struct{ git *gitError }

func (r reworded) Error() string { return "the checkout failed" }
func (r reworded) Unwrap() error { return r.git }

func TestServerTextKeepsAShorterGitErrorFromLeavingADiagnosticBehind(t *testing.T) {
	useLanguage(t, "en")
	plain := config.Text("remote.git_unverified")
	bare := &gitError{plain: plain}
	detailed := &gitError{plain: plain, diagnostic: "fatal: kept-out"}
	got := ServerText(errors.Join(bare, detailed))
	if strings.Contains(got, "kept-out") {
		t.Fatalf("server text %q holds the diagnostic", got)
	}
}
