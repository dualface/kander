//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeDevin = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_DEVIN_ARGV"
cat > "$FAKE_DEVIN_STDIN"
printf '%s\n' 'DEVIN REPORT BODY'
exit 0
`

func TestDevinReviewUsesArgvInstructionWithoutDevinHome(t *testing.T) {
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "home")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-devin")
	h.argvLog = filepath.Join(root, "argv.log")
	h.stdinLog = filepath.Join(root, "stdin.log")
	writeFake(t, h.fake, fakeDevin)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "base")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "change")
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	// HOME exists but has no .devin directory; the optional home policy must accept it.
	t.Setenv("HOME", h.home)
	t.Setenv("DEVIN_REVIEW_BIN", h.fake)
	t.Setenv("DEVIN_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("DEVIN_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_DEVIN_ARGV", h.argvLog)
	t.Setenv("FAKE_DEVIN_STDIN", h.stdinLog)

	code, out, err := h.review("devin", "PMQA", "confirm the change")
	if code != 0 {
		t.Fatalf("devin review without ~/.devin code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "DEVIN REPORT BODY") {
		t.Fatalf("out=%q err=%q", out, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	for _, want := range []string{"--print", "--permission-mode", "auto", "--"} {
		if !contains(argv, want) {
			t.Fatalf("argv missing %q: %v", want, argv)
		}
	}
	if !contains(argv, "--respect-workspace-trust") {
		t.Fatalf("argv missing trust flag: %v", argv)
	}
	if !strings.Contains(strings.Join(argv, "\n"), "task file at ") {
		t.Fatalf("instruction not in argv: %v", argv)
	}
	if stdin := readFile(t, h.stdinLog); strings.Contains(stdin, "task file at ") {
		t.Fatalf("stdin should not carry the instruction, got %q", stdin)
	}
}
