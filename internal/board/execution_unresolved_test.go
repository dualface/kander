package board

import (
	"errors"
	"testing"
)

func cursorRecord(t *testing.T, root, id string) ExecutionRecord {
	t.Helper()
	r, ok, err := CurrentExecution(root, id)
	if err != nil || !ok {
		t.Fatalf("record %+v %v %v", r, ok, err)
	}
	return r
}

func TestStandingIgnoresTheLaunchersOwnWrites(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "own-writes")
	if _, err := RecordExecutionWindow(root, moved, "tmux:a:b:c", ""); err != nil {
		t.Fatal(err)
	}
	standing, err := ReadExecutionStanding(root, moved)
	if err != nil || standing != (ExecutionStanding{}) {
		t.Fatalf("standing %+v err=%v", standing, err)
	}
}

// A successful entrance moves the cursor to the latest revision. The executor's
// earlier write must stay evidence after that.
func TestStandingKeepsEvidenceAcrossAnEntrance(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "kept-evidence")
	s := transactionSnapshot(t, root, moved.TaskID)
	updateSnapshot(t, root, s, s.Text+"\nexecutor record\n")
	standing, err := ReadExecutionStanding(root, moved)
	if err != nil || !standing.Evidence {
		t.Fatalf("before the write: %+v err=%v", standing, err)
	}
	if _, err = RecordExecutionWindow(root, moved, "tmux:a:b:c", ""); err != nil {
		t.Fatal(err)
	}
	if standing, err = ReadExecutionStanding(root, moved); err != nil || !standing.Evidence {
		t.Fatalf("after the write: %+v err=%v", standing, err)
	}
}

func TestStandingSeesAColumnChange(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "column-evidence")
	s := transactionSnapshot(t, root, moved.TaskID)
	if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	standing, err := ReadExecutionStanding(root, moved)
	if err != nil || !standing.Evidence {
		t.Fatalf("standing %+v err=%v", standing, err)
	}
}

func TestUnresolveKeepsCycleAndRecordsTheAddress(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "unresolve")
	id := moved.TaskID
	before := transactionSnapshot(t, root, id)
	if _, err := UnresolveExecution(root, moved, "tmux:a:b:c", "late failure"); err != nil {
		t.Fatal(err)
	}
	after := transactionSnapshot(t, root, id)
	r := cursorRecord(t, root, id)
	if r.Status != ExecutionUnresolved || r.Failure != "late failure" || r.Window != "tmux:a:b:c" || r.Cycle != planCycle(before) {
		t.Fatalf("record %+v", r)
	}
	if planCycle(after) != planCycle(before) || MetadataFrom(after.Text, FieldWindow) != "tmux:a:b:c" || after.Entry.State != "working" {
		t.Fatalf("card %s", after.Text)
	}
	l := ledgerOf(t, root, id)
	if tail, _ := l.tail(); tail.Kind != TransitionUnresolve || tail.FromStatus != ExecutionPending || tail.ToCycle != tail.FromCycle {
		t.Fatalf("tail %+v", tail)
	}
	if err := l.assertCard(after); err != nil {
		t.Fatal(err)
	}
}

func TestUnresolveAcceptsOnlyTheLauncher(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "unresolve-writer")
	id := moved.TaskID
	stranger := bound(t, root, id)
	if _, err := UnresolveExecution(root, stranger.Entry, "", "forged"); err == nil {
		t.Fatal("a writer that is not the launcher wrote unresolved")
	}
	stale := moved
	version := &Version{revision: moved.Version.revision - 1, fence: moved.Version.fence}
	stale.Version = version
	if _, err := UnresolveExecution(root, stale, "", "stale"); err == nil {
		t.Fatal("a cursor behind the attempt wrote unresolved")
	}
	if _, err := UnresolveExecution(root, moved, "", " "); err == nil {
		t.Fatal("unresolved without a reason")
	}
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := UnresolveExecution(root, moved, "", "too late"); err == nil {
		t.Fatal("a running record became unresolved")
	}
}

