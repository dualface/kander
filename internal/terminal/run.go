package terminal

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/probe"
)

// ProbeRunner bounds a command with the caller deadline or the default probe
// budget and owns its process tree.
func ProbeRunner(ctx context.Context, program string, args []string) (probe.Result, error) {
	return probe.CaptureContext(ctx, program, args)
}

// ProbeInputRunner executes a bounded request/response command with finite
// stdin. Four MiB per stream bounds full terminal-list responses.
func ProbeInputRunner(ctx context.Context, program string, args []string, input string) (probe.Result, error) {
	return probe.CaptureInput(ctx, program, args, input, 4<<20)
}

// ProbeEnvRunner runs a command like ProbeRunner, with every variable named in
// unset removed from the inherited environment. Names compare exactly, except
// on Windows where environment names are case-insensitive.
func ProbeEnvRunner(ctx context.Context, program string, args []string, unset []string) (probe.Result, error) {
	return probe.CaptureWithEnv(ctx, program, args, withoutEnv(os.Environ(), unset, runtime.GOOS == "windows"))
}

// withoutEnv returns environ without the variables in unset; foldCase makes
// the name comparison case-insensitive.
func withoutEnv(environ, unset []string, foldCase bool) []string {
	same := func(a, b string) bool { return a == b }
	if foldCase {
		same = strings.EqualFold
	}
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.ContainsFunc(unset, func(target string) bool { return same(name, target) }) {
			kept = append(kept, entry)
		}
	}
	return kept
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
