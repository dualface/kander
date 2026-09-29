package notify

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/process"
)

// The delivery line shows the message file through process.AgentPath, while the file is still
// written and removed at its native path.
func TestNotifyInstructionShowsAgentMessagePath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	path, err := writeNotifyMessage("hello")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = removeNotifyMessage(path) })
	if data, err := os.ReadFile(path); err != nil || string(data) != "hello\n" {
		t.Fatalf("message file at native path: %q %v", data, err)
	}
	line := notifyInstruction(board.Entry{TaskID: "task-1"}, path, "KANDER-NOTIFY-ACK:x")
	if !strings.Contains(line, process.AgentPath(path)) {
		t.Fatalf("delivery line misses the agent path: %s", line)
	}
	if runtime.GOOS == "windows" && strings.Contains(line, `\`) {
		t.Fatalf("Windows delivery line still has a backslash: %s", line)
	}
	if runtime.GOOS != "windows" && !strings.Contains(line, path) {
		t.Fatalf("POSIX delivery line changed the path: %s", line)
	}
}
