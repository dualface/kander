//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeCursor = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_CURSOR_ARGV"
printf '%s\n' "$CURSOR_CONFIG_DIR" > "$FAKE_CURSOR_CONFIG_DIR"
printf '%s\n' "$CURSOR_DATA_DIR" > "$FAKE_CURSOR_DATA_DIR"
instruction=$(cat)
prompt=${instruction#*task file at }
prompt=${prompt%%; read the complete file first*}
cat "$prompt" > "$FAKE_CURSOR_PROMPT"
pwd -P > "$FAKE_CURSOR_CWD"
if [ -n "${FAKE_CURSOR_TAMPER:-}" ]; then
    printf '%s\n' 'tampered' > "$FAKE_CURSOR_TAMPER"
fi
if [ -n "${FAKE_CURSOR_OWN_CHILD:-}" ]; then
    sleep 30 &
    sleep 1
fi
if [ -n "${FAKE_CURSOR_DETACHED_CHILD:-}" ]; then
    ( sleep 30 & ) &
    sleep 1
fi
if [ -n "${FAKE_CURSOR_FAIL:-}" ]; then
    printf '%s\n' 'fake cursor failure' >&2
    exit 3
fi
if [ -n "${FAKE_CURSOR_BAD_OUTPUT:-}" ]; then
    printf '%s\n' '{}'
else
    printf '{"type":"result","subtype":"success","is_error":false,"result":"%s"}\n' \
        "${FAKE_CURSOR_REPORT:-REPORT BODY}"
fi
exit 0
`

func newCursorHarness(t *testing.T) *reviewHarness {
	t.Helper()
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "cursor")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-cursor")
	h.argvLog = filepath.Join(root, "argv.log")
	h.promptLog = filepath.Join(root, "prompt.log")
	h.cwdLog = filepath.Join(root, "cwd.log")
	h.configDirLog = filepath.Join(root, "config-dir.log")
	h.dataDirLog = filepath.Join(root, "data-dir.log")
	writeFake(t, h.fake, fakeCursor)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "基线")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "改动")
	real, _ := filepath.EvalSymlinks(h.repo)
	h.repoReal = real
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	t.Setenv("CURSOR_CONFIG_DIR", h.home)
	t.Setenv("CURSOR_REVIEW_BIN", h.fake)
	t.Setenv("CURSOR_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("CURSOR_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_CURSOR_ARGV", h.argvLog)
	t.Setenv("FAKE_CURSOR_PROMPT", h.promptLog)
	t.Setenv("FAKE_CURSOR_CWD", h.cwdLog)
	t.Setenv("FAKE_CURSOR_CONFIG_DIR", h.configDirLog)
	t.Setenv("FAKE_CURSOR_DATA_DIR", h.dataDirLog)
	return h
}

func TestCursorOwnChildrenAllowedDetachedRejected(t *testing.T) {
	h := newCursorHarness(t)
	t.Setenv("FAKE_CURSOR_OWN_CHILD", "1")
	code, out, err := h.review("cursor", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("own child code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "REPORT BODY") || strings.Contains(err, "background child processes") {
		t.Fatalf("out=%q err=%q", out, err)
	}
	t.Setenv("FAKE_CURSOR_OWN_CHILD", "")
	t.Setenv("FAKE_CURSOR_DETACHED_CHILD", "1")
	code, out, err = h.review("cursor", "QA", "确认改动正确")
	if code != 2 || !strings.Contains(err, "background child processes") {
		t.Fatalf("detached code=%d err=%q", code, err)
	}
	if strings.Contains(out, "REPORT BODY") {
		t.Fatal("report must be rejected")
	}
}

func TestCursorIsolationFlags(t *testing.T) {
	h := newCursorHarness(t)
	code, _, err := h.review("cursor", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	assertArg(t, argv, "--output-format", "json")
	if !contains(argv, "--print") || !contains(argv, "--trust") {
		t.Fatalf("argv=%v", argv)
	}
	assertArg(t, argv, "--add-dir", h.repoReal)
	assertArg(t, argv, "--model", "cursor-grok-4.6-xhigh")
	for _, banned := range []string{"--sandbox", "--mode", "--effort", "--force"} {
		if contains(argv, banned) {
			t.Fatalf("unexpected %s", banned)
		}
	}
	runtimeConfig := strings.TrimSpace(readFile(t, h.configDirLog))
	runtimeData := strings.TrimSpace(readFile(t, h.dataDirLog))
	if runtimeConfig != runtimeData || !strings.HasPrefix(runtimeConfig, h.tmp) {
		t.Fatalf("config=%s data=%s tmp=%s", runtimeConfig, runtimeData, h.tmp)
	}
	if runtimeConfig == h.home {
		t.Fatal("must isolate from user cursor home")
	}
}

func TestCursorMissingHomeAndSpecSnapshot(t *testing.T) {
	h := newCursorHarness(t)
	missing := filepath.Join(h.root, "absent-cursor-home")
	t.Setenv("CURSOR_CONFIG_DIR", missing)
	code, _, err := h.review("cursor", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatal("must not create missing home")
	}
	spec := filepath.Join(h.root, "spec.md")
	if err := os.WriteFile(spec, []byte("# 任务契约\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, err = h.review("cursor", "PM", spec)
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	prompt := readFile(t, h.promptLog)
	if strings.Contains(prompt, spec) || !strings.Contains(prompt, "task-spec.md") {
		t.Fatalf("prompt=%s", prompt)
	}
}

func TestCursorIncompleteOutput(t *testing.T) {
	h := newCursorHarness(t)
	t.Setenv("FAKE_CURSOR_BAD_OUTPUT", "1")
	code, out, err := h.review("cursor", "QA", "确认改动正确")
	if code != 1 || !strings.Contains(err, "did not complete with review text") {
		t.Fatalf("code=%d err=%q", code, err)
	}
	if !strings.Contains(out, "{}") {
		t.Fatalf("out=%q", out)
	}
}
