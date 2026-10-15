package board

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordMoveObserver installs one observer for the test and records every call.
func recordMoveObserver(t *testing.T) *[]string {
	t.Helper()
	moveObserversMu.Lock()
	saved := moveObservers
	moveObservers = nil
	moveObserversMu.Unlock()
	t.Cleanup(func() {
		moveObserversMu.Lock()
		moveObservers = saved
		moveObserversMu.Unlock()
	})
	var calls []string
	RegisterMoveObserver(func(root string, entry Entry, target string) func(io.Writer, Entry) {
		calls = append(calls, entry.TaskID+":"+entry.State+">"+target)
		return func(w io.Writer, moved Entry) {
			fmt.Fprintf(w, "observed %s in %s\n", moved.TaskID, moved.State)
		}
	})
	return &calls
}

func TestMoveObserverNoticeKeepsStdoutAndExitCode(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := workingCard(t, root, "observer-done", "observer")
	calls := recordMoveObserver(t)
	code, out, errText := capture(t, func() int { return RunMove([]string{id, "done", "--result", "completed"}) })
	if code != 0 {
		t.Fatalf("done: %s", errText)
	}
	if out != filepath.Join(root, "done", id)+"\n" {
		t.Fatalf("stdout changed: %q", out)
	}
	if !strings.Contains(errText, "observed "+id+" in done\n") {
		t.Fatalf("notice missing:\n%s", errText)
	}
	if len(*calls) != 1 || (*calls)[0] != id+":working>done" {
		t.Fatalf("observer calls = %v", *calls)
	}
}

func TestMoveObserverNoticeSkipsFailedMove(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := workingCard(t, root, "observer-fail", "observer")
	recordMoveObserver(t)
	// A completed archive needs a reason and a decision; without them the move fails.
	code, _, errText := capture(t, func() int { return RunMove([]string{id, "archived", "--result", "completed"}) })
	if code == 0 {
		t.Fatal("archive without a decision succeeded")
	}
	if strings.Contains(errText, "observed ") {
		t.Fatalf("notice printed after a failed move:\n%s", errText)
	}
	if _, err := os.Stat(filepath.Join(root, "working", id)); err != nil {
		t.Fatal(err)
	}
}

func TestMoveObserverNoticeSkipsDispatchReplay(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "observer-replay")
	path := filepath.Join(t.TempDir(), "intent.json")
	b, _ := json.Marshal(dispatchInput(s, ""))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := capture(t, func() int { return RunDispatch([]string{"prepare", path}) })
	if code != 0 {
		t.Fatal(stderr)
	}
	var d Dispatch
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	recordMoveObserver(t)
	args := []string{s.Entry.TaskID, "working", "--dispatch-id", d.Input.ID, "--execution-epoch", "1"}
	code, out, stderr = capture(t, func() int { return RunMove(args) })
	if code != 0 || !strings.Contains(out, `"replayed":false`) || !strings.Contains(stderr, "observed ") {
		t.Fatalf("first move: %s %s", out, stderr)
	}
	code, out, stderr = capture(t, func() int { return RunMove(args) })
	if code != 0 || !strings.Contains(out, `"replayed":true`) {
		t.Fatalf("replay: %s %s", out, stderr)
	}
	if strings.Contains(stderr, "observed ") {
		t.Fatalf("notice printed on replay:\n%s", stderr)
	}
}