// A card that already left the two columns keeps its text; the record ends anyway.
func TestUnresolveLeavesAFinishedCardUnchanged(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "unresolve-finished")
	id := moved.TaskID
	s := transactionSnapshot(t, root, id)
	if _, err := MoveWithOptions(s.Entry, root, "trash", MoveOptions{Result: "trashed", Reason: "gone", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	before := transactionSnapshot(t, root, id)
	if _, err := UnresolveExecution(root, moved, "tmux:a:b:c", "late failure"); err != nil {
		t.Fatal(err)
	}
	if after := transactionSnapshot(t, root, id); after.Revision != before.Revision {
		t.Fatalf("card changed: %s", after.Text)
	}
}

func TestAdoptEndsAnUnfinishedStart(t *testing.T) {
	for _, status := range []string{ExecutionPending, ExecutionUnresolved} {
		t.Run(status, func(t *testing.T) {
			root := tempBoard(t)
			moved := startedCard(t, root, "adopt")
			id := moved.TaskID
			if status == ExecutionUnresolved {
				if _, err := UnresolveExecution(root, moved, "", "late failure"); err != nil {
					t.Fatal(err)
				}
			}
			before := transactionSnapshot(t, root, id)
			if _, err := AdoptExecution(root, bound(t, root, id), "user"); err != nil {
				t.Fatal(err)
			}
			r := cursorRecord(t, root, id)
			if r.Status != ExecutionRunning || r.Failure != "" {
				t.Fatalf("record %+v", r)
			}
			if after := transactionSnapshot(t, root, id); after.Revision != before.Revision {
				t.Fatal("adoption changed the card")
			}
			tail, _ := ledgerOf(t, root, id).tail()
			if tail.Kind != TransitionAdopt || tail.Decision != "user" || tail.FromStatus != status || tail.ToRecord != r.ID || tail.CardRevision != before.Revision {
				t.Fatalf("tail %+v", tail)
			}
		})
	}
}

func TestAdoptRefuses(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "adopt-refused")
	id := moved.TaskID
	var e *Error
	if _, err := AdoptExecution(root, bound(t, root, id), " "); !errors.As(err, &e) || e.Code != "board.adopt_decision_required" {
		t.Fatalf("err=%v", err)
	}
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptExecution(root, bound(t, root, id), "user"); !errors.As(err, &e) || e.Code != "board.adopt_needs_unfinished_start" {
		t.Fatalf("running: %v", err)
	}
}

// A cursor bound before a handoff cannot adopt what the handoff replaced.
func TestAdoptIsFencedToTheObservedRecord(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "adopt-fenced")
	id := moved.TaskID
	observed := bound(t, root, id)
	taker := bound(t, root, id)
	if _, err := BeginTakeover(root, taker.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptExecution(root, observed, "user"); err == nil {
		t.Fatal("a stale observation adopted the new attempt")
	}
}

func TestTakeoverOfAnUnfinishedStartNeedsADecision(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "takeover-decision")
	id := moved.TaskID
	if _, err := UnresolveExecution(root, moved, "", "late failure"); err != nil {
		t.Fatal(err)
	}
	s := bound(t, root, id)
	var e *Error
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next"}); !errors.As(err, &e) || e.Code != "board.takeover_decision_required" {
		t.Fatalf("err=%v", err)
	}
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	tail, _ := ledgerOf(t, root, id).tail()
	if tail.Kind != TransitionHandoff || tail.FromStatus != ExecutionUnresolved || tail.Decision != "user" {
		t.Fatalf("tail %+v", tail)
	}
	// The new attempt ran, so a takeover of it needs no decision.
	if err := ConfirmTaskStart(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	next := bound(t, root, id)
	if _, err := BeginTakeover(root, next.Entry, TakeoverInput{Owner: "codex", Session: "codex again"}); err != nil {
		t.Fatal(err)
	}
}

