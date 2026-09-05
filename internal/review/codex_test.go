//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestMissingArgsReportsUsage(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := captureRun(t, nil)
	if code != 2 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(err, "Usage: kander review") {
		t.Fatalf("stderr=%q", err)
	}
	_ = h
}

func TestUnsupportedAgentRejected(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := captureRun(t, []string{"other", h.repo, h.base, h.head, "QA", "目标"})
	if code != 2 || !strings.Contains(err, "unsupported reviewer agent") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestUnsupportedRoleRejected(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := h.review("codex", "Architect", "目标")
	if code != 2 || !strings.Contains(err, "unsupported role") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestRelativeCWDRejected(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := captureRun(t, []string{"codex", "repo", h.base, h.head, "QA", "目标"})
	if code != 2 || !strings.Contains(err, "CWD must be an absolute path") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestPathOutsideGitRejected(t *testing.T) {
	h := newCodexHarness(t)
	outside := filepath.Join(h.root, "plain")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, err := captureRun(t, []string{"codex", outside, h.base, h.head, "QA", "目标"})
	if code != 2 || !strings.Contains(err, "not inside a Git worktree") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestEmptyTaskGoalRejected(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := h.review("codex", "QA", "")
	if code != 2 || !strings.Contains(err, "task goal must not be empty") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestUnreadableSpecRejected(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := h.review("codex", "PM", filepath.Join(h.root, "missing.md"))
	if code != 2 || !strings.Contains(err, "spec path is not a readable file") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestWindowsStyleAbsoluteIsTaskGoalOnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows-style path is absolute on windows")
	}
	h := newCodexHarness(t)
	goal := `C:\release`
	code, _, err := h.review("codex", "PM", goal)
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt, _ := os.ReadFile(h.stdinLog)
	if !strings.Contains(string(prompt), "Authoritative task goal: "+goal) {
		t.Fatalf("prompt=%s", prompt)
	}
}

func TestAbbreviatedSHARejected(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := captureRun(t, []string{"codex", h.repo, h.base[:8], h.head, "QA", "目标"})
	if code != 2 || !strings.Contains(err, "must be a full commit SHA") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestBaseNotAncestorRejected(t *testing.T) {
	h := newCodexHarness(t)
	gitRepo(t, h.repo, "checkout", "-q", "-b", "side", h.base)
	sibling := commitFile(t, h.repo, "c.txt", "side\n", "旁支")
	gitRepo(t, h.repo, "checkout", "-q", "main")
	code, _, err := captureRun(t, []string{"codex", h.repo, sibling, h.head, "QA", "目标"})
	if code != 2 || !strings.Contains(err, "not an ancestor") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestHEADMismatchRejected(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := captureRun(t, []string{"codex", h.repo, h.base, h.base, "QA", "目标"})
	if code != 2 || !strings.Contains(err, "HEAD does not match commit") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestUntrackedBlocksReview(t *testing.T) {
	h := newCodexHarness(t)
	if err := os.WriteFile(filepath.Join(h.repo, "scratch.txt"), []byte("未提交\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, err := h.defaultReview()
	if code != 2 || !strings.Contains(err, "uncommitted or untracked changes") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestGitStatusFailureRejected(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("GIT_INDEX_FILE", h.fake)
	code, _, err := h.defaultReview()
	if code != 2 || !strings.Contains(err, "failed to inspect worktree status") {
		t.Fatalf("code=%d err=%q", code, err)
	}
	if _, statErr := os.Stat(h.argvLog); !os.IsNotExist(statErr) {
		t.Fatal("gate must not launch reviewer")
	}
}

func TestWorktreeInsideCodexHomeRejected(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("CODEX_HOME", h.root)
	code, _, err := h.defaultReview()
	if code != 2 || !strings.Contains(err, "overlaps a Codex-writable directory") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestMissingBinaryReports127(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("CODEX_REVIEW_BIN", filepath.Join(h.root, "absent"))
	code, _, err := h.defaultReview()
	if code != 127 || !strings.Contains(err, "Codex CLI is unavailable") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestCodexFailurePropagated(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("FAKE_CODEX_FAIL", "1")
	code, _, err := h.defaultReview()
	if code != 3 || !strings.Contains(err, "fake codex failure") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestWorktreeTamperingDetected(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("FAKE_CODEX_TAMPER", filepath.Join(h.repo, "injected.txt"))
	code, _, err := h.defaultReview()
	if code != 2 || !strings.Contains(err, "modified the target worktree") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestCleanReviewReturnsReport(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("FAKE_CODEX_REPORT", "QA-1 没有发现问题")
	code, out, err := h.defaultReview()
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "QA-1 没有发现问题") {
		t.Fatalf("out=%q", out)
	}
}

func TestCodexIsolationFlags(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := h.defaultReview()
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	if argv[0] != "exec" {
		t.Fatalf("argv0=%q", argv[0])
	}
	assertArg(t, argv, "--sandbox", "read-only")
	if !contains(argv, "--ephemeral") {
		t.Fatal("missing --ephemeral")
	}
	assertArg(t, argv, "--cd", h.repoReal)
	if !contains(argv, "--model") {
		t.Fatal("missing --model")
	}
}

func TestModelConfigAndEnvOverride(t *testing.T) {
	h := newCodexHarness(t)
	payload, _ := json.Marshal(map[string]any{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "codex", "launcher": "tmux",
		"reviewers": map[string]string{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
		"models":    map[string]any{"review": map[string]any{"codex": map[string]string{"model": "config-model", "effort": "medium"}}},
	})
	if err := os.WriteFile(os.Getenv("KANDER_CONFIG"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, err := h.defaultReview()
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	assertArg(t, argv, "--model", "config-model")
	if !contains(argv, `model_reasoning_effort="medium"`) {
		t.Fatalf("argv=%v", argv)
	}
	t.Setenv("CODEX_REVIEW_MODEL", "env-model")
	code, _, err = h.defaultReview()
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv = strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	assertArg(t, argv, "--model", "env-model")
}

func TestMalformedModelConfigFallsBack(t *testing.T) {
	h := newCodexHarness(t)
	if err := os.WriteFile(os.Getenv("KANDER_CONFIG"), []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, err := h.defaultReview()
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	assertArg(t, argv, "--model", "gpt-5.6-sol")
	if !contains(argv, `model_reasoning_effort="high"`) {
		t.Fatalf("argv=%v", argv)
	}
}

func TestEmptyConfigModelOmitsFlag(t *testing.T) {
	h := newCodexHarness(t)
	payload, _ := json.Marshal(map[string]any{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "codex", "launcher": "tmux",
		"reviewers": map[string]string{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
		"models":    map[string]any{"review": map[string]any{"codex": map[string]string{"model": ""}}},
	})
	if err := os.WriteFile(os.Getenv("KANDER_CONFIG"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, err := h.defaultReview()
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	if contains(argv, "--model") {
		t.Fatalf("unexpected --model: %v", argv)
	}
	if !contains(argv, `model_reasoning_effort="high"`) {
		t.Fatalf("argv=%v", argv)
	}
}

func TestPromptCarriesRoleAndQARules(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := h.defaultReview()
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt := readFile(t, h.stdinLog)
	normalized := strings.Join(strings.Fields(prompt), " ")
	for _, needle := range []string{
		"You are the QA review agent",
		"Authoritative task goal: 确认改动正确",
		h.base + ".." + h.head,
		"Tag each such item with [mechanical]",
		"Do not modify files",
		"existing module responsibilities, dependency directions",
		"a non-generated code file must not exceed 1000 physical lines",
		"Do not enumerate contrived combinations, extreme edge cases",
	} {
		if !strings.Contains(normalized, needle) && !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q", needle)
		}
	}
}

func TestSpecFileAuthoritative(t *testing.T) {
	h := newCodexHarness(t)
	spec := filepath.Join(h.root, "spec.md")
	if err := os.WriteFile(spec, []byte("# 任务契约\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(spec)
	abs, _ := filepath.Abs(resolved)
	code, _, err := h.review("codex", "PM", spec)
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt := readFile(t, h.stdinLog)
	if !strings.Contains(prompt, "Authoritative spec file: "+abs) {
		t.Fatalf("prompt=%s", prompt)
	}
	if !strings.Contains(prompt, "suggest tagged [out-of-contract]") {
		t.Fatal("missing out-of-contract rule")
	}
}

func TestIncrementalReReview(t *testing.T) {
	h := newCodexHarness(t)
	fixed := commitFile(t, h.repo, "c.txt", "fix\n", "修复")
	code, _, err := captureRun(t, []string{
		"codex", h.repo, h.base, fixed, "PM", "确认改动正确",
		"Prior findings: PM-001 closed by c.txt", h.head,
	})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt := readFile(t, h.stdinLog)
	if !strings.Contains(prompt, "incremental re-review") {
		t.Fatal(prompt)
	}
	if !strings.Contains(prompt, "fix range "+h.head+".."+fixed) {
		t.Fatal(prompt)
	}
	if !strings.Contains(prompt, "Prior findings: PM-001 closed by c.txt") {
		t.Fatal(prompt)
	}
	if strings.Contains(prompt, "Review the complete code state") {
		t.Fatal("full review prompt leaked")
	}
}

func TestEmptyReviewedKeepsFullReview(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := captureRun(t, []string{"codex", h.repo, h.base, h.head, "PM", "确认改动正确", "", ""})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt := readFile(t, h.stdinLog)
	if !strings.Contains(prompt, "Review the complete code state") {
		t.Fatal(prompt)
	}
	if strings.Contains(prompt, "incremental re-review") {
		t.Fatal(prompt)
	}
	if !strings.Contains(prompt, "Additional caller-supplied review context: None provided.") {
		t.Fatal(prompt)
	}
}

func TestReviewedCommitMustSitBetween(t *testing.T) {
	h := newCodexHarness(t)
	fixed := commitFile(t, h.repo, "c.txt", "fix\n", "修复")
	gitRepo(t, h.repo, "checkout", "-q", "-b", "other", h.base)
	stray := commitFile(t, h.repo, "d.txt", "stray\n", "无关")
	gitRepo(t, h.repo, "checkout", "-q", "main")
	cases := []struct {
		reviewed, msg string
	}{
		{fixed, "must differ from base-commit and commit"},
		{h.base, "must differ from base-commit and commit"},
		{stray, "reviewed-commit is not an ancestor of commit"},
		{h.head[:12], "must be a full commit SHA"},
	}
	for _, tc := range cases {
		_ = os.Remove(h.argvLog)
		code, _, err := captureRun(t, []string{
			"codex", h.repo, h.base, fixed, "PM", "确认改动正确",
			"Prior findings: PM-001 open", tc.reviewed,
		})
		if code != 2 || !strings.Contains(err, tc.msg) {
			t.Fatalf("reviewed=%s code=%d err=%q want %q", tc.reviewed, code, err, tc.msg)
		}
		if _, statErr := os.Stat(h.argvLog); !os.IsNotExist(statErr) {
			t.Fatal("must not launch")
		}
	}
}

func TestIncrementalRequiresLedger(t *testing.T) {
	h := newCodexHarness(t)
	fixed := commitFile(t, h.repo, "c.txt", "fix\n", "修复")
	for _, ctx := range []string{"", "   ", "\n\t"} {
		_ = os.Remove(h.argvLog)
		code, _, err := captureRun(t, []string{
			"codex", h.repo, h.base, fixed, "PM", "确认改动正确", ctx, h.head,
		})
		if code != 2 || !strings.Contains(err, "requires the prior-finding ledger") {
			t.Fatalf("ctx=%q code=%d err=%q", ctx, code, err)
		}
	}
}

func TestIgnoredFilesDoesNotBlock(t *testing.T) {
	h := newCodexHarness(t)
	if err := os.WriteFile(filepath.Join(h.repo, ".gitignore"), []byte(".cache/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, h.repo, "add", ".gitignore")
	gitRepo(t, h.repo, "commit", "-q", "-m", "ignore cache")
	h.head = gitRepo(t, h.repo, "rev-parse", "HEAD")
	if err := os.Mkdir(filepath.Join(h.repo, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.repo, ".cache", "note.md"), []byte("cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, err := captureRun(t, []string{"codex", h.repo, h.base, h.head, "QA", "确认改动正确"})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
}

func TestDefaultLocaleReportsChineseUsage(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("KANDER_LANG", "")
	t.Setenv("KANDER_LANG_CLI", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	config.BindConfigLanguage(nil)
	code, _, err := captureRun(t, nil)
	if code != 2 || !strings.Contains(err, "用法: kander review") {
		t.Fatalf("code=%d err=%q", code, err)
	}
	_ = h
}

func TestTooManyArgsUsage(t *testing.T) {
	h := newCodexHarness(t)
	code, _, err := captureRun(t, []string{
		"codex", h.repo, h.base, h.head, "PM", "确认改动正确", "", h.base, "extra",
	})
	if code != 2 || !strings.Contains(err, "[reviewed-commit]") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestImplicitAgentFromConfig(t *testing.T) {
	h := newCodexHarness(t)
	payload, _ := json.Marshal(map[string]any{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "codex", "launcher": "tmux",
		"reviewers": map[string]string{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
	})
	if err := os.WriteFile(os.Getenv("KANDER_CONFIG"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, err := captureRun(t, []string{h.repo, h.base, h.head, "QA", "确认改动正确"})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "REPORT BODY") {
		t.Fatalf("out=%q", out)
	}
}
