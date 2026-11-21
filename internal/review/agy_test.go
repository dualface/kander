//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeAgy = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_AGY_ARGV"
printf '%s\n' '{"status":"SUCCESS","response":"AGY REPORT BODY"}'
exit 0
`

// runAgyReview runs the whole review entry with a fake agy and returns the
// argv the fake received. home is the user home, below which the installed
// rules directory lives.
func runAgyReview(t *testing.T, installRules bool) []string {
	t.Helper()
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "home")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-agy")
	h.argvLog = filepath.Join(root, "argv.log")
	writeFake(t, h.fake, fakeAgy)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "base")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "change")
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	t.Setenv("HOME", h.home)
	t.Setenv("AGY_REVIEW_BIN", h.fake)
	t.Setenv("AGY_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("AGY_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_AGY_ARGV", h.argvLog)
	if installRules {
		if err := os.MkdirAll(filepath.Join(h.home, ".agents", "kander"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errOut := h.review("agy", "PMQA", "confirm the change")
	if code != 0 || !strings.Contains(out, "AGY REPORT BODY") {
		t.Fatalf("agy review code=%d out=%q err=%q", code, out, errOut)
	}
	return strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
}

// The review entry hands agy the installed rules directory only when it
// exists, and drops the flag before it otherwise.
func TestAgyReviewAddsTheInstalledRulesDirectory(t *testing.T) {
	argv := runAgyReview(t, true)
	var dirs []string
	for i, arg := range argv {
		if arg == "--add-dir" && i+1 < len(argv) {
			dirs = append(dirs, argv[i+1])
		}
	}
	if len(dirs) != 2 || filepath.Base(dirs[1]) != "kander" || filepath.Base(filepath.Dir(dirs[1])) != ".agents" {
		t.Fatalf("want the runtime and the rules directory in --add-dir, got %v in %v", dirs, argv)
	}
}

func TestAgyReviewDropsTheMissingRulesDirectory(t *testing.T) {
	argv := runAgyReview(t, false)
	if count := strings.Count(strings.Join(argv, "\n"), "--add-dir"); count != 1 {
		t.Fatalf("only the runtime directory may remain, got %d --add-dir in %v", count, argv)
	}
	for _, arg := range argv {
		if arg == "" || strings.HasSuffix(arg, filepath.Join(".agents", "kander")) {
			t.Fatalf("a missing rules directory leaked into argv: %v", argv)
		}
	}
}
