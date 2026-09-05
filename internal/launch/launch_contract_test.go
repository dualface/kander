package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func freezeClock(t *testing.T) {
	t.Helper()
	origin := time.Now()
	var extra time.Duration
	nowFn = func() time.Time { return origin.Add(extra) }
	sleepFn = func(d time.Duration) {
		if d > 0 {
			extra += d
		}
	}
	t.Cleanup(func() {
		nowFn = time.Now
		sleepFn = time.Sleep
	})
}

func installHerdr(t *testing.T, root, fakeBin string) string {
	t.Helper()
	log := filepath.Join(root, "herdr.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	writeFakeHerdr(t, filepath.Join(fakeBin, "herdr"), log)
	return log
}

func startThenReview(t *testing.T, root, agent, taskID, path string) string {
	t.Helper()
	if _, _, err := capture(t, func() error { return commandStart(root, agent, "", taskID) }); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "working", filepath.Base(path))
	setBranch(t, working)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, taskID)
	if _, err := board.MoveEntry(entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	return working
}

func taskFileFromCommand(t *testing.T, cmd string) string {
	t.Helper()
	idx := strings.Index(cmd, "UTF-8 task file at ")
	if idx < 0 {
		t.Fatalf("no task file in %s", cmd)
	}
	rest := cmd[idx+len("UTF-8 task file at "):]
	pathEnd := strings.IndexByte(rest, ';')
	if pathEnd < 0 {
		t.Fatalf("no path end in %s", cmd)
	}
	return strings.TrimSpace(rest[:pathEnd])
}

func TestAutoLauncherResolvesHerdrOverTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux/herdr fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	installHerdr(t, root, fakeBin)
	taskID, path := makeTodo(t, root, "auto-herdr-first")
	out, _, err := capture(t, func() error { return commandStart(root, "", "auto", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "启动方式=herdr") || strings.Contains(out, "启动方式=auto") {
		t.Fatalf("stdout=%s", out)
	}
	if _, stat := os.Stat(filepath.Join(root, "herdr.log.create")); stat != nil {
		t.Fatal("herdr tab was not created")
	}
	if _, stat := os.Stat(filepath.Join(root, "tmux.log")); stat == nil {
		t.Fatal("tmux should not have launched")
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(path))); err != nil {
		t.Fatal(err)
	}
}

func TestAutoLauncherUsesTmuxWhenNotInHerdr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	t.Setenv("HERDR_ENV", "")
	taskID, path := makeTodo(t, root, "auto-tmux")
	out, _, err := capture(t, func() error { return commandStart(root, "", "auto", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "启动方式=tmux") || strings.Contains(out, "启动方式=auto") {
		t.Fatalf("stdout=%s", out)
	}
	args := mustRead(t, filepath.Join(root, "tmux.log"))
	if !strings.Contains(args, "new-window") {
		t.Fatalf("tmux=%s", args)
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(path))); err != nil {
		t.Fatal(err)
	}
}

func TestAutoLauncherWithoutHerdrOrTmuxDoesNotClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("HERDR_ENV", "")
	loadEffective = func() (*config.Config, error) {
		return envConfig("codex", "auto", nil), nil
	}
	taskID, path := makeTodo(t, root, "auto-none")
	original, _ := os.ReadFile(path)
	_, _, err := capture(t, func() error { return commandStart(root, "", "", taskID) })
	if err == nil || !strings.Contains(err.Error(), "auto 无法解析") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "tmux-session") || !strings.Contains(err.Error(), "foreground") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("claimed")
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(path))); err == nil {
		t.Fatal("should remain in todo")
	}
}

