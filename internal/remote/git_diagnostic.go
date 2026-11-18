package remote

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/config"
)

const (
	// gitDiagnosticLimit bounds a diagnostic in runes, the "..." prefix of a
	// truncated one included.
	gitDiagnosticLimit = 300
	// gitDiagnosticInput bounds the stderr bytes read before sanitizing, so a
	// verbose Git run costs a bounded amount of regexp work.
	gitDiagnosticInput = 64 << 10
	gitRedacted        = "[REDACTED]"
)

var (
	gitANSIPattern = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	// A header line carries its credential after the keyword in a shape no
	// single pattern covers ("Basic <base64>", "http.extraheader=AUTHORIZATION:
	// basic ..."), so the rest of the line goes.
	gitHeaderPattern   = regexp.MustCompile(`(?i)authorization|extraheader`)
	gitUserinfoPattern = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/]+@`)
	gitQueryPattern    = regexp.MustCompile(`(?i)([?&;][A-Za-z0-9_.-]*(?:token|secret|password|passwd|key|sig|auth)[A-Za-z0-9_.-]*=)[^\s&#'"]*`)
	// Token shapes are recognized for defense in depth: credentials must not
	// reach a diagnostic even when Git did not mask them.
	gitTokenPatterns = []*regexp.Regexp{
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`),
		regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{12,}`),
	}
	gitAssignPattern = regexp.MustCompile(`(?i)((?:token|password|passwd|secret)\s*[:=]\s*)[^\s'"&]+`)
)

// gitError is the error of a Git process that exited non-zero. Err.Error() is
// the localized plain text; the sanitized stderr rides along for the local
// user only. Anything sent to the server goes through ServerText.
type gitError struct {
	plain      string
	diagnostic string
}

func newGitError(stderr string) *gitError {
	return &gitError{plain: config.Text("remote.git_unverified"), diagnostic: gitDiagnostic(stderr)}
}

func (e *gitError) Error() string {
	if e.diagnostic == "" {
		return e.plain
	}
	return config.Text("remote.git_diagnostic", e.plain, e.diagnostic)
}

// gitDiagnostic turns Git stderr into one bounded line without control
// characters, URL user information, authorization headers or token-shaped
// strings. It keeps the end of the output, where Git writes its final error.
func gitDiagnostic(stderr string) string {
	if len(stderr) > gitDiagnosticInput {
		stderr = stderr[len(stderr)-gitDiagnosticInput:]
	}
	text := gitANSIPattern.ReplaceAllString(strings.ToValidUTF8(stderr, ""), "")
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	words := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Map(func(r rune) rune {
			switch {
			case r == '\t':
				return ' '
			case unicode.IsPrint(r):
				return r
			}
			return -1
		}, line)
		// The header rule runs per line, before lines are merged: after a merge
		// the end of the credential is no longer findable.
		if loc := gitHeaderPattern.FindStringIndex(line); loc != nil {
			line = line[:loc[1]] + " " + gitRedacted
		}
		words = append(words, strings.Fields(line)...)
	}
	result := redactGitSecrets(strings.Join(words, " "))
	if runes := []rune(result); len(runes) > gitDiagnosticLimit {
		result = "..." + string(runes[len(runes)-gitDiagnosticLimit+3:])
	}
	return result
}

func redactGitSecrets(text string) string {
	text = gitUserinfoPattern.ReplaceAllString(text, "${1}"+gitRedacted+"@")
	text = gitQueryPattern.ReplaceAllString(text, "${1}"+gitRedacted)
	for _, pattern := range gitTokenPatterns {
		text = pattern.ReplaceAllString(text, gitRedacted)
	}
	return gitAssignPattern.ReplaceAllString(text, "${1}"+gitRedacted)
}

// ServerText renders err for a payload that leaves this machine. Every Git
// diagnostic in the error tree is replaced by the plain text of its Git error.
// When a Git error no longer appears verbatim in the text, because a caller
// wrapped it with %v or reworded it, the original text is never returned: it
// could still embed the diagnostic. The result is then the plain texts of the
// Git errors, one per line. An error flattened to a string before it reaches
// here (fmt.Errorf with %v, err.Error()) has left the tree and cannot be
// cleaned, so no code between gitCommand and a server payload may do that.
func ServerText(err error) string {
	if err == nil {
		return ""
	}
	var found []*gitError
	collectGitErrors(err, &found)
	text := err.Error()
	if len(found) == 0 {
		return text
	}
	// A longer Error() is replaced first so a plain text that is a prefix of
	// another Git error cannot leave that error's diagnostic behind.
	ordered := append([]*gitError(nil), found...)
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i].Error()) > len(ordered[j].Error()) })
	for _, git := range ordered {
		if !strings.Contains(text, git.Error()) {
			plain := make([]string, len(found))
			for i, entry := range found {
				plain[i] = entry.plain
			}
			return strings.Join(plain, "\n")
		}
		text = strings.ReplaceAll(text, git.Error(), git.plain)
	}
	return text
}

func collectGitErrors(err error, found *[]*gitError) {
	if git, ok := err.(*gitError); ok {
		*found = append(*found, git)
		return
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() error }:
		if inner := wrapped.Unwrap(); inner != nil {
			collectGitErrors(inner, found)
		}
	case interface{ Unwrap() []error }:
		for _, inner := range wrapped.Unwrap() {
			if inner != nil {
				collectGitErrors(inner, found)
			}
		}
	}
}
