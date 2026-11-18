package takeover

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/launch"
)

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
