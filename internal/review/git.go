package review

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

func gitCommand(arguments []string, cwd, inputText string) (stdout, stderr string, code int, err error) {
	return gitCommandContext(context.Background(), arguments, cwd, inputText)
}

func gitCommandContext(ctx context.Context, arguments []string, cwd, inputText string) (stdout, stderr string, code int, err error) {
	cmd := exec.CommandContext(ctx, "git", arguments...)
	cmd.WaitDelay = 100 * time.Millisecond
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = strings.NewReader(inputText)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr == nil {
		return stdout, stderr, 0, nil
	}
	if ee, ok := runErr.(*exec.ExitError); ok {
		return stdout, stderr, ee.ExitCode(), nil
	}
	return stdout, stderr, 0, newGate(2, "review.could_not_run_git", runErr.Error())
}

func gitStatus(root string) (bool, string) {
	stdout, _, code, err := gitCommand([]string{
		"-c", "core.fsmonitor=false",
		"status",
		"--porcelain=v1",
		"--untracked-files=all",
		"--ignore-submodules=none",
	}, root, "")
	if err != nil {
		return false, ""
	}
	return code == 0, strings.TrimRight(stdout, "\r\n")
}

func pathsOverlap(first, second string) bool {
	left, err1 := filepath.EvalSymlinks(first)
	right, err2 := filepath.EvalSymlinks(second)
	if err1 != nil {
		left = first
	}
	if err2 != nil {
		right = second
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtimeEqualFold() {
		left = strings.ToLower(left)
		right = strings.ToLower(right)
	}
	rel, err := filepath.Rel(left, right)
	if err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
		return true
	}
	rel, err = filepath.Rel(right, left)
	if err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
		return true
	}
	return false
}

func runtimeEqualFold() bool {
	return os.PathSeparator == '\\'
}

func writeEvidence(ctx reviewContext, runtime, path string) (string, error) {
	var out strings.Builder
	out.WriteString("Review range: " + ctx.base + ".." + ctx.commit + "\n")
	type revisionRange struct{ from, prefix string }
	ranges := []revisionRange{{ctx.base, ""}}
	if ctx.reviewed != "" {
		out.WriteString("Incremental re-review; fix range: " + ctx.reviewed + ".." + ctx.commit + "\n")
		ranges = append(ranges, revisionRange{ctx.reviewed, "FIX RANGE "})
	}
	out.WriteString("Diffs are not included. Inspect them with read-only Git in the review worktree; see the review contract.\n")

	var largestName string
	var largestBytes int
	for _, interval := range ranges {
		revisions := interval.from + ".." + ctx.commit
		for _, section := range []struct {
			name string
			args []string
		}{
			{"COMMITS", []string{"log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", revisions}},
			{"FILE LEDGER", []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", revisions}},
		} {
			content, err := evidenceGit(ctx.root, section.args)
			if err != nil {
				return "", err
			}
			name := interval.prefix + section.name
			out.WriteString("\n=== " + name + " ===\n")
			out.WriteString(content)
			if len(content) > largestBytes {
				largestName, largestBytes = name, len(content)
			}
		}
	}
	largestSection := ""
	if largestBytes > 0 {
		largestSection = fmt.Sprintf("%s (%d bytes)", largestName, largestBytes)
	}
	return largestSection, fs.WriteTextAtomic(runtime, path, out.String(), false)
}

func evidenceGit(root string, args []string) (string, error) {
	stdout, stderr, code, err := gitCommand(args, root, "")
	if err != nil {
		return "", err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = "failed to create review evidence"
		}
		return "", newGateMsg(2, msg)
	}
	return stdout, nil
}

func targetIsUnchanged(ctx reviewContext) bool {
	stdout, _, code, err := gitCommand([]string{"rev-parse", "HEAD"}, ctx.root, "")
	if err != nil || code != 0 || strings.TrimSpace(stdout) != ctx.commit {
		return false
	}
	ok, status := gitStatus(ctx.root)
	return ok && status == ""
}
