package board

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const otherRemoveGroup = "20260909-other-removal-group"

// removeGroup is a group of unstarted members in todo, claimed and reconciled.
func removeGroup(t *testing.T, root string, slugs ...string) (ids []string, c CoordinatorCheckpoint) {
	t.Helper()
	for _, slug := range slugs {
		ids = append(ids, coordinatorTodo(t, root, slug).Entry.TaskID)
	}
	return ids, coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
}

func removeRequest(c CoordinatorCheckpoint, id string) CoordinatorRemoveMember {
	return CoordinatorRemoveMember{GroupID: c.GroupID, Member: id, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Decision: "user-decision-2", Reason: "the card depends on another group"}
}

func removeMemberOf(t *testing.T, root string, c CoordinatorCheckpoint, id string) CoordinatorCheckpoint {
	t.Helper()
	next, err := RemoveCoordinatorMember(context.Background(), root, removeRequest(c, id))
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// removeSnapshot is every byte a removal may write: the checkpoint file, the
// cards, and the removal records.
func removeSnapshot(t *testing.T, root, group string, ids ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	b, err := os.ReadFile(control(root, "groups", group, "checkpoint.json"))
	if err != nil {
		t.Fatal(err)
	}
	out["checkpoint"] = string(b)
	for _, id := range ids {
		out[id] = transactionSnapshot(t, root, id).Text
	}
	if entries, err := os.ReadDir(control(root, "groups", group, "removals")); err == nil {
		for _, e := range entries {
			out["removals/"+e.Name()] = e.Name()
		}
	}
	return out
}

// refusedWith matches a board error code, or else a text the message holds. The
// message is localized, so a refusal with a code of its own is matched by it.
func refusedWith(err error, want string) bool {
	var e *Error
	if errors.As(err, &e) && e.Code == want {
		return true
	}
	return err != nil && strings.Contains(err.Error(), want)
}

func requireRemovalRefused(t *testing.T, root string, c CoordinatorCheckpoint, ids []string, r CoordinatorRemoveMember, want string) {
	t.Helper()
	before := removeSnapshot(t, root, c.GroupID, ids...)
	_, err := RemoveCoordinatorMember(context.Background(), root, r)
	if !refusedWith(err, want) {
		t.Fatalf("want an error with %q, got %v", want, err)
	}
	after := removeSnapshot(t, root, c.GroupID, ids...)
	if len(before) != len(after) {
		t.Fatalf("a refused removal wrote: %v -> %v", before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("a refused removal changed %s", k)
		}
	}
	requireCheckpointUnchanged(t, root, c)
}

// putCardText replaces the text of a card the way no command would, to build a
// state a refusal needs.
func putCardText(t *testing.T, root, id string, edit func(string) string) {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	if err := WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		return tx.Put(id, "spec.md", edit(s.Text))
	}); err != nil {
		t.Fatal(err)
	}
}

func moveWith(t *testing.T, root, id, state string, o MoveOptions) {
	t.Helper()
	if _, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, state, o); err != nil {
		t.Fatal(err)
	}
}

func prerequisiteBlock(text, ids string) string {
	return strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\n```text\nPREREQUISITES: "+ids+"\n```\n", 1)
}

func TestRemoveMemberTakesAnUnstartedMemberOut(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "remove-a", "remove-b", "remove-c")
	if strings.Contains(reviewJSON(c), `"removals"`) {
		t.Fatal("an empty removals list was written")
	}
	next := removeMemberOf(t, root, c, ids[0])
	if _, still := next.Members[ids[0]]; still || len(next.Members) != 2 || next.MemberVersion != coordinatorHash(ids[1:]) {
		t.Fatalf("members: %v", checkpointIDs(next))
	}
	if next.Revision != c.Revision+1 || next.Authority != c.Authority || next.Schema != checkpointSchema || checkpointSchema != 4 || next.LastRequestHash != "" {
		t.Fatalf("checkpoint: %+v", next)
	}
	if stored := storedCheckpoint(t, root); stored.Revision != next.Revision || len(stored.Removals) != 1 {
		t.Fatalf("stored: %+v", stored)
	}
	r := next.Removals[0]
	if r.TaskID != ids[0] || r.Decision != "user-decision-2" || r.Reason == "" || r.CheckpointRevision != c.Revision || r.CoordinatorEpoch != c.Authority.Epoch || r.FromTaskGroup != coordinatorGroup || r.Schema != removalSchema {
		t.Fatalf("removal: %+v", r)
	}
	if b, err := os.ReadFile(control(root, "groups", c.GroupID, removalName(r.RemovalID))); err != nil || string(b) != reviewJSON(r) {
		t.Fatalf("record: %v", err)
	}
	// The group on the board and the audit record of the card.
	text := transactionSnapshot(t, root, ids[0]).Text
	if TaskGroupFrom(text) != "" {
		t.Fatalf("TASK_GROUP: %q", TaskGroupFrom(text))
	}
	decisions, _ := SectionBody(text, "CONTRACT_DECISIONS")
	if !strings.Contains(decisions, "user-decision-2") || !strings.Contains(decisions, coordinatorGroup) {
		t.Fatalf("decisions: %s", decisions)
	}
	// A plain update cannot rewrite the record.
	s := transactionSnapshot(t, root, ids[0])
	stripped := strings.Replace(s.Text, decisions, "", 1)
	if err := UpdateDocument(root, ids[0], UpdateOptions{Document: "spec.md", Text: stripped, ExpectedRevision: s.Revision}); err == nil {
		t.Fatal("update rewrote CONTRACT_DECISIONS")
	}
}

