package board

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// staleReclaim writes what a reclaim of an old binary left: a new claim on the
// card with no handoff and no ledger link.
func staleReclaim(t *testing.T, root, id string) {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	text, err := NewClaimMetadata(s.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if e := tx.Put(id, "spec.md", text); e != nil {
			return e
		}
		if s.Entry.State == "working" {
			return nil
		}
		return tx.Relocate(id, "working")
	}); err != nil {
		t.Fatal(err)
	}
}

func recoverRequest(c CoordinatorCheckpoint, id string) CoordinatorRecover {
	return CoordinatorRecover{GroupID: c.GroupID, Member: id, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Decision: "user-decision-1", Reason: "the mirror cannot follow this member"}
}

func recoverMember(t *testing.T, root string, c CoordinatorCheckpoint, id string) CoordinatorCheckpoint {
	t.Helper()
	next, err := RecoverCoordinator(context.Background(), root, recoverRequest(c, id), transactionRevision(t, root, id))
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// blockedGroup is a two-member group whose first member the coordinator cannot
// follow any more.
func blockedGroup(t *testing.T, root string) (ids []string, c CoordinatorCheckpoint) {
	t.Helper()
	ids = []string{recoveryMember(t, root, "blocked-a"), recoveryMember(t, root, "blocked-b")}
	c = coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	staleReclaim(t, root, ids[0])
	return ids, coordinatorReconcile(t, root, c)
}

func TestReconcileJudgesEachMemberAlone(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "alone-a"), recoveryMember(t, root, "alone-b")}
	before := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	staleReclaim(t, root, ids[0])
	moveMember(t, root, ids[1], "review")
	c, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, before), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := c.Members[ids[0]], c.Members[ids[1]]
	if a.Blocked == nil || !strings.Contains(a.Blocked.Reason, "execution cycle changed without lifecycle handoff") {
		t.Fatalf("no reason: %+v", a)
	}
	if a.Revision != before.Members[ids[0]].Revision || a.Cycle != before.Members[ids[0]].Cycle {
		t.Fatalf("a blocked mirror moved: %+v", a)
	}
	if b.Blocked != nil || b.State != "review" {
		t.Fatalf("the failure stopped the other member: %+v", b)
	}
	if got := c.BlockedMembers(); !slices.Equal(got, ids[:1]) {
		t.Fatalf("blocked: %v", got)
	}
	if stored, e := ReadCoordinatorCheckpoint(root, c.GroupID); e != nil || stored.Members[ids[0]].Blocked == nil {
		t.Fatalf("the reason is not in the checkpoint: %+v %v", stored, e)
	}
}

func TestReconcileClearsABlockThatEnded(t *testing.T) {
	root := tempBoard(t)
	id := recoveryMember(t, root, "transient")
	other := recoveryMember(t, root, "transient-other")
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, id, other))
	r := coordinatorRequest(t, root, c)
	o := r.Members[id]
	o.DispatchID, o.Epoch = "stale-round", 1
	r.Members[id] = o
	blocked, err := ReconcileCoordinator(context.Background(), root, r, nil)
	if err != nil || blocked.Members[id].Blocked == nil {
		t.Fatalf("%+v %v", blocked.Members[id], err)
	}
	if next := coordinatorReconcile(t, root, blocked); next.Members[id].Blocked != nil {
		t.Fatalf("a correct observation kept the block: %+v", next.Members[id])
	}
}

func TestReconcileReportsEveryMemberBlocked(t *testing.T) {
	root := tempBoard(t)
	id := recoveryMember(t, root, "only")
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, id))
	staleReclaim(t, root, id)
	next, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil)
	var blocked *CoordinatorBlockedError
	if !errors.As(err, &blocked) || next.Members[id].Blocked == nil {
		t.Fatalf("%v %+v", err, next.Members[id])
	}
	if stored, e := ReadCoordinatorCheckpoint(root, c.GroupID); e != nil || stored.Revision != c.Revision+1 {
		t.Fatalf("the reason was not published: %+v %v", stored, e)
	}
}

