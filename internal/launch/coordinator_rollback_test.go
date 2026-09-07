package launch

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestCoordinatorRecoversStartRollbackAndRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	for _, missRollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback-snapshot", true: "missed-rollback"}[missRollback], func(t *testing.T) {
			root, _, _ := setupBoard(t)
			const group = "20260908-start-rollback-group"
			ids := []string{}
			for _, slug := range []string{"running", "retry"} {
				id, _ := makeTodo(t, root, slug)
				ids = append(ids, id)
				s, err := board.ReadSnapshot(root, id)
				if err != nil {
					t.Fatal(err)
				}
				text := strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
				if err = board.WithTransaction(root, board.LockScope{Tasks: []string{id}}, func(tx *board.Transaction) error { return tx.Put(id, "spec.md", text) }); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := capture(t, func() error { return commandStart(root, "claude", "tmux", ids[0]) }); err != nil {
				t.Fatal(err)
			}
			c, err := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "first", Basis: "测试启动失败恢复", Members: ids})
			if err != nil {
				t.Fatal(err)
			}
			reconcile := func() {
				t.Helper()
				r := board.CoordinatorReconcile{GroupID: group, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{}}
				for _, id := range ids {
					s, e := board.ReadSnapshot(root, id)
					if e != nil {
						t.Fatal(e)
					}
					r.Members[id] = board.CoordinatorObservation{Revision: s.Revision}
				}
				c, err = ReconcileCoordinator(context.Background(), root, r)
				if err != nil {
					t.Fatal("recovery blocked", err)
				}
			}
			oldWrite := writeDocumentFn
			oldStamp := nowStamp
			nowStamp = func() string { return "2026-09-08 10:00" }
			t.Cleanup(func() { nowStamp = oldStamp })
			t.Cleanup(func() { writeDocumentFn = oldWrite })
			writeDocumentFn = func(root string, e board.Entry, text string) error {
				if err := oldWrite(root, e, text); err != nil {
					return err
				}
				// Deterministically observe committed metadata before the real
				// launcher executes and fails. No task mutation comes from polling.
				reconcile()
				if !c.Members[ids[1]].AwaitingStart {
					t.Fatal("unconfirmed launch bound the execution cycle")
				}
				return nil
			}
			t.Setenv("KANBAN_TMUX_FAIL", "1")
			if _, _, err = capture(t, func() error { return commandStart(root, "claude", "tmux", ids[1]) }); err == nil {
				t.Fatal("launcher failure not injected")
			}
			writeDocumentFn = oldWrite
			s, err := board.ReadSnapshot(root, ids[1])
			if err != nil || s.Entry.State != "todo" || board.MetadataFrom(s.Text, board.FieldStartedAt) != "" {
				t.Fatal("real rollback did not restore todo", err)
			}
			c, err = board.ReadCoordinatorCheckpoint(root, group)
			if err != nil {
				t.Fatal(err)
			}
			c, err = board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: group, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Owner: "restarted", Token: "second", Basis: "测试重启", Members: ids})
			if err != nil {
				t.Fatal(err)
			}
			if !missRollback {
				reconcile()
			}
			t.Setenv("KANBAN_TMUX_FAIL", "")
			nowStamp = func() string { return "2026-09-08 10:01" }
			if _, _, err = capture(t, func() error { return commandStart(root, "claude", "tmux", ids[1]) }); err != nil {
				t.Fatal(err)
			}
			reconcile()
			if c.Members[ids[1]].AwaitingStart {
				t.Fatal("successful retry never confirmed")
			}
			revision := c.Revision
			reconcile()
			if c.Revision != revision {
				t.Fatal("repeat recovery changed cursor")
			}
		})
	}
}