func TestRemoveMemberLeavesTheRestOfTheGroupWorking(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "rest-a", "rest-b", "rest-c")
	oldClaim := CoordinatorClaim{GroupID: coordinatorGroup, Owner: "coordinator", Token: "session-one", Basis: "测试编排授权", Members: slices.Clone(ids)}
	next := removeMemberOf(t, root, c, ids[0])
	// A reconcile of the remaining members, at the new revision, is accepted.
	reconciled := coordinatorReconcile(t, root, next)
	if len(reconciled.Members) != 2 {
		t.Fatalf("members: %v", checkpointIDs(reconciled))
	}
	// The earlier claim replayed byte for byte is not a replay of the new set.
	if _, err := ClaimCoordinator(context.Background(), root, oldClaim); err == nil {
		t.Fatal("the claim before the removal was accepted")
	}
	claim := func(token string, members ...string) (CoordinatorCheckpoint, error) {
		stored := storedCheckpoint(t, root)
		return ClaimCoordinator(context.Background(), root, CoordinatorClaim{GroupID: coordinatorGroup, ExpectedRevision: stored.Revision, ExpectedEpoch: stored.Authority.Epoch, Owner: "coordinator", Token: token, Basis: "测试编排授权", Members: members})
	}
	if _, err := claim("session-two", ids...); err == nil {
		t.Fatal("a claim that still names the removed member was accepted")
	}
	taken, err := claim("session-two", ids[1:]...)
	if err != nil || taken.Authority.Epoch != c.Authority.Epoch+1 || len(taken.Removals) != 1 {
		t.Fatalf("%+v %v", taken, err)
	}
	// A second removal needs the checkpoint it was decided on.
	if _, err := RemoveCoordinatorMember(context.Background(), root, removeRequest(c, ids[1])); err == nil || !strings.Contains(err.Error(), "remove CAS") {
		t.Fatalf("a request on the old revision: %v", err)
	}
}

func TestRemovedCardIsAnOrdinaryCard(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "ordinary-a", "ordinary-b")
	removeMemberOf(t, root, c, ids[0])
	s := transactionSnapshot(t, root, ids[0])
	// It can be regrouped into a group with no checkpoint, with a decision.
	text, err := setMetadata(s.Text, "TASK_GROUP", otherRemoveGroup)
	if err != nil {
		t.Fatal(err)
	}
	if err = UpdateDocument(root, ids[0], UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, ContractDecision: "user-decision-3"}); err != nil {
		t.Fatal(err)
	}
	if got := TaskGroupFrom(transactionSnapshot(t, root, ids[0]).Text); got != otherRemoveGroup {
		t.Fatalf("group: %q", got)
	}
	// And it can start: the claim into working takes an owner.
	s = transactionSnapshot(t, root, ids[0])
	if _, err = MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex"}); err != nil {
		t.Fatal(err)
	}
}

