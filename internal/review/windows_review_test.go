//go:build windows

package review

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) >= 3 && os.Args[1] == "review" && os.Args[2] == windowsJobBootstrap {
		os.Exit(windowsJobBootstrapMain(os.Args[3:]))
	}
	if os.Getenv("KANDER_FAKE_REVIEWER") == "1" || os.Getenv("KANDER_FAKE_CHILD") == "sleep-write" {
		os.Exit(runWindowsFakeReviewer())
	}
	os.Exit(m.Run())
}

func runWindowsFakeReviewer() int {
	if os.Getenv("KANDER_FAKE_CHILD") == "sleep-write" {
		time.Sleep(2 * time.Second)
		if path := os.Getenv("FAKE_DELAYED_TAMPER"); path != "" {
			_ = os.WriteFile(path, []byte("escaped\n"), 0o644)
		}
		return 0
	}
	if path := os.Getenv("FAKE_REVIEW_ARGV"); path != "" {
		_ = os.WriteFile(path, []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o644)
	}
	if path := os.Getenv("FAKE_REVIEW_CWD"); path != "" {
		wd, _ := os.Getwd()
		_ = os.WriteFile(path, []byte(wd+"\n"), 0o644)
	}
	if path := os.Getenv("FAKE_REVIEW_CODEX_HOME"); path != "" {
		_ = os.WriteFile(path, []byte(os.Getenv("CODEX_HOME")+"\n"), 0o644)
	}
	if path := os.Getenv("FAKE_REVIEW_CURSOR_CONFIG"); path != "" {
		_ = os.WriteFile(path, []byte(os.Getenv("CURSOR_CONFIG_DIR")+"\n"), 0o644)
	}
	if path := os.Getenv("FAKE_REVIEW_CURSOR_DATA"); path != "" {
		_ = os.WriteFile(path, []byte(os.Getenv("CURSOR_DATA_DIR")+"\n"), 0o644)
	}
	if os.Getenv("FAKE_REVIEW_SLEEP") != "" {
		time.Sleep(30 * time.Second)
	}
	if tamper := os.Getenv("FAKE_REVIEW_TAMPER"); tamper != "" {
		_ = os.WriteFile(tamper, []byte("tampered\n"), 0o644)
	}
	if dest := os.Getenv("FAKE_DELAYED_TAMPER"); dest != "" {
		self, err := os.Executable()
		if err == nil {
			cmd := exec.Command(self)
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, "KANDER_FAKE_CHILD=sleep-write")
			_ = cmd.Start()
		}
	}
	report := os.Getenv("FAKE_REVIEW_REPORT")
	if report == "" {
		report = "REPORT BODY"
	}
	switch os.Getenv("FAKE_REVIEW_KIND") {
	case "codex":
		out := ""
		for i, a := range os.Args {
			if a == "--output-last-message" && i+1 < len(os.Args) {
				out = os.Args[i+1]
				break
			}
		}
		if out != "" {
			_ = os.WriteFile(out, []byte(report+"\n"), 0o644)
		}
	case "grok":
		fmt.Printf("{\"stopReason\":\"end_turn\",\"text\":%q}\n", report)
	default:
		fmt.Printf("{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":%q}\n", report)
	}
	return 0
}

type windowsHarness struct {
	t        *testing.T
	root     string
	repo     string
	tmp      string
	home     string
	argvLog  string
	cwdLog   string
	homeLog  string
	base     string
	head     string
	repoReal string
	self     string
}

func newWindowsHarness(t *testing.T, agent string) *windowsHarness {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	h := &windowsHarness{
		t: t, root: root, self: self,
		repo:    filepath.Join(root, "repo"),
		tmp:     filepath.Join(root, "tmp"),
		home:    filepath.Join(root, "home"),
		argvLog: filepath.Join(root, "argv.log"),
		cwdLog:  filepath.Join(root, "cwd.log"),
		homeLog: filepath.Join(root, "home.log"),
	}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "基线")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "改动")
	h.repoReal = h.repo
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	t.Setenv("TMP", h.tmp)
	t.Setenv("TEMP", h.tmp)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	t.Setenv("KANDER_FAKE_REVIEWER", "1")
	t.Setenv("FAKE_REVIEW_ARGV", h.argvLog)
	t.Setenv("FAKE_REVIEW_CWD", h.cwdLog)
	t.Setenv("FAKE_REVIEW_KIND", agent)
	prefix := strings.ToUpper(agent)
	if agent == "claude" {
		prefix = "CLAUDE"
	}
	t.Setenv(prefix+"_REVIEW_BIN", self)
	t.Setenv(prefix+"_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv(prefix+"_REVIEW_MAX_RUNTIME_SECONDS", "30")
	switch agent {
	case "codex":
		t.Setenv("CODEX_HOME", h.home)
		t.Setenv("FAKE_REVIEW_CODEX_HOME", h.homeLog)
	case "claude":
		t.Setenv("CLAUDE_CONFIG_DIR", h.home)
	case "grok":
		t.Setenv("GROK_HOME", h.home)
	case "cursor":
		t.Setenv("CURSOR_CONFIG_DIR", h.home)
		t.Setenv("FAKE_REVIEW_CURSOR_CONFIG", filepath.Join(root, "cursor-config.log"))
		t.Setenv("FAKE_REVIEW_CURSOR_DATA", filepath.Join(root, "cursor-data.log"))
	}
	return h
}