func TestReconcileStaleRevisionFailsTheWholeRequest(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "stale-a"), recoveryMember(t, root, "stale-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	r := coordinatorRequest(t, root, c)
	moveMember(t, root, ids[0], "review")
	if _, err := ReconcileCoordinator(context.Background(), root, r, nil); err == nil || !strings.Contains(err.Error(), ids[0]) {
		t.Fatalf("a stale observation read as a verdict: %v", err)
	}
	if stored, e := ReadCoordinatorCheckpoint(root, c.GroupID); e != nil || stored.Revision != c.Revision {
		t.Fatalf("%+v %v", stored, e)
	}
}

func TestBlockedMemberRefusesWrapUp(t *testing.T) {
	root := tempBoard(t)
	a := coordinatorCard(t, root, "wrap-a").Entry.TaskID
	b := coordinatorCard(t, root, "wrap-b").Entry.TaskID
	ids := []string{a, b}
	c := coordinatorClaim(t, root, ids...)
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	prepared := prepareTestDispatch(t, root, coordinatorWrapInput(t, root, b, "wrap-prepared"))
	r := coordinatorRequest(t, root, c)
	o := r.Members[a]
	o.DispatchID, o.Epoch = "stale-round", 1
	r.Members[a] = o
	verify := func(context.Context, CoordinatorGitFacts) error { return nil }
	if c, _ = ReconcileCoordinator(context.Background(), root, r, verify); c.Members[a].Blocked == nil {
		t.Fatalf("no block: %+v", c.Members[a])
	}
	refused := func(err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), a) {
			t.Fatalf("wrap-up passed a blocked member: %v", err)
		}
	}
	_, err := PrepareDispatch(root, coordinatorWrapInput(t, root, a, "wrap-new"))
	refused(err)
	_, err = PrepareDispatch(root, prepared.Input)
	refused(err)
	_, err = BeginDispatchAttempt(root, b, prepared.Input.ID, prepared.Revision)
	refused(err)
	s := transactionSnapshot(t, root, b)
	_, err = AuthorizeDispatchWrapUp(root, b, prepared.Input.ID, prepared.Revision, "coordinator", "reason", modelStopped(s))
	refused(err)
	// A sync round is no part of ending the group.
	if _, err = PrepareDispatch(root, dispatchInput(transactionSnapshot(t, root, a), "sync-still-works")); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverRefusals(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	revision := transactionRevision(t, root, ids[0])
	for name, change := range map[string]func(*CoordinatorRecover){
		"group":    func(r *CoordinatorRecover) { r.GroupID = "" },
		"member":   func(r *CoordinatorRecover) { r.Member = "" },
		"revision": func(r *CoordinatorRecover) { r.ExpectedRevision = 0 },
		"epoch":    func(r *CoordinatorRecover) { r.ExpectedEpoch = 0 },
		"decision": func(r *CoordinatorRecover) { r.Decision = "  " },
		"reason":   func(r *CoordinatorRecover) { r.Reason = "" },
		"stale":    func(r *CoordinatorRecover) { r.ExpectedRevision-- },
		"fenced":   func(r *CoordinatorRecover) { r.ExpectedEpoch++ },
		"unknown":  func(r *CoordinatorRecover) { r.Member = "20200101-unknown-task" },
	} {
		t.Run(name, func(t *testing.T) {
			r := recoverRequest(c, ids[0])
			change(&r)
			if _, err := RecoverCoordinator(context.Background(), root, r, revision); err == nil {
				t.Fatal("refusal missing")
			}
		})
	}
	if _, err := RecoverCoordinator(context.Background(), root, recoverRequest(c, ids[1]), transactionRevision(t, root, ids[1])); err == nil || !strings.Contains(err.Error(), ids[1]) {
		t.Fatalf("recovered a member that is not blocked: %v", err)
	}
	if _, err := RecoverCoordinator(context.Background(), root, recoverRequest(c, ids[0]), revision-1); err == nil {
		t.Fatal("recovered a card the probe did not see")
	}
	if stored, err := ReadCoordinatorCheckpoint(root, c.GroupID); err != nil || stored.Revision != c.Revision || len(stored.Recoveries) != 0 {
		t.Fatalf("%+v %v", stored, err)
	}
}