func TestRemovedBacklogCardCanBePicked(t *testing.T) {
	root := tempBoard(t)
	other := coordinatorTodo(t, root, "pick-other").Entry.TaskID
	s := transactionCard(t, root, "pick-removed", false)
	text, err := setMetadata(strings.ReplaceAll(readyText(s), "<FILL_IN>", "fixture"), "TASK_GROUP", coordinatorGroup)
	if err != nil {
		t.Fatal(err)
	}
	updateSnapshot(t, root, s, text)
	id := s.Entry.TaskID
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, id, other))
	removeMemberOf(t, root, c, id)
	s = transactionSnapshot(t, root, id)
	if s.Entry.State != "backlog" || TaskGroupFrom(s.Text) != "" {
		t.Fatalf("%s %q", s.Entry.State, TaskGroupFrom(s.Text))
	}
	if _, err = MoveEntry(s.Entry, root, "todo"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveMemberClearsAGroupWrittenInDiscussion(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "legacy-a", "legacy-b")
	putCardText(t, root, ids[0], func(text string) string {
		text, _ = setMetadata(text, "TASK_GROUP", "")
		return strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nTASK_GROUP: "+coordinatorGroup+"\n", 1)
	})
	if TaskGroupFrom(transactionSnapshot(t, root, ids[0]).Text) != coordinatorGroup {
		t.Fatal("the fixture does not read as a member")
	}
	c = coordinatorReconcile(t, root, c)
	removeMemberOf(t, root, c, ids[0])
	text := transactionSnapshot(t, root, ids[0]).Text
	if got := TaskGroupFrom(text); got != "" {
		t.Fatalf("group: %q", got)
	}
	if discussion, _ := SectionBody(text, SectionDiscussion); strings.Contains(discussion, "TASK_GROUP:") {
		t.Fatalf("discussion: %s", discussion)
	}
}

func TestRemoveMemberKeepsItsOwnPrerequisites(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "own-a", "own-b")
	putCardText(t, root, ids[1], func(text string) string { return prerequisiteBlock(text, ids[0]) })
	c = coordinatorReconcile(t, root, c)
	// The removed card waits for a member that stays: allowed, and unchanged.
	next := removeMemberOf(t, root, c, ids[1])
	if len(next.Members) != 1 {
		t.Fatalf("members: %v", checkpointIDs(next))
	}
	if got, _ := SectionBody(transactionSnapshot(t, root, ids[1]).Text, SectionDiscussion); !strings.Contains(got, "PREREQUISITES: "+ids[0]) {
		t.Fatalf("discussion: %s", got)
	}
}

