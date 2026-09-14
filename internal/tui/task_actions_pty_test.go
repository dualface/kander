//go:build linux

package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestTaskActionsLifecycleOnPTY(t *testing.T) {
	bin := buildKander(t)
	root, env := boardEnv(t)
	writeCompleteConfig(t, env)
	path, err := board.NewTask(root, "feature", "action-menu", "Action menu", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	session := startPTY(t, bin, env)
	if !session.waitFor("Action menu", 8*time.Second) {
		t.Fatalf("board: %s", session.text())
	}
	session.send("m")
	if !session.waitFor("Move task to trash", 8*time.Second) {
		t.Fatalf("menu: %s", session.text())
	}
	for i := 0; i < 3; i++ {
		session.send("j")
		time.Sleep(50 * time.Millisecond)
	}
	session.send("\r")
	if !session.waitFor("Reason", 8*time.Second) {
		t.Fatalf("form: %s", session.text())
	}
	session.send("\r")
	if !session.waitFor("Required", 8*time.Second) {
		t.Fatalf("validation: %s", session.text())
	}
	session.send("User requested removal\r")
	if !session.waitFor("Decision reference", 8*time.Second) {
		t.Fatalf("decision: %s", session.text())
	}
	session.send("Interactive task action\r")
	if !session.waitFor("moved to trash", 8*time.Second) {
		t.Fatalf("completion: %s", session.text())
	}
	snapshot, err := board.ReadSnapshot(root, filepath.Base(path))
	if err != nil || snapshot.Entry.State != "trash" {
		t.Fatalf("state=%s error=%v", snapshot.Entry.State, err)
	}
	session.send("q")
	if err := session.waitExit(8 * time.Second); err != nil {
		t.Fatal(err)
	}
}
