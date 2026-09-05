package liveness

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func capture(t *testing.T, fn func() int) (int, string, string) {
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
	code := fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ := io.ReadAll(outR)
	errb, _ := io.ReadAll(errR)
	_ = outR.Close()
	_ = errR.Close()
	return code, string(out), string(errb)
}

func tempBoard(t *testing.T) string {
	t.Helper()
	resetLang(t)
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	return root
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

func setMeta(t *testing.T, path, old, neu string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, neu, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func todayID(slug string) string {
	return time.Now().Format("20060102") + "-" + slug + "-task"
}

func makeWorking(t *testing.T, slug, title string) (string, string) {
	t.Helper()
	id := todayID(slug)
	if _, err := board.NewTask(os.Getenv(board.EnvBoardDir), "chore", slug, title, false); err != nil {
		t.Fatal(err)
	}
	root := os.Getenv(board.EnvBoardDir)
	path := filepath.Join(root, "backlog", id+".md")
	makeReady(t, path)
	setMeta(t, path, "- 任务分支:\n", "- 任务分支: task/"+slug+"\n")
	moved, err := board.MoveEntry(board.Entry{
		TaskID: id, State: "backlog", Path: path, Document: path, Kind: "small",
	}, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	moved, err = board.MoveEntry(moved, root, "working")
	if err != nil {
		t.Fatal(err)
	}
	return id, moved.Path
}

func setLocation(t *testing.T, path, session, window string) {
	t.Helper()
	setMeta(t, path, "- 会话:\n", "- 会话: "+session+"\n")
	setMeta(t, path, "- 窗口:\n", "- 窗口: "+window+"\n")
}

func writeFakeTmux(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "list-panes" ] && [ "$2" = "-a" ] && [ "$3" = "-F" ]; then
  if [ "${KANBAN_TMUX_LIST_PANES_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake tmux list-panes failure' >&2
    exit 1
  fi
  printf '%s\n' "${KANBAN_TMUX_LIST_PANES:-}"
  exit 0
fi
if [ "$1" = "display-message" ]; then
  if [ -n "${KANBAN_TMUX_STALE_PANE:-}" ] && [ "$4" = "$KANBAN_TMUX_STALE_PANE" ]; then
    printf '%s\n' "can't find pane: $4" >&2
    exit 1
  fi
  printf '%s\t%s\t%s\n' "${KANBAN_TMUX_CURRENT_COMMAND:-codex}" "${KANBAN_TMUX_IN_MODE:-0}" "${KANBAN_TMUX_DEAD:-0}"
  exit 0
fi
if [ "$1" = "show-options" ] && [ "$2" = "-p" ]; then
  if [ -n "${KANBAN_TMUX_PANE_SESSION:-}" ]; then
    printf '%s\n' "$KANBAN_TMUX_PANE_SESSION"
    exit 0
  fi
  printf '%s\n' 'invalid option: '"$6" >&2
  exit 1
fi
printf '%s\n' "unexpected tmux args: $*" >&2
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeHerdr(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  if [ -n "${KANBAN_HERDR_STALE_PANE:-}" ] && [ "$3" = "$KANBAN_HERDR_STALE_PANE" ]; then
    printf '%s\n' '{"id":"cli:pane:get","error":{"code":"pane_not_found","message":"gone"}}' >&2
    exit 1
  fi
  if [ "${KANBAN_HERDR_GET_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake pane not found' >&2
    exit 1
  fi
  printf '{"id":"cli:pane:get","result":{"type":"pane_info","pane":{"pane_id":"%s","tab_id":"%s","agent":"%s","agent_status":"%s","agent_session":{"value":"%s"}}}}\n' \
    "$3" "${KANBAN_HERDR_TAB_ID:-w1:t9}" "${KANBAN_HERDR_AGENT:-codex}" "${KANBAN_HERDR_STATUS:-idle}" "${KANBAN_HERDR_SESSION:-}"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "list" ]; then
  if [ -n "${KANBAN_HERDR_LIST_JSON:-}" ]; then
    printf '%s\n' "$KANBAN_HERDR_LIST_JSON"
    exit 0
  fi
  printf '{"id":"cli:pane:list","result":{"type":"pane_list","panes":[{"pane_id":"w1:p9","tab_id":"%s","agent":"%s","agent_status":"idle","agent_session":{"value":"%s"}}]}}\n' \
    "${KANBAN_HERDR_TAB_ID:-w1:t9}" "${KANBAN_HERDR_AGENT:-codex}" "${KANBAN_HERDR_SESSION:-}"
  exit 0
fi
printf '%s\n' "unexpected herdr args: $*" >&2
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func installFakes(t *testing.T, herdr bool) {
	t.Helper()
	bin := t.TempDir()
	writeFakeTmux(t, filepath.Join(bin, "tmux"))
	if herdr {
		writeFakeHerdr(t, filepath.Join(bin, "herdr"))
	}
	t.Setenv("PATH", bin)
}

func TestCheckReportsAliveStoppedAndUnknown(t *testing.T) {
	root := tempBoard(t)
	_ = root
	installFakes(t, false)
	aliveID, alive := makeWorking(t, "liveness-alive", "存活")
	stoppedID, stopped := makeWorking(t, "liveness-stopped", "停止")
	unknownID, unknown := makeWorking(t, "liveness-unknown", "未知")
	setLocation(t, alive, "codex session-1", "tmux:$1:@1:%1")
	setLocation(t, stopped, "codex session-1", "tmux:$1:@2:%2")
	setLocation(t, unknown, "codex session-1", "foreground")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
	t.Setenv("KANBAN_TMUX_STALE_PANE", "%2")

	code, out, _ := capture(t, func() int { return RunCheck(nil) })
	if code != 0 {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "存活: "+aliveID+"\tAgent=codex\t状态=alive") {
		t.Fatalf("alive missing: %s", out)
	}
	if !strings.Contains(out, "存活: "+stoppedID+"\tAgent=codex\t状态=stopped") {
		t.Fatalf("stopped missing: %s", out)
	}
	if !strings.Contains(out, "存活: "+unknownID+"\tAgent=codex\t状态=unknown") {
		t.Fatalf("unknown missing: %s", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "通过: 3 个任务") {
		t.Fatalf("summary: %s", out)
	}
}

func TestCheckReportsDriftedWithNewAddress(t *testing.T) {
	tempBoard(t)
	installFakes(t, false)
	taskID, path := makeWorking(t, "liveness-drifted", "漂移")
	setLocation(t, path, "codex session-2", "tmux:$1:@1:%1")
	t.Setenv("KANBAN_TMUX_STALE_PANE", "%1")
	t.Setenv("KANBAN_TMUX_LIST_PANES", "%9\t$9\tnew-session\t@9\tcodex\t0\tsession-2")

	code, out, _ := capture(t, func() int { return RunCheck([]string{taskID}) })
	if code != 0 {
		t.Fatalf("code=%d %s", code, out)
	}
	if !strings.Contains(out, "状态=drifted") || !strings.Contains(out, "新地址=tmux:$9:@9:%9") {
		t.Fatalf("out=%s", out)
	}
	if !strings.Contains(out, "建议=kander notify "+taskID) {
		t.Fatalf("suggestion: %s", out)
	}
}

func TestCheckUnknownReverseLookedUpHerdrStatusIsNotDrifted(t *testing.T) {
	tempBoard(t)
	installFakes(t, true)
	taskID, path := makeWorking(t, "liveness-drifted-unknown", "反查未知")
	setLocation(t, path, "claude session-unknown", "herdr:w0:t0:w0:p0")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w0:p0")
	t.Setenv("KANBAN_HERDR_AGENT", "claude")
	t.Setenv("KANBAN_HERDR_SESSION", "session-unknown")
	t.Setenv("KANBAN_HERDR_STATUS", "unknown")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{"result":{"panes":[{"pane_id":"w9:p9","tab_id":"w9:t9","agent":"claude","agent_status":"unknown","agent_session":{"value":"session-unknown"}}]}}`)

	code, out, _ := capture(t, func() int { return RunCheck([]string{taskID}) })
	if code != 0 {
		t.Fatalf("code=%d %s", code, out)
	}
	if !strings.Contains(out, "状态=unknown") || strings.Contains(out, "状态=drifted") || strings.Contains(out, "新地址=") {
		t.Fatalf("out=%s", out)
	}
	if !strings.Contains(out, "反查 pane 的 Agent 状态不可判定") {
		t.Fatalf("out=%s", out)
	}
}

func TestCheckLivenessDoesNotAffectExitCode(t *testing.T) {
	tempBoard(t)
	installFakes(t, false)
	_, path := makeWorking(t, "liveness-exit", "退出码")
	setLocation(t, path, "codex session-3", "tmux:$1:@1:%1")
	t.Setenv("KANBAN_TMUX_STALE_PANE", "%1")

	code, out, _ := capture(t, func() int { return RunCheck(nil) })
	if code != 0 || !strings.Contains(out, "状态=stopped") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestCheckSkipsReviewAndProbesOnlyWorking(t *testing.T) {
	root := tempBoard(t)
	installFakes(t, false)
	workingID, working := makeWorking(t, "liveness-working-only", "工作")
	reviewID, review := makeWorking(t, "liveness-review-skip", "审核")
	setLocation(t, working, "codex session-4", "tmux:$1:@1:%1")
	setLocation(t, review, "codex session-4", "tmux:$1:@1:%1")
	moved, err := board.MoveEntry(board.Entry{
		TaskID: reviewID, State: "working", Path: review, Document: review, Kind: "small",
	}, root, "review")
	if err != nil {
		t.Fatal(err)
	}
	_ = moved
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-4")

	code, out, _ := capture(t, func() int { return RunCheck([]string{workingID, reviewID}) })
	if code != 0 {
		t.Fatalf("code=%d %s", code, out)
	}
	if !strings.Contains(out, "存活: "+workingID) {
		t.Fatalf("working missing: %s", out)
	}
	if strings.Contains(out, "存活: "+reviewID) {
		t.Fatalf("review should skip: %s", out)
	}
}

func TestCheckProbeFailuresDegradeToUnknown(t *testing.T) {
	tempBoard(t)
	installFakes(t, true)
	taskID, path := makeWorking(t, "liveness-probe-failure", "探测失败")
	setLocation(t, path, "codex session-5", "herdr:w1:t1:w1:p1")
	t.Setenv("KANBAN_HERDR_GET_FAIL", "1")

	code, out, _ := capture(t, func() int { return RunCheck([]string{taskID}) })
	if code != 0 || !strings.Contains(out, "状态=unknown") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestCheckUnreportedHerdrSessionIsAliveUndeliverable(t *testing.T) {
	tempBoard(t)
	installFakes(t, true)
	taskID, path := makeWorking(t, "liveness-unreported", "未上报")
	setLocation(t, path, "codex session-6", "herdr:w1:t1:w1:p1")
	t.Setenv("KANBAN_HERDR_SESSION", "")

	code, out, _ := capture(t, func() int { return RunCheck([]string{taskID}) })
	if code != 0 {
		t.Fatalf("code=%d %s", code, out)
	}
	if !strings.Contains(out, "状态=alive") || !strings.Contains(out, "会话身份未上报") || !strings.Contains(out, "直投不可用") {
		t.Fatalf("out=%s", out)
	}
}

func TestCheckCodexWithoutReferenceUsesAgentIdentity(t *testing.T) {
	tempBoard(t)
	installFakes(t, true)
	taskID, path := makeWorking(t, "liveness-codex-reference", "无引用")
	setLocation(t, path, "codex", "herdr:w1:t1:w1:p1")
	t.Setenv("KANBAN_HERDR_SESSION", "reported-session")

	code, out, _ := capture(t, func() int { return RunCheck([]string{taskID}) })
	if code != 0 || !strings.Contains(out, "状态=alive") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestWindowsLivenessIsUnknown(t *testing.T) {
	tempBoard(t)
	orig := isWindows
	isWindows = func() bool { return true }
	t.Cleanup(func() { isWindows = orig })
	taskID, path := makeWorking(t, "liveness-windows", "Windows")
	setLocation(t, path, "codex session-w", "tmux:$1:@1:%1")
	code, out, _ := capture(t, func() int { return RunCheck([]string{taskID}) })
	if code != 0 || !strings.Contains(out, "状态=unknown") || !strings.Contains(out, "Windows") {
		t.Fatalf("out=%s", out)
	}
}

func TestTmuxReverseLookupUniqueMarker(t *testing.T) {
	resetLang(t)
	installFakes(t, false)
	t.Setenv("KANBAN_TMUX_LIST_PANES", strings.Join([]string{
		"%1\t$1\tone\t@1\tclaude\t0\tother",
		"%2\t$2\ttwo\t@2\tcodex\t0\twanted",
		"%3\t$3\tthree\t@3\tclaude\t1\twanted",
		"%4\t$4\tfour\t@4\tclaude\t0\twanted",
	}, "\n"))
	loc, err := TmuxReverseLookup("tmux", TaskSession{Agent: "claude", Reference: "wanted"})
	if err != nil {
		t.Fatal(err)
	}
	if loc != (TmuxPaneLocation{SessionID: "$4", SessionName: "four", WindowID: "@4", PaneID: "%4"}) {
		t.Fatalf("%+v", loc)
	}
	if RenderTmuxWindow("tmux", loc) != "tmux:$4:@4:%4" {
		t.Fatal(RenderTmuxWindow("tmux", loc))
	}
	if RenderTmuxWindow("tmux-session", loc) != "tmux-session:four:@4:%4" {
		t.Fatal(RenderTmuxWindow("tmux-session", loc))
	}
	t.Setenv("KANBAN_TMUX_LIST_PANES", os.Getenv("KANBAN_TMUX_LIST_PANES")+"\n%5\t$5\tfive\t@5\tclaude\t0\twanted")
	_, err = TmuxReverseLookup("tmux", TaskSession{Agent: "claude", Reference: "wanted"})
	if err == nil || !strings.Contains(err.Error(), "匹配不唯一") {
		t.Fatalf("err=%v", err)
	}
}

func setTaskGroup(t *testing.T, path, group string) {
	t.Helper()
	setMeta(t, path, "- 任务组:\n", "- 任务组: "+group+"\n")
}

// subscriptionOutput permits the test to inspect output while Subscribe writes it.
type subscriptionOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *subscriptionOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *subscriptionOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startTestSubscription(t *testing.T, root string, opts subscribeOptions) (*subscriptionOutput, <-chan struct{}, func()) {
	t.Helper()
	buf := new(subscriptionOutput)
	stop := make(chan struct{})
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = Subscribe(root, opts, buf, stop)
		close(done)
	}()
	var once sync.Once
	finish := func() {
		t.Helper()
		once.Do(func() {
			close(stop)
			<-done
			if runErr != nil {
				t.Errorf("subscribe: %v", runErr)
			}
		})
	}
	// Join before earlier cleanups restore environment variables or remove the board.
	t.Cleanup(finish)
	return buf, done, finish
}

func TestSubscribeHeartbeatIncludesWorkingLiveness(t *testing.T) {
	tempBoard(t)
	installFakes(t, false)
	groupID := time.Now().Format("20060102") + "-liveness-heartbeat-group"
	taskID, path := makeWorking(t, "liveness-heartbeat", "心跳")
	setTaskGroup(t, path, groupID)
	setLocation(t, path, "codex session-7", "tmux:$1:@1:%1")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-7")

	buf, _, finish := startTestSubscription(t, os.Getenv(board.EnvBoardDir), subscribeOptions{
		Group: groupID, Members: []string{taskID}, Refresh: 0.05, Heartbeat: 0.12,
	})
	deadline := time.Now().Add(3 * time.Second)
	var snapshot, heartbeat map[string]any
	for time.Now().Before(deadline) {
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) >= 2 && lines[0] != "" && lines[1] != "" {
			if err := json.Unmarshal([]byte(lines[0]), &snapshot); err == nil {
				if err := json.Unmarshal([]byte(lines[1]), &heartbeat); err == nil && heartbeat["event"] == "heartbeat" {
					break
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	finish()
	if snapshot["event"] != "snapshot" {
		t.Fatalf("snapshot=%v", snapshot)
	}
	if _, ok := snapshot["liveness"]; ok {
		t.Fatalf("snapshot should omit liveness: %v", snapshot)
	}
	live, _ := heartbeat["liveness"].(map[string]any)
	item, _ := live[taskID].(map[string]any)
	if item["status"] != "alive" || item["channel"] != "tmux" {
		t.Fatalf("heartbeat=%v", heartbeat)
	}
}

func TestSubscribeHeartbeatProbeFailureReportsUnknownWithoutExiting(t *testing.T) {
	tempBoard(t)
	installFakes(t, true)
	groupID := time.Now().Format("20060102") + "-liveness-failure-group"
	taskID, path := makeWorking(t, "liveness-heartbeat-failure", "心跳失败")
	setTaskGroup(t, path, groupID)
	setLocation(t, path, "codex session-8", "herdr:w1:t1:w1:p1")
	t.Setenv("KANBAN_HERDR_GET_FAIL", "1")

	buf, done, finish := startTestSubscription(t, os.Getenv(board.EnvBoardDir), subscribeOptions{
		Group: groupID, Members: []string{taskID}, Refresh: 0.05, Heartbeat: 0.12,
	})
	deadline := time.Now().Add(3 * time.Second)
	var heartbeat map[string]any
	for time.Now().Before(deadline) {
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) >= 2 {
			_ = json.Unmarshal([]byte(lines[0]), new(map[string]any))
			if err := json.Unmarshal([]byte(lines[len(lines)-1]), &heartbeat); err == nil && heartbeat["event"] == "heartbeat" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if heartbeat["event"] != "heartbeat" {
		t.Fatalf("no heartbeat: %s", buf.String())
	}
	live, _ := heartbeat["liveness"].(map[string]any)
	item, _ := live[taskID].(map[string]any)
	if item["status"] != "unknown" {
		t.Fatalf("heartbeat=%v", heartbeat)
	}
	select {
	case <-done:
		t.Fatal("subscribe exited early")
	default:
	}
	finish()
}

func TestSubscribeSnapshotStateChangeAndWatch(t *testing.T) {
	root := tempBoard(t)
	groupID := time.Now().Format("20060102") + "-events-group"
	firstID, first := makeWorking(t, "event-first", "成员一")
	_, err := board.NewTask(root, "chore", "event-second", "成员二", false)
	if err != nil {
		t.Fatal(err)
	}
	secondID := todayID("event-second")
	second := filepath.Join(root, "backlog", secondID+".md")
	makeReady(t, second)
	setMeta(t, second, "- 任务分支:\n", "- 任务分支: task/event-second\n")
	setTaskGroup(t, first, groupID)
	setTaskGroup(t, second, groupID)
	todo, err := board.MoveEntry(board.Entry{TaskID: secondID, State: "backlog", Path: second, Document: second, Kind: "small"}, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	externalID, _ := func() (string, string) {
		id := todayID("watch-external")
		if _, err := board.NewTask(root, "chore", "watch-external", "外部", false); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "backlog", id+".md")
		makeReady(t, path)
		setMeta(t, path, "- 任务分支:\n", "- 任务分支: task/ext\n")
		moved, err := board.MoveEntry(board.Entry{TaskID: id, State: "backlog", Path: path, Document: path, Kind: "small"}, root, "todo")
		if err != nil {
			t.Fatal(err)
		}
		return id, moved.Path
	}()
	_ = todo

	buf, _, finish := startTestSubscription(t, root, subscribeOptions{
		Group: groupID, Members: []string{firstID, secondID}, Watch: []string{externalID},
		Refresh: 0.05, Heartbeat: 2,
	})
	deadline := time.Now().Add(2 * time.Second)
	var snapshot map[string]any
	for time.Now().Before(deadline) {
		if line := firstJSONLine(buf.String()); line != "" {
			if err := json.Unmarshal([]byte(line), &snapshot); err == nil && snapshot["event"] == "snapshot" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if snapshot["event"] != "snapshot" {
		t.Fatalf("snapshot=%s", buf.String())
	}
	watched, _ := snapshot["watched"].([]any)
	if len(watched) != 1 || watched[0] != externalID {
		t.Fatalf("watched=%v", snapshot["watched"])
	}
	extPath := filepath.Join(root, "todo", externalID+".md")
	if err := os.Rename(extPath, filepath.Join(root, "working", externalID+".md")); err != nil {
		t.Fatal(err)
	}
	var changed map[string]any
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(line), &payload) == nil && payload["event"] == "state-change" {
				changed = payload
			}
		}
		if changed != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	finish()
	if changed == nil {
		t.Fatalf("no state-change: %s", buf.String())
	}
	items, _ := changed["changed"].([]any)
	if len(items) != 1 {
		t.Fatalf("changed=%v", changed["changed"])
	}
}

func firstJSONLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

func TestSubscribeRejectsInvalidWatchAndMembership(t *testing.T) {
	root := tempBoard(t)
	groupID := time.Now().Format("20060102") + "-watch-reject-group"
	memberID, member := makeWorking(t, "watch-reject-member", "成员")
	setTaskGroup(t, member, groupID)
	date := time.Now().Format("20060102")

	code, _, errb := capture(t, func() int {
		return RunSubscribe([]string{groupID, memberID, "--watch", date + "-missing-watch-task"})
	})
	if code == 0 || !strings.Contains(errb, "任务不存在") {
		t.Fatalf("missing: code=%d err=%s", code, errb)
	}
	code, _, errb = capture(t, func() int {
		return RunSubscribe([]string{groupID, memberID, "--watch", memberID})
	})
	if code == 0 || !strings.Contains(errb, "外部监控目标与成员任务重复") {
		t.Fatalf("dup watch: %s", errb)
	}
	code, _, errb = capture(t, func() int {
		return RunSubscribe([]string{groupID, memberID, "--watch", date + "-empty-watch-group"})
	})
	if code == 0 || !strings.Contains(errb, "外部监控任务组没有成员") {
		t.Fatalf("empty group: %s", errb)
	}

	wrongID, _ := makeWorking(t, "event-wrong-group", "错组")
	code, _, errb = capture(t, func() int {
		return RunSubscribe([]string{groupID, wrongID})
	})
	if code == 0 || !strings.Contains(errb, "任务不属于指定任务组") {
		t.Fatalf("wrong group: %s", errb)
	}
	code, _, errb = capture(t, func() int {
		return RunSubscribe([]string{groupID, memberID, memberID})
	})
	if code == 0 || !strings.Contains(errb, "成员任务 ID 不得重复") {
		t.Fatalf("dup member: %s", errb)
	}
	code, _, errb = capture(t, func() int {
		return RunSubscribe([]string{"--refresh", "nan", groupID, memberID})
	})
	if code == 0 || !strings.Contains(errb, "间隔必须大于 0") {
		t.Fatalf("nan: %s", errb)
	}
	_ = root
}

func TestSubscribeExpandsWatchedTaskGroup(t *testing.T) {
	root := tempBoard(t)
	groupID := time.Now().Format("20060102") + "-watch-source-group"
	watchedGroup := time.Now().Format("20060102") + "-watched-group"
	memberID, member := makeWorking(t, "watch-group-member", "成员")
	setTaskGroup(t, member, groupID)
	firstID, first := makeWorking(t, "watched-group-first", "外部一")
	secondID, second := makeWorking(t, "watched-group-second", "外部二")
	setTaskGroup(t, first, watchedGroup)
	setTaskGroup(t, second, watchedGroup)

	buf, _, finish := startTestSubscription(t, root, subscribeOptions{
		Group: groupID, Members: []string{memberID}, Watch: []string{watchedGroup},
		Refresh: 10, Heartbeat: 10,
	})
	deadline := time.Now().Add(2 * time.Second)
	var snapshot map[string]any
	for time.Now().Before(deadline) {
		if line := firstJSONLine(buf.String()); line != "" {
			if json.Unmarshal([]byte(line), &snapshot) == nil && snapshot["event"] == "snapshot" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	finish()
	watched, _ := snapshot["watched"].([]any)
	got := map[string]bool{}
	for _, item := range watched {
		got[item.(string)] = true
	}
	if !got[firstID] || !got[secondID] || len(watched) != 2 {
		t.Fatalf("watched=%v", watched)
	}
}
