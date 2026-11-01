package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

const oldReclaimSession = "claude 2f1b7c7e-6c8a-4e0e-9d55-0a6f2c1d3b4e"

// oldReclaimedCard is a card 1.0.0 started with a succeeded schema 1 attempt and
// reclaimed from review: OWNER and STARTED_AT changed, a second claim line, no handoff.
func oldReclaimedCard(t *testing.T, root, slug, owner string) string {
	t.Helper()
	task, _ := makeTodo(t, root, slug)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text, err := board.NewClaimMetadata(s.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.WithTransaction(root, board.LockScope{Tasks: []string{task}, ExclusiveBoard: true}, func(tx *board.Transaction) error {
		if e := tx.Put(task, "spec.md", text); e != nil {
			return e
		}
		return tx.Relocate(task, "working")
	}); err != nil {
		t.Fatal(err)
	}
	setCardField(t, root, task, "OWNER", "claude")
	setCardField(t, root, task, "STARTED_AT", "2026-10-01 10:00")
	setCardField(t, root, task, "SESSION", oldReclaimSession)
	s, err = board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	attempt := map[string]any{"id": "old-attempt", "task_id": task, "cycle": board.ExecutionCycle(s.Entry, s.Text), "owner": "claude", "session": oldReclaimSession, "revision": s.Revision, "status": "pending"}
	dir := filepath.Join(root, ".kander", "groups", "00000000-start-group", task)
	if err = os.MkdirAll(filepath.Join(dir, "old-attempt"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(path string, value map[string]any) {
		t.Helper()
		data, e := json.Marshal(value)
		if e == nil {
			e = os.WriteFile(path, append(data, '\n'), 0o600)
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	write(filepath.Join(dir, "old-attempt", "pending.json"), attempt)
	attempt["status"], attempt["finished_revision"] = "succeeded", s.Revision
	write(filepath.Join(dir, "old-attempt", "result.json"), attempt)
	write(filepath.Join(dir, "current.json"), attempt)
	if text, err = board.NewClaimMetadata(s.Text); err != nil {
		t.Fatal(err)
	}
	if err = writeCardFixture(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	setCardField(t, root, task, "OWNER", owner)
	setCardField(t, root, task, "STARTED_AT", "2026-10-02 11:00")
	return task
}

func requireOldReclaimResumeRefused(t *testing.T, root, task string) {
	t.Helper()
	_, _, err := capture(t, func() error { return commandResume(root, nil, "tmux", task, "continue", "", true, 61) })
	if err == nil || err.Error() != launchError("launch.execution_has_no_session", task, board.ExecutionReclaim).Error() || !strings.Contains(err.Error(), "kander resume --agent") {
		t.Fatalf("err=%v", err)
	}
}

func TestOldReclaimResumes(t *testing.T) {
	skipOnWindows(t)
	for name, owner := range map[string]string{"owner changes": "codex", "owner stays": "claude"} {
		for _, written := range []bool{false, true} {
			title := name + " before the first write"
			if written {
				title = name + " after the first write"
			}
			t.Run(title, func(t *testing.T) {
				root, _, _ := setupBoard(t)
				task := oldReclaimedCard(t, root, "old-reclaim", owner)
				record, exists, err := board.CurrentExecution(root, task)
				if err != nil || !exists || record.Kind != board.ExecutionReclaim {
					t.Fatalf("record %+v %v", record, err)
				}
				if written {
					s, e := board.ReadSnapshot(root, task)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = board.BindExecutionCursor(root, s.Entry); e != nil {
						t.Fatal(e)
					}
					if _, e = board.RecordExecutionWindow(root, s.Entry, "tmux:$1:@1:%1", ""); e != nil {
						t.Fatal(e)
					}
				}
				requireOldReclaimResumeRefused(t, root, task)
				agent := "claude"
				if _, _, err = capture(t, func() error {
					return commandResume(root, &agent, "tmux", task, "take over", "", true, 61, DispatchOptions{Decision: "user approved"})
				}); err != nil {
					t.Fatal(err)
				}
				after := requireRecord(t, root, task, board.ExecutionRunning)
				if after.Kind != board.ExecutionTakeover || after.Owner != agent {
					t.Fatalf("record %+v", after)
				}
			})
		}
	}
}
