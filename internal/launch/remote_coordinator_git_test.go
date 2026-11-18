package launch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// verbWrapper formats its cause with %v and still exposes it through Unwrap.
type verbWrapper struct{ err error }

func (w verbWrapper) Error() string { return fmt.Sprintf("verify delivery: %v", w.err) }
func (w verbWrapper) Unwrap() error { return w.err }

// rewordedError replaces the text of its cause, so the cause text is not a
// substring of the outer text.
type rewordedError struct{ err error }

func (w rewordedError) Error() string { return "the delivery cannot be used" }
func (w rewordedError) Unwrap() error { return w.err }

// TestCoordinatorGitCheckCarriesNoGitDiagnostic proves the verdict that leaves
// this machine in a reconcile request has the text of an error without Git
// diagnostics, for every way a caller can wrap a Git error.
func TestCoordinatorGitCheckCarriesNoGitDiagnostic(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	// The repository has no origin, so the first Git call of FetchCommit fails
	// with a message that names the missing remote.
	gitErr := (&remote.Session{}).FetchCommit(t.Context(), dir, strings.Repeat("a", 40))
	if gitErr == nil {
		t.Fatal("FetchCommit succeeded without a remote")
	}
	plain := config.Text("remote.git_unverified")
	_, diagnostic, found := strings.Cut(gitErr.Error(), "Git reported: ")
	if !found || diagnostic == "" || !strings.HasPrefix(gitErr.Error(), plain) {
		t.Fatalf("local error %q lacks a diagnostic after %q", gitErr, plain)
	}

	other := errors.New("recovery required")
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"direct":        {gitErr, plain},
		"joined":        {errors.Join(gitErr, other), plain + "\n" + other.Error()},
		"wrapped":       {fmt.Errorf("verify delivery: %w", gitErr), "verify delivery: " + plain},
		"%v wrapper":    {verbWrapper{gitErr}, "verify delivery: " + plain},
		"reworded":      {rewordedError{gitErr}, plain},
		"no Git error":  {other, other.Error()},
		"joined reword": {errors.Join(rewordedError{gitErr}, other), plain},
	} {
		t.Run(name, func(t *testing.T) {
			check := coordinatorGitCheck(board.CoordinatorGitFacts{}, tc.err)
			if check.Error != tc.want {
				t.Fatalf("Error = %q, want %q", check.Error, tc.want)
			}
			raw, err := json.Marshal(check)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), diagnostic) {
				t.Fatalf("request %s holds the Git diagnostic %q", raw, diagnostic)
			}
		})
	}
	if check := coordinatorGitCheck(board.CoordinatorGitFacts{}, nil); check.Error != "" {
		t.Fatalf("a passing check carries %q", check.Error)
	}
}
