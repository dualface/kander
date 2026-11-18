package board

import (
	"slices"
	"strings"
	"testing"
)

// todoGroupCard creates a todo card of the group of membersWorld. It takes the
// same move to todo as a real card, so that a start can run on it.
func todoGroupCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text := strings.ReplaceAll(readyText(s), "<FILL_IN>", "fixture")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: 20260907-evidence-group", 1)
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: evidence", 1)
	updateSnapshot(t, root, s, text)
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err := MoveEntry(s.Entry, root, "todo"); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

// beginStart is the first half of a real start: the start metadata and the
// execution record, with the card in working and the launcher not yet confirmed.
func beginStart(t *testing.T, root string, s Snapshot) Entry {
	t.Helper()
	moved, err := BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	return moved
}

// startGroupCard starts a todo card through the real start path.
func startGroupCard(t *testing.T, root string, s Snapshot) Entry {
	t.Helper()
	moved := beginStart(t, root, s)
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	return moved
}

func (w *membersWorld) putFile(t *testing.T, id, name, text string) {
	t.Helper()
	err := WithTransaction(w.root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error { return tx.Put(id, name, text) })
	if err != nil {
		t.Fatal(err)
	}
}

// A member that starts after the plan exists and its first batch closed cannot
// join while it is in todo. After a real start it joins with the cycle the start
// wrote, takes its own batch, and the plan seals with no cycle rebind.
func TestAddMembersAfterALateStart(t *testing.T) {
	w := newMembersWorld(t, "members-late", false)
	late := todoGroupCard(t, w.root, "members-late-c")
	id := late.Entry.TaskID
	w.candidate = id

	err := w.extend(ReviewPlanExtension{AddMembers: []string{id}})
	if err == nil || !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "is in todo") {
		t.Fatalf("error: %v", err)
	}
	w.untouched(t, "")

	startGroupCard(t, w.root, late)
	cycle := planCycle(transactionSnapshot(t, w.root, id))
	if err = w.extend(ReviewPlanExtension{AddMembers: []string{id}}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	if w.plan.Cycles[id] != cycle || w.plan.Sealed || !slices.Contains(w.plan.TaskIDs, id) {
		t.Fatalf("plan after the addition: %+v", w.plan)
	}
	batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: []string{id}, Base: sha("b"), TargetCommit: sha("c"), Requirements: archiveRequirements()}
	if err = w.extend(ReviewPlanExtension{Batch: &batch}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	closeOwedBatch(t, w.root, batch)
	if err = w.extend(ReviewPlanExtension{Seal: true}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	if !w.plan.Sealed || w.plan.Cycles[id] != cycle {
		t.Fatalf("plan after the seal: %+v", w.plan)
	}
	if problems := CheckReviewGate(w.root, []string{w.first, id}); len(problems) != 0 {
		t.Fatalf("%v", problems)
	}
}

// The capture behind untouched covers each file it names: a change to any one of
// them is a difference.
func TestAddMembersCaptureSeesEachFile(t *testing.T) {
	cases := []struct {
		label  string
		tamper func(t *testing.T, w *membersWorld)
	}{
		{"batch record first", func(t *testing.T, w *membersWorld) {
			w.writeGroup(t, reviewBatchName("first"), map[string]string{"tampered": "yes"})
		}},
		{"plan copy of the first member", func(t *testing.T, w *membersWorld) { w.putFile(t, w.first, "reviews/plan.json", "{}") }},
		{"card file of the candidate", func(t *testing.T, w *membersWorld) {
			w.edit(t, w.candidate, func(text string) (string, error) { return text + "\ntampered\n", nil })
		}},
		{"plan copy of the candidate", func(t *testing.T, w *membersWorld) { w.putFile(t, w.candidate, "reviews/plan.json", "{}") }},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			w := newMembersWorld(t, "members-capture", false)
			w.capture(t)
			if changed := w.differences(t); len(changed) != 0 {
				t.Fatalf("differences before any change: %v", changed)
			}
			c.tamper(t, w)
			if changed := w.differences(t); !slices.Equal(changed, []string{c.label}) {
				t.Fatalf("differences: %v", changed)
			}
		})
	}
}

