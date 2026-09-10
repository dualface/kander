package ghcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/issue/ghcli/ghclitest"
)

func TestExecRunnerInvokesDirectArgvInValidatedDirectory(t *testing.T) {
	ghclitest.Install(t)
	record := filepath.Join(t.TempDir(), "invocations.jsonl")
	t.Setenv(ghclitest.RecordEnv, record)
	ghclitest.Set(t, "gh", ghclitest.Options{Stdout: `{"ok":true}`})

	workdir := t.TempDir()
	resolvedWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatal(err)
	}
	runner := &ExecRunner{Program: "gh"}
	argv := []string{"repo", "view", "dualface/kander", "--json", "nameWithOwner,url,isPrivate"}
	stdout, stderr, err := runner.Run(context.Background(), workdir, argv, DefaultStdoutLimit)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(stdout) != `{"ok":true}` {
		t.Fatalf("stdout=%q", stdout)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr=%q", stderr)
	}
	invocations := ghclitest.Record(t, record)
	if len(invocations) != 1 {
		t.Fatalf("invocations=%d want 1", len(invocations))
	}
	if invocations[0].Directory != resolvedWorkdir {
		t.Fatalf("directory=%q want %q", invocations[0].Directory, resolvedWorkdir)
	}
	if strings.Join(invocations[0].Argv, " ") != strings.Join(argv, " ") {
		t.Fatalf("argv=%v want %v", invocations[0].Argv, argv)
	}
}

func TestExecRunnerReportsMissingProgram(t *testing.T) {
	runner := &ExecRunner{Program: "kander-provider-does-not-exist", MissingKind: issue.ErrorCLIUnavailable}
	_, _, err := runner.Run(context.Background(), t.TempDir(), []string{"--version"}, DefaultStdoutLimit)
	if err == nil {
		t.Fatal("expected an error")
	}
	if issue.KindOf(err) != issue.ErrorCLIUnavailable {
		t.Fatalf("kind=%q want %q", issue.KindOf(err), issue.ErrorCLIUnavailable)
	}
}

func TestExecRunnerRejectsUnsafeDirectory(t *testing.T) {
	ghclitest.Install(t)
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &ExecRunner{Program: "gh"}
	if _, _, err := runner.Run(context.Background(), file, []string{"--version"}, DefaultStdoutLimit); issue.KindOf(err) != issue.ErrorInvalidDirectory {
		t.Fatalf("file directory: kind=%q err=%v", issue.KindOf(err), err)
	}
	if _, _, err := runner.Run(context.Background(), filepath.Join(t.TempDir(), "missing"), []string{"--version"}, DefaultStdoutLimit); issue.KindOf(err) != issue.ErrorInvalidDirectory {
		t.Fatalf("missing directory: kind=%q err=%v", issue.KindOf(err), err)
	}
	if _, _, err := runner.Run(context.Background(), "", []string{"--version"}, DefaultStdoutLimit); issue.KindOf(err) != issue.ErrorInvalidDirectory {
		t.Fatalf("empty directory: kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestExecRunnerBoundsStdout(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.Set(t, "gh", ghclitest.Options{Stdout: strings.Repeat("x", 4096), Repeat: 64})
	runner := &ExecRunner{Program: "gh"}
	stdout, _, err := runner.Run(context.Background(), t.TempDir(), []string{"--version"}, 1024)
	if issue.KindOf(err) != issue.ErrorOutputLimit {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if len(stdout) > 1024 {
		t.Fatalf("kept %d bytes, want at most 1024", len(stdout))
	}
}

func TestExecRunnerBoundsStderr(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.Set(t, "gh", ghclitest.Options{Stderr: strings.Repeat("e", 4096), Repeat: 64, Exit: 1})
	runner := &ExecRunner{Program: "gh", StderrLimit: 512}
	_, _, err := runner.Run(context.Background(), t.TempDir(), []string{"--version"}, DefaultStdoutLimit)
	if issue.KindOf(err) != issue.ErrorOutputLimit {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestExecRunnerTimesOut(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.Set(t, "gh", ghclitest.Options{Sleep: 30 * time.Second, Stdout: "late"})
	runner := &ExecRunner{Program: "gh", Timeout: 200 * time.Millisecond}
	started := time.Now()
	_, _, err := runner.Run(context.Background(), t.TempDir(), []string{"--version"}, DefaultStdoutLimit)
	if issue.KindOf(err) != issue.ErrorTimeout {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestExecRunnerRedactsTokensInDiagnostics(t *testing.T) {
	const token = "gho_abcdefghijklmnopqrstuvwxyz012345"
	ghclitest.Install(t)
	ghclitest.Set(t, "gh", ghclitest.Options{Stderr: "authentication failed for " + token, Exit: 1})
	runner := &ExecRunner{Program: "gh"}
	_, _, err := runner.Run(context.Background(), t.TempDir(), []string{"--version"}, DefaultStdoutLimit)
	if err == nil {
		t.Fatal("expected an error")
	}
	if issue.KindOf(err) != issue.ErrorCommandFailed {
		t.Fatalf("kind=%q", issue.KindOf(err))
	}
	message := err.Error()
	if strings.Contains(message, token) {
		t.Fatalf("error leaks the token: %s", message)
	}
	if !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("error has no redaction marker: %s", message)
	}
}

func TestExecRunnerReturnsInvalidUTF8Unchanged(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.Set(t, "gh", ghclitest.Options{InvalidUTF8: true})
	runner := &ExecRunner{Program: "gh"}
	stdout, _, err := runner.Run(context.Background(), t.TempDir(), []string{"--version"}, DefaultStdoutLimit)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if utf8.Valid(stdout) {
		t.Fatalf("expected invalid UTF-8, got %q", stdout)
	}
}
