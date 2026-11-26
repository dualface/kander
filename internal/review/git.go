package review

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
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

func writeEvidence(ctx reviewContext, runtime, path string) (evidenceResult, error) {
	intro := "Review range: " + ctx.base + ".." + ctx.commit + "\n"
	if ctx.reviewed != "" {
		intro += "Incremental re-review; fix range: " + ctx.reviewed + ".." + ctx.commit + "\n"
	}

	run := func(args []string) (string, error) { return evidenceGit(ctx.root, args) }
	baseSections, err := evidenceRange(run, ctx.base, ctx.commit, "")
	if err != nil {
		return evidenceResult{}, err
	}
	tree, err := evidenceGit(ctx.root, []string{"ls-tree", "-r", ctx.commit})
	if err != nil {
		return evidenceResult{}, err
	}
	sections := append(baseSections, evidenceSection{name: "COMMIT TREE", text: tree})
	if ctx.reviewed != "" {
		fixSections, err := evidenceRange(run, ctx.reviewed, ctx.commit, "FIX RANGE ")
		if err != nil {
			return evidenceResult{}, err
		}
		sections = append(sections, fixSections...)
	}
	text, result := renderEvidence(intro, sections, evidenceLimits{
		file: evidenceFileLimit, total: evidenceTotalLimit, hard: evidenceHardLimit,
	})
	return result, fs.WriteTextAtomic(runtime, path, text, false)
}

func evidenceRange(run func([]string) (string, error), from, to, prefix string) ([]evidenceSection, error) {
	revisions := from + ".." + to
	log, err := run([]string{"log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", revisions})
	if err != nil {
		return nil, err
	}
	ledger, err := run(evidenceDiffArgs("--name-status", revisions))
	if err != nil {
		return nil, err
	}
	text, err := run(evidenceDiffArgs("--patch", revisions))
	if err != nil {
		return nil, err
	}
	raw, err := run(evidenceDiffArgs("--raw", "-z", "--no-abbrev", revisions))
	if err != nil {
		return nil, err
	}
	numstat, err := run(evidenceDiffArgs("--numstat", "-z", revisions))
	if err != nil {
		return nil, err
	}
	section := prefix + "PATCH"
	patch, warning, blocks, entries := collectEvidencePatch(section, from, to, text, raw, numstat)
	if warning != "" {
		command.Notify("review_evidence_metadata_unavailable", config.Text("review.evidence_metadata_unavailable", section, blocks, entries))
	}
	return []evidenceSection{
		{name: prefix + "COMMITS", text: log},
		{name: prefix + "FILE LEDGER", text: ledger},
		{name: section, patch: patch},
	}, nil
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
