// Package ghcli implements the GitHub CLI backed repository provider. It owns
// the exact process boundary to `gh` and to the `git` remote probe: direct argv
// arrays without a shell, a validated working directory, a deadline, bounded
// stdout/stderr, strict UTF-8 and JSON decoding, and redacted diagnostics.
package ghcli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/issue"
)

// Provider command defaults. Every one of them can be lowered by Options and is
// never raised by user input.
const (
	DefaultTimeout     = 30 * time.Second
	DefaultStdoutLimit = 1 << 20
	DefaultStderrLimit = 64 << 10
	probeWaitDelay     = 2 * time.Second
)

// CommandRunner runs one provider command and returns its captured streams.
// The returned error is already structured with an issue.ErrorKind.
type CommandRunner interface {
	Run(ctx context.Context, directory string, argv []string, stdoutLimit int64) (stdout []byte, stderr []byte, err error)
}

// ExecRunner is the production CommandRunner. It resolves one executable,
// invokes it with an argv array and no shell, pins the working directory,
// bounds the runtime and both output streams, and never writes a token into an
// argument or the environment.
type ExecRunner struct {
	Program     string
	MissingKind issue.ErrorKind
	Timeout     time.Duration
	StdoutLimit int64
	StderrLimit int64
}

var errOutputLimit = errors.New("provider output limit exceeded")

// Run implements CommandRunner.
func (r *ExecRunner) Run(ctx context.Context, directory string, argv []string, stdoutLimit int64) ([]byte, []byte, error) {
	program := strings.TrimSpace(r.Program)
	if program == "" {
		program = "gh"
	}
	missing := r.MissingKind
	if missing == "" {
		missing = issue.ErrorCLIUnavailable
	}
	path, err := resolveProgram(program)
	if err != nil {
		if errors.Is(err, errBatchWrapper) {
			return nil, nil, issue.WrapError(err, issue.ErrorCLIUnsupported, program, issue.Sanitize(err.Error()))
		}
		return nil, nil, issue.WrapError(err, missing, program, issue.Sanitize(err.Error()))
	}
	dir, err := validatedDirectory(directory)
	if err != nil {
		return nil, nil, err
	}
	if len(argv) == 0 {
		return nil, nil, issue.NewError(issue.ErrorCommandFailed, program, "empty argument list")
	}
	stdout, stderr, err := runCommand(ctx, r, path, dir, argv, stdoutLimit)
	return stdout, stderr, err
}

func runCommand(ctx context.Context, runner *ExecRunner, path, dir string, argv []string, stdoutLimit int64) ([]byte, []byte, error) {
	limit := stdoutLimit
	if limit <= 0 {
		limit = runner.StdoutLimit
	}
	if limit <= 0 {
		limit = DefaultStdoutLimit
	}
	stderrLimit := runner.StderrLimit
	if stderrLimit <= 0 {
		stderrLimit = DefaultStderrLimit
	}
	timeout := runner.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(ctx, path, argv...)
	command.Dir = dir
	// The provider inherits the user's environment because gh resolves its
	// credentials from it; Kander adds no variable of its own.
	command.Env = os.Environ()
	command.WaitDelay = probeWaitDelay
	stdout := &limitedBuffer{limit: limit, cancel: cancel}
	stderr := &limitedBuffer{limit: stderrLimit, cancel: cancel}
	command.Stdout = stdout
	command.Stderr = stderr

	runErr := command.Run()
	if stdout.overflowed || stderr.overflowed {
		return stdout.Bytes(), stderr.Bytes(), issue.NewError(issue.ErrorOutputLimit, path, "output limit exceeded")
	}
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.Bytes(), stderr.Bytes(), issue.WrapError(ctx.Err(), issue.ErrorTimeout, path, "deadline exceeded")
	}
	if runErr != nil {
		detail := issue.Sanitize(stderr.String())
		if detail == "" {
			detail = issue.Sanitize(runErr.Error())
		}
		return stdout.Bytes(), stderr.Bytes(), issue.WrapError(runErr, issue.ErrorCommandFailed, path, detail)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

// limitedBuffer keeps at most limit bytes and cancels the command once more
// arrives, so a hostile provider cannot exhaust memory or the user's terminal.
type limitedBuffer struct {
	limit      int64
	buffer     bytes.Buffer
	overflowed bool
	cancel     context.CancelFunc
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.overflowed = true
		b.cancel()
		return 0, errOutputLimit
	}
	if int64(len(data)) > remaining {
		written, err := b.buffer.Write(data[:remaining])
		if err != nil {
			return written, err
		}
		b.overflowed = true
		b.cancel()
		return written, errOutputLimit
	}
	return b.buffer.Write(data)
}

func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }

var errBatchWrapper = errors.New("batch wrapper executables are not supported")

func resolveProgram(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = absolute
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".cmd", ".bat":
			return "", errBatchWrapper
		}
	}
	return path, nil
}

func validatedDirectory(directory string) (string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", issue.NewError(issue.ErrorInvalidDirectory, "directory", "empty")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", issue.WrapError(err, issue.ErrorInvalidDirectory, "directory", issue.Sanitize(err.Error()))
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", issue.WrapError(err, issue.ErrorInvalidDirectory, "directory", issue.Sanitize(err.Error()))
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", issue.WrapError(err, issue.ErrorInvalidDirectory, "directory", issue.Sanitize(err.Error()))
	}
	if !info.IsDir() {
		return "", issue.NewError(issue.ErrorInvalidDirectory, "directory", issue.Sanitize(resolved))
	}
	if fs.IsReparsePoint(resolved) {
		return "", issue.NewError(issue.ErrorInvalidDirectory, "directory", issue.Sanitize(resolved))
	}
	return resolved, nil
}
