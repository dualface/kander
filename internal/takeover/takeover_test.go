package takeover

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
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

func setupBoard(t *testing.T) (root, fakeBin string) {
	t.Helper()
	resetLang(t)
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root = t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	fakeBin = filepath.Join(root, "fake-bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeHerdr(t, filepath.Join(fakeBin, "herdr"), filepath.Join(root, "herdr.log"))
	writeFakeTmux(t, filepath.Join(fakeBin, "tmux"), filepath.Join(root, "tmux.log"))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KANBAN_HERDR_LOG", filepath.Join(root, "herdr.log"))
	t.Setenv("KANBAN_TMUX_LOG", filepath.Join(root, "tmux.log"))
	return root, fakeBin
}

func writeFakeHerdr(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_HERDR_LOG}"
if [ "$1" = "agent" ] && [ "$2" = "prompt" ]; then
  printf '%s\n' "$@" > "$log.prompt"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  if [ -n "${KANBAN_HERDR_STALE_PANE:-}" ] && [ "$3" = "$KANBAN_HERDR_STALE_PANE" ]; then
    printf '%s\n' '{"error":{"code":"pane_not_found","message":"gone"}}' >&2
    exit 1
  fi
  agent="${KANBAN_HERDR_AGENT:-claude}"
  status="${KANBAN_HERDR_STATUS:-idle}"
  tab="${KANBAN_HERDR_TAB_ID:-w1:t8}"
  session="${KANBAN_HERDR_SESSION:-session-1}"
  if [ -f "$log.prompt" ]; then
    printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"$tab\"}}}"
    exit 0
  fi
  printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"$tab\",\"agent\":\"$agent\",\"agent_status\":\"$status\",\"agent_session\":{\"value\":\"$session\"}}}}"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "list" ]; then
  if [ -n "${KANBAN_HERDR_LIST_JSON:-}" ]; then
    printf '%s\n' "$KANBAN_HERDR_LIST_JSON"
    exit 0
  fi
  session="${KANBAN_HERDR_SESSION:-session-1}"
  printf '%s\n' "{\"id\":\"cli:pane:list\",\"result\":{\"panes\":[{\"pane_id\":\"w1:p8\",\"tab_id\":\"w1:t8\",\"agent\":\"claude\",\"agent_status\":\"idle\",\"agent_session\":{\"value\":\"$session\"}}]}}"
  exit 0
fi
if [ "$1" = "tab" ] && [ "$2" = "close" ]; then
  printf '%s\n' "$@" > "$log.close"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeTmux(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_TMUX_LOG}"
if [ "$1" = "display-message" ]; then
  target=""
  i=1
  while [ "$i" -le "$#" ]; do
    eval "arg=\${$i}"
    if [ "$arg" = "-t" ]; then
      i=$((i+1))
      eval "target=\${$i}"
    fi
    i=$((i+1))
  done
  if [ -n "${KANBAN_TMUX_STALE_PANE:-}" ] && [ "$target" = "$KANBAN_TMUX_STALE_PANE" ]; then
    printf '%s\n' "can't find pane $target" >&2
    exit 1
  fi
  case "${5:-$6}" in
    *pane_current_command*pane_in_mode*pane_dead*)
      dead="${KANBAN_TMUX_DEAD:-0}"
      [ -f "$log.instruction" ] && dead=1
      printf '%s\t%s\t%s\n' "${KANBAN_TMUX_CURRENT_COMMAND:-claude}" "${KANBAN_TMUX_IN_MODE:-0}" "$dead"
      exit 0
      ;;
    *session_id*session_name*window_id*window_panes*)
      printf '%s\t%s\t%s\t%s\n' "${KANBAN_TMUX_TARGET_SESSION:-\$88}" "${KANBAN_TMUX_TARGET_SESSION_NAME:-relocated}" "${KANBAN_TMUX_TARGET_WINDOW:-@8}" "${KANBAN_TMUX_WINDOW_PANES:-1}"
      exit 0
      ;;
    '#{window_id}')
      printf '%s\n' "$target"
      exit 0
      ;;
  esac
  printf '%s\n' '$88'
  exit 0
fi
if [ "$1" = "show-options" ]; then
  printf '%s\n' "${KANBAN_TMUX_PANE_SESSION:-session-1}"
  exit 0
fi
if [ "$1" = "list-panes" ]; then
  printf '%s\n' "$KANBAN_TMUX_LIST_PANES"
  exit 0
fi
if [ "$1" = "send-keys" ]; then
  printf '%s\n' "$5" >> "$log.instruction"
  exit 0
fi
if [ "$1" = "kill-window" ]; then
  printf '%s\n' "$@" > "$log.kill"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func doneCard(title string) string {
	return "# " + title + "\n\n- 类型: Feature\n- 任务组:\n- 创建时间: 2026-09-04 02:19\n- 负责人: claude\n- 会话: claude session-1\n- 窗口: herdr:w1:t9:w1:p9\n- 开始时间: 2026-09-04 11:00\n- 完成时间: 2026-09-04 12:00\n- 任务分支: task/demo\n- 结果: completed\n\n## 任务目标\n\n实现目标\n\n## 用户决策\n\nN/A\n\n## 预期成果\n\n产生可验证结果\n\n## 验收条件\n\n- [ ] 满足验收\n\n## 威胁模型\n\nN/A\n\n## 不在本轮范围\n\n- 无额外范围\n\n## 讨论与决策\n\n自审: 通过\n卡审: 通过\n\n## 实施与验证\n\n## 完成总结\n完成\n"
}

