package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Resolving the target of a request delivery types nothing; the prompt it
// returns types the line into the pane the resolution found.
func TestResolveRequestResolvesWithoutTypingThenPromptsTheResolvedPane(t *testing.T) {
	root, _ := setupBoard(t)
	file := writeNotifySessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_SESSION", file)
	prompt, err := resolveRequest("herdr:w1:t9:w1:p9", "pi wanted")
	if err != nil {
		t.Fatal(err)
	}
	if herdrLogExists(root, "prompt") {
		t.Fatal("resolving the target typed into the pane")
	}
	if err := prompt("# kander-request one line"); err != nil {
		t.Fatal(err)
	}
	typed, err := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	if err != nil || !strings.Contains(string(typed), "w1:p9") || !strings.Contains(string(typed), "# kander-request one line") {
		t.Fatalf("the prompt must type the line into the resolved pane: %q %v", typed, err)
	}
}

// A pane that cannot prove which session it runs is never typed into, and the
// error is returned before any prompt exists.
func TestResolveRequestFailsBeforeAnyTextReachesAnUncertainPane(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_SESSION", filepath.Join(t.TempDir(), "gone.jsonl"))
	prompt, err := resolveRequest("herdr:w1:t9:w1:p9", "pi wanted")
	if err == nil || prompt != nil {
		t.Fatalf("an uncertain identity must fail the resolution: %v", err)
	}
	if herdrLogExists(root, "prompt") {
		t.Fatal("nothing may reach the pane when the resolution fails")
	}
}

func TestResolveRequestRefusesACardWithoutAWindow(t *testing.T) {
	if prompt, err := resolveRequest("", "pi wanted"); err == nil || prompt != nil {
		t.Fatalf("no WINDOW to deliver to: %v", err)
	}
}

func herdrLogExists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, "herdr.log."+name))
	return err == nil
}
