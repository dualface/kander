package probe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Result is the exit code and output of one external program run.
type Result struct {
	Code   int
	Stdout string
	Stderr string
}

type runFunc func(context.Context, string, []string) (Result, error)

var runCommand runFunc = defaultRun

// DefaultCommandTimeout bounds probes without a caller-owned deadline.
const DefaultCommandTimeout = 10 * time.Second

// WithDefaultTimeout preserves a caller deadline, or supplies the default budget.
// The caller must cancel the returned context when its entire operation ends.
func WithDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, DefaultCommandTimeout)
}

func timeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func defaultRun(ctx context.Context, program string, args []string) (Result, error) {
	return captureWithEnv(ctx, program, args, nil)
}

// CaptureWithEnv runs an argv command with explicit environment and the same
// deadline and process-tree ownership as probes. A nil environment inherits.
func CaptureWithEnv(ctx context.Context, program string, args, env []string) (Result, error) {
	ctx, cancel := WithDefaultTimeout(ctx)
	defer cancel()
	return captureWithEnv(ctx, program, args, env)
}

func captureWithEnv(ctx context.Context, program string, args, env []string) (res Result, runErr error) {
	if err := ctx.Err(); err != nil {
		return res, err
	}
	cmd := exec.Command(program, args...)
	cmd.Env = env
	tree, err := newProcessTree(cmd)
	if err != nil {
		return res, err
	}
	defer func() { runErr = errors.Join(runErr, tree.close()) }()
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		return res, err
	}
	defer stdout.Close()
	defer stdoutWriter.Close()
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		return res, err
	}
	defer stderr.Close()
	defer stderrWriter.Close()
	cmd.Stdout, cmd.Stderr = stdoutWriter, stderrWriter
	if err := ctx.Err(); err != nil {
		return res, err
	}
	if err := tree.start(ctx); err != nil {
		if cmd.Process != nil {
			err = errors.Join(err, tree.kill())
			// Assignment/resume can fail before the Windows job owns the process.
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return res, err
	}
	// Only the child owns the write ends. Own the read ends so cancellation can
	// unblock even an escaped descendant retaining an inherited output handle.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	var out, diagnostic bytes.Buffer
	reads := make(chan error, 2)
	go func() { _, err := io.Copy(&out, stdout); reads <- err }()
	go func() { _, err := io.Copy(&diagnostic, stderr); reads <- err }()
	var killOnce sync.Once
	var killErr error
	kill := func() { killOnce.Do(func() { killErr = tree.kill() }) }
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(canceled)
		// Close first: pipe readers do not depend on process termination completing.
		_ = stdout.Close()
		_ = stderr.Close()
		kill()
	})
	// Wait reaps the direct child; it owns no output-copy goroutines because its
	// streams are *os.File. OS process creation/reaping itself is not interruptible.
	waitErr := cmd.Wait()
	kill()
	readErr := errors.Join(<-reads, <-reads)
	if !stop() {
		<-canceled
	}
	res.Stdout, res.Stderr = out.String(), diagnostic.String()
	if err := ctx.Err(); err != nil {
		return res, errors.Join(err, killErr)
	}
	if killErr != nil || readErr != nil {
		return res, errors.Join(killErr, readErr, waitErr)
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			res.Code = exitErr.ExitCode()
			return res, nil
		}
		return res, waitErr
	}
	return res, nil
}

func failureDetail(res Result) string {
	if s := strings.TrimSpace(res.Stderr); s != "" {
		return s
	}
	return "exit " + strconv.Itoa(res.Code)
}
