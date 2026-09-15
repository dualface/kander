package review

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
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

func writeEvidence(ctx reviewContext, runtime, path string) error {
	commands := []struct {
		heading string
		args    []string
	}{
		{"\n=== COMMITS ===\n", []string{"log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", ctx.base + ".." + ctx.commit}},
		{"\n=== FILE LEDGER ===\n", []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", ctx.base + ".." + ctx.commit}},
		{"\n=== PATCH ===\n", []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--patch", ctx.base + ".." + ctx.commit}},
		{"\n=== COMMIT TREE ===\n", []string{"ls-tree", "-r", ctx.commit}},
	}
	if ctx.reviewed != "" {
		fixRange := ctx.reviewed + ".." + ctx.commit
		commands = append(commands,
			struct {
				heading string
				args    []string
			}{"\n=== FIX RANGE COMMITS ===\n", []string{"log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", fixRange}},
			struct {
				heading string
				args    []string
			}{"\n=== FIX RANGE FILE LEDGER ===\n", []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", fixRange}},
			struct {
				heading string
				args    []string
			}{"\n=== FIX RANGE PATCH ===\n", []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--patch", fixRange}},
		)
	}
	var b strings.Builder
	b.WriteString("Review range: " + ctx.base + ".." + ctx.commit + "\n")
	if ctx.reviewed != "" {
		b.WriteString("Incremental re-review; fix range: " + ctx.reviewed + ".." + ctx.commit + "\n")
	}
	for _, command := range commands {
		stdout, stderr, code, err := gitCommand(command.args, ctx.root, "")
		if err != nil {
			return err
		}
		if code != 0 {
			msg := strings.TrimSpace(stderr)
			if msg == "" {
				msg = "failed to create review evidence"
			}
			return newGateMsg(2, msg)
		}
		b.WriteString(command.heading)
		b.WriteString(stdout)
	}
	return fs.WriteTextAtomic(runtime, path, b.String(), false)
}

func targetIsUnchanged(ctx reviewContext) bool {
	stdout, _, code, err := gitCommand([]string{"rev-parse", "HEAD"}, ctx.root, "")
	if err != nil || code != 0 || strings.TrimSpace(stdout) != ctx.commit {
		return false
	}
	ok, status := gitStatus(ctx.root)
	return ok && status == ""
}
