package liveness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
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

func writeCompleteConfig(t *testing.T) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "cn"
	cfg.AgentLanguage = "zh-CN"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
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
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	writeCompleteConfig(t)
	return root
}

func makeReady(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout gives the card template CRLF line ends; the edits of these
	// tests match LF.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, replacement := range []string{"实现目标", "产生可验证结果", "满足验收", "无额外范围"} {
		text = strings.Replace(text, "<FILL_IN>", replacement, 1)
	}
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: 通过\nCARD_REVIEW: 通过\n", 1)
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
	if _, err := board.NewTask(os.Getenv(board.EnvBoardDir), "chore", slug, title, "en", false); err != nil {
		t.Fatal(err)
	}
	root := os.Getenv(board.EnvBoardDir)
	path := filepath.Join(root, "backlog", id, "spec.md")
	makeReady(t, path)
	setMeta(t, path, "- TASK_BRANCH:\n", "- TASK_BRANCH: task/"+slug+"\n")
	moved, err := board.MoveEntry(currentEntry(t, root, id), root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	moved, err = board.MoveEntry(moved, root, "working")
	if err != nil {
		t.Fatal(err)
	}
	return id, moved.Document
}

func setLocation(t *testing.T, path, session, window string) {
	t.Helper()
	setMeta(t, path, "- SESSION:\n", "- SESSION: "+session+"\n")
	setMeta(t, path, "- WINDOW:\n", "- WINDOW: "+window+"\n")
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
  if [ -n "${KANBAN_HERDR_PANE2:-}" ] && [ "$3" = "$KANBAN_HERDR_PANE2" ]; then
    if [ -n "${KANBAN_HERDR_SESSION_KIND2:-}" ]; then
      printf '{"id":"cli:pane:get","result":{"type":"pane_info","pane":{"pane_id":"%s","tab_id":"%s","agent":"%s","agent_status":"%s","agent_session":{"kind":"%s","value":"%s"}}}}\n' \
        "$3" "${KANBAN_HERDR_TAB_ID2:-w1:t9}" "${KANBAN_HERDR_AGENT2:-codex}" "${KANBAN_HERDR_STATUS2:-idle}" "$KANBAN_HERDR_SESSION_KIND2" "${KANBAN_HERDR_SESSION2:-}"
    else
      printf '{"id":"cli:pane:get","result":{"type":"pane_info","pane":{"pane_id":"%s","tab_id":"%s","agent":"%s","agent_status":"%s","agent_session":{"value":"%s"}}}}\n' \
        "$3" "${KANBAN_HERDR_TAB_ID2:-w1:t9}" "${KANBAN_HERDR_AGENT2:-codex}" "${KANBAN_HERDR_STATUS2:-idle}" "${KANBAN_HERDR_SESSION2:-}"
    fi
    exit 0
  fi
  if [ -n "${KANBAN_HERDR_SESSION_KIND:-}" ]; then
    printf '{"id":"cli:pane:get","result":{"type":"pane_info","pane":{"pane_id":"%s","tab_id":"%s","agent":"%s","agent_status":"%s","agent_session":{"kind":"%s","value":"%s"}}}}\n' \
      "$3" "${KANBAN_HERDR_TAB_ID:-w1:t9}" "${KANBAN_HERDR_AGENT:-codex}" "${KANBAN_HERDR_STATUS:-idle}" "$KANBAN_HERDR_SESSION_KIND" "${KANBAN_HERDR_SESSION:-}"
  else
    printf '{"id":"cli:pane:get","result":{"type":"pane_info","pane":{"pane_id":"%s","tab_id":"%s","agent":"%s","agent_status":"%s","agent_session":{"value":"%s"}}}}\n' \
      "$3" "${KANBAN_HERDR_TAB_ID:-w1:t9}" "${KANBAN_HERDR_AGENT:-codex}" "${KANBAN_HERDR_STATUS:-idle}" "${KANBAN_HERDR_SESSION:-}"
  fi
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

func installPOSIXFakes(t *testing.T, herdr bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tmux/herdr fakes are POSIX")
	}
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	writeCompleteConfig(t)
	bin := t.TempDir()
	writeFakeTmux(t, filepath.Join(bin, "tmux"))
	if herdr {
		writeFakeHerdr(t, filepath.Join(bin, "herdr"))
	}
	t.Setenv("PATH", bin)
}

