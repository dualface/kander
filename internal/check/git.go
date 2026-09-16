package check

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	gitTimeout      = 60 * time.Second
	gitStdoutLimit  = 16 << 20
	gitStderrLimit  = 64 << 10
	gitWaitDelay    = 2 * time.Second
	gitOverallLimit = 2 * time.Minute
)

var (
	errOutputLimit = errors.New("git output limit exceeded")
	errGitTimeout  = errors.New("git command deadline exceeded")
)

type gitExec func(ctx context.Context, args ...string) (stdout, stderr []byte, code int, err error)

type gitRunner struct {
	exec gitExec
}

func newGitRunner(exec gitExec) gitRunner {
	if exec == nil {
		exec = defaultGitExec
	}
	return gitRunner{exec: exec}
}

func defaultGitExec(ctx context.Context, args ...string) ([]byte, []byte, int, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, nil, 0, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = gitCommandEnv()
	cmd.WaitDelay = gitWaitDelay
	stdout := &limitedBuffer{limit: gitStdoutLimit, cancel: cancel}
	stderr := &limitedBuffer{limit: gitStderrLimit, cancel: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	if stdout.overflowed || stderr.overflowed {
		return stdout.Bytes(), stderr.Bytes(), 0, errOutputLimit
	}
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.Bytes(), stderr.Bytes(), 0, errGitTimeout
	}
	if runErr == nil {
		return stdout.Bytes(), stderr.Bytes(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
	}
	return stdout.Bytes(), stderr.Bytes(), 0, runErr
}

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

func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func gitCommandEnv() []string {
	skip := func(key string) bool {
		switch strings.ToUpper(key) {
		case "GIT_OPTIONAL_LOCKS", "GIT_TERMINAL_PROMPT", "GIT_NO_LAZY_FETCH",
			"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE":
			return true
		default:
			return false
		}
	}
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if skip(key) {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_NO_LAZY_FETCH=1",
		"LC_ALL=C",
		"LANG=C",
		"LC_MESSAGES=C",
		"LANGUAGE=",
	)
}

func gitConfigArgs(args ...string) []string {
	prefix := []string{
		"-c", "core.fsmonitor=false",
		"-c", "core.quotepath=true",
		"-c", "diff.renames=copies",
	}
	return append(prefix, args...)
}

func (g gitRunner) run(ctx context.Context, args ...string) ([]byte, []byte, int, error) {
	return g.exec(ctx, gitConfigArgs(args...)...)
}

func (g gitRunner) resolve(ctx context.Context, ref string) (string, *CheckError) {
	stdout, stderr, code, err := g.run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if classified := classifyExec(err); classified != nil {
		return "", classified
	}
	if code != 0 {
		return "", classifyRefError(stderr, code)
	}
	sha := strings.TrimSpace(string(stdout))
	if sha == "" {
		return "", classifyRefError(stderr, code)
	}
	return sha, nil
}

func (g gitRunner) isAncestor(ctx context.Context, base, target string) *CheckError {
	_, stderr, code, err := g.run(ctx, "merge-base", "--is-ancestor", base, target)
	if classified := classifyExec(err); classified != nil {
		return classified
	}
	if code == 0 {
		return nil
	}
	if code == 1 {
		return &CheckError{
			Code:    errCodeNotAncestor,
			Message: t("check.not_ancestor", base, target),
		}
	}
	return classifyGitFailure(stderr, code)
}

func (g gitRunner) mergeBase(ctx context.Context, a, b string) (string, *CheckError) {
	stdout, stderr, code, err := g.run(ctx, "merge-base", a, b)
	if classified := classifyExec(err); classified != nil {
		return "", classified
	}
	if code != 0 {
		if code == 1 {
			return "", &CheckError{
				Code:    errCodeNoMergeBase,
				Message: t("check.no_merge_base", a, b),
			}
		}
		return "", classifyGitFailure(stderr, code)
	}
	sha := strings.TrimSpace(string(stdout))
	if sha == "" {
		return "", &CheckError{
			Code:    errCodeNoMergeBase,
			Message: t("check.no_merge_base", a, b),
		}
	}
	return sha, nil
}

func (g gitRunner) nameStatus(ctx context.Context, a, b string) ([]change, *CheckError) {
	stdout, stderr, code, err := g.run(ctx, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--find-copies-harder", "--name-status", "-z", a, b, "--")
	if classified := classifyExec(err); classified != nil {
		return nil, classified
	}
	if code != 0 {
		return nil, classifyGitFailure(stderr, code)
	}
	changes, parseErr := parseNameStatus(stdout)
	if parseErr != nil {
		return nil, &CheckError{Code: errCodeParse, Message: t("check.parse_truncated")}
	}
	return changes, nil
}

func (g gitRunner) diffCheck(ctx context.Context, a, b string) (DiffCheck, *CheckError) {
	stdout, stderr, code, err := g.run(ctx, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--check", a, b, "--")
	if classified := classifyExec(err); classified != nil {
		return DiffCheck{}, classified
	}
	if code >= 128 {
		return DiffCheck{}, classifyGitFailure(stderr, code)
	}
	result := DiffCheck{Status: statusPass, Diagnostics: emptyDiagnostics()}
	for line := range bytes.SplitSeq(stdout, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, string(line))
	}
	if code != 0 {
		result.Status = statusFail
	}
	return result, nil
}

func (g gitRunner) blob(ctx context.Context, commit string, path []byte) ([]byte, bool, *CheckError) {
	stdout, stderr, code, err := g.run(ctx, "--literal-pathspecs", "ls-tree", "-z", "--full-tree", commit, "--", string(path))
	if classified := classifyExec(err); classified != nil {
		return nil, false, classified
	}
	if code != 0 {
		return nil, false, classifyGitFailure(stderr, code)
	}
	objectType, objectID, ok := parseTreeEntry(stdout)
	if !ok {
		return nil, false, &CheckError{Code: errCodeParse, Message: t("check.parse_tree_entry")}
	}
	if objectType != "blob" {
		return nil, false, nil
	}
	stdout, stderr, code, err = g.run(ctx, "cat-file", "blob", objectID)
	if classified := classifyExec(err); classified != nil {
		return nil, false, classified
	}
	if code != 0 {
		return nil, false, classifyGitFailure(stderr, code)
	}
	return stdout, true, nil
}

func parseTreeEntry(data []byte) (objectType, objectID string, ok bool) {
	if len(data) < 2 || data[len(data)-1] != 0 || bytes.IndexByte(data[:len(data)-1], 0) >= 0 {
		return "", "", false
	}
	record := data[:len(data)-1]
	tab := bytes.IndexByte(record, '\t')
	if tab < 0 {
		return "", "", false
	}
	fields := bytes.Fields(record[:tab])
	if len(fields) != 3 {
		return "", "", false
	}
	return string(fields[1]), string(fields[2]), true
}

func classifyExec(err error) *CheckError {
	if err == nil {
		return nil
	}
	if errors.Is(err, errOutputLimit) {
		return &CheckError{Code: errCodeOutputLimit, Message: t("check.output_limit")}
	}
	if errors.Is(err, errGitTimeout) {
		return &CheckError{Code: errCodeInternal, Message: t("check.internal")}
	}
	if errors.Is(err, exec.ErrNotFound) || isPathError(err) {
		return &CheckError{Code: errCodeGitUnavailable, Message: t("check.git_unavailable")}
	}
	return &CheckError{Code: errCodeInternal, Message: cleanMessage(err.Error())}
}

func isPathError(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr)
}

func classifyRefError(stderr []byte, code int) *CheckError {
	failure := classifyGitFailure(stderr, code)
	if failure.Code == errCodeNotRepository {
		return failure
	}
	return &CheckError{
		Code:    errCodeInvalidRef,
		Message: t("check.invalid_ref"),
	}
}

func classifyGitFailure(stderr []byte, code int) *CheckError {
	text := strings.ToLower(string(stderr))
	switch {
	case strings.Contains(text, "not a git repository"):
		return &CheckError{Code: errCodeNotRepository, Message: t("check.not_repository")}
	case code >= 128 && looksLikeInvalidRef(text):
		return &CheckError{Code: errCodeInvalidRef, Message: t("check.invalid_ref")}
	default:
		message := cleanMessage(string(stderr))
		if message == "" {
			message = t("check.internal")
		}
		return &CheckError{Code: errCodeInternal, Message: message}
	}
}

func looksLikeInvalidRef(text string) bool {
	return strings.Contains(text, "needed a single revision") ||
		strings.Contains(text, "bad revision") ||
		strings.Contains(text, "ambiguous argument") ||
		strings.Contains(text, "unknown revision") ||
		strings.Contains(text, "invalid object name") ||
		strings.Contains(text, "not a valid object name")
}
