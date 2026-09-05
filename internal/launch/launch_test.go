package launch

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func resetLang(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
}

func capture(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	runErr := fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ := io.ReadAll(outR)
	errb, _ := io.ReadAll(errR)
	_ = outR.Close()
	_ = errR.Close()
	return string(out), string(errb), runErr
}

func makeReady(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, replacement := range []string{"实现目标", "产生可验证结果", "满足验收", "无额外范围"} {
		text = strings.Replace(text, "<填写>", replacement, 1)
	}
	text = strings.Replace(text, "## 讨论与决策\n", "## 讨论与决策\n\n自审: 通过\n卡审: 通过\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setBranch(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "- 任务分支:\n", "- 任务分支: task-branch\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func envConfig(agent, launcher string, agents map[string]string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.KanbanAgent = agent
	if agents == nil {
		agents = map[string]string{"large": agent, "small": agent}
	}
	cfg.KanbanAgents = agents
	cfg.Launcher = launcher
	return cfg
}

func setupBoard(t *testing.T) (root, home, fakeBin string) {
	t.Helper()
	resetLang(t)
	root = t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("KANDER_CONFIG", filepath.Join(home, "missing-config.json"))
	fakeBin = filepath.Join(root, "fake-bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	tmuxLog := filepath.Join(root, "tmux.log")
	writeFakeTmux(t, filepath.Join(fakeBin, "tmux"), tmuxLog)
	for _, name := range []string{"codex", "claude", "grok", "cursor-agent"} {
		writeFakeAgent(t, filepath.Join(fakeBin, name))
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "/tmp/fake-tmux,1,0")
	t.Setenv("TMUX_PANE", "%7")
	t.Setenv("KANBAN_TMUX_LOG", tmuxLog)
	loadEffective = func() (*config.Config, error) { return envConfig("codex", "tmux", nil), nil }
	currentInstallPaths = func() (config.InstallPaths, error) {
		return config.InstallPaths{Mode: config.ModeGlobal, BinDir: fakeBin}, nil
	}
	t.Cleanup(func() {
		loadEffective = func() (*config.Config, error) { return config.Effective(nil) }
		currentInstallPaths = config.CurrentInstallPaths
	})
	return root, home, fakeBin
}

func writeFakeTmux(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_TMUX_LOG:-` + log + `}"
if [ "$1" = "display-message" ]; then
  case "${5:-}" in
    *pane_current_command*pane_in_mode*pane_dead*)
      current="${KANBAN_TMUX_CURRENT_COMMAND:-}"
      [ -n "$current" ] || { [ -f "$log.current" ] && current=$(sed -n '1p' "$log.current"); }
      printf '%s\t%s\t%s\n' "${current:-codex}" "${KANBAN_TMUX_IN_MODE:-0}" "${KANBAN_TMUX_DEAD:-0}"
      exit 0
      ;;
    *pane_current_command*pane_dead*)
      printf '%s\t0\n' "${KANBAN_TMUX_CURRENT_COMMAND:-codex}"
      exit 0
      ;;
    '#{session_id}')
      printf '%s\n' '$42'
      exit 0
      ;;
  esac
  printf '%s\n' '$42'
  exit 0
fi
if [ "$1" = "has-session" ]; then
  for name in ${KANBAN_TMUX_SESSIONS:-}; do
    [ "$name" = "${3#=}" ] && exit 0
  done
  exit 1
fi
if [ "$1" = "show-options" ]; then
  if [ "${2:-}" = "-p" ]; then
    if [ -n "${KANBAN_TMUX_PANE_SESSION:-}" ]; then
      printf '%s\n' "$KANBAN_TMUX_PANE_SESSION"
    elif [ -f "$log.pane-session" ]; then
      sed -n '1p' "$log.pane-session"
    else
      exit 1
    fi
    exit 0
  fi
  [ -n "${KANBAN_TMUX_PROJECT:-}" ] || exit 1
  printf '%s\n' "$KANBAN_TMUX_PROJECT"
  exit 0