func TestTmuxReverseLookupUniqueMarker(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, false)
	t.Setenv("KANBAN_TMUX_LIST_PANES", strings.Join([]string{
		"%1\t$1\tone\t@1\tclaude\t0\tother",
		"%2\t$2\ttwo\t@2\tcodex\t0\twanted",
		"%3\t$3\tthree\t@3\tclaude\t1\twanted",
		"%4\t$4\tfour\t@4\tclaude\t0\twanted",
	}, "\n"))
	tmuxBackend := backendFor(t, "tmux")
	loc, err := ReverseLookup(context.Background(), tmuxBackend, "tmux", TaskSession{Agent: "claude", Reference: "wanted"})
	if err != nil {
		t.Fatal(err)
	}
	if loc != (terminal.Address{Session: "$4", Container: "@4", Pane: "%4"}) {
		t.Fatalf("%+v", loc)
	}
	if terminal.FormatAddress(tmuxBackend, loc) != "tmux:$4:@4:%4" {
		t.Fatal(terminal.FormatAddress(tmuxBackend, loc))
	}
	sessionBackend := backendFor(t, "tmux-session")
	sessionLoc, err := ReverseLookup(context.Background(), sessionBackend, "tmux", TaskSession{Agent: "claude", Reference: "wanted"})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.FormatAddress(sessionBackend, sessionLoc) != "tmux-session:four:@4:%4" {
		t.Fatal(terminal.FormatAddress(sessionBackend, sessionLoc))
	}
	t.Setenv("KANBAN_TMUX_LIST_PANES", os.Getenv("KANBAN_TMUX_LIST_PANES")+"\n%5\t$5\tfive\t@5\tclaude\t0\twanted")
	_, err = ReverseLookup(context.Background(), tmuxBackend, "tmux", TaskSession{Agent: "claude", Reference: "wanted"})
	if err == nil || !strings.Contains(err.Error(), "匹配不唯一") {
		t.Fatalf("err=%v", err)
	}
}

func setTaskGroup(t *testing.T, path, group string) {
	t.Helper()
	setMeta(t, path, "- TASK_GROUP:\n", "- TASK_GROUP: "+group+"\n")
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

func (b *subscriptionOutput) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return b.Write(p)
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
	// These tests read events until they stop the subscription themselves.
	opts.Follow = true
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
	installPOSIXFakes(t, false)
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
	installPOSIXFakes(t, true)
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
	_, err := board.NewTask(root, "chore", "event-second", "成员二", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	secondID := todayID("event-second")
	second := filepath.Join(root, "backlog", secondID, "spec.md")
	makeReady(t, second)
	setMeta(t, second, "- TASK_BRANCH:\n", "- TASK_BRANCH: task/event-second\n")
	setTaskGroup(t, first, groupID)
	setTaskGroup(t, second, groupID)
	todo, err := board.MoveEntry(currentEntry(t, root, secondID), root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	externalID, _ := func() (string, string) {
		id := todayID("watch-external")
		if _, err := board.NewTask(root, "chore", "watch-external", "外部", "en", false); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "backlog", id, "spec.md")
		makeReady(t, path)
		setMeta(t, path, "- TASK_BRANCH:\n", "- TASK_BRANCH: task/ext\n")
		moved, err := board.MoveEntry(currentEntry(t, root, id), root, "todo")
		if err != nil {
			t.Fatal(err)
		}
		return id, moved.Document
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
	extPath := filepath.Join(root, "todo", externalID)
	if err := os.Rename(extPath, filepath.Join(root, "working", externalID)); err != nil {
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

func currentEntry(t *testing.T, root, id string) board.Entry {
	t.Helper()
	s, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return s.Entry
}

func TestSubscribeLegacyCardSurvivesDirectoryMigration(t *testing.T) {
	root := tempBoard(t)
	id := "20260907-subscribe-legacy-task"
	group := "20260907-subscribe-migration-group"
	legacy := filepath.Join(root, "backlog", id+".md")
	text := "# Legacy\n- TYPE: Chore\n- TASK_GROUP: " + group + "\n"
	if err := os.WriteFile(legacy, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	buf, done, finish := startTestSubscription(t, root, subscribeOptions{Group: group, Members: []string{id}, Refresh: 0.01, Heartbeat: 0.03})
	deadline := time.Now().Add(2 * time.Second)
	for firstJSONLine(buf.String()) == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if firstJSONLine(buf.String()) == "" {
		t.Fatal("missing legacy snapshot")
	}
	if n, err := board.MigrateCards(root, board.InitOptions{}); err != nil || n != 1 {
		t.Fatalf("migration %d %v", n, err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), `"heartbeat"`) && time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatal("subscription exited during migration")
		default:
		}
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(buf.String(), `"heartbeat"`) {
		t.Fatal("subscription did not continue after migration")
	}
	finish()
	s, err := board.ReadSnapshot(root, id)
	if err != nil || !s.Entry.IsDirectory() || s.Entry.Kind != "small" {
		t.Fatalf("snapshot %+v %v", s, err)
	}
}
