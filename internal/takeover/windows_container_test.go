package takeover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

// portableBoard only creates directories and a card file, no POSIX fakes, so it runs on Windows too.
func portableBoard(t *testing.T, state, taskID, windowValue string) string {
	t.Helper()
	resetLang(t)
	root := t.TempDir()
	for _, name := range board.States {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	card := "# 冒烟\n\n- 类型: Chore\n- 会话: codex session-x\n- 窗口: " + windowValue + "\n"
	if err := os.WriteFile(filepath.Join(root, state, taskID+".md"), []byte(card), 0o644); err != nil {
		t.Fatal(err)
	}
	// Empty PATH: herdr is unavailable, so the command must reach the herdr lookup before failing.
	t.Setenv("PATH", t.TempDir())
	return root
}

// herdr has a native Windows build, so cleanup no longer short-circuits to N/A
// by platform; it has to look at the container topology for real.
func TestCleanupAttemptsHerdrContainerOnEveryPlatform(t *testing.T) {
	resetLang(t)
	t.Setenv("PATH", t.TempDir())
	result := Cleanup(
		"herdr:w1:t1:w1:p1",
		launch.AgentSession{Agent: "codex", Reference: "session-x"},
		"herdr:w1:t2:w1:p2",
		5,
	)
	if result.Cleaned {
		t.Fatalf("herdr 容器不该被当成无需清理: %+v", result)
	}
	if !strings.Contains(result.Detail, "herdr") {
		t.Fatalf("没有走到 herdr 查找: %+v", result)
	}
	if result.OldWindow != "herdr:w1:t1:w1:p1" {
		t.Fatalf("原地址丢失: %+v", result)
	}
}

// console/foreground still have no container to close, so that short-circuit stays.
func TestCleanupStillSkipsConsoleAndForeground(t *testing.T) {
	resetLang(t)
	for _, oldWindow := range []string{"", "foreground", "console"} {
		if result := Cleanup(oldWindow, launch.AgentSession{Agent: "codex"}, "herdr:w1:t2:w1:p2", 5); !result.Cleaned {
			t.Fatalf("%q: %+v", oldWindow, result)
		}
	}
}

// dismiss no longer rejects by platform; without herdr it must report "herdr is
// not in PATH" rather than an unsupported platform.
func TestDismissReachesHerdrLookupOnEveryPlatform(t *testing.T) {
	root := portableBoard(t, "done", "20260906-dismiss-gate-task", "herdr:w1:t1:w1:p1")
	err := commandDismiss(root, "20260906-dismiss-gate-task", 90)
	if err == nil {
		t.Fatal("expected a herdr lookup failure")
	}
	if !strings.Contains(err.Error(), "herdr") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "Windows") {
		t.Fatalf("dismiss 仍在按平台拒绝: %v", err)
	}
}
