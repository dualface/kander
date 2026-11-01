package board

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// writeOldReclaim writes what kander move <id> working --owner <agent> of 1.0.0 left
// on a review card no release preceded: the move metadata, no handoff, no ledger link.
func writeOldReclaim(root string, s Snapshot, owner string) error {
	text, err := moveMetadata(s.Text, "review", "working", MoveOptions{Owner: owner})
	if err != nil {
		return err
	}
	return WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if e := tx.Put(s.Entry.TaskID, "spec.md", text); e != nil {
			return e
		}
		return tx.Relocate(s.Entry.TaskID, "working")
	})
}

// legacyReviewCard is a card 1.0.0 started that its executor moved to review.
func legacyReviewCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	id := legacyRunningCard(t, root, slug).Entry.TaskID
	if err := WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		return tx.Relocate(id, "review")
	}); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, id)
}

func oldReclaim(t *testing.T, root string, s Snapshot, owner string) Snapshot {
	t.Helper()
	if err := writeOldReclaim(root, s, owner); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

// oldReclaimOwners holds both forms: --owner names a new agent, or the same one.
var oldReclaimOwners = map[string]string{"owner changes": "claude", "owner stays": "codex"}

func forOldReclaimOwners(t *testing.T, run func(t *testing.T, owner string)) {
	for name, owner := range oldReclaimOwners {
		t.Run(name, func(t *testing.T) { run(t, owner) })
	}
}

func TestOldReclaimFixtureHasTheShapeOfTheOldBinary(t *testing.T) {
	root := tempBoard(t)
	before := legacyReviewCard(t, root, "old-shape")
	after := oldReclaim(t, root, before, "claude")
	if after.Entry.State != "working" || MetadataFrom(after.Text, FieldOwner) != "claude" || MetadataFrom(after.Text, FieldStartedAt) == "" {
		t.Fatalf("card %s\n%s", after.Entry.State, after.Text)
	}
	if MetadataFrom(after.Text, FieldSession) != MetadataFrom(before.Text, FieldSession) || MetadataFrom(after.Text, FieldWindow) != MetadataFrom(before.Text, FieldWindow) {
		t.Fatal("the reclaim changed SESSION or WINDOW")
	}
	if got, want := claimLines(after.Text), claimLines(before.Text)+1; got != want {
		t.Fatalf("claim lines %d, want %d", got, want)
	}
	if strings.Contains(modelSection(after.Text, "LIFECYCLE_DECISION"), `"kind":"reclaim"`) {
		t.Fatal("the old binary wrote no handoff here")
	}
	if planCycle(after) == planCycle(before) {
		t.Fatal("the reclaim kept the cycle")
	}
}

func TestOldReclaimReadsAsAReclaimOfTheCardCycle(t *testing.T) {
	forOldReclaimOwners(t, func(t *testing.T, owner string) {
		root := tempBoard(t)
		s := oldReclaim(t, root, legacyReviewCard(t, root, "old-read"), owner)
		id := s.Entry.TaskID
		before, err := os.ReadFile(s.Entry.Document)
		if err != nil {
			t.Fatal(err)
		}
		r := requireRecordMatchesCard(t, root, id)
		if r.Cycle != planCycle(s) || r.Kind != ExecutionReclaim || !r.SelfClaimed() || r.Status != ExecutionRunning || r.PreviousID != legacyAttemptID || r.Session != "codex old" {
			t.Fatalf("record %+v", r)
		}
		l := ledgerOf(t, root, id)
		if got := strings.Join(chainKinds(l), " "); got != "start confirm reclaim" || l.synthesized != len(l.chain) {
			t.Fatalf("chain %s at %d", got, l.synthesized)
		}
		if tail, _ := l.tail(); !tail.Unrecorded || tail.ToRecord != r.ID || tail.ToCycle != planCycle(s) {
			t.Fatalf("link %+v", tail)
		}
		if err = l.assertCard(s); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(s.Entry.Document)
		if err != nil || string(after) != string(before) {
			t.Fatal("reading the card changed its bytes", err)
		}
		if _, err = os.Stat(filepath.Join(control(root, "groups", taskStartGroup, id), "transitions")); !os.IsNotExist(err) {
			t.Fatal("the synthesized chain was written back", err)
		}
	})
}

func TestOldReclaimPassesReconcileWithItsMark(t *testing.T) {
	forOldReclaimOwners(t, func(t *testing.T, owner string) {
		root := tempBoard(t)
		before := legacyReviewCard(t, root, "old-reconcile")
		id := before.Entry.TaskID
		c := coordinatorReconcile(t, root, coordinatorClaim(t, root, id))
		after := oldReclaim(t, root, before, owner)
		m := coordinatorReconcile(t, root, c).Members[id]
		want := []CoordinatorUnrecordedReclaim{{FromCycle: planCycle(before), ToCycle: planCycle(after), Record: ledgerOf(t, root, id).current.ID}}
		if m.Blocked != nil || m.Cycle != planCycle(after) || len(m.Handoffs) != 0 || !reflect.DeepEqual(m.UnrecordedReclaims, want) {
			t.Fatalf("mirror %+v", m)
		}
		stored, err := ReadCoordinatorCheckpoint(root, coordinatorGroup)
		if err != nil || !reflect.DeepEqual(stored.Members[id].UnrecordedReclaims, want) {
			t.Fatalf("checkpoint %+v %v", stored.Members[id], err)
		}
		// A claim after the reclaim sees the mark as well.
		other, err := ClaimCoordinator(t.Context(), root, CoordinatorClaim{GroupID: coordinatorGroup, Owner: "next", Token: "session-next", ExpectedRevision: stored.Revision, ExpectedEpoch: stored.Authority.Epoch, Basis: "takeover", Members: []string{id}})
		if err != nil {
			t.Fatal(err)
		}
		if got := coordinatorReconcile(t, root, other).Members[id]; !reflect.DeepEqual(got.UnrecordedReclaims, want) {
			t.Fatalf("mirror %+v", got)
		}
	})
}

// A schema 1 checkpoint has no chain position: the old cycle selects one.
func TestOldReclaimPassesASchemaOneCheckpoint(t *testing.T) {
	root := tempBoard(t)
	s := legacyReviewCard(t, root, "old-schema-one")
	id := s.Entry.TaskID
	c := coordinatorClaim(t, root, id)
	if err := WithTransaction(root, LockScope{Groups: []string{c.GroupID}}, func(tx *Transaction) error {
		m := c.Members[id]
		m.TransitionSeq, m.DispatchSeq = 0, 0
		c.Members, c.Schema = map[string]CoordinatorMember{id: m}, 1
		c.Revision++
		return putCheckpoint(tx, &c)
	}); err != nil {
		t.Fatal(err)
	}
	after := oldReclaim(t, root, s, "claude")
	if m := coordinatorReconcile(t, root, c).Members[id]; m.Blocked != nil || m.Cycle != planCycle(after) || len(m.UnrecordedReclaims) != 1 {
		t.Fatalf("mirror %+v", m)
	}
}

func TestOldReclaimKeepsWorkingAfterTheFirstWrite(t *testing.T) {
	for _, first := range legacyFirstWrites {
		t.Run(first.name, func(t *testing.T) {
			forOldReclaimOwners(t, func(t *testing.T, owner string) {
				root := tempBoard(t)
				s := legacyReviewCard(t, root, "old-first")
				id := s.Entry.TaskID
				c := coordinatorClaim(t, root, id)
				oldReclaim(t, root, s, owner)
				originals := legacyOriginals(t, root, id)
				if err := first.write(t, root, bound(t, root, id)); err != nil {
					t.Fatal(err)
				}
				r := requireRecordMatchesCard(t, root, id)
				if r.Kind != ExecutionReclaim {
					t.Fatalf("record %+v", r)
				}
				l := ledgerOf(t, root, id)
				if l.synthesized != 3 || len(l.chain) != 4 || !l.chain[2].Unrecorded {
					t.Fatalf("chain %v at %d", chainKinds(l), l.synthesized)
				}
				c = coordinatorReconcile(t, root, c)
				if m := c.Members[id]; m.Blocked != nil || m.Cycle != r.Cycle || len(m.UnrecordedReclaims) != 1 {
					t.Fatalf("mirror %+v", m)
				}
				next := bound(t, root, id)
				if _, err := BeginTakeover(root, next.Entry, TakeoverInput{Owner: "grok", Session: "grok next", Decision: "user"}); err != nil {
					t.Fatal(err)
				}
				if err := ConfirmTaskStart(root, next.Entry); err != nil {
					t.Fatal(err)
				}
				if r = requireRecordMatchesCard(t, root, id); r.Owner != "grok" || r.Kind != ExecutionTakeover {
					t.Fatalf("record %+v", r)
				}
				if m := coordinatorReconcile(t, root, c).Members[id]; m.Blocked != nil {
					t.Fatalf("mirror %+v", m)
				}
				if got := legacyOriginals(t, root, id); !reflect.DeepEqual(got, originals) {
					t.Fatalf("the attempt originals changed: %v", got)
				}
			})
		})
	}
}

// The takeover is the first write here, so it stores the first link after the prefix.
func TestOldReclaimTakesATakeoverAsItsFirstWrite(t *testing.T) {
	forOldReclaimOwners(t, func(t *testing.T, owner string) {
		root := tempBoard(t)
		s := legacyReviewCard(t, root, "old-takeover")
		id := s.Entry.TaskID
		c := coordinatorClaim(t, root, id)
		reclaimed := oldReclaim(t, root, s, owner)
		next := bound(t, root, id)
		if _, err := BeginTakeover(root, next.Entry, TakeoverInput{Owner: "grok", Session: "grok next", Decision: "user"}); err != nil {
			t.Fatal(err)
		}
		if err := ConfirmTaskStart(root, next.Entry); err != nil {
			t.Fatal(err)
		}
		l := ledgerOf(t, root, id)
		if got := strings.Join(chainKinds(l), " "); got != "start confirm reclaim handoff confirm" {
			t.Fatalf("chain %s", got)
		}
		if l.chain[l.synthesized].FromCycle != planCycle(reclaimed) || !l.chain[2].Unrecorded {
			t.Fatalf("handoff %+v", l.chain[l.synthesized])
		}
		if m := coordinatorReconcile(t, root, c).Members[id]; m.Blocked != nil || m.Cycle != planCycle(reclaimed) {
			t.Fatalf("mirror %+v", m)
		}
	})
}

// dispatchlessHandoff appends a reclaim record that binds no release, with its claim.
func dispatchlessHandoff(t *testing.T, root string, s Snapshot) Snapshot {
	t.Helper()
	text, err := NewClaimMetadata(s.Text)
	if err != nil {
		t.Fatal(err)
	}
	h := lifecycleHandoff{Kind: "reclaim", OldCycle: planCycle(s), NewCycle: planCycle(Snapshot{Entry: s.Entry, Text: text}), Decision: "user", Owner: "pi", CardRevision: s.Revision + 1}
	if err = WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error {
		return tx.Put(s.Entry.TaskID, "spec.md", appendLifecycleRecord(text, "LIFECYCLE_DECISION", lifecycleJSON(h)))
	}); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

// A silent reclaim can sit on either side of a recorded one. The mirror crosses it
// in both orders, also on the replay of the handoffs it kept.
func TestOldReclaimBesideARecordedHandoff(t *testing.T) {
	toReview := func(t *testing.T, root string, s Snapshot) Snapshot {
		t.Helper()
		if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, ExclusiveBoard: true}, func(tx *Transaction) error {
			return tx.Relocate(s.Entry.TaskID, "review")
		}); err != nil {
			t.Fatal(err)
		}
		return transactionSnapshot(t, root, s.Entry.TaskID)
	}
	t.Run("silent first", func(t *testing.T) {
		root := tempBoard(t)
		s := legacyReviewCard(t, root, "silent-first")
		id := s.Entry.TaskID
		c := coordinatorClaim(t, root, id)
		dispatchlessHandoff(t, root, oldReclaim(t, root, s, "claude"))
		if got := strings.Join(chainKinds(ledgerOf(t, root, id)), " "); got != "start confirm reclaim reclaim" {
			t.Fatalf("chain %s", got)
		}
		c = coordinatorReconcile(t, root, c)
		if m := c.Members[id]; m.Blocked != nil || len(m.Handoffs) != 1 || len(m.UnrecordedReclaims) != 1 {
			t.Fatalf("mirror %+v", m)
		}
		if m := coordinatorReconcile(t, root, c).Members[id]; m.Blocked != nil {
			t.Fatalf("replay %+v", m)
		}
	})
	t.Run("silent last", func(t *testing.T) {
		root := tempBoard(t)
		s := legacyReviewCard(t, root, "silent-last")
		id := s.Entry.TaskID
		c := coordinatorClaim(t, root, id)
		s = dispatchlessHandoff(t, root, s)
		c = coordinatorReconcile(t, root, c)
		if m := c.Members[id]; m.Blocked != nil || len(m.Handoffs) != 1 || len(m.UnrecordedReclaims) != 0 {
			t.Fatalf("mirror %+v", m)
		}
		oldReclaim(t, root, toReview(t, root, s), "claude")
		c = coordinatorReconcile(t, root, c)
		if m := c.Members[id]; m.Blocked != nil || len(m.Handoffs) != 1 || len(m.UnrecordedReclaims) != 1 {
			t.Fatalf("mirror %+v", m)
		}
		if m := coordinatorReconcile(t, root, c).Members[id]; m.Blocked != nil {
			t.Fatalf("replay %+v", m)
		}
	})
}

