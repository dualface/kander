//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakePi = `#!/bin/sh
cat > "$FAKE_PI_STDIN"
printf '%s\n' 'PI REPORT BODY'
exit 0
`

func TestPiReviewRunsWithoutPiHome(t *testing.T) {
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "home")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-pi")
	h.stdinLog = filepath.Join(root, "stdin.log")
	writeFake(t, h.fake, fakePi)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "base")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "change")
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	// HOME exists but has no .pi directory; the optional home policy must accept it.
	t.Setenv("HOME", h.home)
	t.Setenv("PI_REVIEW_BIN", h.fake)
	t.Setenv("PI_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("PI_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_PI_STDIN", h.stdinLog)

	code, out, err := h.review("pi", "QA", "confirm the change")
	if code != 0 {
		t.Fatalf("pi review without ~/.pi code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "PI REPORT BODY") {
		t.Fatalf("out=%q err=%q", out, err)
	}
	if stdin := readFile(t, h.stdinLog); !strings.Contains(stdin, "task file at ") {
		t.Fatalf("stdin instruction=%q", stdin)
	}
}