func TestRecoverRefusesBacklogAndTodo(t *testing.T) {
	for _, state := range []string{"backlog", "todo"} {
		t.Run(state, func(t *testing.T) {
			root := tempBoard(t)
			id := coordinatorTodo(t, root, "not-started").Entry.TaskID
			other := recoveryMember(t, root, "other-started")
			c := coordinatorReconcile(t, root, coordinatorClaim(t, root, id, other))
			if state == "backlog" {
				moveMember(t, root, id, "backlog")
			}
			r := coordinatorRequest(t, root, c)
			o := r.Members[id]
			o.DispatchID, o.Epoch = "stale-round", 1
			r.Members[id] = o
			c, err := ReconcileCoordinator(context.Background(), root, r, nil)
			if err != nil || c.Members[id].Blocked == nil {
				t.Fatalf("%+v %v", c.Members[id], err)
			}
			if _, err = RecoverCoordinator(context.Background(), root, recoverRequest(c, id), transactionRevision(t, root, id)); err == nil || !strings.Contains(err.Error(), state) {
				t.Fatalf("recovered a member with no execution: %v", err)
			}
		})
	}
}

func TestRecoverResetRefusesABoundMember(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	moveMember(t, root, ids[0], "review")
	d := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, ids[0]), "bound-round"))
	if _, err := RecoverCoordinator(context.Background(), root, recoverRequest(c, ids[0]), transactionRevision(t, root, ids[0])); err == nil || !strings.Contains(err.Error(), d.Input.ID) {
		t.Fatalf("reset passed a binding: %v", err)
	}
}

func TestRecoverConcurrentRequestsOneWins(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	revision := transactionRevision(t, root, ids[0])
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = RecoverCoordinator(context.Background(), root, recoverRequest(c, ids[0]), revision)
		})
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d recoveries won: %v", won, errs)
	}
	if stored, err := ReadCoordinatorCheckpoint(root, c.GroupID); err != nil || len(stored.Recoveries) != 1 {
		t.Fatalf("%+v %v", stored, err)
	}
}

