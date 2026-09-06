package notify

import (
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

func setupBoard(t *testing.T) (root, fakeBin string) {
	t.Helper()
	resetLang(t)
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root = t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(root, "config.json"))
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
log="${KANBAN_HERDR_LOG:-` + log + `}"
if [ "$1" = "tab" ] && [ "$2" = "create" ]; then
  printf '%s\n' "$@" >> "$log.order"
  printf '%s\n' '{"id":"cli:tab:create","result":{"type":"tab_created","tab":{"tab_id":"w1:t9"},"root_pane":{"pane_id":"w1:p9","tab_id":"w1:t9"}}}'
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "wait-output" ]; then
  printf '%s\n' "$@" >> "$log.wait"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "run" ]; then
  printf '%s\n' "$@" >> "$log.run"
  printf '%s\n' "tab create" >> "$log.order"
  exit 0
fi
if [ "$1" = "agent" ] && [ "$2" = "prompt" ]; then
  printf '%s\n' "$@" > "$log.prompt"
  printf '%s\n' "$@" >> "$log.order"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  if [ "${KANBAN_HERDR_GET_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr get failure' >&2
    exit 1
  fi
  if [ -n "${KANBAN_HERDR_STALE_PANE:-}" ] && [ "$3" = "$KANBAN_HERDR_STALE_PANE" ]; then
    printf '%s\n' '{"error":{"code":"pane_not_found","message":"gone"}}' >&2
    exit 1
  fi
  if [ "${KANBAN_HERDR_BUSY_ONCE:-}" = "1" ] && [ ! -f "$log.busy" ]; then
    printf '%s\n' busy > "$log.busy"
    status=working
  else
    status="${KANBAN_HERDR_STATUS:-idle}"
  fi
  agent="${KANBAN_HERDR_AGENT:-claude}"
  tab="${KANBAN_HERDR_TAB_ID:-w1:t9}"
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
  agent="${KANBAN_HERDR_AGENT:-claude}"
  printf '%s\n' "{\"id\":\"cli:pane:list\",\"result\":{\"type\":\"pane_list\",\"panes\":[{\"pane_id\":\"w1:p9\",\"tab_id\":\"w1:t9\",\"agent\":\"$agent\",\"agent_status\":\"idle\",\"agent_session\":{\"value\":\"$session\"}}]}}"
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
log="${KANBAN_TMUX_LOG:-` + log + `}"
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
      printf '%s\t%s\t%s\t%s\n' "${KANBAN_TMUX_TARGET_SESSION:-\$42}" "${KANBAN_TMUX_TARGET_SESSION_NAME:-fake-session}" "${KANBAN_TMUX_TARGET_WINDOW:-@9}" "${KANBAN_TMUX_WINDOW_PANES:-1}"
      exit 0
      ;;
    '#{window_id}')
      printf '%s\n' "$target"
      exit 0
      ;;
  esac
  printf '%s\n' '$42'
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
  printf '%s\n' "$@" >> "$log.send"
  printf '%s\n' "$5" >> "$log.instruction"
  exit 0
fi
if [ "$1" = "capture-pane" ]; then
  cat "$log.send" 2>/dev/null
  printf '\n'
  exit 0
fi
if [ "$1" = "kill-window" ]; then
  printf '%s\n' "$@" > "$log.kill"
  exit 0
fi
printf '%s\t%s\n' '@9' '%9'
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func cardTemplate(title string) string {
	return "# " + title + "\n\n- 类型: Feature\n- 任务组:\n- 创建时间: 2026-09-04 02:19\n- 负责人: claude\n- 会话: claude session-1\n- 窗口: herdr:w1:t9:w1:p9\n- 开始时间: 2026-09-04 11:00\n- 完成时间:\n- 任务分支: task/demo\n- 结果:\n\n## 任务目标\n\n实现目标\n\n## 用户决策\n\nN/A\n\n## 预期成果\n\n产生可验证结果\n\n## 验收条件\n\n- [ ] 满足验收\n\n## 威胁模型\n\nN/A\n\n## 不在本轮范围\n\n- 无额外范围\n\n## 讨论与决策\n\n自审: 通过\n卡审: 通过\n\n## 实施与验证\n\n## 完成总结\n"
}

