//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeKimi = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_KIMI_ARGV"
agent_file=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--agent-file" ]; then agent_file="$a"; fi
  prev="$a"
done
if [ -n "$agent_file" ]; then cat "$agent_file" > "$FAKE_KIMI_AGENT_FILE" 2>/dev/null; fi
printf '%s\n' '{"role":"meta","type":"system.version","version":"0.43.1"}'
printf '%s\n' '{"role":"assistant","tool_calls":[{"type":"function","id":"t1","function":{"name":"Read","arguments":"{}"}}]}'
printf '%s\n' '{"role":"tool","tool_call_id":"t1","content":"file body"}'
printf '%s\n' '{"role":"assistant","content":"KIMI REPORT BODY"}'
printf '%s\n' '{"role":"meta","type":"session.resume_hint","session_id":"ks1","command":"kimi -r ks1","content":"resume"}'
exit 0
`

func TestKimiReviewRunsReadOnlyAgentFile(t *testing.T) {
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "home")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-kimi")
	h.argvLog = filepath.Join(root, "argv.log")
	h.stdinLog = filepath.Join(root, "stdin.log")
	agentFileLog := filepath.Join(root, "agent-file.log")
	writeFake(t, h.fake, fakeKimi)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "base")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "change")
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	// HOME exists but holds no .kimi directory; the optional home policy must accept it.
	t.Setenv("HOME", h.home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(h.home, ".kimi-code"))
	t.Setenv("KIMI_REVIEW_BIN", h.fake)
	t.Setenv("KIMI_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("KIMI_REVIEW_MAX_RUNTIME_SECONDS", "30")
	t.Setenv("FAKE_KIMI_ARGV", h.argvLog)
	t.Setenv("FAKE_KIMI_AGENT_FILE", agentFileLog)

	code, out, err := h.review("kimi", "PMQA", "confirm the change")
	if code != 0 {
		t.Fatalf("kimi review code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "KIMI REPORT BODY") {
		t.Fatalf("out=%q err=%q", out, err)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	for _, want := range []string{"--prompt", "--output-format", "stream-json", "--agent-file", "--add-dir"} {
		if !contains(argv, want) {
			t.Fatalf("argv missing %q: %v", want, argv)
		}
	}
	if !strings.Contains(strings.Join(argv, "\n"), "task file at ") {
		t.Fatalf("instruction not in argv: %v", argv)
	}
	for _, banned := range []string{"--auto", "--yolo", "--continue", "--session"} {
		if contains(argv, banned) {
			t.Fatalf("review argv must not carry %s: %v", banned, argv)
		}
	}
	agentFile := readFile(t, agentFileLog)
	for _, want := range []string{"name: kander-reviewer", "tools: [Read, Grep, Glob]", "disallowedTools", "Bash", "AgentSwarm"} {
		if !strings.Contains(agentFile, want) {
			t.Fatalf("agent file missing %q:\n%s", want, agentFile)
		}
	}
}