fi
if [ "$1" = "set-option" ] && [ "$2" = "-p" ]; then
  printf '%s\n' "$@" > "$log.pane-setopt"
  if [ "${KANBAN_TMUX_PANE_SETOPT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake pane setopt failure' >&2
    exit 1
  fi
  printf '%s\n' "$6" > "$log.pane-session"
  exit 0
fi
if [ "$1" = "set-option" ]; then
  printf '%s\n' "$@" > "$log.setopt"
  exit 0
fi
if [ "$1" = "kill-window" ]; then
  printf '%s\n' "$@" > "$log.kill"
  exit 0
fi
if [ "$1" = "respawn-pane" ]; then
  printf '%s\n' "$5" > "$log.command"
  current=${5%% *}
  printf '%s\n' "${current##*/}" > "$log.current"
  if [ "${current##*/}" = "codex" ]; then
    task=$(printf '%s\n' "$5" | grep -Eo '[0-9]{8}-[a-z0-9-]+-task' | head -n 1)
    if [ -n "$task" ]; then
      session_dir="${CODEX_HOME:-$HOME/.codex}/sessions/fake"
      mkdir -p "$session_dir"
      printf '%s\n' '{"type":"session_meta","payload":{"id":"fake-codex-session"}}' > "$session_dir/rollout-$task.jsonl"
      printf '{"type":"event_msg","payload":{"type":"user_message","message":"执行 Kanban 任务 %s; full instructions are in the UTF-8 task file at /tmp/task.md; read the complete file first and follow it exactly."}}\n' "$task" >> "$session_dir/rollout-$task.jsonl"
    fi
  fi
  if [ -n "${KANBAN_TMUX_MUTATE_CARD:-}" ]; then
    printf '%s\n' '# agent mutation' >> "$KANBAN_TMUX_MUTATE_CARD"
  fi
  if [ "${KANBAN_TMUX_RESPAWN_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake tmux respawn failure' >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "capture-pane" ]; then
  exit 1
fi
printf '%s\n' "$@" > "$log"
if [ "${KANBAN_TMUX_FAIL:-}" = "1" ]; then
  printf '%s\n' 'fake tmux failure' >&2
  exit 1
fi
printf '%s\t%s\n' '@9' '%9'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeAgent(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "create-chat" ]; then
  if [ "${KANBAN_CURSOR_CHAT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake create-chat failure' >&2
    exit 1
  fi
  printf '%s\n' "${KANBAN_CURSOR_CHAT_ID:-chat-fake-0001}"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeHerdr(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_HERDR_LOG}"
if [ "$1" = "tab" ] && [ "$2" = "create" ]; then
  printf '%s\n' "tab create" >> "$log.order"
  printf '%s\n' "$@" > "$log.create"
  if [ "${KANBAN_HERDR_CREATE_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr create failure' >&2
    exit 1
  fi
  printf '%s\n' '{"id":"cli:tab:create","result":{"type":"tab_created","tab":{"tab_id":"w1:t9"},"root_pane":{"pane_id":"w1:p9","tab_id":"w1:t9"}}}'
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "wait-output" ]; then
  printf '%s\n' "pane wait-output" >> "$log.order"
  printf '%s\n' "$@" > "$log.wait"
  if [ "${KANBAN_HERDR_WAIT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr wait failure' >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "run" ]; then
  printf '%s\n' "pane run" >> "$log.order"
  printf '%s\n' "$@" > "$log.run"
  if [ "${KANBAN_HERDR_RUN_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr run failure' >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  if [ -n "${KANBAN_HERDR_PANE_JSON:-}" ]; then
    printf '%s\n' "$KANBAN_HERDR_PANE_JSON"
    exit 0
  fi
  agent="${KANBAN_HERDR_AGENT:-claude}"
  status="${KANBAN_HERDR_STATUS:-idle}"
  printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"w1:t9\",\"agent\":\"$agent\",\"agent_status\":\"$status\",\"agent_session\":{\"value\":\"${KANBAN_HERDR_SESSION:-}\"}}}}"
  exit 0
fi
if [ "$1" = "tab" ] && [ "$2" = "close" ]; then
  printf '%s\n' "$@" > "$log.close"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "read" ]; then
  printf '%s\n' "${KANBAN_HERDR_OUTPUT:-}"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func makeTodo(t *testing.T, root, slug string) (string, string) {
	t.Helper()
	path, err := board.NewTask(root, "chore", slug, "任务 "+slug, false)
	if err != nil {
		t.Fatal(err)
	}
	makeReady(t, path)
	loaded, err := board.LoadBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := board.Locate(loaded, filepath.Base(strings.TrimSuffix(path, ".md")))
	if err != nil {
		entry, err = board.Locate(loaded, strings.TrimSuffix(filepath.Base(path), ".md"))
		if err != nil {
			t.Fatal(err)
		}
	}
	moved, err := board.MoveEntry(entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	return moved.TaskID, moved.Path
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func lastCommand(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "tmux.log.command"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLookPathTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	_, _, fakeBin := setupBoard(t)
	p, err := lookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(fakeBin, "tmux") {
		t.Fatalf("lookPath=%s want %s PATH=%s", p, filepath.Join(fakeBin, "tmux"), os.Getenv("PATH"))
	}
	id, err := tmuxSessionID(p)
	if err != nil {
		t.Fatal(err)
	}
	if id != "$42" {
		t.Fatalf("session=%q", id)
	}
}

func TestStartSelectsAgentByScaleAndRecordsWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	loadEffective = func() (*config.Config, error) {
		return envConfig("codex", "tmux", map[string]string{"large": "codex", "small": "cursor"}), nil
	}
	smallID, smallPath := makeTodo(t, root, "scale-small")
	out, _, err := capture(t, func() error { return commandStart(root, "", "", smallID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "规模=小任务\tAgent=cursor") {
		t.Fatalf("stdout=%s", out)
	}
	cmd := lastCommand(t, root)
	if !strings.Contains(cmd, filepath.Join(fakeBin, "cursor-agent")) || !strings.Contains(cmd, "--resume chat-fake-0001") {
		t.Fatalf("command=%s", cmd)
	}
	text, _ := os.ReadFile(filepath.Join(root, "working", filepath.Base(smallPath)))
	if !strings.Contains(string(text), "- 负责人: cursor\n- 会话: cursor chat-fake-0001\n- 窗口: tmux:$42:@9:%9\n") {
		t.Fatalf("card=%s", text)
	}

	largePath, err := board.NewTask(root, "chore", "scale-large", "大任务", true)
	if err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(largePath, "spec.md")
	makeReady(t, spec)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, filepath.Base(largePath))
	if _, err := board.MoveEntry(entry, root, "todo"); err != nil {
		t.Fatal(err)
	}
	out, _, err = capture(t, func() error { return commandStart(root, "", "", entry.TaskID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "规模=大任务\tAgent=codex") {
		t.Fatalf("stdout=%s", out)
	}
	cmd = lastCommand(t, root)
	if !strings.Contains(cmd, filepath.Join(fakeBin, "codex")) || !strings.Contains(cmd, `model_reasoning_effort="high"`) {
		t.Fatalf("large command=%s", cmd)
	}
	if strings.Contains(cmd, " resume ") {
		t.Fatalf("start should not resume: %s", cmd)
	}
}

func TestStartAgentOverrideAndClaudeSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "claude-session")
	_, _, err := capture(t, func() error { return commandStart(root, "claude", "", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	cmd := lastCommand(t, root)
	match := regexp.MustCompile(`--session-id ([0-9a-f-]{36})`).FindStringSubmatch(cmd)
	if match == nil {
		t.Fatalf("command=%s", cmd)
	}
	text, _ := os.ReadFile(filepath.Join(root, "working", filepath.Base(path)))
	if !strings.Contains(string(text), "- 会话: claude "+match[1]+"\n") {
		t.Fatalf("card=%s", text)
	}
	if strings.Contains(cmd, "--resume") {
		t.Fatalf("start should not resume: %s", cmd)
	}
}

func TestStartCursorCreateChatFailureDoesNotClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	t.Setenv("KANBAN_CURSOR_CHAT_FAIL", "1")
	taskID, path := makeTodo(t, root, "cursor-chat-fail")
	_, errb, err := capture(t, func() error { return commandStart(root, "cursor", "", taskID) })
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error()+errb, "create-chat") {
		t.Fatalf("err=%v stderr=%s", err, errb)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("todo card should remain")
	}
	if _, err := os.Stat(filepath.Join(root, "tmux.log")); err == nil {
		t.Fatal("tmux should not have launched")
	}
}

func TestResumeClaudeAndGroupPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	taskID, path := makeTodo(t, root, "resume-claude")
	_, _, err := capture(t, func() error { return commandStart(root, "claude", "", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	cmd := lastCommand(t, root)
	session := regexp.MustCompile(`--session-id ([0-9a-f-]{36})`).FindStringSubmatch(cmd)[1]
	working := filepath.Join(root, "working", filepath.Base(path))
	setBranch(t, working)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, taskID)
	if _, err := board.MoveEntry(entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	out, _, err := capture(t, func() error {
		return commandResume(root, nil, "", taskID, "QA finding: 补齐空输入校验", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已唤醒: "+taskID+"\t规模=小任务\tAgent=claude") {
		t.Fatalf("stdout=%s", out)
	}
	// resume 不迁卡: review 卡保持原位, 迁移由被唤醒的 Agent 自行执行.
	if _, err := os.Stat(filepath.Join(root, "review", filepath.Base(path))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(working); err == nil {
		t.Fatal("resume must not move the card to working")
	}
	cmd = lastCommand(t, root)
	if !strings.Contains(cmd, filepath.Join(fakeBin, "claude")) || !strings.Contains(cmd, "--resume "+session) || strings.Contains(cmd, "--session-id") {
		t.Fatalf("command=%s", cmd)
	}
	if strings.Contains(cmd, "QA finding") {
		t.Fatalf("message leaked onto argv: %s", cmd)
	}
	idx := strings.Index(cmd, "UTF-8 task file at ")
	if idx < 0 {
		t.Fatalf("no task file in %s", cmd)
	}
	rest := cmd[idx+len("UTF-8 task file at "):]
	pathEnd := strings.IndexByte(rest, ';')
	resumeFile := strings.TrimSpace(rest[:pathEnd])
	resumeBody, _ := os.ReadFile(resumeFile)
	if !strings.Contains(string(resumeBody), "QA finding: 补齐空输入校验") {
		t.Fatalf("resume prompt=%s", resumeBody)
	}

	groupID, groupPath := makeTodo(t, root, "group-prompt")
	loadEffective = func() (*config.Config, error) {
		cfg := envConfig("claude", "tmux", nil)
		cfg.Rules[config.RuleGit] = true
		cfg.Rules[config.RuleTaskGroups] = true
		return cfg, nil
	}
	data, _ := os.ReadFile(groupPath)
	updated := strings.Replace(string(data), "- 任务组:\n", "- 任务组: 20260901-demo-group\n", 1)
	_ = os.WriteFile(groupPath, []byte(updated), 0o644)
	_, _, err = capture(t, func() error { return commandStart(root, "", "", groupID) })
	if err != nil {
		t.Fatal(err)
	}
	groupCmd := lastCommand(t, root)
	idx = strings.Index(groupCmd, "UTF-8 task file at ")
	if idx < 0 {
		t.Fatalf("group cmd=%s", groupCmd)
	}
	rest = groupCmd[idx+len("UTF-8 task file at "):]
	pathEnd = strings.IndexByte(rest, ';')
	file := strings.TrimSpace(rest[:pathEnd])
	body, _ := os.ReadFile(file)
	if !strings.Contains(string(body), "kander move "+groupID+" review") || !strings.Contains(string(body), "已启用的任务组编排模块") {
		t.Fatalf("group prompt=%s", body)
	}
}

func TestResumeRequiresMessageAndCodexRollout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, home, fakeBin := setupBoard(t)
	taskID, path := makeTodo(t, root, "resume-codex")
	_, _, err := capture(t, func() error { return commandStart(root, "", "", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "working", filepath.Base(path))
	setBranch(t, working)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, taskID)
	if _, err := board.MoveEntry(entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "", taskID, "", "", false, 61)
	})
	if err == nil || !strings.Contains(err.Error(), "--message") {
		t.Fatalf("expected message error, got %v", err)
	}
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "", taskID, "x", "y", true, 61)
	})
	if err == nil || !strings.Contains(err.Error(), "--message") {
		t.Fatalf("expected xor error, got %v", err)
	}
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "", taskID, "  ", "", true, 61)
	})
	if err == nil || !strings.Contains(err.Error(), "不得为空") {
		t.Fatalf("expected empty, got %v", err)
	}
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "", taskID, "x", "", true, 60)
	})
	if err == nil || !strings.Contains(err.Error(), "大于 60") {
		t.Fatalf("expected timeout, got %v", err)
	}

	_ = os.RemoveAll(filepath.Join(home, ".codex", "sessions"))
	day := filepath.Join(home, ".codex", "sessions", "2026", "09", "01")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRollout := func(name, id, prompt string) {
		lines := []map[string]any{
			{"timestamp": "t", "type": "session_meta", "payload": map[string]any{"id": id, "cwd": "/p"}},
			{"timestamp": "t", "type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}},
		}
		var b strings.Builder
		for _, line := range lines {
			raw, _ := json.Marshal(line)
			b.Write(raw)
			b.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(day, name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRollout("rollout-other.jsonl", "aaaaaaaa-0000-0000-0000-000000000001", "执行 Kanban 任务 other.")
	writeRollout("rollout-orch.jsonl", "cccccccc-0000-0000-0000-000000000003", "请用 kander start 启动 "+taskID+" 并跟踪")
	target := filepath.Join(day, "rollout-target.jsonl")
	writeRollout("rollout-target.jsonl", "bbbbbbbb-0000-0000-0000-000000000002",
		"执行 Kanban 任务 "+taskID+"; full instructions are in the UTF-8 task file at /tmp/kander-task.md; read the complete file first and follow it exactly.")
	_ = os.Chtimes(target, time.Unix(1_700_000_000, 0), time.Unix(1_700_000_000, 0))
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "", taskID, "PM finding", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := lastCommand(t, root)
	if !strings.Contains(cmd, filepath.Join(fakeBin, "codex")+" resume") && !strings.Contains(cmd, "codex resume") {
		t.Fatalf("command=%s", cmd)
	}
	if !strings.Contains(cmd, "bbbbbbbb-0000-0000-0000-000000000002") {
		t.Fatalf("command=%s", cmd)
	}
}

func TestStartFailureRestoresTodo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "rollback")
	original, _ := os.ReadFile(path)
	t.Setenv("KANBAN_TMUX_FAIL", "1")
	_, _, err := capture(t, func() error { return commandStart(root, "", "", taskID) })
	if err == nil || !strings.Contains(err.Error(), "tmux new-window 失败") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("document changed")
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(path))); err == nil {
		t.Fatal("should not remain in working")
	}
}

func TestTmuxPersistsWindowBeforeMutationAndPaneMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "tmux-order")
	working := filepath.Join(root, "working", filepath.Base(path))
	t.Setenv("KANBAN_TMUX_MUTATE_CARD", working)
	_, _, err := capture(t, func() error { return commandStart(root, "claude", "tmux", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(working)
	if !strings.Contains(string(text), "- 窗口: tmux:$42:@9:%9\n") || !strings.HasSuffix(string(text), "# agent mutation\n") {
		t.Fatalf("card=%s", text)
	}
	setopt, _ := os.ReadFile(filepath.Join(root, "tmux.log.pane-setopt"))
	if !strings.Contains(string(setopt), "@kander_session") {
		t.Fatalf("setopt=%s", setopt)
	}
}

func TestTmuxPaneSessionWriteFailureRollsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "pane-session-rollback")
	original, _ := os.ReadFile(path)
	t.Setenv("KANBAN_TMUX_PANE_SETOPT_FAIL", "1")
	_, _, err := capture(t, func() error { return commandStart(root, "claude", "tmux", taskID) })
	if err == nil || !strings.Contains(err.Error(), "tmux 写入 pane 会话失败") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("not restored")
	}
	kill, _ := os.ReadFile(filepath.Join(root, "tmux.log.kill"))
	if strings.TrimSpace(string(kill)) != "kill-window\n-t\n@9" && !strings.Contains(string(kill), "kill-window") {
		t.Fatalf("kill=%q", kill)
	}
}

