//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeOpenCode = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_OPENCODE_ARGV"
printf '%s\n' "$OPENCODE_PERMISSION" > "$FAKE_OPENCODE_ENV"
printf '%s\n' '{"type":"step_start","part":{"type":"step-start"}}'
printf '%s\n' '{"type":"text","part":{"type":"text","text":"OPENCODE REPORT BODY"}}'
printf '%s\n' '{"type":"step_finish","part":{"type":"step-finish","reason":"stop"}}'
exit 0
`

func TestOpenCodeReviewRunsPureJsonWithDeniedWrites(t *testing.T) {
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "home")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-opencode")
	h.argvLog = filepath.Join(root, "argv.log")
	h.stdinLog = filepath.Join(root, "stdin.log")
	envLog := filepath.Join(root, "env.log")
	writeFake(t, h.fake, fakeOpenCode)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "base")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "change")
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	// HOME exists but has no .opencode directory; the optional home policy must accept it.
	t.Setenv("HOME", h.home)
	t.Setenv("OPENCODE_REVIEW_BIN", h.fake)
	t.Setenv("OPENCODE_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("OPENCODE_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_OPENCODE_ARGV", h.argvLog)
	t.Setenv("FAKE_OPENCODE_ENV", envLog)

	code, out, err := h.review("opencode", "PMQA", "confirm the change")
	if code != 0 {
		t.Fatalf("opencode review without ~/.opencode code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "OPENCODE REPORT BODY") {
		t.Fatalf("out=%q err=%q", out, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	for _, want := range []string{"run", "--pure", "--variant", "--format", "json"} {
		if !contains(argv, want) {
			t.Fatalf("argv missing %q: %v", want, argv)
		}
	}
	if !strings.Contains(strings.Join(argv, "\n"), "task file at ") {
		t.Fatalf("instruction not in argv: %v", argv)
	}
	if contains(argv, "--auto") || contains(argv, "--permission-mode") {
		t.Fatalf("review argv must not widen permissions: %v", argv)
	}
	permission := readFile(t, envLog)
	if !strings.Contains(permission, `"*":"deny"`) || !strings.Contains(permission, `"read":"allow"`) {
		t.Fatalf("OPENCODE_PERMISSION=%q", permission)
	}
}
