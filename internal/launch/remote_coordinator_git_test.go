package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
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
	// Git translates its messages, and the diagnostic below is matched by its
	// English wording.
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANGUAGE", "C")
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
	// The language of the catalog is process-wide state another test may have
	// fixed, so the diagnostic is identified by Git's own wording.
	const diagnostic = "No such remote 'origin'"
	if !strings.HasPrefix(gitErr.Error(), plain) || !strings.Contains(gitErr.Error(), diagnostic) {
		t.Fatalf("local error %q lacks the diagnostic %q after %q", gitErr, diagnostic, plain)
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

// TestCoordinatorGitCheckCarriesNoTransportCause proves a Git process that did
// not run to its end is local text with its cause and server text without it.
func TestCoordinatorGitCheckCarriesNoTransportCause(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	// A valid SHA makes the first Git call of FetchCommit run under ctx.
	err := (&remote.Session{}).FetchCommit(ctx, t.TempDir(), strings.Repeat("a", 40))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("FetchCommit error %v is not a deadline failure", err)
	}
	plain := config.Text("remote.git_interrupted")
	if !strings.HasPrefix(err.Error(), plain) || len(err.Error()) <= len(plain) {
		t.Fatalf("local text %q does not state a cause after %q", err, plain)
	}
	if check := coordinatorGitCheck(board.CoordinatorGitFacts{}, err); check.Error != plain {
		t.Fatalf("Error = %q, want %q", check.Error, plain)
	}
}
