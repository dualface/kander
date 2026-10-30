package board

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func ledgerOf(t *testing.T, root, id string) executionLedger {
	t.Helper()
	var l executionLedger
	err := WithTransaction(root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(id)
		if e != nil {
			return e
		}
		l, e = readExecutionLedger(tx, s)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func chainKinds(l executionLedger) []string {
	kinds := []string{}
	for _, t := range l.chain {
		kinds = append(kinds, t.Kind)
	}
	return kinds
}

func startedCard(t *testing.T, root, slug string) Entry {
	t.Helper()
	s := coordinatorTodo(t, root, slug)
	moved, err := BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	return moved
}

func bound(t *testing.T, root, id string) Snapshot {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	if _, err := BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLedgerRecordsEveryOwnershipChange(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "ledger-chain")
	id := moved.TaskID
	if _, err := RecordExecutionWindow(root, moved, "tmux:s:w:p", ""); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmTaskStartSession(root, moved, "codex", "codex found"); err != nil {
		t.Fatal(err)
	}
	s := bound(t, root, id)
	if _, err := RecordExecutionSession(context.Background(), root, s.Entry, "codex found", "codex repaired"); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExecutionProcess(root, s.Entry, ExecutionProcess{Launcher: "foreground", AgentPID: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Window: "foreground", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmTaskStart(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	review := transactionSnapshot(t, root, id)
	if _, err := MoveEntry(review.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	review = transactionSnapshot(t, root, id)
	if _, err := MoveWithOptions(review.Entry, root, "working", MoveOptions{Owner: "codex", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, root, id)
	want := []string{TransitionStart, TransitionWindowUpdate, TransitionConfirm, TransitionSessionRepair, TransitionProcessIdentity, TransitionHandoff, TransitionConfirm, TransitionReclaim}
	if !slices.Equal(chainKinds(l), want) {
		t.Fatalf("chain %v", chainKinds(l))
	}
	for i, link := range l.chain {
		if link.Seq != uint64(i)+1 {
			t.Fatalf("seq %d at %d", link.Seq, i)
		}
	}
	if l.current.Kind != ExecutionReclaim || l.current.Status != ExecutionRunning {
		t.Fatalf("current %+v", l.current)
	}
	if err := l.assertCard(transactionSnapshot(t, root, id)); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(control(root, "groups", taskStartGroup, id), "transitions", "8.json")
	if _, err := os.Stat(stored); err != nil {
		t.Fatal(err)
	}
}

func TestLedgerRecordsAreProducerOwned(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "ledger-owned")
	id := moved.TaskID
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, id)
	l := ledgerOf(t, root, id)
	err := WithTransaction(root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{id}}, func(tx *Transaction) error {
		if e := putRecordOriginal(tx, *l.current, "pending"); e == nil {
			t.Fatal("an original was overwritten")
		}
		replay := l
		replay.chain = replay.chain[:1]
		if e := putTransition(tx, &replay, l.chain[1]); e == nil {
			t.Fatal("a transition was overwritten")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The ledger is outside every card, so update has no path that reaches it.
	for _, document := range []string{"../../.kander/groups/" + taskStartGroup + "/" + id + "/current.json", "transitions/1.json/../../x"} {
		if err := UpdateDocument(root, id, UpdateOptions{Document: document, Text: "{}", ExpectedRevision: s.Revision}); err == nil {
			t.Fatalf("update wrote %s", document)
		}
	}
	for _, field := range []string{FieldOwner, FieldSession, FieldWindow} {
		text, _ := setMetadata(s.Text, field, "forged")
		if err := UpdateDocument(root, id, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err == nil {
			t.Fatalf("update changed %s", field)
		}
	}
}

// The entrance reports an executor write even when its own write then catches the
// cursor up. Without the fact, the launcher's write would hide the executor's.
func TestEntranceWritesOnLatestSnapshotAndReportsAdvance(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "ledger-advance")
	id := moved.TaskID
	first, err := RecordExecutionWindow(root, moved, "first", "")
	if err != nil || first.Advanced {
		t.Fatalf("own write counted as advance: %+v %v", first, err)
	}
	s := transactionSnapshot(t, root, id)
	updateSnapshot(t, root, s, s.Text+"\nexecutor record\n")
	second, err := RecordExecutionWindow(root, moved, "second", "")
	if err != nil || !second.Advanced {
		t.Fatalf("executor write hidden: %+v %v", second, err)
	}
	third, err := RecordExecutionWindow(root, moved, "third", "")
	if err != nil || third.Advanced || !moved.AdvancedPastCursor() {
		t.Fatalf("advance fact: %+v %v", third, err)
	}
	s = transactionSnapshot(t, root, id)
	if !strings.Contains(s.Text, "executor record") || MetadataFrom(s.Text, FieldWindow) != "third" {
		t.Fatalf("entrance lost a write: %s", s.Text)
	}
	if err := RollbackDocument(root, moved, s.Text, "todo"); err == nil {
		t.Fatal("rollback erased executor work behind a current cursor")
	}
}

func TestEntranceFencesRecordAndCycle(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "ledger-fence")
	id := moved.TaskID
	s := bound(t, root, id)
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExecutionWindow(root, moved, "stale", ""); err == nil {
		t.Fatal("a handed-off launcher wrote WINDOW")
	}
	if err := ConfirmTaskStartSession(root, moved, "codex", "codex found"); err == nil {
		t.Fatal("a handed-off launcher confirmed")
	}
}

func TestConfirmSessionIgnoresTheCardRevision(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "ledger-confirm")
	id := moved.TaskID
	s := transactionSnapshot(t, root, id)
	updateSnapshot(t, root, s, s.Text+"\nfast executor\n")
	s = transactionSnapshot(t, root, id)
	if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmTaskStartSession(root, moved, "codex", "codex found"); err != nil {
		t.Fatal(err)
	}
	if got := ledgerOf(t, root, id).current; got.Status != ExecutionRunning || got.Session != "codex found" {
		t.Fatalf("record %+v", got)
	}
}

func TestProcessIdentityIsWrittenOnce(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "ledger-process")
	before := transactionSnapshot(t, root, moved.TaskID)
	if _, err := RecordExecutionProcess(root, moved, ExecutionProcess{Launcher: "console", AgentPID: 9}); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExecutionProcess(root, moved, ExecutionProcess{Launcher: "console", AgentPID: 10}); err == nil {
		t.Fatal("process identity was rewritten")
	}
	after := transactionSnapshot(t, root, moved.TaskID)
	if after.Revision != before.Revision {
		t.Fatal("process identity touched the card")
	}
	l := ledgerOf(t, root, moved.TaskID)
	if l.current.Process == nil || l.current.Process.AgentPID != 9 || !slices.Contains(chainKinds(l), TransitionProcessIdentity) {
		t.Fatalf("record %+v", l.current)
	}
	path := filepath.Join(control(root, "groups", taskStartGroup, moved.TaskID), l.current.ID, "process.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestPlainMoveToWorkingHasACurrentRecord(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "plain-working")
	if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	cur := ledgerOf(t, root, s.Entry.TaskID).current
	if cur == nil || cur.Status != ExecutionRunning || cur.Kind != ExecutionClaim || cur.Owner != "" {
		t.Fatalf("record %+v", cur)
	}
}

func TestTakeoverRollbackRestoresTheReplacedRecord(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "takeover-rollback")
	id := moved.TaskID
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	s := bound(t, root, id)
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next"}); err != nil {
		t.Fatal(err)
	}
	if err := RollbackDocument(root, s.Entry, s.Text, "working"); err != nil {
		t.Fatal(err)
	}
	after := transactionSnapshot(t, root, id)
	cur := ledgerOf(t, root, id).current
	if MetadataFrom(after.Text, FieldOwner) != "codex" || cur.Status != ExecutionRunning || cur.Kind != ExecutionStart || cur.Owner != "codex" {
		t.Fatalf("record %+v", cur)
	}
	if err := ledgerOf(t, root, id).assertCard(after); err != nil {
		t.Fatal(err)
	}
}

// An old binary decodes the record strictly and rejects it; a newer schema fails
// closed the same way in this binary.
func TestUnknownRecordSchemasFailClosed(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "ledger-schema")
	id := moved.TaskID
	current := control(root, "groups", taskStartGroup, id, "current.json")
	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	var oldReader struct {
		ID               string `json:"id"`
		TaskID           string `json:"task_id"`
		PreviousID       string `json:"previous_id,omitempty"`
		Cycle            string `json:"cycle"`
		Owner            string `json:"owner"`
		Session          string `json:"session"`
		Revision         uint64 `json:"revision"`
		Status           string `json:"status"`
		FinishedRevision uint64 `json:"finished_revision,omitempty"`
	}
	if err = DecodeReviewJSON(data, &oldReader); err == nil {
		t.Fatal("the old start attempt shape accepted a schema 2 record")
	}
	var record map[string]any
	if err = json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["schema"] = 3
	next, _ := json.Marshal(record)
	if err = os.WriteFile(current, next, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = CurrentExecution(root, id); err == nil {
		t.Fatal("an unknown record schema was accepted")
	}
}

// The transition entrances are the only writers of OWNER, SESSION and WINDOW. The
// packages that launch and notify may read those fields, never render them.
func TestLaunchPackagesDoNotSpellExecutionFields(t *testing.T) {
	forbidden := map[string]bool{"RenderField": true, "FieldLineRe": true, "WithResolvedRecord": true, "NewClaimMetadata": true}
	for _, dir := range []string{"launch", "notify", "window"} {
		packages, err := parser.ParseDir(token.NewFileSet(), filepath.Join("..", dir), func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range packages {
			for name, file := range pkg.Files {
				ast.Inspect(file, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok && forbidden[sel.Sel.Name] {
						t.Errorf("%s spells execution metadata through %s", name, sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
}