// A member added while its start is unconfirmed cannot be rolled back: the
// addition publishes a plan copy into the card, so the launcher's revision is
// stale, and no other caller owns the rollback. The plan stays consistent with the
// card in working, which cannot reach the state "member rolled back to todo". The
// start finishes through unresolve and adopt, and the plan seals with no rebind.
func TestAddMembersThenStartRollback(t *testing.T) {
	w := newMembersWorld(t, "members-rollback", false)
	late := todoGroupCard(t, w.root, "members-rollback-c")
	id := late.Entry.TaskID
	w.candidate = id
	moved := beginStart(t, w.root, late)
	if err := w.extend(ReviewPlanExtension{AddMembers: []string{id}}); err != nil {
		t.Fatalf("a member with an unconfirmed start was refused: %v", err)
	}
	w.refresh(t)
	if cycle := planCycle(transactionSnapshot(t, w.root, id)); w.plan.Cycles[id] != cycle {
		t.Fatalf("cycle %q, want %q", w.plan.Cycles[id], cycle)
	}

	err := RollbackDocument(w.root, moved, late.Text, "todo")
	if err == nil || !strings.Contains(err.Error(), "expected revision") || !strings.Contains(err.Error(), "current revision") {
		t.Fatalf("rollback with the launcher entry: %v", err)
	}
	err = RollbackDocument(w.root, bound(t, w.root, id).Entry, late.Text, "todo")
	if err == nil || !strings.Contains(err.Error(), "start rollback binding") {
		t.Fatalf("rollback with a rebound entry: %v", err)
	}
	s := transactionSnapshot(t, w.root, id)
	if pointer, ok := w.planOf(t, id); s.Entry.State != "working" || !ok || pointer != w.plan.PlanID || planCycle(s) != w.plan.Cycles[id] {
		t.Fatalf("after the refused rollback: state %s, pointer %q %v, cycle %q, plan %q", s.Entry.State, pointer, ok, planCycle(s), w.plan.Cycles[id])
	}
	err = w.extend(ReviewPlanExtension{Seal: true})
	if err == nil || !strings.Contains(err.Error(), "every member assigned") || strings.Contains(err.Error(), "terminal") {
		t.Fatalf("seal before the member has a batch: %v", err)
	}
	if problems := CheckReviewGate(w.root, []string{w.first, id}); len(problems) != 0 {
		t.Fatalf("%v", problems)
	}
	if code, out, errs, err := CheckBoard(w.root, []string{w.first, id}, false); err != nil || code != 0 {
		t.Fatalf("check: %d %s %v %v", code, out, errs, err)
	}

	if _, err = UnresolveExecution(w.root, moved, "tmux:s:w:p", "late step failed"); err != nil {
		t.Fatal(err)
	}
	if _, err = AdoptExecution(w.root, bound(t, w.root, id), "user:adopt"); err != nil {
		t.Fatal(err)
	}
	if kinds := chainKinds(ledgerOf(t, w.root, id)); !slices.Equal(kinds, []string{TransitionStart, TransitionUnresolve, TransitionAdopt}) {
		t.Fatalf("chain %v", kinds)
	}
	if planCycle(transactionSnapshot(t, w.root, id)) != w.plan.Cycles[id] {
		t.Fatal("the adoption changed the execution cycle")
	}
	batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: []string{id}, Base: sha("b"), TargetCommit: sha("c"), Requirements: archiveRequirements()}
	if err = w.extend(ReviewPlanExtension{Batch: &batch}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	closeOwedBatch(t, w.root, batch)
	if err = w.extend(ReviewPlanExtension{Seal: true}); err != nil {
		t.Fatal(err)
	}
	if problems := CheckReviewGate(w.root, []string{w.first, id}); len(problems) != 0 {
		t.Fatalf("%v", problems)
	}
}
