//go:build unix

package review

import (
	"os"
	"path/filepath"
	"testing"
)

type reviewHarness struct {
	t            *testing.T
	root         string
	repo         string
	tmp          string
	home         string
	fake         string
	argvLog      string
	stdinLog     string
	promptLog    string
	cwdLog       string
	configDirLog string
	dataDirLog   string
	homeLog      string
	specLog      string
	base         string
	head         string
	repoReal     string
}

func writeFake(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

const fakeCodex = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_CODEX_ARGV"
instruction=$(cat)
prompt=${instruction#*task file at }
prompt=${prompt%%; read the complete file first*}
cat "$prompt" > "$FAKE_CODEX_STDIN"

out=""
while [ "$#" -gt 0 ]; do
    if [ "$1" = "--output-last-message" ]; then
        out="$2"
    fi
    shift
done

if [ -n "${FAKE_CODEX_TAMPER:-}" ]; then
    printf '%s\n' 'tampered' > "$FAKE_CODEX_TAMPER"
fi
if [ -n "${FAKE_CODEX_FAIL:-}" ]; then
    printf '%s\n' 'fake codex failure' >&2
    exit 3
fi
if [ -n "$out" ]; then
    printf '%s\n' "${FAKE_CODEX_REPORT:-REPORT BODY}" > "$out"
fi
exit 0
`

func newCodexHarness(t *testing.T) *reviewHarness {
	t.Helper()
	root := t.TempDir()
	h := &reviewHarness{
		t: t, root: root,
		repo: filepath.Join(root, "repo"),
		tmp:  filepath.Join(root, "tmp"),
		home: filepath.Join(root, "codex"),
	}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-codex")
	h.argvLog = filepath.Join(root, "argv.log")
	h.stdinLog = filepath.Join(root, "stdin.log")
	writeFake(t, h.fake, fakeCodex)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "基线")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "改动")
	real, err := filepath.EvalSymlinks(h.repo)
	if err != nil {
		real = h.repo
	}
	h.repoReal = real
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	t.Setenv("TMP", h.tmp)
	t.Setenv("TEMP", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	t.Setenv("CODEX_HOME", h.home)
	t.Setenv("CODEX_REVIEW_BIN", h.fake)
	t.Setenv("CODEX_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("CODEX_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_CODEX_ARGV", h.argvLog)
	t.Setenv("FAKE_CODEX_STDIN", h.stdinLog)
	return h
}

func (h *reviewHarness) review(agent string, extra ...string) (int, string, string) {
	h.t.Helper()
	args := append([]string{agent, h.repo, h.base, h.head}, extra...)
	return captureRun(h.t, args)
}

func (h *reviewHarness) defaultReview(roleTask ...string) (int, string, string) {
	h.t.Helper()
	if len(roleTask) == 0 {
		roleTask = []string{"QA", "确认改动正确"}
	}
	return h.review("codex", roleTask...)
}