func TestRemoveMemberRefusals(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "refuse-a", "refuse-b", "refuse-c")
	good := removeRequest(c, ids[0])
	for name, edit := range map[string]func(*CoordinatorRemoveMember){
		"group":             func(r *CoordinatorRemoveMember) { r.GroupID = "" },
		"member":            func(r *CoordinatorRemoveMember) { r.Member = "" },
		"expected revision": func(r *CoordinatorRemoveMember) { r.ExpectedRevision = 0 },
		"expected epoch":    func(r *CoordinatorRemoveMember) { r.ExpectedEpoch = 0 },
		"decision":          func(r *CoordinatorRemoveMember) { r.Decision = "" },
		"blank decision":    func(r *CoordinatorRemoveMember) { r.Decision = " \t " },
		"reason":            func(r *CoordinatorRemoveMember) { r.Reason = "" },
		"blank reason":      func(r *CoordinatorRemoveMember) { r.Reason = "\n" },
	} {
		t.Run("missing "+name, func(t *testing.T) {
			r := good
			edit(&r)
			want := "board.remove_member_request_invalid"
			if name == "member" {
				want = ""
			}
			requireRemovalRefused(t, root, c, ids, r, want)
		})
	}
	t.Run("stale revision", func(t *testing.T) {
		r := good
		r.ExpectedRevision++
		requireRemovalRefused(t, root, c, ids, r, "remove CAS")
	})
	t.Run("fenced epoch", func(t *testing.T) {
		r := good
		r.ExpectedEpoch++
		requireRemovalRefused(t, root, c, ids, r, "remove CAS")
	})
	t.Run("no checkpoint", func(t *testing.T) {
		r := good
		r.GroupID = otherRemoveGroup
		if _, err := RemoveCoordinatorMember(context.Background(), root, r); err == nil || !strings.Contains(err.Error(), "checkpoint missing") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("not a member of the checkpoint", func(t *testing.T) {
		root := tempBoard(t)
		outsider := transactionCard(t, root, "refuse-outsider", false)
		updateSnapshot(t, root, outsider, strings.ReplaceAll(readyText(outsider), "<FILL_IN>", "fixture"))
		ids, c := removeGroup(t, root, "refuse-member")
		requireRemovalRefused(t, root, c, append(slices.Clone(ids), outsider.Entry.TaskID), removeRequest(c, outsider.Entry.TaskID), "board.remove_member_unknown")
	})
	t.Run("another group on the board", func(t *testing.T) {
		putCardText(t, root, ids[2], func(text string) string { text, _ = setMetadata(text, "TASK_GROUP", otherRemoveGroup); return text })
		defer putCardText(t, root, ids[2], func(text string) string { text, _ = setMetadata(text, "TASK_GROUP", coordinatorGroup); return text })
		requireRemovalRefused(t, root, c, ids, removeRequest(c, ids[2]), "board.remove_member_group_mismatch")
	})
	t.Run("a remaining member lists it", func(t *testing.T) {
		putCardText(t, root, ids[2], func(text string) string { return prerequisiteBlock(text, ids[0]+","+ids[1]) })
		defer putCardText(t, root, ids[2], func(text string) string {
			return strings.Replace(text, "```text\nPREREQUISITES: "+ids[0]+","+ids[1]+"\n```\n", "", 1)
		})
		requireRemovalRefused(t, root, c, ids, removeRequest(c, ids[0]), "board.remove_member_dependents")
	})
}

func TestRemoveMemberRefusesAStartedMember(t *testing.T) {
	cases := map[string]func(t *testing.T, root, id string){
		"working": func(t *testing.T, root, id string) { moveMember(t, root, id, "working") },
		"review": func(t *testing.T, root, id string) {
			moveMember(t, root, id, "working")
			moveMember(t, root, id, "review")
		},
		"done": func(t *testing.T, root, id string) {
			// The review evidence of a real completion is not what this case is about.
			if err := WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error { return tx.Relocate(id, "done") }); err != nil {
				t.Fatal(err)
			}
		},
		"archived": func(t *testing.T, root, id string) {
			moveWith(t, root, id, "archived", MoveOptions{Result: "cancelled", Reason: "no longer needed", Decision: "user-decision-4"})
		},
		"trash": func(t *testing.T, root, id string) {
			moveWith(t, root, id, "trash", MoveOptions{Result: "trashed", Reason: "no longer needed", Decision: "user-decision-4"})
		},
		"started at": func(t *testing.T, root, id string) {
			putCardText(t, root, id, func(text string) string { text, _ = setMetadata(text, FieldStartedAt, nowStamp()); return text })
		},
		"execution record": func(t *testing.T, root, id string) {
			s := transactionSnapshot(t, root, id)
			first, err := BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			if err = RollbackDocument(root, first, s.Text, "todo"); err != nil {
				t.Fatal(err)
			}
		},
		"dispatch binding": func(t *testing.T, root, id string) {
			putCardText(t, root, id, func(text string) string { text, _ = setMetadata(text, "DISPATCH_ID", "bound-round"); return text })
		},
		"review runs": func(t *testing.T, root, id string) {
			putCardText(t, root, id, func(text string) string { text, _ = setMetadata(text, "REVIEWS", "reviews/index.json"); return text })
		},
		"review plan": func(t *testing.T, root, id string) {
			plan := ReviewPlan{Schema: 1, PlanID: "plan-removal-test", TaskIDs: []string{id}}
			if err := WithTransaction(root, LockScope{Groups: []string{reviewControlGroup}, ExclusiveBoard: true}, func(tx *Transaction) error {
				if err := tx.PutGroup(reviewControlGroup, planName(plan.PlanID), reviewJSON(plan)); err != nil {
					return err
				}
				return tx.PutGroup(reviewControlGroup, taskPlanName(id), `{"plan_id":"plan-removal-test"}`)
			}); err != nil {
				t.Fatal(err)
			}
		},
		"member exclusion": func(t *testing.T, root, id string) {
			e := ReviewExclusion{Schema: 1, ExclusionID: "exclusion-removal-test", TaskID: id}
			if err := WithTransaction(root, LockScope{Groups: []string{reviewControlGroup}, ExclusiveBoard: true}, func(tx *Transaction) error {
				return tx.PutGroup(reviewControlGroup, exclusionName(id), reviewJSON(e))
			}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, start := range cases {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			ids, c := removeGroup(t, root, "started-a", "started-b")
			start(t, root, ids[0])
			requireRemovalRefused(t, root, c, ids, removeRequest(c, ids[0]), "board.remove_member_started")
		})
	}
}

func TestRemoveMemberRefusesTheLastMember(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "last-a")
	requireRemovalRefused(t, root, c, ids, removeRequest(c, ids[0]), "board.remove_member_last")
}