func TestRollbackLaunchRestoresOrKeepsWorking(t *testing.T) {
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "rollback-ok")
	original, _ := os.ReadFile(path)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, taskID)
	moved, err := board.MoveEntry(entry, root, "working")
	if err != nil {
		t.Fatal(err)
	}
	working := moved.Path
	mut := strings.Replace(string(original), "- 负责人:\n", "- 负责人: codex\n", 1)
	_ = os.WriteFile(working, []byte(mut), 0o644)
	orig := string(original)
	err = rollbackLaunch(root, moved, "todo", &LaunchFailure{Err: launchError("tmux new-window 失败", "tmux new-window 失败")}, &orig)
	if err == nil || err.Error() != "tmux new-window 失败" {
		t.Fatalf("err=%v", err)
	}
	if _, stat := os.Stat(path); stat != nil {
		t.Fatal("should be back in todo")
	}

	taskID, path = makeTodo(t, root, "rollback-restore")
	original, _ = os.ReadFile(path)
	loaded, _ = board.LoadBoard(root)
	entry, _ = board.Locate(loaded, taskID)
	moved, _ = board.MoveEntry(entry, root, "working")
	_ = os.WriteFile(moved.Path, []byte(strings.Replace(string(original), "- 负责人:\n", "- 负责人: codex\n", 1)), 0o644)
	oldWrite := writeDocumentFn
	writeDocumentFn = func(string, board.Entry, string) error { return os.ErrPermission }
	t.Cleanup(func() { writeDocumentFn = oldWrite })
	orig = string(original)
	err = rollbackLaunch(root, moved, "todo", &LaunchFailure{Err: launchError("tmux new-window 失败", "tmux new-window 失败")}, &orig)
	if err == nil || !strings.Contains(err.Error(), "卡片保留在 working") {
		t.Fatalf("err=%v", err)
	}
	if _, stat := os.Stat(moved.Path); stat != nil {
		t.Fatal("should stay in working")
	}
}

