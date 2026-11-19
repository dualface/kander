package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
)

func writeReceipt(t *testing.T, home, namespace, name, kind, task string) {
	t.Helper()
	dir := filepath.Join(home, "projects", namespace, "launch-actions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	text := `{"schema":1,"kind":"` + kind + `","execution_id":"` + NewExecutionID() + `","phase":"complete","task_id":"` + task + `"}`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func listAll(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil {
			names = append(names, strings.TrimPrefix(path, dir))
		}
		return nil
	})
	return names
}

// MayReplayAction only reads: with no client directory, or with a client directory that has no
// receipt for the task, it creates nothing and says there is nothing to replay.
func TestMayReplayActionReadsOnly(t *testing.T) {
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(EnvClientHome, home)
	if maybe, err := MayReplayAction("task-launch", "20261119-a-task"); err != nil || maybe {
		t.Fatalf("no client directory: %v %v", maybe, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the lookup created the client directory: %v", err)
	}
	// An existing client with a project that has no receipt directory.
	if err := os.MkdirAll(filepath.Join(home, "projects", "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "client-id"), []byte("c-existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := listAll(t, home)
	if maybe, err := MayReplayAction("task-launch", "20261119-a-task"); err != nil || maybe {
		t.Fatalf("no receipts: %v %v", maybe, err)
	}
	if after := listAll(t, home); strings.Join(before, "|") != strings.Join(after, "|") {
		t.Fatalf("the lookup changed the client directory: %v -> %v", before, after)
	}
	writeReceipt(t, home, "other", "a.json", "task-launch", "20261119-b-task")
	writeReceipt(t, home, "other", "b.json", "orchestrate", "")
	for _, tc := range []struct {
		kind, task string
		want       bool
	}{
		{"task-launch", "20261119-b-task", true},
		{"task-launch", "20261119-a-task", false},
		{"orchestrate", "", true},
		{"native-resume-command", "20261119-b-task", false},
	} {
		if maybe, err := MayReplayAction(tc.kind, tc.task); err != nil || maybe != tc.want {
			t.Fatalf("%s %s: %v %v, want %v", tc.kind, tc.task, maybe, err, tc.want)
		}
	}
	// A receipt that does not name a task yet may belong to any task.
	writeReceipt(t, home, "other", "c.json", "task-launch", "")
	if maybe, err := MayReplayAction("task-launch", "20261119-a-task"); err != nil || !maybe {
		t.Fatalf("receipt with no task: %v %v", maybe, err)
	}
}

// The gate of BeginActionGated runs under the lock with the receipt as it stands and before
// anything is written: a refusal leaves a receipt, and the absence of one, exactly as it was, and
// releases the lock.
func TestBeginActionGatedRefusesBeforeAnyWrite(t *testing.T) {
	session := &Session{Home: t.TempDir(), ServerURL: "http://127.0.0.1:1", Info: api.Project{ID: "p"}}
	refuse := errors.New("refused")
	var seen []*LocalAction
	gate := func(existing *LocalAction) error {
		seen = append(seen, existing)
		return refuse
	}
	if _, _, err := session.BeginActionGated("task-launch", "input", true, gate); !errors.Is(err, refuse) {
		t.Fatalf("fresh: %v", err)
	}
	if seen[0] != nil {
		t.Fatalf("fresh receipt seen as %+v", seen[0])
	}
	if _, found, err := session.PeekAction("task-launch", "input"); err != nil || found {
		t.Fatalf("a refused fresh call wrote a receipt: %v %v", found, err)
	}
	// An existing failed receipt is kept as it is, although BeginAction would start over it.
	journal, _, err := session.BeginAction("task-launch", "input")
	if err != nil {
		t.Fatal(err)
	}
	journal.Value.Phase = "failed"
	if err := journal.Save(); err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	before, _ := os.ReadFile(journal.Path)
	if _, _, err := session.BeginActionGated("task-launch", "input", true, gate); !errors.Is(err, refuse) {
		t.Fatalf("failed: %v", err)
	}
	if seen[1] == nil || seen[1].Phase != "failed" {
		t.Fatalf("failed receipt seen as %+v", seen[1])
	}
	if after, _ := os.ReadFile(journal.Path); string(after) != string(before) {
		t.Fatalf("the refusal rewrote the failed receipt:\n%s\n%s", before, after)
	}
	// The lock was released: a call without the gate proceeds.
	again, fresh, err := session.BeginAction("task-launch", "input")
	if err != nil || !fresh {
		t.Fatalf("after the refusal: %v %v", fresh, err)
	}
	_ = again.Close()
}

// PeekAction and BeginAction take the name and the checks of a receipt from one place: a damaged
// receipt is refused by both, and Peek creates nothing.
func TestPeekActionCreatesNothingAndChecksLikeBegin(t *testing.T) {
	session := &Session{Home: t.TempDir(), ServerURL: "http://127.0.0.1:1", Info: api.Project{ID: "p"}}
	if _, found, err := session.PeekAction("task-launch", "input"); err != nil || found {
		t.Fatalf("%v %v", found, err)
	}
	if names := listAll(t, session.Home); len(names) > 1 {
		t.Fatalf("the peek created %v", names)
	}
	journal, _, err := session.BeginAction("task-launch", "input")
	if err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	// A receipt without a valid execution identity is damaged for both readers.
	if err := os.WriteFile(journal.Path, []byte(`{"schema":1,"kind":"task-launch","execution_id":"","phase":"complete"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.PeekAction("task-launch", "input"); err == nil {
		t.Fatal("peek accepted a receipt without an execution identity")
	}
	if _, _, err := session.BeginAction("task-launch", "input"); err == nil {
		t.Fatal("begin accepted a receipt without an execution identity")
	}
}