func TestTmuxSessionLauncherCreateReuseForeignAndRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	project := filepath.Dir(root)
	session := projectSessionName(project)

	taskID, _ := makeTodo(t, root, "session-create")
	out, _, err := capture(t, func() error { return commandStart(root, "", "tmux-session", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "会话="+session) || !strings.Contains(out, "tmux attach -t "+session) {
		t.Fatalf("stdout=%s", out)
	}
	args := mustRead(t, filepath.Join(root, "tmux.log"))
	if !strings.HasPrefix(args, "new-session\n") || !strings.Contains(args, session) {
		t.Fatalf("tmux=%s", args)
	}
	setopt := mustRead(t, filepath.Join(root, "tmux.log.setopt"))
	if !strings.Contains(setopt, "@kander_project") || !strings.Contains(setopt, project) {
		t.Fatalf("setopt=%s", setopt)
	}
	cmd := lastCommand(t, root)
	if !strings.Contains(cmd, filepath.Join(fakeBin, "codex")) {
		t.Fatalf("command=%s", cmd)
	}

	reuseID, _ := makeTodo(t, root, "session-reuse")
	t.Setenv("KANBAN_TMUX_SESSIONS", session)
	t.Setenv("KANBAN_TMUX_PROJECT", project)
	t.Setenv("TMUX", "/tmp/fake-tmux,1,0")
	out, _, err = capture(t, func() error { return commandStart(root, "", "tmux-session", reuseID) })
	if err != nil {
		t.Fatal(err)
	}
	args = mustRead(t, filepath.Join(root, "tmux.log"))
	if !strings.HasPrefix(args, "new-window\n") || !strings.Contains(args, session+":") {
		t.Fatalf("reuse tmux=%s", args)
	}
	if !strings.Contains(out, "tmux switch-client -t "+session) {
		t.Fatalf("stdout=%s", out)
	}

	t.Setenv("TMUX", "")
	t.Setenv("KANBAN_TMUX_PROJECT", "/somewhere/else")
	conflictID, _ := makeTodo(t, root, "session-conflict")
	out, _, err = capture(t, func() error { return commandStart(root, "", "tmux-session", conflictID) })
	if err != nil {
		t.Fatal(err)
	}
	alt := projectSessionName(project) + "-2"
	args = mustRead(t, filepath.Join(root, "tmux.log"))
	if !strings.HasPrefix(args, "new-session\n") || !strings.Contains(args, alt) {
		t.Fatalf("conflict tmux=%s", args)
	}
	if !strings.Contains(out, "会话="+alt) {
		t.Fatalf("stdout=%s", out)
	}

	failID, failPath := makeTodo(t, root, "session-rollback")
	original, _ := os.ReadFile(failPath)
	t.Setenv("KANBAN_TMUX_FAIL", "1")
	t.Setenv("KANBAN_TMUX_SESSIONS", "")
	t.Setenv("KANBAN_TMUX_PROJECT", "")
	_, _, err = capture(t, func() error { return commandStart(root, "", "tmux-session", failID) })
	if err == nil || !strings.Contains(err.Error(), "tmux new-session 失败") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(failPath)
	if string(got) != string(original) {
		t.Fatal("not restored")
	}
}

func TestHerdrReportWarnsWithoutRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr fakes are POSIX")
	}
	freezeClock(t)
	root, _, fakeBin := setupBoard(t)
	log := installHerdr(t, root, fakeBin)
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(root, "missing-herdr.sock"))
	taskID, path := makeTodo(t, root, "herdr-report-socket-fail")
	out, errb, err := capture(t, func() error { return commandStart(root, "cursor", "herdr", taskID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已启动: "+taskID) {
		t.Fatalf("stdout=%s", out)
	}
	if strings.Count(errb, "警告: herdr 会话身份上报失败") != 1 {
		t.Fatalf("stderr=%s", errb)
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(path))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log + ".close"); err == nil {
		t.Fatal("should not close tab")
	}
}

