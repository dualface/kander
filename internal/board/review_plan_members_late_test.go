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
