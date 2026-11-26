package remote

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

func expiredContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	return ctx
}

func canceledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

// requireTransportFailure checks one error of a Git process that did not run to
// its end: the cause class is reachable, the local text states the cause after
// the fixed text, and the text for the server is the fixed text alone.
func requireTransportFailure(t *testing.T, err, cause error) {
	t.Helper()
	if err == nil {
		t.Fatal("no error")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%q, %v) is false", err, cause)
	}
	var git *gitError
	if !errors.As(err, &git) {
		t.Fatalf("%T holds no *gitError", err)
	}
	plain := config.Text("remote.git_interrupted")
	if !strings.HasPrefix(err.Error(), plain) || len(err.Error()) <= len(plain) {
		t.Fatalf("local text %q does not state a cause after %q", err, plain)
	}
	if got := ServerText(err); got != plain {
		t.Fatalf("ServerText = %q, want %q", got, plain)
	}
}

func TestGitCommandTransportFailureKeepsTheCause(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		t.Run(lang, func(t *testing.T) {
			useLanguage(t, lang)
			dir := t.TempDir()
			_, err := gitCommand(expiredContext(t), dir, "version")
			requireTransportFailure(t, err, context.DeadlineExceeded)
			_, err = gitCommand(canceledContext(t), dir, "version")
			requireTransportFailure(t, err, context.Canceled)
			t.Setenv("PATH", t.TempDir())
			_, err = gitCommand(t.Context(), dir, "version")
			requireTransportFailure(t, err, exec.ErrNotFound)
		})
	}
}

func TestIntegrationGitCallsKeepTheTransportCause(t *testing.T) {
	useLanguage(t, "en")
	dir := t.TempDir()
	ctx := expiredContext(t)
	_, err := gitAncestor(ctx, dir, strings.Repeat("a", 40), strings.Repeat("b", 40))
	requireTransportFailure(t, err, context.DeadlineExceeded)
	_, err = gitStatus(ctx, dir, "version")
	requireTransportFailure(t, err, context.DeadlineExceeded)
	_, err = commitPatch(ctx, dir, strings.Repeat("a", 40))
	requireTransportFailure(t, err, context.DeadlineExceeded)
}