func TestHerdrTabAndPaneFailuresCloseAndRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	installHerdr(t, root, fakeBin)

	createID, createPath := makeTodo(t, root, "herdr-create-fail")
	original, _ := os.ReadFile(createPath)
	t.Setenv("KANBAN_HERDR_CREATE_FAIL", "1")
	_, _, err := capture(t, func() error { return commandStart(root, "", "herdr", createID) })
	if err == nil || !strings.Contains(err.Error(), "herdr tab create 失败") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(createPath)
	if string(got) != string(original) {
		t.Fatal("create not restored")
	}
	if _, err := os.Stat(filepath.Join(root, "herdr.log.close")); err == nil {
		t.Fatal("create fail should not close")
	}

	t.Setenv("KANBAN_HERDR_CREATE_FAIL", "")
	waitID, waitPath := makeTodo(t, root, "herdr-wait-fail")
	original, _ = os.ReadFile(waitPath)
	t.Setenv("KANBAN_HERDR_WAIT_FAIL", "1")
	_, _, err = capture(t, func() error { return commandStart(root, "", "herdr", waitID) })
	if err == nil || !strings.Contains(err.Error(), "herdr pane 未就绪") {
		t.Fatalf("err=%v", err)
	}
	got, _ = os.ReadFile(waitPath)
	if string(got) != string(original) {
		t.Fatal("wait not restored")
	}
	if !strings.Contains(mustRead(t, filepath.Join(root, "herdr.log.close")), "w1:t9") {
		t.Fatal("wait fail should close tab")
	}

	t.Setenv("KANBAN_HERDR_WAIT_FAIL", "")
	_ = os.Remove(filepath.Join(root, "herdr.log.close"))
	runID, runPath := makeTodo(t, root, "herdr-run-fail")
	original, _ = os.ReadFile(runPath)
	t.Setenv("KANBAN_HERDR_RUN_FAIL", "1")
	_, _, err = capture(t, func() error { return commandStart(root, "", "herdr", runID) })
	if err == nil || !strings.Contains(err.Error(), "herdr pane run 失败") {
		t.Fatalf("err=%v", err)
	}
	got, _ = os.ReadFile(runPath)
	if string(got) != string(original) {
		t.Fatal("run not restored")
	}
	if !strings.Contains(mustRead(t, filepath.Join(root, "herdr.log.close")), "tab\nclose\nw1:t9") &&
		!strings.Contains(mustRead(t, filepath.Join(root, "herdr.log.close")), "w1:t9") {
		t.Fatalf("close=%s", mustRead(t, filepath.Join(root, "herdr.log.close")))
	}
}

func TestResumeMessageFileCursorWorkingAndMissingSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	fileID, filePath := makeTodo(t, root, "resume-file")
	startThenReview(t, root, "grok", fileID, filePath)
	findings := filepath.Join(root, "findings.md")
	if err := os.WriteFile(findings, []byte("- [QA][high] 越界读取\n- [PM][medium] 缺少验收 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := capture(t, func() error {
		return commandResume(root, nil, "", fileID, "", findings, false, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(taskFileFromCommand(t, lastCommand(t, root)))
	if !strings.Contains(string(body), "越界读取") || !strings.Contains(string(body), "缺少验收 3") {
		t.Fatalf("prompt=%s", body)
	}

	t.Setenv("KANBAN_CURSOR_CHAT_ID", "chat-resume-77")
	cursorID, cursorPath := makeTodo(t, root, "resume-cursor")
	startThenReview(t, root, "cursor", cursorID, cursorPath)
	t.Setenv("KANBAN_CURSOR_CHAT_FAIL", "1")
	out, _, err := capture(t, func() error {
		return commandResume(root, nil, "", cursorID, "QA finding: 修复空指针", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Agent=cursor") {
		t.Fatalf("stdout=%s", out)
	}
	cmd := lastCommand(t, root)
	if !strings.Contains(cmd, filepath.Join(fakeBin, "cursor-agent")) ||
		!strings.Contains(cmd, "--resume chat-resume-77") ||
		!strings.Contains(cmd, "--trust") ||
		!strings.Contains(cmd, "--force") {
		t.Fatalf("command=%s", cmd)
	}

	workID, workPath := makeTodo(t, root, "resume-working")
	if _, _, err := capture(t, func() error { return commandStart(root, "grok", "", workID) }); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "working", filepath.Base(workPath))
	out, _, err = capture(t, func() error {
		return commandResume(root, nil, "", workID, "进程已退出, 请继续", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已唤醒") {
		t.Fatalf("stdout=%s", out)
	}
	if _, err := os.Stat(working); err != nil {
		t.Fatal(err)
	}

	manualID, manualPath := makeTodo(t, root, "resume-manual")
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, manualID)
	if _, err := board.MoveEntry(entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "", manualID, "x", "", true, 61)
	})
	if err == nil || !strings.Contains(err.Error(), "会话") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(manualPath))); err != nil {
		t.Fatal(err)
	}
}

