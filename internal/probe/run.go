package probe

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Result is the exit code and output of one external program run.
type Result struct {
	Code   int
	Stdout string
	Stderr string
}

type runFunc func(program string, args []string, timeout time.Duration) (Result, error)

var runCommand runFunc = defaultRun

// DefaultCommandTimeout bounds probes without a caller-owned deadline.
const DefaultCommandTimeout = 10 * time.Second

func defaultRun(program string, args []string, timeout time.Duration) (Result, error) {
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return res, ctx.Err()
		}
		if ee, ok := err.(*exec.ExitError); ok {
			res.Code = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

func failureDetail(res Result) string {
	if s := strings.TrimSpace(res.Stderr); s != "" {
		return s
	}
	return "exit " + strconv.Itoa(res.Code)
}