func TestRecoverResetStartsANewCycle(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	id := ids[0]
	moveMember(t, root, id, "review")
	before := transactionSnapshot(t, root, id)
	if c = coordinatorRequestAfter(t, root, c); c.Members[id].Blocked == nil {
		t.Fatal("the fixture stopped blocking")
	}
	next := recoverMember(t, root, c, id)
	s := transactionSnapshot(t, root, id)
	if s.Entry.State != "working" || planCycle(s) == planCycle(before) {
		t.Fatalf("no new cycle: %s", s.Entry.State)
	}
	view, err := readExecutionView(context.Background(), root, id)
	if err != nil || view.Record.Kind != ExecutionRecover || !view.Record.SelfClaimed() || view.Record.Status != ExecutionRunning {
		t.Fatalf("%+v %v", view, err)
	}
	m := next.Members[id]
	record := next.Recoveries[0]
	if m.Blocked != nil || m.Cycle != planCycle(s) || m.Revision != s.Revision || m.DeliveryCommit != "" || m.Dispatch != nil || m.Review != nil {
		t.Fatalf("mirror not rebound: %+v", m)
	}
	if record.Mode != RecoverReset || record.Decision != "user-decision-1" || record.CheckpointRevision != c.Revision || record.CoordinatorEpoch != c.Authority.Epoch || record.ToCycle != m.Cycle || record.TransitionSeq != m.TransitionSeq || record.BlockedReason == "" {
		t.Fatalf("%+v", record)
	}
	var link ExecutionTransition
	if err = WithTransaction(root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		l, e := readExecutionLedger(tx, s)
		if e == nil {
			link, _ = l.tail()
			e = l.assertCard(s)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if link.Kind != TransitionRecover || link.Decision != "user-decision-1" {
		t.Fatalf("%+v", link)
	}
	if after := coordinatorReconcile(t, root, next); len(after.BlockedMembers()) != 0 {
		t.Fatalf("the reset did not end the block: %+v", after.Members[id])
	}
}

// coordinatorRequestAfter reconciles the current board from the stored checkpoint.
func coordinatorRequestAfter(t *testing.T, root string, c CoordinatorCheckpoint) CoordinatorCheckpoint {
	t.Helper()
	stored, err := ReadCoordinatorCheckpoint(root, c.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	return coordinatorReconcile(t, root, stored)
}

func TestRecoverRecordStaysOutOfUpdateAndCheckpointsMatchIt(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	next := recoverMember(t, root, c, ids[0])
	s := transactionSnapshot(t, root, ids[0])
	for _, path := range []string{"../../.kander/groups/" + c.GroupID + "/recoveries/x.json", ".kander/groups/" + c.GroupID + "/recoveries/x.json"} {
		if err := UpdateDocument(root, ids[0], UpdateOptions{Document: path, Text: "{}", ExpectedRevision: s.Revision}); err == nil {
			t.Fatalf("update reached %s", path)
		}
	}
	path := control(root, "groups", c.GroupID, recoveryName(next.Recoveries[0].RecoveryID))
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(strings.Replace(string(saved), "user-decision-1", "forged-decision", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, next), nil); err == nil || !strings.Contains(err.Error(), "recovery record changed") {
		t.Fatalf("a changed record was accepted: %v", err)
	}
}

func TestOldPlainReclaimCheckpointRecovers(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	if !strings.Contains(c.Members[ids[0]].Blocked.Reason, "without lifecycle handoff") {
		t.Fatalf("%+v", c.Members[ids[0]])
	}
	// The old cursor had no chain position.
	if err := WithTransaction(root, LockScope{Groups: []string{c.GroupID}}, func(tx *Transaction) error {
		stored, _, e := readCheckpoint(tx, c.GroupID)
		if e != nil {
			return e
		}
		m := stored.Members[ids[0]]
		m.TransitionSeq, m.DispatchSeq = 0, 0
		stored.Members[ids[0]], stored.Schema = m, 1
		stored.Revision++
		return putCheckpoint(tx, &stored)
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadCoordinatorCheckpoint(root, c.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	next := recoverMember(t, root, stored, ids[0])
	if next.Schema != checkpointSchema {
		t.Fatalf("schema %d", next.Schema)
	}
	if after := coordinatorReconcile(t, root, next); len(after.BlockedMembers()) != 0 {
		t.Fatalf("%+v", after.Members[ids[0]])
	}
}

// recoveredPlan builds a group with a closed required batch and resets its first
// member.
func recoveredPlan(t *testing.T, root string) (ids []string, p ReviewPlan) {
	t.Helper()
	ids = []string{recoveryMember(t, root, "plan-a"), recoveryMember(t, root, "plan-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	gatePlan(t, root, ids, archiveRequirements())
	run := gateRun(t, root, archiveInput(ids, "plan-pmqa", "PMQA"), emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	staleReclaim(t, root, ids[0])
	recoverMember(t, root, coordinatorRequestAfter(t, root, c), ids[0])
	return ids, deadEndRebind(t, root, ids[0])
}

func appendBatch(t *testing.T, root string, p ReviewPlan, id, batch string, requirements map[string]string) ReviewPlan {
	t.Helper()
	last := p.Batches[len(p.Batches)-1]
	b := ReviewPlanBatch{BatchID: batch, PreviousBatchID: last.BatchID, TaskIDs: []string{id}, Base: last.TargetCommit, TargetCommit: strings.Repeat(string(rune('c'+len(p.Batches))), 40), Requirements: requirements}
	if err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Batch: &b, Author: "coordinator", Basis: "new cycle"}); err != nil {
		t.Fatal(err)
	}
	next, err := ReadReviewPlan(root, p.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func closeActualBatch(t *testing.T, root string, p ReviewPlan, id, batch string) {
	t.Helper()
	b := p.Batches[len(p.Batches)-1]
	in := archiveInput([]string{id}, batch+"-run", "PMQA")
	in.BatchID, in.Base, in.Commit = batch, b.Base, b.TargetCommit
	run := gateRun(t, root, in, emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := deadEndClose(t, root, batch, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveredMemberNeedsAnActualReview(t *testing.T) {
	root := tempBoard(t)
	ids, p := recoveredPlan(t, root)
	if err := deadEndDone(t, root, ids[0]); err == nil {
		t.Fatal("the old cycle's evidence passed the gate")
	}
	p = appendBatch(t, root, p, ids[0], "cycle-na", noReviewRequirements())
	if _, err := deadEndClose(t, root, "cycle-na", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	err := deadEndDone(t, root, ids[0])
	if err == nil || !strings.Contains(err.Error(), "recovery-review:"+ids[0]) {
		t.Fatalf("an N/A batch laundered the recovery: %v", err)
	}
	progress, err := ReviewTaskProgress(root, ids[0])
	if err != nil || !slices.ContainsFunc(progress.NextActions, func(a ReviewNextAction) bool { return a.Action == "append-cycle-batch" && a.TaskID == ids[0] }) {
		t.Fatalf("%+v %v", progress, err)
	}
	// The sealed plan takes the batch the recovery owes: this is no dead end.
	p = appendBatch(t, root, p, ids[0], "cycle-real", archiveRequirements())
	closeActualBatch(t, root, p, ids[0], "cycle-real")
	if err = deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
}

func TestReclaimAfterRecoveryKeepsTheReviewOwed(t *testing.T) {
	root := tempBoard(t)
	ids, p := recoveredPlan(t, root)
	deadEndReclaim(t, root, ids[0])
	p = deadEndRebind(t, root, ids[0])
	appendBatch(t, root, p, ids[0], "cycle-na", noReviewRequirements())
	if _, err := deadEndClose(t, root, "cycle-na", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[0]); err == nil || !strings.Contains(err.Error(), "recovery-review:"+ids[0]) {
		t.Fatalf("recover, reclaim, N/A passed the gate: %v", err)
	}
}

func TestReviewOwedAfterRecoveryEndsWithTheActualBatch(t *testing.T) {
	root := tempBoard(t)
	ids, p := recoveredPlan(t, root)
	p = appendBatch(t, root, p, ids[0], "cycle-real", archiveRequirements())
	closeActualBatch(t, root, p, ids[0], "cycle-real")
	deadEndReclaim(t, root, ids[0])
	p = deadEndRebind(t, root, ids[0])
	appendBatch(t, root, p, ids[0], "cycle-na", noReviewRequirements())
	if _, err := deadEndClose(t, root, "cycle-na", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[0]); err != nil {
		t.Fatalf("a reclaim after the actual review owes only the Q8 batch: %v", err)
	}
}

func TestRoleOutsideThePlanUnionMayStayNA(t *testing.T) {
	if got := planRequiredRoles(ReviewPlan{Batches: []ReviewPlanBatch{{Requirements: noReviewRequirements()}, {Requirements: archiveRequirements()}, {Requirements: map[string]string{"PM": "required", "QA": "N/A: x", "CSA": "N/A: x", "Hacker": "N/A: x"}}}}); !slices.Equal(got, []string{"PMQA"}) {
		t.Fatalf("union %v", got)
	}
}

func TestRecoverConfirmLeavesTheCardAlone(t *testing.T) {
	for _, state := range []string{"done", "archived", "trash"} {
		t.Run(state, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{recoveryMember(t, root, "confirm-a"), recoveryMember(t, root, "confirm-b")}
			c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
			gatePlan(t, root, ids, noReviewRequirements())
			if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
				t.Fatal(err)
			}
			// A change no record carries makes the finished member unreadable for
			// the mirror; the terminal states reach this after the same failure.
			staleReclaim(t, root, ids[0])
			if err := WithTransaction(root, LockScope{Tasks: []string{ids[0]}, Groups: []string{reviewControlGroup}, ExclusiveBoard: true}, func(tx *Transaction) error {
				return tx.Relocate(ids[0], "done")
			}); err != nil {
				t.Fatal(err)
			}
			if state != "done" {
				deadEndExit(t, root, ids[0], state)
			}
			c = coordinatorRequestAfter(t, root, c)
			if c.Members[ids[0]].Blocked == nil {
				t.Fatalf("%s reads without a block", state)
			}
			before := transactionSnapshot(t, root, ids[0])
			view, err := readExecutionView(context.Background(), root, ids[0])
			if err != nil {
				t.Fatal(err)
			}
			next := recoverMember(t, root, c, ids[0])
			after := transactionSnapshot(t, root, ids[0])
			if after.Text != before.Text || after.Revision != before.Revision || planCycle(after) != planCycle(before) {
				t.Fatal("confirmation wrote the card")
			}
			if view2, e := readExecutionView(context.Background(), root, ids[0]); e != nil || view2 != view {
				t.Fatalf("confirmation wrote the ledger: %+v %v", view2, e)
			}
			r := next.Recoveries[0]
			if r.Mode != RecoverConfirm || r.FromCycle != r.ToCycle || r.FromState != state || next.Members[ids[0]].Confirmed == nil {
				t.Fatalf("%+v", r)
			}
			if final := coordinatorReconcile(t, root, next); len(final.BlockedMembers()) != 0 {
				t.Fatalf("%+v", final.Members[ids[0]])
			}
		})
	}
}

func TestConfirmationSurvivesAnUnreadableLedger(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "damage-a"), recoveryMember(t, root, "damage-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(control(root, "groups", taskStartGroup, ids[0], "current.json")); err != nil {
		t.Fatal(err)
	}
	c = coordinatorRequestAfter(t, root, c)
	if c.Members[ids[0]].Blocked == nil {
		t.Fatalf("%+v", c.Members[ids[0]])
	}
	next := recoverMember(t, root, c, ids[0])
	if final := coordinatorReconcile(t, root, next); len(final.BlockedMembers()) != 0 {
		t.Fatalf("%+v", final.Members[ids[0]])
	}
}

func TestWrapUpEvidenceRecordsOnceAndStopsGit(t *testing.T) {
	root := tempBoard(t)
	a := coordinatorCard(t, root, "evidence-a").Entry.TaskID
	b := coordinatorCard(t, root, "evidence-b").Entry.TaskID
	ids := []string{a, b}
	c := coordinatorClaim(t, root, ids...)
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	d := prepareTestDispatch(t, root, coordinatorWrapInput(t, root, a, "wrap-once"))
	calls := 0
	verify := func(context.Context, CoordinatorGitFacts) error { calls++; return nil }
	reconcile := func() {
		t.Helper()
		var err error
		if c, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify); err != nil {
			t.Fatal(err)
		}
		if len(c.BlockedMembers()) != 0 {
			t.Fatalf("%+v", c.Members)
		}
	}
	reconcile()
	for _, state := range []string{"working", "done"} {
		if _, err := dispatchMove(t, root, d, state); err != nil {
			t.Fatal(err)
		}
	}
	calls = 0
	reconcile()
	if calls != 1 {
		t.Fatalf("an old wrap-up verifies once, got %d", calls)
	}
	path := control(root, "groups", coordinatorGroup, wrapUpEvidenceName(d.Input.ID))
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e WrapUpGitEvidence
	if err = json.Unmarshal(stored, &e); err != nil || e.VerifiedBy != wrapUpVerifiedByReconcile || e.DispatchID != d.Input.ID {
		t.Fatalf("%+v %v", e, err)
	}
	verify = func(context.Context, CoordinatorGitFacts) error { t.Fatal("Git ran again"); return nil }
	reconcile()
	if err = os.WriteFile(path, []byte(strings.Replace(string(stored), d.Input.ID, "wrap-other", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	broken, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify)
	if err != nil || broken.Members[a].Blocked == nil {
		t.Fatalf("evidence of another dispatch passed: %+v %v", broken.Members[a], err)
	}
}

func TestRejectedWrapUpGitBlocksOnlyThatMemberUntilConfirmed(t *testing.T) {
	root := tempBoard(t)
	a := coordinatorCard(t, root, "rejected-a").Entry.TaskID
	b := coordinatorCard(t, root, "rejected-b").Entry.TaskID
	ids := []string{a, b}
	c := coordinatorClaim(t, root, ids...)
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	d := prepareTestDispatch(t, root, coordinatorWrapInput(t, root, a, "wrap-rejected"))
	for _, state := range []string{"working", "done"} {
		if _, err := dispatchMove(t, root, d, state); err != nil {
			t.Fatal(err)
		}
	}
	rejected := errors.New("integration left develop")
	c, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), func(context.Context, CoordinatorGitFacts) error { return rejected })
	if err != nil || c.Members[a].Blocked == nil || !strings.Contains(c.Members[a].Blocked.Reason, rejected.Error()) || c.Members[b].Blocked != nil {
		t.Fatalf("%+v %v", maps.Collect(maps.All(c.Members)), err)
	}
	next := recoverMember(t, root, c, a)
	final, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, next), func(context.Context, CoordinatorGitFacts) error { return rejected })
	if err != nil || len(final.BlockedMembers()) != 0 {
		t.Fatalf("%+v %v", final.Members[a], err)
	}
}

func TestConfirmationCoversOneCycle(t *testing.T) {
	s := Snapshot{Entry: Entry{TaskID: "x", State: "done"}, Text: "- STARTED_AT: now\n"}
	m := CoordinatorMember{Confirmed: &CoordinatorConfirmation{RecoveryID: "r", Cycle: planCycle(s)}}
	if !memberConfirmed(s, m) {
		t.Fatal("confirmed cycle not accepted")
	}
	m.Confirmed.Cycle = "other"
	if memberConfirmed(s, m) {
		t.Fatal("another cycle was accepted")
	}
	s.Entry.State = "working"
	m.Confirmed.Cycle = planCycle(s)
	if memberConfirmed(s, m) {
		t.Fatal("a live member was accepted")
	}
}

func TestRecoveredMemberReachesDone(t *testing.T) {
	for _, state := range []string{"working", "review"} {
		t.Run(state, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{recoveryMember(t, root, "finish-a"), recoveryMember(t, root, "finish-b")}
			id := ids[0]
			c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
			gatePlan(t, root, ids, archiveRequirements())
			run := gateRun(t, root, archiveInput(ids, "finish-pmqa", "PMQA"), emptyFindings())
			assignGate(t, root, run, map[string][]string{})
			if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
				t.Fatal(err)
			}
			staleReclaim(t, root, id)
			if state == "review" {
				moveMember(t, root, id, "review")
			}
			c = coordinatorRequestAfter(t, root, c)
			if got := transactionSnapshot(t, root, id).Entry.State; got != state || c.Members[id].Blocked == nil {
				t.Fatalf("%s %+v", got, c.Members[id])
			}
			c = recoverMember(t, root, c, id)
			s := transactionSnapshot(t, root, id)
			if _, err := BindExecutionCursor(root, s.Entry); err != nil {
				t.Fatal(err)
			}
			if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude new-session"}); err != nil {
				t.Fatal(err)
			}
			if err := ConfirmTaskStart(root, s.Entry); err != nil {
				t.Fatal(err)
			}
			p := deadEndRebind(t, root, id)
			p = appendBatch(t, root, p, id, "cycle-real", archiveRequirements())
			closeActualBatch(t, root, p, id, "cycle-real")
			if err := deadEndDone(t, root, id); err != nil {
				t.Fatal(err)
			}
			if err := deadEndDone(t, root, ids[1]); err != nil {
				t.Fatal(err)
			}
			if c = coordinatorRequestAfter(t, root, c); len(c.BlockedMembers()) != 0 {
				t.Fatalf("%+v", c.Members)
			}
		})
	}
}