// A link after the prefix proves nothing by itself: a stored cycle change needs a
// handoff, also on a legacy card.
func TestStoredCycleChangeStillNeedsAHandoff(t *testing.T) {
	root := tempBoard(t)
	s := legacyReviewCard(t, root, "stored-reclaim")
	id := s.Entry.TaskID
	c := coordinatorClaim(t, root, id)
	if _, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "pi", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	moved := transactionSnapshot(t, root, id)
	var lines []string
	for line := range strings.SplitSeq(moved.Text, "\n") {
		if !strings.Contains(line, `"kind":"reclaim"`) {
			lines = append(lines, line)
		}
	}
	if err := WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		return tx.Put(id, "spec.md", strings.Join(lines, "\n"))
	}); err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, root, id)
	if len(l.unrecordedReclaims()) != 0 || l.chain[l.synthesized].Kind != TransitionReclaim {
		t.Fatalf("chain %v at %d", chainKinds(l), l.synthesized)
	}
	var blocked *CoordinatorBlockedError
	if _, err := ReconcileCoordinator(t.Context(), root, coordinatorRequest(t, root, c), nil); !errors.As(err, &blocked) || !strings.Contains(err.Error(), "without lifecycle handoff") {
		t.Fatalf("err=%v", err)
	}
}

func TestOnlyThePrefixCanHoldAMark(t *testing.T) {
	l := executionLedger{chain: []ExecutionTransition{{Kind: TransitionReclaim, Unrecorded: true}, {Kind: TransitionReclaim, Unrecorded: true}}, synthesized: 1}
	if got := l.unrecordedReclaims(); len(got) != 1 {
		t.Fatalf("marks %+v", got)
	}
	data, err := json.Marshal(ExecutionTransition{Unrecorded: true})
	if err != nil || strings.Contains(string(data), "nrecorded") {
		t.Fatalf("the mark reached a stored link: %s %v", data, err)
	}
}

