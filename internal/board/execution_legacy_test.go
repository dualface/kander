package board

import (
	"os"
	"path/filepath"
	"testing"
)

// legacyAttempt writes a schema 1 start attempt the way the old binary did.
func legacyAttempt(t *testing.T, root string, s Snapshot, id, status string, finished uint64) {
	t.Helper()
	cycle, owner, session := planCycle(s), MetadataFrom(s.Text, FieldOwner), MetadataFrom(s.Text, FieldSession)
	type attempt struct {
		ID               string `json:"id"`
		TaskID           string `json:"task_id"`
		Cycle            string `json:"cycle"`
		Owner            string `json:"owner"`
		Session          string `json:"session"`
		Revision         uint64 `json:"revision"`
		Status           string `json:"status"`
		FinishedRevision uint64 `json:"finished_revision,omitempty"`
	}
	dir := control(root, "groups", taskStartGroup, s.Entry.TaskID)
	if err := os.MkdirAll(filepath.Join(dir, id), 0o700); err != nil {
		t.Fatal(err)
	}
	pending := attempt{id, s.Entry.TaskID, cycle, owner, session, s.Revision, "pending", 0}
	result := pending
	result.Status, result.FinishedRevision = status, finished
	write := func(path string, value attempt) {
		if err := os.WriteFile(path, []byte(reviewJSON(value)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, id, "pending.json"), pending)
	if status != "pending" {
		write(filepath.Join(dir, id, "result.json"), result)
	}
	write(filepath.Join(dir, "current.json"), result)
}

// fixtureClaim gives a card the metadata an old binary's start left, with no ledger.
func legacyStartedCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := coordinatorTodo(t, root, slug)
	text, err := moveMetadata(s.Text, "todo", "working", MoveOptions{Owner: "codex"})
	if err == nil {
		text, err = setMetadata(text, FieldSession, "codex old")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if e := tx.Put(s.Entry.TaskID, "spec.md", text); e != nil {
			return e
		}
		return tx.Relocate(s.Entry.TaskID, "working")
	}); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

func TestLegacyCardReadsThroughASynthesizedChain(t *testing.T) {
	root := tempBoard(t)
	s := legacyStartedCard(t, root, "legacy-running")
	id := s.Entry.TaskID
	legacyAttempt(t, root, s, "old-attempt", "succeeded", s.Revision)
	before, err := os.ReadFile(s.Entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, root, id)
	if l.synthesized == 0 || l.synthesized != len(l.chain) || l.current.Status != ExecutionRunning || l.current.Kind != ExecutionStart || l.current.Session != "codex old" {
		t.Fatalf("view %+v %+v", l, l.current)
	}
	c := coordinatorClaim(t, root, id)
	if c = coordinatorReconcile(t, root, c); c.Members[id].AwaitingStart || c.Members[id].Cycle != planCycle(s) {
		t.Fatalf("mirror %+v", c.Members[id])
	}
	after, err := os.ReadFile(s.Entry.Document)
	if err != nil || string(after) != string(before) {
		t.Fatal("reading a legacy card changed its bytes", err)
	}
	if _, err = os.Stat(filepath.Join(control(root, "groups", taskStartGroup, id), "transitions")); !os.IsNotExist(err) {
		t.Fatal("the synthesized chain was written back", err)
	}
}

func TestLegacyManualClaimReadsAsSelfClaimed(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "legacy-claim")
	text, err := moveMetadata(s.Text, "todo", "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if e := tx.Put(s.Entry.TaskID, "spec.md", text); e != nil {
			return e
		}
		return tx.Relocate(s.Entry.TaskID, "working")
	}); err != nil {
		t.Fatal(err)
	}
	if cur := ledgerOf(t, root, s.Entry.TaskID).current; cur == nil || !cur.SelfClaimed() || cur.Status != ExecutionRunning {
		t.Fatalf("record %+v", cur)
	}
}

// The first write on a legacy card turns its synthesized record into a stored one.
func TestFirstWriteStoresASynthesizedRecord(t *testing.T) {
	root := tempBoard(t)
	s := legacyStartedCard(t, root, "legacy-first-write")
	id := s.Entry.TaskID
	if _, err := BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExecutionWindow(root, s.Entry, "foreground", ""); err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, root, id)
	if l.current == nil || l.current.Window != "foreground" || l.chain[len(l.chain)-1].Kind != TransitionWindowUpdate || l.chain[len(l.chain)-1].Seq != uint64(l.synthesized)+1 {
		t.Fatalf("view %+v %v", l.current, chainKinds(l))
	}
	if err := l.assertCard(transactionSnapshot(t, root, id)); err != nil {
		t.Fatal(err)
	}
}

// A legacy card stuck in pending opens to a takeover, and the first stored link
// continues the synthesized prefix.
func TestLegacyPendingAcceptsATakeover(t *testing.T) {
	root := tempBoard(t)
	s := legacyStartedCard(t, root, "legacy-pending")
	id := s.Entry.TaskID
	legacyAttempt(t, root, s, "stuck-attempt", "pending", 0)
	s = transactionSnapshot(t, root, id)
	c := coordinatorClaim(t, root, id)
	if _, err := BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmTaskStart(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, root, id)
	if l.synthesized == 0 || len(l.chain) != l.synthesized+2 || l.chain[l.synthesized].Kind != TransitionHandoff {
		t.Fatalf("chain %v at %d", chainKinds(l), l.synthesized)
	}
	if c = coordinatorReconcile(t, root, c); c.Members[id].AwaitingStart {
		t.Fatalf("mirror %+v", c.Members[id])
	}
}

// A schema 1 checkpoint has no chain position, so its facts select one, and the
// first published reconcile upgrades it.
func TestSchemaOneCheckpointFindsItsPositionByFacts(t *testing.T) {
	root := tempBoard(t)
	s := legacyStartedCard(t, root, "legacy-mirror")
	id := s.Entry.TaskID
	legacyAttempt(t, root, s, "old-attempt", "succeeded", s.Revision)
	c := coordinatorClaim(t, root, id)
	if c.Members[id].TransitionSeq == 0 {
		t.Fatal("claim recorded no position")
	}
	old := c
	m := old.Members[id]
	m.TransitionSeq, m.DispatchSeq = 0, 0
	old.Members = map[string]CoordinatorMember{id: m}
	old.Schema = 1
	old.Revision++
	if err := WithTransaction(root, LockScope{Groups: []string{old.GroupID}}, func(tx *Transaction) error {
		return putCheckpoint(tx, &old)
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadCoordinatorCheckpoint(root, old.GroupID)
	if err != nil || loaded.Schema != 1 {
		t.Fatal("a schema 1 checkpoint stopped loading", err)
	}
	next := coordinatorReconcile(t, root, loaded)
	if next.Schema != checkpointSchema || next.Members[id].TransitionSeq == 0 {
		t.Fatalf("checkpoint %+v", next)
	}
	// A cycle nothing on the chain reaches fails with the existing refusal.
	m = next.Members[id]
	m.TransitionSeq, m.Cycle = 0, ReviewDigest([]byte("unknown"))
	if _, err := coordinatorChainPosition(ledgerOf(t, root, id), m); err == nil {
		t.Fatal("an unmatched mirror found a position")
	}
}
