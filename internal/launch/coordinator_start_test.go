package launch

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestCoordinatorReconcilesSequentialCommandStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	const group = "20260908-sequential-start-group"
	ids := []string{}
	for _, slug := range []string{"first-member", "next-member"} {
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
	c, err := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "sequential", Basis: "测试顺序启动", Members: ids})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Members[ids[1]].AwaitingStart || c.Members[ids[0]].AwaitingStart {
		t.Fatal("wrong initial start facts")
	}
	if _, _, err = capture(t, func() error { return commandStart(root, "claude", "tmux", ids[1]) }); err != nil {
		t.Fatal(err)
	}
	r := board.CoordinatorReconcile{GroupID: group, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{}}
	for _, id := range ids {
		s, err := board.ReadSnapshot(root, id)
		if err != nil {
			t.Fatal(err)
		}
		if board.MetadataFrom(s.Text, board.FieldStartedAt) == "" || board.MetadataFrom(s.Text, board.FieldOwner) != "claude" {
			t.Fatal("start metadata not published")
		}
		r.Members[id] = board.CoordinatorObservation{Revision: s.Revision}
	}
	next, err := ReconcileCoordinator(context.Background(), root, r)
	if err != nil {
		t.Fatal("actual start producer blocked recovery", err)
	}
	if next.Members[ids[1]].AwaitingStart || next.Members[ids[1]].Cycle == c.Members[ids[1]].Cycle || next.Members[ids[0]].Cycle != c.Members[ids[0]].Cycle {
		t.Fatal("sequential start cycle not retained")
	}
}