func TestResumeLaunchAndLivenessFailureRestoresReview(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux/herdr fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	taskID, path := makeTodo(t, root, "resume-rollback")
	working := startThenReview(t, root, "claude", taskID, path)
	reviewPath := filepath.Join(root, "review", filepath.Base(path))
	before, _ := os.ReadFile(reviewPath)
	t.Setenv("KANBAN_TMUX_FAIL", "1")
	_, _, err := capture(t, func() error {
		return commandResume(root, nil, "", taskID, "x", "", true, 61)
	})
	if err == nil || !strings.Contains(err.Error(), "tmux new-window 失败") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(working); err == nil {
		t.Fatal("should leave working")
	}
	got, _ := os.ReadFile(reviewPath)
	if string(got) != string(before) {
		t.Fatal("review document changed")
	}

	liveID, livePath := makeTodo(t, root, "resume-herdr-exit")
	t.Setenv("KANBAN_TMUX_FAIL", "")
	startThenReview(t, root, "claude", liveID, livePath)
	installHerdr(t, root, fakeBin)
	t.Setenv("KANBAN_HERDR_STATUS", "done")
	t.Setenv("KANBAN_HERDR_OUTPUT", "thread already has an active writer (code -32600)")
	freezeClock(t)
	reviewLive := filepath.Join(root, "review", filepath.Base(livePath))
	before, _ = os.ReadFile(reviewLive)
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "herdr", liveID, "x", "", true, 61)
	})
	if err == nil || (!strings.Contains(err.Error(), "active writer") && !strings.Contains(err.Error(), "存活校验超时")) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(livePath))); err == nil {
		t.Fatal("herdr liveness should restore review")
	}
	got, _ = os.ReadFile(reviewLive)
	if string(got) != string(before) {
		t.Fatal("herdr liveness mutated review")
	}
	if _, err := os.Stat(filepath.Join(root, "herdr.log.close")); err != nil {
		t.Fatal("should close herdr tab after failed resume")
	}
}

func TestValidateResumedAgentHerdrIdentityPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	log := installHerdr(t, root, fakeBin)
	plan := LaunchPlan{Launcher: "herdr", HerdrBin: filepath.Join(fakeBin, "herdr")}
	outcome := LaunchOutcome{Tab: "w1:t9", Pane: "w1:p9"}
	session := AgentSession{Agent: "cursor", Reference: "expected-session"}
	t.Setenv("KANBAN_HERDR_LOG", log)

	paneJSON := func(agent, status, extra string) string {
		body := `{"id":"cli:pane:get","result":{"pane":{"pane_id":"w1:p9","tab_id":"w1:t9","agent":"` + agent + `","agent_status":"` + status + `"` + extra + `}}}`
		return body
	}
	absent := []string{
		"",
		`,"agent_session":null`,
		`,"agent_session":{}`,
		`,"agent_session":{"value":""}`,
	}
	for _, status := range []string{"idle", "working", "blocked"} {
		for _, extra := range absent {
			t.Setenv("KANBAN_HERDR_PANE_JSON", paneJSON("cursor", status, extra))
			if err := validateResumedAgent(plan, outcome, session, 61); err != nil {
				t.Fatalf("status=%s extra=%q err=%v", status, extra, err)
			}
		}
	}
	freezeClock(t)
	for _, status := range []string{"done", "unknown"} {
		t.Setenv("KANBAN_HERDR_PANE_JSON", paneJSON("cursor", status, ""))
		if err := validateResumedAgent(plan, outcome, session, 61); err == nil || !strings.Contains(err.Error(), "存活校验超时") {
			t.Fatalf("status=%s err=%v", status, err)
		}
	}
	t.Setenv("KANBAN_HERDR_PANE_JSON", paneJSON("cursor", "idle", `,"agent_session":{"value":"expected-session"}`))
	if err := validateResumedAgent(plan, outcome, session, 61); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_HERDR_PANE_JSON", paneJSON("cursor", "idle", `,"agent_session":{"value":"other-session"}`))
	if err := validateResumedAgent(plan, outcome, session, 61); err == nil || !strings.Contains(err.Error(), "存活校验超时") {
		t.Fatalf("mismatch err=%v", err)
	}
	t.Setenv("KANBAN_HERDR_PANE_JSON", paneJSON("codex", "idle", `,"agent_session":{"value":"expected-session"}`))
	if err := validateResumedAgent(plan, outcome, session, 61); err == nil || !strings.Contains(err.Error(), "存活校验超时") {
		t.Fatalf("agent mismatch err=%v", err)
	}
}

func TestWindowsRejectsAutoHerdrAndConsoleCreateRollback(t *testing.T) {
	root, _, _ := setupBoard(t)
	runtimeWindows = func() bool { return true }
	t.Cleanup(func() { runtimeWindows = func() bool { return isWindowsGOOS() } })
	for _, launcher := range []string{"auto", "herdr", "tmux-session"} {
		taskID, path := makeTodo(t, root, "win-"+launcher)
		original, _ := os.ReadFile(path)
		_, _, err := capture(t, func() error { return commandStart(root, "", launcher, taskID) })
		if err == nil || !strings.Contains(err.Error(), "Windows") {
			t.Fatalf("launcher=%s err=%v", launcher, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != string(original) {
			t.Fatalf("claimed launcher=%s", launcher)
		}
	}
	loadEffective = func() (*config.Config, error) {
		return envConfig("codex", "auto", nil), nil
	}
	taskID, path := makeTodo(t, root, "win-cfg-auto")
	original, _ := os.ReadFile(path)
	_, _, err := capture(t, func() error { return commandStart(root, "", "", taskID) })
	if err == nil || !strings.Contains(err.Error(), "Windows") {
		t.Fatalf("configured auto err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("configured auto claimed")
	}

	oldStart := startProcessFn
	startProcessFn = func([]string, map[string]string, string, bool) (*startedProc, error) {
		return nil, os.ErrPermission
	}
	t.Cleanup(func() { startProcessFn = oldStart })
	consoleID, consolePath := makeTodo(t, root, "console-create-fail")
	original, _ = os.ReadFile(consolePath)
	_, _, err = capture(t, func() error { return commandStart(root, "claude", "console", consoleID) })
	if err == nil || (!strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "启动 Agent 失败")) {
		t.Fatalf("err=%v", err)
	}
	got, _ = os.ReadFile(consolePath)
	if string(got) != string(original) {
		t.Fatal("console create should restore todo")
	}
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(consolePath))); err == nil {
		t.Fatal("should not remain in working")
	}
}

func TestStartHelpListsAuto(t *testing.T) {
	out, _, err := capture(t, func() error {
		if code := RunStart([]string{"--help"}); code != 0 {
			return launchError("help exit "+itoa(code), "help exit "+itoa(code))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "auto") {
		t.Fatalf("help=%s", out)
	}
}