// A cycle that changed with no second claim line is no reclaim: the card stays off
// its chain, and the mirror stays refused.
func TestCycleChangeWithoutAClaimStaysRefused(t *testing.T) {
	root := tempBoard(t)
	s := legacyReviewCard(t, root, "forged-start")
	id := s.Entry.TaskID
	c := coordinatorClaim(t, root, id)
	text, err := setMetadata(s.Text, FieldStartedAt, "2030-01-01 00:00")
	if err != nil {
		t.Fatal(err)
	}
	if err = fixtureWrite(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	if len(ledgerOf(t, root, id).unrecordedReclaims()) != 0 {
		t.Fatal("a forged time read as a reclaim")
	}
	var blocked *CoordinatorBlockedError
	if _, err = ReconcileCoordinator(t.Context(), root, coordinatorRequest(t, root, c), nil); !errors.As(err, &blocked) || !strings.Contains(err.Error(), "without lifecycle handoff") {
		t.Fatalf("err=%v", err)
	}
}

// The binding check stays as strict as it was: a stored record of another cycle
// fails, whichever way the card left that cycle.
func TestStartBindingStillRefusesAStoredRecordOfAnotherCycle(t *testing.T) {
	builds := map[string]func(*testing.T, string) string{
		"ledger card": func(t *testing.T, root string) string {
			moved := startedCard(t, root, "stored-ledger")
			if err := ConfirmTaskStart(root, moved); err != nil {
				t.Fatal(err)
			}
			return moved.TaskID
		},
		"legacy card after its first write": func(t *testing.T, root string) string {
			id := legacyRunningCard(t, root, "stored-legacy").Entry.TaskID
			if _, err := RecordExecutionWindow(root, bound(t, root, id).Entry, "tmux:drifted", ""); err != nil {
				t.Fatal(err)
			}
			return id
		},
	}
	for name, build := range builds {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			id := build(t, root)
			staleReclaim(t, root, id)
			s := transactionSnapshot(t, root, id)
			if _, err := BindExecutionCursor(root, s.Entry); err != nil {
				t.Fatal(err)
			}
			if _, err := RecordExecutionWindow(root, s.Entry, "tmux:next", ""); err == nil || !strings.Contains(err.Error(), "start success binding") {
				t.Fatalf("err=%v", err)
			}
			if slices.ContainsFunc(ledgerOf(t, root, id).chain, func(l ExecutionTransition) bool { return l.Unrecorded }) {
				t.Fatal("a cycle change after a stored link got a mark")
			}
		})
	}
}