// A failed takeover restores the unresolved record it replaced, with its reason.
func TestRollbackRestoresTheUnresolvedRecord(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "restore-unresolved")
	id := moved.TaskID
	if _, err := UnresolveExecution(root, moved, "", "late failure"); err != nil {
		t.Fatal(err)
	}
	s := bound(t, root, id)
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := RollbackDocument(root, s.Entry, s.Text, "working"); err != nil {
		t.Fatal(err)
	}
	if r := cursorRecord(t, root, id); r.Status != ExecutionUnresolved || r.Failure != "late failure" || r.Kind != ExecutionStart {
		t.Fatalf("record %+v", r)
	}
}

func TestSupersededLauncherStepsAside(t *testing.T) {
	overtake := map[string]func(t *testing.T, root, id string){
		"handoff": func(t *testing.T, root, id string) {
			s := bound(t, root, id)
			if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Decision: "user"}); err != nil {
				t.Fatal(err)
			}
		},
		"adoption": func(t *testing.T, root, id string) {
			if _, err := AdoptExecution(root, bound(t, root, id), "user"); err != nil {
				t.Fatal(err)
			}
		},
		"reclaim": func(t *testing.T, root, id string) {
			s := transactionSnapshot(t, root, id)
			moved, err := MoveEntry(s.Entry, root, "review")
			if err != nil {
				t.Fatal(err)
			}
			s = transactionSnapshot(t, root, moved.TaskID)
			if _, err = MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "claude", Decision: "user"}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, run := range overtake {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			moved := startedCard(t, root, "superseded")
			id := moved.TaskID
			run(t, root, id)
			before := cursorRecord(t, root, id)
			standing, err := ReadExecutionStanding(root, moved)
			if err != nil || !standing.Superseded {
				t.Fatalf("standing %+v err=%v", standing, err)
			}
			if err = ConfirmTaskStart(root, moved); !errors.Is(err, ErrExecutionSuperseded) {
				t.Fatalf("confirm: %v", err)
			}
			if _, err = UnresolveExecution(root, moved, "", "late failure"); !errors.Is(err, ErrExecutionSuperseded) {
				t.Fatalf("unresolve: %v", err)
			}
			if after := cursorRecord(t, root, id); after.ID != before.ID || after.Status != before.Status {
				t.Fatalf("record changed: %+v", after)
			}
		})
	}
}

func TestDispatchWaitsForAnUnfinishedStart(t *testing.T) {
	for _, status := range []string{ExecutionPending, ExecutionUnresolved} {
		t.Run(status, func(t *testing.T) {
			root := tempBoard(t)
			moved := startedCard(t, root, "dispatch-unfinished")
			id := moved.TaskID
			if status == ExecutionUnresolved {
				if _, err := UnresolveExecution(root, moved, "", "late failure"); err != nil {
					t.Fatal(err)
				}
			}
			in := dispatchInput(transactionSnapshot(t, root, id), "dispatch-unfinished")
			var e *Error
			if _, err := PrepareDispatch(root, in); !errors.As(err, &e) || e.Code != "board.dispatch_start_unfinished" {
				t.Fatalf("err=%v", err)
			}
			if _, err := AdoptExecution(root, bound(t, root, id), "user"); err != nil {
				t.Fatal(err)
			}
			prepareTestDispatch(t, root, in)
		})
	}
}

// A schema 1 start stuck in pending opens to an adoption. The first stored link
// continues the synthesized prefix.
func TestLegacyPendingAcceptsAnAdoption(t *testing.T) {
	root := tempBoard(t)
	s := legacyStartedCard(t, root, "legacy-adopt")
	id := s.Entry.TaskID
	legacyAttempt(t, root, s, "stuck-attempt", "pending", 0)
	s = transactionSnapshot(t, root, id)
	c := coordinatorClaim(t, root, id)
	if _, err := BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptExecution(root, s, "user"); err != nil {
		t.Fatal(err)
	}
	if r := cursorRecord(t, root, id); r.Status != ExecutionRunning {
		t.Fatalf("record %+v", r)
	}
	if c = coordinatorReconcile(t, root, c); c.Members[id].AwaitingStart {
		t.Fatalf("mirror %+v", c.Members[id])
	}
}