func makeDone(t *testing.T, root, slug, window string) (string, string) {
	t.Helper()
	path, err := board.NewTask(root, "feature", slug, "任务 "+slug, false)
	if err != nil {
		t.Fatal(err)
	}
	text := doneCard("任务 " + slug)
	text = regexp.MustCompile(`(?m)^- 窗口:.*$`).ReplaceAllLiteralString(text, "- 窗口: "+window)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := board.LoadBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := board.Locate(loaded, filepath.Base(strings.TrimSuffix(path, ".md")))
	if err != nil {
		t.Fatal(err)
	}
	todo, err := board.MoveEntry(entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	working, err := board.MoveEntry(todo, root, "working")
	if err != nil {
		t.Fatal(err)
	}
	review, err := board.MoveEntry(working, root, "review")
	if err != nil {
		t.Fatal(err)
	}
	done, err := board.MoveEntry(review, root, "done")
	if err != nil {
		t.Fatal(err)
	}
	return done.TaskID, done.Path
}

func TestDismissStaleHerdrDoesNotRewriteWindow(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p9")
	t.Setenv("KANBAN_HERDR_TAB_ID", "w1:t8")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{"id":"cli:pane:list","result":{"type":"pane_list","panes":[{"pane_id":"w1:p8","tab_id":"w1:t8","agent":"claude","agent_status":"idle","agent_session":{"value":"session-1"}}]}}`)
	taskID, path := makeDone(t, root, "dismiss-stale-herdr", "herdr:w1:t9:w1:p9")
	before, _ := os.ReadFile(path)
	out, _, err := capture(t, func() error { return commandDismiss(root, taskID, 61) })
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("card mutated")
	}
	if !strings.Contains(out, "关闭容器=w1:t8") {
		t.Fatalf("out=%s", out)
	}
	prompt, _ := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	if string(prompt) != "agent prompt w1:p8 /exit\n" && !strings.Contains(string(prompt), "w1:p8") {
		t.Fatalf("prompt=%q", prompt)
	}
	closeLog, _ := os.ReadFile(filepath.Join(root, "herdr.log.close"))
	if !strings.Contains(string(closeLog), "w1:t8") {
		t.Fatalf("close=%q", closeLog)
	}
}

func TestDismissStaleTmuxRevalidatesContainer(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_TMUX_STALE_PANE", "%9")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "claude")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
	t.Setenv("KANBAN_TMUX_LIST_PANES", "%8\t$88\trelocated\t@8\tclaude\t0\tsession-1")
	t.Setenv("KANBAN_TMUX_TARGET_SESSION", "$88")
	t.Setenv("KANBAN_TMUX_TARGET_SESSION_NAME", "relocated")
	t.Setenv("KANBAN_TMUX_TARGET_WINDOW", "@8")
	taskID, path := makeDone(t, root, "dismiss-stale-tmux", "tmux:$1:@1:%9")
	before, _ := os.ReadFile(path)
	out, _, err := capture(t, func() error { return commandDismiss(root, taskID, 61) })
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("card mutated")
	}
	if !strings.Contains(out, "通道=tmux") || !strings.Contains(out, "关闭容器=@8") {
		t.Fatalf("out=%s", out)
	}
}

func TestCleanupRetainsMismatchedHerdr(t *testing.T) {
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION", "other")
	t.Setenv("KANBAN_HERDR_TAB_ID", "w1:t1")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{"result":{"panes":[{"pane_id":"w1:p1","tab_id":"w1:t1","agent":"claude","agent_status":"idle","agent_session":{"value":"other"}}]}}`)
	result := Cleanup("herdr:w1:t1:w1:p1", launch.AgentSession{Agent: "claude", Reference: "old"}, "herdr:w1:t2:w1:p2", 61)
	if result.Cleaned {
		t.Fatalf("%+v", result)
	}
	if !strings.Contains(result.Detail, "身份不匹配") {
		t.Fatalf("detail=%s", result.Detail)
	}
}

func TestCleanupClosesMatchingHerdr(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION", "old")
	t.Setenv("KANBAN_HERDR_TAB_ID", "w1:t1")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{"result":{"panes":[{"pane_id":"w1:p1","tab_id":"w1:t1","agent":"claude","agent_status":"idle","agent_session":{"value":"old"}}]}}`)
	result := Cleanup("herdr:w1:t1:w1:p1", launch.AgentSession{Agent: "claude", Reference: "old"}, "herdr:w1:t2:w1:p2", 61)
	if !result.Cleaned {
		t.Fatalf("%+v", result)
	}
	prompt, _ := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	if !strings.Contains(string(prompt), "/exit") {
		t.Fatalf("prompt=%s", prompt)
	}
}

func TestCleanupForegroundIsNA(t *testing.T) {
	result := Cleanup("foreground", launch.AgentSession{Agent: "claude", Reference: "x"}, "tmux:$1:@1:%1", 61)
	if !result.Cleaned || result.Channel != "N/A" {
		t.Fatalf("%+v", result)
	}
}

func TestAgentExitCommands(t *testing.T) {
	exit, err := AgentExitCommand("claude")
	if err != nil || exit != "/exit" {
		t.Fatal(exit, err)
	}
	quit, err := AgentExitCommand("cursor")
	if err != nil || quit != "/quit" {
		t.Fatal(quit, err)
	}
}
