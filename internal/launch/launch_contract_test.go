package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
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
	working := filepath.Join(root, "working", filepath.Base(filepath.Dir(path)), "spec.md")
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
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(filepath.Dir(path)), "spec.md")); err != nil {
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
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(filepath.Dir(path)), "spec.md")); err != nil {
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
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(filepath.Dir(path)), "spec.md")); err == nil {
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
	session := "kb-" + terminal.ProjectKey(project)

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
	alt := "kb-" + terminal.ProjectKey(project) + "-2"
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
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(filepath.Dir(path)), "spec.md")); err != nil {
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

func TestWindowsRejectsTmuxAndRequiresHerdrEnvCreateRollback(t *testing.T) {
	root, _, _ := setupBoard(t)
	runtimeWindows = func() bool { return true }
	t.Cleanup(func() { runtimeWindows = func() bool { return isWindowsGOOS() } })
	t.Setenv("HERDR_ENV", "")
	// tmux is still rejected by platform.
	for _, launcher := range []string{"tmux", "tmux-session"} {
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
	// herdr and auto are no longer rejected by platform on Windows, but they still require being inside herdr.
	for _, launcher := range []string{"auto", "herdr"} {
		taskID, path := makeTodo(t, root, "win-"+launcher)
		original, _ := os.ReadFile(path)
		_, _, err := capture(t, func() error { return commandStart(root, "", launcher, taskID) })
		if err == nil || strings.Contains(err.Error(), "Windows") {
			t.Fatalf("launcher=%s err=%v", launcher, err)
		}
		if !strings.Contains(err.Error(), "herdr") {
			t.Fatalf("launcher=%s err=%v", launcher, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != string(original) {
			t.Fatalf("claimed launcher=%s", launcher)
		}
	}
	loadEffective = func() (*config.Config, error) {
		return envConfig("codex", "tmux-session", nil), nil
	}
	taskID, path := makeTodo(t, root, "win-cfg-tmux")
	original, _ := os.ReadFile(path)
	_, _, err := capture(t, func() error { return commandStart(root, "", "", taskID) })
	if err == nil || !strings.Contains(err.Error(), "Windows") {
		t.Fatalf("configured tmux-session err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("configured tmux-session claimed")
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
	if _, err := os.Stat(filepath.Join(root, "working", filepath.Base(filepath.Dir(consolePath)), "spec.md")); err == nil {
		t.Fatal("should not remain in working")
	}
}