func makeReview(t *testing.T, root, slug string) (string, string) {
	t.Helper()
	path, err := board.NewTask(root, "feature", slug, "任务 "+slug, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(cardTemplate("任务 "+slug)), 0o644); err != nil {
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
	return review.TaskID, review.Path
}

func setWindow(t *testing.T, path, window string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := regexp.MustCompile(`(?m)^- 窗口:.*$`).ReplaceAllLiteralString(string(data), "- 窗口: "+window)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyStaleHerdrRewritesAndDelivers(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w0:p0")
	taskID, path := makeReview(t, root, "notify-stale-herdr")
	setWindow(t, path, "herdr:w0:t0:w0:p0")
	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "通道=herdr-direct") {
		t.Fatalf("out=%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, "working", filepath.Base(path))); !os.IsNotExist(statErr) {
		t.Fatal("notify must not move the card out of review")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "- 窗口: herdr:w1:t9:w1:p9\n") {
		t.Fatalf("window not rewritten: %s", body)
	}
	prompt, _ := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	if !strings.Contains(string(prompt), "w1:p9") {
		t.Fatalf("prompt=%s", prompt)
	}
}

func TestNotifyKeepsReviewAndRequiresSelfMove(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	taskID, path := makeReview(t, root, "notify-self-move")
	setWindow(t, path, "herdr:w1:t9:w1:p9")
	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("card must stay in review after notify")
	}
	if _, statErr := os.Stat(filepath.Join(root, "working", filepath.Base(path))); !os.IsNotExist(statErr) {
		t.Fatal("notify must not move the card to working")
	}
	fields := regexp.MustCompile(`消息文件=(\S+)`).FindStringSubmatch(out)
	if len(fields) != 2 {
		t.Fatalf("no message file in output: %s", out)
	}
	payload, err := os.ReadFile(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "move "+taskID+" working") {
		t.Fatalf("payload must require self move: %s", payload)
	}
}

func TestNotifyAmbiguousLookupDegradesToResumeReason(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w0:p0")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{
  "id": "cli:pane:list",
  "result": {"type": "pane_list", "panes": [
    {"pane_id": "w1:p9", "tab_id": "w1:t9", "agent": "claude", "agent_status": "idle", "agent_session": {"value": "session-1"}},
    {"pane_id": "w2:p8", "tab_id": "w2:t8", "agent": "claude", "agent_status": "idle", "agent_session": {"value": "session-1"}}
  ]}
}`)
	taskID, path := makeReview(t, root, "notify-stale-ambiguous")
	setWindow(t, path, "herdr:w0:t0:w0:p0")
	out, errOut, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err == nil {
		if !strings.Contains(out, "通道=resume") {
			t.Fatalf("expected resume, out=%s err=%s", out, errOut)
		}
		if !strings.Contains(out, "直投原因=地址过期:") || !strings.Contains(out, "反查=") {
			t.Fatalf("missing dual reasons: %s", out)
		}
		return
	}
	if !strings.Contains(err.Error(), "直投=") || !strings.Contains(err.Error(), "恢复=") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestNotifyBusyTimeoutDoesNotResumeOrWrite(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_STATUS", "working")
	taskID, path := makeReview(t, root, "notify-busy-timeout")
	before, _ := os.ReadFile(path)
	oldNow, oldSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = oldNow, oldSleep })
	base := time.Unix(0, 0)
	nowFn = func() time.Time { return base }
	sleepFn = func(time.Duration) { base = base.Add(62 * time.Second) }
	_, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err == nil || !isBusy(err) {
		t.Fatalf("want busy, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("card mutated")
	}
	if _, statErr := os.Stat(filepath.Join(root, "working", filepath.Base(path))); !os.IsNotExist(statErr) {
		t.Fatal("moved to working")
	}
}

func TestNotifyBusyRetriesThenDelivers(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_BUSY_ONCE", "1")
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	taskID, _ := makeReview(t, root, "notify-busy-retry")
	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "通道=herdr-direct") {
		t.Fatalf("out=%s", out)
	}
}

func TestNotifyCopyModeIsBusyWithoutLookup(t *testing.T) {
	root, _ := setupBoard(t)
	taskID, path := makeReview(t, root, "notify-copy")
	setWindow(t, path, "tmux:$1:@1:%1")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "claude")
	t.Setenv("KANBAN_TMUX_IN_MODE", "1")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
	oldNow, oldSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = oldNow, oldSleep })
	base := time.Unix(0, 0)
	nowFn = func() time.Time { return base }
	sleepFn = func(time.Duration) { base = base.Add(62 * time.Second) }
	_, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err == nil || !isBusy(err) {
		t.Fatalf("want busy, got %v", err)
	}
}

func TestNotifyStaleTmuxRewritesWindow(t *testing.T) {
	root, _ := setupBoard(t)
	taskID, path := makeReview(t, root, "notify-stale-tmux")
	setWindow(t, path, "tmux:$42:@old:%old")
	t.Setenv("KANBAN_TMUX_STALE_PANE", "%old")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "claude")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
	t.Setenv("KANBAN_TMUX_LIST_PANES", "%new\t$42\tfake-session\t@new\tclaude\t0\tsession-1")
	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "通道=tmux-direct") {
		t.Fatalf("out=%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, "working", filepath.Base(path))); !os.IsNotExist(statErr) {
		t.Fatal("notify must not move the card out of review")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "- 窗口: tmux:$42:@new:%new\n") {
		t.Fatalf("body=%s", body)
	}
}

// On Windows, FlushFileBuffers on the write end of a pipe blocks until the read end drains it.
// The output of kander notify is usually consumed by an agent or `| more`, so a broken guard means a deadlock.
func TestFlushStdoutDoesNotBlockOnPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()
	if _, err := writer.WriteString("pending output\n"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		flushStdout()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("flushStdout blocked on an undrained pipe")
	}
}
