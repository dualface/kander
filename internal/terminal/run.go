package terminal

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/dualface/kander/internal/probe"
)

// ProbeRunner bounds a command with the caller deadline or the default probe
// budget and owns its process tree.
func ProbeRunner(ctx context.Context, program string, args []string) (probe.Result, error) {
	return probe.CaptureContext(ctx, program, args)
}

// SpawnRunner runs a plain child process. It enforces only a deadline present
// on the context, by killing the child when it expires; a context without a
// deadline never times out.
func SpawnRunner(ctx context.Context, program string, args []string) (probe.Result, error) {
	cmd := exec.Command(program, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if deadline, ok := ctx.Deadline(); ok {
		if timeout := time.Until(deadline); timeout > 0 {
			timer := time.AfterFunc(timeout, func() { _ = cmd.Process.Kill() })
			defer timer.Stop()
		}
	}
	err := cmd.Run()
	res := probe.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.Code = exitErr.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// SpawnRunnerWithin returns a SpawnRunner that bounds every command it runs
// by timeout, independently of the other commands of the same operation.
func SpawnRunnerWithin(timeout time.Duration) Runner {
	return func(ctx context.Context, program string, args []string) (probe.Result, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return SpawnRunner(ctx, program, args)
	}
}

// VersionOutput runs a version command, bounded by five seconds, and returns
// its combined output.
func VersionOutput(path string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}