func (h *windowsHarness) review(agent string, extra ...string) (int, string, string) {
	h.t.Helper()
	args := append([]string{agent, h.repo, h.base, h.head}, extra...)
	return captureRun(h.t, args)
}

func (h *windowsHarness) argv() []string {
	h.t.Helper()
	return strings.Split(strings.TrimRight(readFile(h.t, h.argvLog), "\n"), "\n")
}

func leftoverRuntimes(t *testing.T, tmp, prefix string) {
	t.Helper()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			t.Fatalf("runtime leftover %s", e.Name())
		}
	}
}

func TestWindowsCodexIsolationAndRuntimeLease(t *testing.T) {
	h := newWindowsHarness(t, "codex")
	code, out, err := h.review("codex", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "REPORT BODY") {
		t.Fatalf("out=%q", out)
	}
	argv := h.argv()
	assertArg(t, argv, "--sandbox", "read-only")
	if !contains(argv, "--ephemeral") {
		t.Fatalf("argv=%v", argv)
	}
	leftoverRuntimes(t, h.tmp, "codex-review.")
}

func TestWindowsClaudeIsolationArgv(t *testing.T) {
	h := newWindowsHarness(t, "claude")
	code, _, err := h.review("claude", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := h.argv()
	assertArg(t, argv, "--permission-mode", "plan")
	assertArg(t, argv, "--tools", "Read,Grep,Glob")
	if !contains(argv, "--safe-mode") || !contains(argv, "--no-session-persistence") {
		t.Fatalf("argv=%v", argv)
	}
	cwd := strings.TrimSpace(readFile(t, h.cwdLog))
	if cwd == h.repo {
		t.Fatal("claude must run outside worktree")
	}
	leftoverRuntimes(t, h.tmp, "claude-review.")
}

func TestWindowsGrokIsolationArgv(t *testing.T) {
	h := newWindowsHarness(t, "grok")
	code, _, err := h.review("grok", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := h.argv()
	assertArg(t, argv, "--sandbox", "read-only")
	for _, flag := range []string{"--no-memory", "--no-subagents"} {
		if !contains(argv, flag) {
			t.Fatalf("missing %s in %v", flag, argv)
		}
	}
}

func TestWindowsCursorIsolationAndDirs(t *testing.T) {
	h := newWindowsHarness(t, "cursor")
	code, _, err := h.review("cursor", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := h.argv()
	if !contains(argv, "--print") || !contains(argv, "--trust") {
		t.Fatalf("argv=%v", argv)
	}
	assertArg(t, argv, "--output-format", "json")
	cfg := strings.TrimSpace(readFile(t, filepath.Join(h.root, "cursor-config.log")))
	data := strings.TrimSpace(readFile(t, filepath.Join(h.root, "cursor-data.log")))
	cwd := strings.TrimSpace(readFile(t, h.cwdLog))
	if cfg != cwd || data != cwd {
		t.Fatalf("cursor dirs cfg=%q data=%q cwd=%q", cfg, data, cwd)
	}
	leftoverRuntimes(t, h.tmp, "cursor-review.")
}

func TestWindowsTimeoutKillsJob(t *testing.T) {
	h := newWindowsHarness(t, "claude")
	t.Setenv("FAKE_REVIEW_SLEEP", "1")
	t.Setenv("CLAUDE_REVIEW_MAX_RUNTIME_SECONDS", "1")
	code, _, err := h.review("claude", "QA", "确认改动正确")
	if code != 124 || !strings.Contains(err, "Claude review exceeded 1 seconds") {
		t.Fatalf("code=%d err=%q", code, err)
	}
	leftoverRuntimes(t, h.tmp, "claude-review.")
}

func TestWindowsDelayedTamperCollected(t *testing.T) {
	h := newWindowsHarness(t, "claude")
	target := filepath.Join(h.repo, "escaped.txt")
	t.Setenv("FAKE_DELAYED_TAMPER", target)
	code, _, err := h.review("claude", "QA", "确认改动正确")
	if code != 2 || !strings.Contains(err, "left background child processes") {
		t.Fatalf("code=%d err=%q", code, err)
	}
	time.Sleep(2200 * time.Millisecond)
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatal("tamper file should not exist")
	}
}

func TestWindowsUserProfileReviewHome(t *testing.T) {
	h := newWindowsHarness(t, "codex")
	profile := filepath.Join(h.root, "profile")
	if err := os.Mkdir(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(profile, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", profile)
	t.Setenv("HOME", "")
	t.Setenv("CODEX_HOME", "")
	code, _, err := h.review("codex", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	got := strings.TrimSpace(readFile(t, h.homeLog))
	want := filepath.Join(profile, ".codex")
	if got != want && filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("CODEX_HOME=%q want %q", got, want)
	}
}

func TestWindowsArgvMetacharactersThroughBootstrap(t *testing.T) {
	h := newWindowsHarness(t, "codex")
	t.Setenv("CODEX_REVIEW_MODEL", "gpt-5.6-sol&whoami")
	code, _, err := h.review("codex", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	argv := h.argv()
	assertArg(t, argv, "--model", "gpt-5.6-sol&whoami")
	assertArg(t, argv, "--sandbox", "read-only")
}
