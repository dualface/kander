//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeGrok = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_GROK_ARGV"
prompt=""
while [ "$#" -gt 0 ]; do
    if [ "$1" = "--prompt-file" ]; then
        prompt="$2"
    fi
    shift
done
cp "$prompt" "$FAKE_GROK_PROMPT"
if [ -n "${FAKE_GROK_TAMPER:-}" ]; then
    printf '%s\n' 'tampered' > "$FAKE_GROK_TAMPER"
fi
if [ -n "${FAKE_GROK_FAIL:-}" ]; then
    printf '%s\n' 'fake grok failure' >&2
    exit 3
fi
if [ -n "${FAKE_GROK_BAD_OUTPUT:-}" ]; then
    printf '%s\n' '{}'
else
    printf '{"stopReason":"end_turn","text":"%s"}\n' \
        "${FAKE_GROK_REPORT:-REPORT BODY}"
fi
exit 0
`

func newGrokHarness(t *testing.T) *reviewHarness {
	t.Helper()
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "grok")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-grok")
	h.argvLog = filepath.Join(root, "argv.log")
	h.promptLog = filepath.Join(root, "prompt.log")
	writeFake(t, h.fake, fakeGrok)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "基线")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "改动")
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	t.Setenv("GROK_HOME", h.home)
	t.Setenv("GROK_REVIEW_BIN", h.fake)
	t.Setenv("GROK_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("GROK_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_GROK_ARGV", h.argvLog)
	t.Setenv("FAKE_GROK_PROMPT", h.promptLog)
	return h
}

func TestGrokIsolationFlags(t *testing.T) {
	h := newGrokHarness(t)
	code, out, err := h.review("grok", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "REPORT BODY") {
		t.Fatalf("out=%q", out)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	assertArg(t, argv, "--sandbox", "read-only")
	for _, flag := range []string{"--disable-web-search", "--no-memory", "--no-subagents", "--no-plan", "--prompt-file"} {
		if !contains(argv, flag) {
			t.Fatalf("missing %s in %v", flag, argv)
		}
	}
	if contains(argv, "--model") || contains(argv, "--reasoning-effort") {
		t.Fatalf("unexpected model flags: %v", argv)
	}
}

func TestGrokIncompleteOutput(t *testing.T) {
	h := newGrokHarness(t)
	t.Setenv("FAKE_GROK_BAD_OUTPUT", "1")
	code, _, err := h.review("grok", "QA", "确认改动正确")
	if code != 1 || !strings.Contains(err, "did not complete with review text") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestGrokTamperDetected(t *testing.T) {
	h := newGrokHarness(t)
	t.Setenv("FAKE_GROK_TAMPER", filepath.Join(h.repo, "injected.txt"))
	code, _, err := h.review("grok", "QA", "确认改动正确")
	if code != 2 || !strings.Contains(err, "modified the target worktree") {
		t.Fatalf("code=%d err=%q", code, err)
	}
}

func TestGrokIncremental(t *testing.T) {
	h := newGrokHarness(t)
	fixed := commitFile(t, h.repo, "c.txt", "fix\n", "修复")
	code, _, err := captureRun(t, []string{"grok", h.repo, h.base, fixed, "PM", "确认改动正确", "Prior findings: PM-001 closed by c.txt", h.head})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(readFile(t, h.promptLog), "incremental re-review") {
		t.Fatal("missing incremental prompt")
	}
}
