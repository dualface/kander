package launch

import (
	"github.com/dualface/kander/internal/board"
	"os"
	"strings"
	"testing"
)

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
	working := moved.Document
	mut := strings.Replace(string(original), "- OWNER:\n", "- OWNER: codex\n", 1)
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
	_ = os.WriteFile(moved.Document, []byte(strings.Replace(string(original), "- OWNER:\n", "- OWNER: codex\n", 1)), 0o644)

	newer, readErr := board.ReadSnapshot(root, taskID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if writeErr := board.WriteManagedDocument(root, newer.Entry, newer.Text+"\nnew executor record\n"); writeErr != nil {
		t.Fatal(writeErr)
	}
	orig = string(original)
	err = rollbackLaunch(root, moved, "todo", &LaunchFailure{Err: launchError("tmux new-window 失败", "tmux new-window 失败")}, &orig)
	if err == nil || !strings.Contains(err.Error(), "卡片保留在 working") {
		t.Fatalf("err=%v", err)
	}
	if _, stat := os.Stat(moved.Path); stat != nil {
		t.Fatal("should stay in working")
	}
}
