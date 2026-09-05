//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fakeClaude = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_CLAUDE_ARGV"
instruction=$(cat)
prompt=${instruction#*task file at }
prompt=${prompt%%; read the complete file first*}
cat "$prompt" > "$FAKE_CLAUDE_PROMPT"
pwd -P > "$FAKE_CLAUDE_CWD"
printf '%s\n' "$CLAUDE_CONFIG_DIR" > "$FAKE_CLAUDE_HOME"
if [ -n "${FAKE_CLAUDE_SPEC_SNAPSHOT_LOG:-}" ]; then
    spec_path=$(sed -n 's/^Authoritative spec file: \(.*\)\. Read it completely before reviewing\.$/\1/p' "$FAKE_CLAUDE_PROMPT")
    if [ -n "$spec_path" ]; then
        cat "$spec_path" > "$FAKE_CLAUDE_SPEC_SNAPSHOT_LOG"
    fi
fi
if [ -n "${FAKE_CLAUDE_TAMPER:-}" ]; then
    printf '%s\n' 'tampered' > "$FAKE_CLAUDE_TAMPER"
fi
if [ -n "${FAKE_CLAUDE_SLEEP:-}" ]; then
    sleep 30
fi
if [ -n "${FAKE_CLAUDE_DELAYED_TAMPER:-}" ]; then
    (sleep 1; printf '%s\n' 'escaped' > "$FAKE_CLAUDE_DELAYED_TAMPER") &
    printf '%s\n' "$!" > "$FAKE_CLAUDE_CHILD_PID"
fi
if [ -n "${FAKE_CLAUDE_FAIL:-}" ]; then
    printf '%s\n' 'fake claude failure' >&2
    exit 3
fi
if [ -n "${FAKE_CLAUDE_BAD_OUTPUT:-}" ]; then
    printf '%s\n' '{}'
else
    printf '{"type":"result","subtype":"success","is_error":false,"result":"%s"}\n' \
        "${FAKE_CLAUDE_REPORT:-REPORT BODY}"
fi
exit 0
`

func newClaudeHarness(t *testing.T) *reviewHarness {
	t.Helper()
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "claude")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-claude")
	h.argvLog = filepath.Join(root, "argv.log")
	h.promptLog = filepath.Join(root, "prompt.log")
	h.cwdLog = filepath.Join(root, "cwd.log")
	h.homeLog = filepath.Join(root, "home.log")
	h.specLog = filepath.Join(root, "spec-snapshot.log")
	writeFake(t, h.fake, fakeClaude)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "基线")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "改动")
	real, _ := filepath.EvalSymlinks(h.repo)
	h.repoReal = real
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	t.Setenv("CLAUDE_CONFIG_DIR", h.home)
	t.Setenv("CLAUDE_REVIEW_BIN", h.fake)
	t.Setenv("CLAUDE_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("CLAUDE_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_CLAUDE_ARGV", h.argvLog)
	t.Setenv("FAKE_CLAUDE_PROMPT", h.promptLog)
	t.Setenv("FAKE_CLAUDE_CWD", h.cwdLog)
	t.Setenv("FAKE_CLAUDE_HOME", h.homeLog)
	t.Setenv("FAKE_CLAUDE_SPEC_SNAPSHOT_LOG", h.specLog)
	t.Setenv("FAKE_CLAUDE_CHILD_PID", filepath.Join(root, "child.pid"))
	return h
}

func TestClaudeIsolationAndReport(t *testing.T) {
	h := newClaudeHarness(t)
	code, out, err := h.review("claude", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "REPORT BODY") {
		t.Fatalf("out=%q", out)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	assertArg(t, argv, "--permission-mode", "plan")
	assertArg(t, argv, "--tools", "Read,Grep,Glob")
	if !contains(argv, "--safe-mode") || !contains(argv, "--no-session-persistence") {
		t.Fatalf("argv=%v", argv)
	}
	assertArg(t, argv, "--add-dir", h.repoReal)
	runtime := strings.TrimSpace(readFile(t, h.cwdLog))
	if runtime == h.repoReal {
		t.Fatal("claude must run outside worktree")
	}
	if filepath.Dir(runtime) != h.tmp {
		t.Fatalf("runtime parent=%s", filepath.Dir(runtime))
	}
	if !strings.HasPrefix(filepath.Base(runtime), "claude-review.") {
		t.Fatalf("runtime=%s", runtime)
	}
}

func TestClaudeInterruptCollectsProcessGroup(t *testing.T) {
	h := newClaudeHarness(t)
	t.Setenv("FAKE_CLAUDE_SLEEP", "1")
	t.Setenv("CLAUDE_REVIEW_MAX_RUNTIME_SECONDS", "30")
	type result struct {
		code int
		err  string
	}
	ch := make(chan result, 1)
	go func() {
		code, _, err := h.review("claude", "QA", "确认改动正确")
		ch <- result{code, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(h.argvLog); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(h.argvLog); err != nil {
		t.Fatal("reviewer never started")
	}
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.code != 130 {
		t.Fatalf("code=%d err=%q", got.code, got.err)
	}
	entries, err := os.ReadDir(h.tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "claude-review.") {
			t.Fatalf("runtime leftover %s", e.Name())
		}
	}
}

func TestClaudeTimeout(t *testing.T) {
	h := newClaudeHarness(t)
	t.Setenv("FAKE_CLAUDE_SLEEP", "1")
	t.Setenv("CLAUDE_REVIEW_MAX_RUNTIME_SECONDS", "1")
	code, _, err := h.review("claude", "QA", "确认改动正确")
	if code != 124 || !strings.Contains(err, "Claude review exceeded 1 seconds") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestClaudeDelayedTamperRejected(t *testing.T) {
	h := newClaudeHarness(t)
	target := filepath.Join(h.repo, "escaped.txt")
	t.Setenv("FAKE_CLAUDE_DELAYED_TAMPER", target)
	code, _, err := h.review("claude", "QA", "确认改动正确")
	if code != 2 || !strings.Contains(err, "left background child processes") {
		t.Fatalf("code=%d err=%q", code, err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatal("tamper file should not exist")
	}
}

func TestClaudeIncompleteOutput(t *testing.T) {
	h := newClaudeHarness(t)
	t.Setenv("FAKE_CLAUDE_BAD_OUTPUT", "1")
	code, _, err := h.review("claude", "QA", "确认改动正确")
	if code != 1 || !strings.Contains(err, "did not complete with review text") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestClaudeSpecSnapshot(t *testing.T) {
	h := newClaudeHarness(t)
	specDir := filepath.Join(h.root, "specs")
	if err := os.Mkdir(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(specDir, "spec.md")
	if err := os.WriteFile(spec, []byte("# 任务契约\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, err := h.review("claude", "PM", spec)
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt := readFile(t, h.promptLog)
	if strings.Contains(prompt, spec) {
		t.Fatal("prompt must not name original spec path")
	}
	if readFile(t, h.specLog) != "# 任务契约\n" {
		t.Fatal("snapshot mismatch")
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	for i, a := range argv {
		if a == "--add-dir" && argv[i+1] != h.repoReal {
			t.Fatalf("unexpected add-dir %s", argv[i+1])
		}
	}
}

func TestClaudeIncremental(t *testing.T) {
	h := newClaudeHarness(t)
	fixed := commitFile(t, h.repo, "c.txt", "fix\n", "修复")
	code, _, err := captureRun(t, []string{"claude", h.repo, h.base, fixed, "PM", "确认改动正确", "Prior findings: PM-001 closed by c.txt", h.head})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt := readFile(t, h.promptLog)
	if !strings.Contains(prompt, "incremental re-review") {
		t.Fatal(prompt)
	}
}