func TestRemoveMemberUpgradesASchema3Checkpoint(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "old-a", "old-b")
	old := c
	old.Schema = 3
	old.ContentHash = ""
	old.ContentHash = coordinatorHash(old)
	if err := WithTransaction(root, LockScope{Groups: []string{c.GroupID}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if err := tx.PutGroup(c.GroupID, "checkpoints/"+strconv.FormatUint(old.Revision, 10)+".json", reviewJSON(old)); err != nil {
			return err
		}
		return tx.PutGroup(c.GroupID, "checkpoint.json", reviewJSON(old))
	}); err != nil {
		t.Fatal(err)
	}
	read := storedCheckpoint(t, root)
	if read.Schema != 3 {
		t.Fatalf("schema: %d", read.Schema)
	}
	if next := removeMemberOf(t, root, read, ids[0]); next.Schema != 4 || len(next.Removals) != 1 {
		t.Fatalf("%+v", next)
	}
}

func TestRemovalRecordIsImmutable(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "immutable-a", "immutable-b")
	next := removeMemberOf(t, root, c, ids[0])
	path := control(root, "groups", c.GroupID, removalName(next.Removals[0].RemovalID))
	var record CoordinatorRemoval
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &record); err != nil {
		t.Fatal(err)
	}
	record.Reason = "rewritten"
	if err = WithTransaction(root, LockScope{Groups: []string{c.GroupID}, ExclusiveBoard: true}, func(tx *Transaction) error {
		return tx.PutGroup(c.GroupID, removalName(record.RemovalID), reviewJSON(record))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, next), nil); err == nil || !strings.Contains(err.Error(), "removal record changed") {
		t.Fatalf("a rewritten record was accepted: %v", err)
	}
}

// Theorem 1 and 2: a removal raises the revision by one, keeps the epoch,
// forgets the last request, and takes the blocked count of the remaining mirrors.
func TestCoordinatorRemoveMemberRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	ids, c := removeGroup(t, root, "kernel-a", "kernel-b", "kernel-c")
	r := removeRequest(c, ids[0])
	g := newGroupKernel(c, true, len(c.Members))
	ok := removeFacts{known: true, unstarted: true, membership: true, remaining: true, noDependents: true}
	want, taken := g.step(g.removeMember(r, ok))
	if !taken {
		t.Fatal("the kernel refused the removal")
	}
	next := removeMemberOf(t, root, c, ids[0])
	if got := groupState(next); got != want || next.Revision != c.Revision+1 || next.Authority != c.Authority {
		t.Fatalf("%+v want %+v", got, want)
	}
	// Theorem 1: each fact the shell reads is the kernel's refusal when it fails.
	g = newGroupKernel(next, true, len(next.Members))
	for name, facts := range map[string]removeFacts{
		"known":        {unstarted: true, membership: true, remaining: true, noDependents: true},
		"unstarted":    {known: true, membership: true, remaining: true, noDependents: true},
		"membership":   {known: true, unstarted: true, remaining: true, noDependents: true},
		"remaining":    {known: true, unstarted: true, membership: true, noDependents: true},
		"noDependents": {known: true, unstarted: true, membership: true, remaining: true},
		"blocked":      {known: true, unstarted: true, membership: true, remaining: true, noDependents: true, blockedAfter: 1},
	} {
		if _, taken := g.step(g.removeMember(removeRequest(next, ids[1]), facts)); taken {
			t.Fatalf("the kernel took a removal with %s failing", name)
		}
	}
	stale := removeRequest(next, ids[1])
	stale.ExpectedRevision--
	fenced := removeRequest(next, ids[1])
	fenced.ExpectedEpoch++
	for name, r := range map[string]CoordinatorRemoveMember{"stale revision": stale, "fenced epoch": fenced} {
		if _, taken := g.step(g.removeMember(r, ok)); taken {
			t.Fatalf("the kernel took a removal with a %s", name)
		}
	}
	// Theorem 1: no checkpoint, no removal.
	if _, taken := newGroupKernel(CoordinatorCheckpoint{}, false, 1).step(g.removeMember(removeRequest(next, ids[1]), ok)); taken {
		t.Fatal("the kernel removed a member of a group with no checkpoint")
	}
}