func TestWindowsRejectsTmuxBeforeClaim(t *testing.T) {
	root, _, _ := setupBoard(t)
	runtimeWindows = func() bool { return true }
	t.Cleanup(func() { runtimeWindows = func() bool { return isWindowsGOOS() } })
	taskID, path := makeTodo(t, root, "win-tmux")
	original, _ := os.ReadFile(path)
	_, _, err := capture(t, func() error { return commandStart(root, "", "tmux", taskID) })
	if err == nil || !strings.Contains(err.Error(), "Windows") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("claimed on windows")
	}
}

func TestStartRemovesTaskFileWhenClaimFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, _ := makeTodo(t, root, "task-file-cleanup")
	oldCreate, oldMove := createTaskFile, moveEntryFn
	var taskFile string
	createTaskFile = func(body, prefix string) (string, error) {
		path, err := oldCreate(body, prefix)
		taskFile = path
		return path, err
	}
	moveEntryFn = func(board.Entry, string, string) (board.Entry, error) {
		return board.Entry{}, os.ErrPermission
	}
	t.Cleanup(func() {
		createTaskFile = oldCreate
		moveEntryFn = oldMove
	})
	if err := commandStart(root, "", "", taskID); err == nil {
		t.Fatal("expected claim failure")
	}
	if taskFile == "" {
		t.Fatal("task file was not created")
	}
	if _, err := os.Stat(taskFile); !os.IsNotExist(err) {
		t.Fatalf("failed launch left task file %s", taskFile)
	}
}

func TestTakeoverHookReportsNA(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "takeover-na")
	_, _, err := capture(t, func() error { return commandStart(root, "claude", "", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "working", filepath.Base(path))
	setBranch(t, working)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, taskID)
	if _, err := board.MoveEntry(entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	agent := "grok"
	out, _, err := capture(t, func() error {
		return commandResume(root, &agent, "", taskID, "继续", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已接管") || !strings.Contains(out, "已清理原容器: N/A") {
		t.Fatalf("stdout=%s", out)
	}
	// 接管不迁卡: 卡片保持 review, 元数据在原位改写.
	text, _ := os.ReadFile(filepath.Join(root, "review", filepath.Base(path)))
	if !strings.Contains(string(text), "- 负责人: grok\n") {
		t.Fatalf("card=%s", text)
	}
	if _, err := os.Stat(working); err == nil {
		t.Fatal("takeover must not move the card to working")
	}
}

func TestForegroundRejectsNonTTY(t *testing.T) {
	root, _, _ := setupBoard(t)
	stdinIsTTY, stdoutIsTTY, stderrIsTTY = func() bool { return false }, func() bool { return false }, func() bool { return false }
	t.Cleanup(func() {
		stdinIsTTY = func() bool { return fileIsTTY(os.Stdin) }
		stdoutIsTTY = func() bool { return fileIsTTY(os.Stdout) }
		stderrIsTTY = func() bool { return fileIsTTY(os.Stderr) }
	})
	taskID, path := makeTodo(t, root, "fg-tty")
	_, _, err := capture(t, func() error { return commandStart(root, "", "foreground", taskID) })
	if err == nil || !strings.Contains(err.Error(), "前台启动模式需要交互终端") {
		t.Fatalf("err=%v", err)
	}
	if _, stat := os.Stat(path); stat != nil {
		t.Fatal("should remain in todo")
	}
}

func TestHerdrStartWritesWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	log := filepath.Join(root, "herdr.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	writeFakeHerdr(t, filepath.Join(fakeBin, "herdr"), log)
	taskID, path := makeTodo(t, root, "herdr-start")
	out, _, err := capture(t, func() error { return commandStart(root, "claude", "herdr", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "启动方式=herdr") || !strings.Contains(out, "tab=w1:t9") {
		t.Fatalf("stdout=%s", out)
	}
	text, _ := os.ReadFile(filepath.Join(root, "working", filepath.Base(path)))
	if !strings.Contains(string(text), "- 窗口: herdr:w1:t9:w1:p9\n") {
		t.Fatalf("card=%s", text)
	}
}

func TestResumeWrongState(t *testing.T) {
	root, _, _ := setupBoard(t)
	taskID, _ := makeTodo(t, root, "resume-todo")
	_, _, err := capture(t, func() error {
		return commandResume(root, nil, "", taskID, "x", "", true, 61)
	})
	if err == nil || !strings.Contains(err.Error(), "todo") {
		t.Fatalf("err=%v", err)
	}
}

func TestWindowsConsoleLauncher(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows console launcher only")
	}
	testConsoleStart(t)
}

func TestConsoleRejectedOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("console is the Windows default")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "console-posix")
	original, _ := os.ReadFile(path)
	_, _, err := capture(t, func() error { return commandStart(root, "", "console", taskID) })
	if err == nil || !strings.Contains(err.Error(), "console") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("claimed without console")
	}
}

func TestConsoleStartRecordsWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by TestWindowsConsoleLauncher")
	}
	runtimeWindows = func() bool { return true }
	t.Cleanup(func() { runtimeWindows = func() bool { return isWindowsGOOS() } })
	testConsoleStart(t)
}

func testConsoleStart(t *testing.T) {
	t.Helper()
	root, _, _ := setupBoard(t)
	oldStart := startProcessFn
	startProcessFn = func(argv []string, env map[string]string, cwd string, console bool) (*startedProc, error) {
		if !console {
			t.Fatal("expected console process")
		}
		if cwd != filepath.Dir(root) {
			t.Fatalf("cwd=%s", cwd)
		}
		ch := make(chan int, 1)
		ch <- 0
		p := &startedProc{proc: &os.Process{Pid: 4242}, wait: ch}
		code := 0
		p.code = &code
		return p, nil
	}
	t.Cleanup(func() { startProcessFn = oldStart })
	taskID, path := makeTodo(t, root, "console-start")
	out, _, err := capture(t, func() error { return commandStart(root, "claude", "console", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "启动方式=console") || !strings.Contains(out, "PID=4242") {
		t.Fatalf("stdout=%s", out)
	}
	text, _ := os.ReadFile(filepath.Join(root, "working", filepath.Base(path)))
	if !strings.Contains(string(text), "- 窗口: console\n") {
		t.Fatalf("card=%s", text)
	}
}
