package board

import (
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/statemachine/reviewobligation"
	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

var (
	pmqaRoles = reviewobligation.RoleSet(0).With(reviewobligation.RolePmqa)
	noRoles   = reviewobligation.RoleSet(0)
)

// The plan kernel events of an accepted extension: its shape, the admission,
// and the operation.
func extendSteps(shape reviewplan.Extend, admit reviewplan.Admit, op reviewplan.Event) []reviewplan.Event {
	return []reviewplan.Event{shape, admit, op}
}

func closeFacts(at uint64) reviewplan.Close {
	return reviewplan.Close{At: at, Revision: true, ViewHash: true, Conclusions: true, Git: true, Current: true, PreviousClosed: true, Bound: true}
}

func appendFacts(seal, owed bool) reviewplan.Append {
	return reviewplan.Append{Seal: seal, Owed: owed, Cas: true, Binding: true, MembersActive: true, PreviousClosed: true, Requirements: true, AllAssigned: true, NoDropOwed: true}
}

func bothMembers(ids []string, e ...reviewobligation.Event) map[string][]reviewobligation.Event {
	m := map[string][]reviewobligation.Event{}
	for _, id := range ids {
		m[id] = e
	}
	return m
}

// Every plan and batch entrance of a two-member plan refines the kernels, from
// creation through a drop, a release, an append, a seal, an advance, closes, and
// both completions; each refused entrance leaves every projection as it was.
func TestReviewEntrancesRefineKernels(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "entrance-a"), gateCard(t, root, "entrance-b")}
	slices.Sort(ids)
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	p := ReviewPlan{Schema: 1, PlanID: "plan-entrance", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{
		{BatchID: "batch", TaskIDs: ids, Base: a, TargetCommit: b, Requirements: archiveRequirements()},
		{BatchID: "second", PreviousBatchID: "batch", TaskIDs: ids, Base: b, TargetCommit: c, Requirements: archiveRequirements()},
	}}
	refines(t, root, ids, kernelSteps{
		members: bothMembers(ids, reviewobligation.Create{}, reviewobligation.AddBatch{Member: true, Roles: pmqaRoles}, reviewobligation.AddBatch{Member: true, Roles: pmqaRoles}),
		plan:    []reviewplan.Event{reviewplan.Create{Batches: 2, Seal: false, CoversAll: true, Chain: true, NoUnplannedRuns: true, MembersFree: true}},
	}, false, func() error { return CreateReviewPlan(root, p) })

	var run ReviewRun
	refines(t, root, ids, kernelSteps{plan: []reviewplan.Event{reviewplan.Run{At: 0}}}, false, func() error {
		run = gateRun(t, root, archiveInput(ids, "first-run", "PMQA"), emptyFindings())
		assignGate(t, root, run, map[string][]string{})
		return nil
	})
	refines(t, root, ids, kernelSteps{}, true, func() error {
		v, err := ReadReviewBatchView(root, "batch")
		if err != nil {
			return err
		}
		r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: "stale", Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PMQA": passRole(run)}}
		_, err = CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: b})
		return err
	})
	refines(t, root, ids, kernelSteps{
		members: bothMembers(ids, reviewobligation.Close{At: 0, Current: pmqaRoles}),
		plan:    []reviewplan.Event{reviewplan.Aggregate{At: 0, Published: true, Lineage: true, Ledger: true}, closeFacts(0)},
	}, false, func() error {
		_, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil)
		return err
	})
	p = readOwedPlan(t, root, p.PlanID)

	refines(t, root, ids, kernelSteps{}, true, func() error {
		x := ReviewPlanExtension{DropBatches: []string{"second"}}
		x.PlanID, x.ExpectedRevision, x.Author, x.Basis = p.PlanID, p.Revision+1, "coordinator", "stale"
		return ExtendReviewPlan(root, x)
	})
	refines(t, root, ids, kernelSteps{
		members: bothMembers(ids, reviewobligation.Drop{Keep: 1}),
		plan:    extendSteps(reviewplan.Extend{Drop: true}, reviewplan.Admit{Cas: true, OtherOp: true}, reviewplan.Drop{N: 1, Cas: true, Trailing: true}),
	}, false, func() error { return tryExtendOwed(root, p, ReviewPlanExtension{DropBatches: []string{"second"}}) })
	p = readOwedPlan(t, root, p.PlanID)

	refines(t, root, ids, kernelSteps{}, true, func() error {
		return tryExtendOwed(root, p, ReviewPlanExtension{Seal: true})
	})
	refines(t, root, ids, kernelSteps{
		members: map[string][]reviewobligation.Event{ids[0]: {reviewobligation.ReleaseDrop{}}},
		plan:    extendSteps(reviewplan.Extend{Release: true, Decision: true}, reviewplan.Admit{Cas: true, OtherOp: true}, reviewplan.Release{Cas: true, Decision: true, Members: true, Accepted: true}),
	}, false, func() error {
		return tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseDropped, TaskIDs: ids[:1]}, Decision: "user-decision"})
	})
	p = readOwedPlan(t, root, p.PlanID)
	refines(t, root, ids, kernelSteps{}, true, func() error {
		return tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: ids[:1]}, Decision: "user-decision"})
	})

	third := owedBatch(p, "third", archiveRequirements(), ids[1])
	refines(t, root, ids, kernelSteps{
		members: map[string][]reviewobligation.Event{
			ids[0]: {reviewobligation.AddBatch{Member: false, Roles: pmqaRoles}},
			ids[1]: {reviewobligation.AddBatch{Member: true, Roles: pmqaRoles}},
		},
		plan: extendSteps(reviewplan.Extend{Batch: true}, reviewplan.Admit{Cas: true, BatchOnly: true}, appendFacts(false, false)),
	}, false, func() error { return tryExtendOwed(root, p, ReviewPlanExtension{Batch: &third}) })
	p = readOwedPlan(t, root, p.PlanID)

	refines(t, root, ids, kernelSteps{
		plan: extendSteps(reviewplan.Extend{Batch: true}, reviewplan.Admit{Cas: true}, reviewplan.Seal{Cas: true, AllAssigned: true, NoDropOwed: true}),
	}, false, func() error { return tryExtendOwed(root, p, ReviewPlanExtension{Seal: true}) })
	p = readOwedPlan(t, root, p.PlanID)
	refines(t, root, ids, kernelSteps{}, true, func() error { return tryExtendOwed(root, p, ReviewPlanExtension{Seal: true}) })
	extra := owedBatch(p, "extra", archiveRequirements(), ids[1])
	refines(t, root, ids, kernelSteps{}, true, func() error { return tryExtendOwed(root, p, ReviewPlanExtension{Batch: &extra}) })

	fixup := strings.Repeat("e1", 20)
	advance := ReviewAdvance{PreviousTarget: third.TargetCommit, Target: fixup, Reason: "fixup", Deliveries: map[string]string{fixup: ids[1]}}
	refines(t, root, ids, kernelSteps{}, true, func() error {
		return AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "third", ExpectedRevision: 2, Advance: advance})
	})
	refines(t, root, ids, kernelSteps{
		plan: []reviewplan.Event{reviewplan.Advance{At: 1, Revision: true, Target: true, NoSuccessor: true, Attribution: true, Settled: true, Dispositions: true}},
	}, false, func() error {
		return AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "third", ExpectedRevision: 1, Advance: advance})
	})
	p = readOwedPlan(t, root, p.PlanID)
	third = p.Batches[1]

	refines(t, root, ids, kernelSteps{}, true, func() error { return deadEndDone(t, root, ids[0]) })
	refines(t, root, ids, kernelSteps{
		members: map[string][]reviewobligation.Event{
			// A conclusion run binds the cycles of its batch's members only.
			ids[0]: {reviewobligation.Close{At: 1, Current: noRoles}},
			ids[1]: {reviewobligation.Close{At: 1, Current: pmqaRoles}},
		},
		plan:    []reviewplan.Event{reviewplan.Run{At: 1}, reviewplan.Aggregate{At: 1, Published: true, Lineage: true, Ledger: true}, closeFacts(1)},
	}, false, func() error {
		in := archiveInput(third.TaskIDs, "third-run", "PMQA")
		in.BatchID, in.Base, in.Commit = third.BatchID, third.Base, third.TargetCommit
		r := gateRun(t, root, in, emptyFindings())
		assignGate(t, root, r, map[string][]string{})
		_, err := deadEndClose(t, root, "third", map[string]ReviewRoleConclusion{"PMQA": passRole(r)}, nil)
		return err
	})
	// The third batch discharges the second member's drop obligation.
	for _, id := range ids {
		refines(t, root, ids, kernelSteps{members: map[string][]reviewobligation.Event{id: {reviewobligation.Done{PlanClear: true}}}}, false, func() error {
			return deadEndDone(t, root, id)
		})
	}
}

// A reset with no plan, a reclaim, and a rebind refine the member kernel.
func TestCycleEntrancesRefineKernels(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	refines(t, root, ids[:1], kernelSteps{members: map[string][]reviewobligation.Event{ids[0]: {reviewobligation.Reset{}}}}, false, func() error {
		recoverMember(t, root, c, ids[0])
		return nil
	})
	p := ReviewPlan{Schema: 1, PlanID: "plan-cycle", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids[:1], Batches: []ReviewPlanBatch{
		{BatchID: "batch", TaskIDs: ids[:1], Base: "N/A", TargetCommit: "N/A", Requirements: noReviewRequirements()},
	}}
	refines(t, root, ids[:1], kernelSteps{
		members: bothMembers(ids[:1], reviewobligation.Create{}, reviewobligation.AddBatch{Member: true, Roles: noRoles}),
		plan:    []reviewplan.Event{reviewplan.Create{Batches: 1, CoversAll: true, Chain: true, NoUnplannedRuns: true, MembersFree: true}},
	}, false, func() error { return CreateReviewPlan(root, p) })
	refines(t, root, ids[:1], kernelSteps{members: bothMembers(ids[:1], reviewobligation.NewCycle{})}, false, func() error {
		deadEndReclaim(t, root, ids[0])
		return nil
	})
	refines(t, root, ids[:1], kernelSteps{
		members: bothMembers(ids[:1], reviewobligation.Rebind{}),
		plan:    extendSteps(reviewplan.Extend{Rebind: true}, reviewplan.Admit{Cas: true, OtherOp: true}, reviewplan.Rebind{Cas: true, Exact: true, MembersActive: true}),
	}, false, func() error {
		deadEndRebind(t, root, ids[0])
		return nil
	})
}

// The two traces of the released no-plan recovery (PROVENANCE.md of the replaced
// experiment) through the public entrances: each step refines the kernels, and
// both the production gate and the kernel refuse the completion.
func TestReleasedRecoveryRefinesKernels(t *testing.T) {
	for _, drop := range []string{"dropReleased", "releaseDrop"} {
		t.Run(drop, func(t *testing.T) {
			root := tempBoard(t)
			id := recoveredWithoutPlan(t, root)
			ids := []string{id}
			p := ReviewPlan{Schema: 1, PlanID: "refinement-plan", Author: "fixture", Basis: "released recovery correspondence", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{
				{BatchID: "batch", TaskIDs: ids, Base: owedCommit(0), TargetCommit: owedCommit(1), Requirements: noReviewRequirements()},
			}}
			refines(t, root, ids, kernelSteps{
				members: bothMembers(ids, reviewobligation.Create{}, reviewobligation.AddBatch{Member: true, Roles: noRoles}),
				plan:    []reviewplan.Event{reviewplan.Create{Batches: 1, CoversAll: true, Chain: true, NoUnplannedRuns: true, MembersFree: true}},
			}, false, func() error { return CreateReviewPlan(root, p) })
			refines(t, root, ids, kernelSteps{
				members: bothMembers(ids, reviewobligation.Close{At: 0, Current: noRoles}),
				plan:    []reviewplan.Event{reviewplan.Aggregate{At: 0, Published: true, Lineage: true, Ledger: true}, closeFacts(0)},
			}, false, func() error {
				_, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{}, nil)
				return err
			})
			p = readOwedPlan(t, root, p.PlanID)
			refines(t, root, ids, kernelSteps{
				members: bothMembers(ids, reviewobligation.ReleaseRecovery{}),
				plan:    extendSteps(reviewplan.Extend{Release: true, Decision: true}, reviewplan.Admit{Cas: true, OtherOp: true}, reviewplan.Release{Cas: true, Decision: true, Members: true, Accepted: true}),
			}, false, func() error {
				return tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: ids}, Decision: "release-no-role-recovery"})
			})
			p = readOwedPlan(t, root, p.PlanID)
			refines(t, root, ids, kernelSteps{}, true, func() error {
				return tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: ids}, Decision: "again"})
			})
			tail := owedBatch(p, "required-tail", archiveRequirements(), id)
			refines(t, root, ids, kernelSteps{
				members: bothMembers(ids, reviewobligation.AddBatch{Member: true, Roles: pmqaRoles}),
				plan:    extendSteps(reviewplan.Extend{Batch: true}, reviewplan.Admit{Cas: true, BatchOnly: true}, appendFacts(false, false)),
			}, false, func() error { return tryExtendOwed(root, p, ReviewPlanExtension{Batch: &tail}) })
			p = readOwedPlan(t, root, p.PlanID)
			x := ReviewPlanExtension{DropBatches: []string{"required-tail"}}
			shape := reviewplan.Extend{Drop: true}
			if drop == "dropReleased" {
				x.Decision, shape.Decision = "release-dropped-review-only", true
			}
			refines(t, root, ids, kernelSteps{
				members: bothMembers(ids, reviewobligation.Drop{Keep: 1, Decision: x.Decision != ""}),
				plan:    extendSteps(shape, reviewplan.Admit{Cas: true, OtherOp: true}, reviewplan.Drop{N: 1, Cas: true, Trailing: true}),
			}, false, func() error { return tryExtendOwed(root, p, x) })
			p = readOwedPlan(t, root, p.PlanID)
			if drop == "releaseDrop" {
				refines(t, root, ids, kernelSteps{
					members: bothMembers(ids, reviewobligation.ReleaseDrop{}),
					plan:    extendSteps(reviewplan.Extend{Release: true, Decision: true}, reviewplan.Admit{Cas: true, OtherOp: true}, reviewplan.Release{Cas: true, Decision: true, Members: true, Accepted: true}),
				}, false, func() error { p = releaseDrop(t, root, p, ids); return nil })
			}
			p = extendOwed(t, root, p, ReviewPlanExtension{Seal: true})
			record, ok := recoveryRecord(t, root, id)
			if !ok || record.PlanID != "" || record.ReleaseDecision == "" || !slices.Equal(record.Roles, []string{"PMQA"}) {
				t.Fatalf("no-plan recovery history lost: %+v", record)
			}
			progress, err := ReviewTaskProgress(root, id)
			if err != nil || !slices.Contains(progress.Pending, "recovery-role:"+id+"/PMQA") {
				t.Fatalf("missing production debt: %+v %v", progress, err)
			}
			snap := snapshotKernels(t, root, 0, id)
			params := reviewobligation.Params{MaxBatch: snap.bound}
			if _, ok := reviewobligation.Step(snap.members[id], reviewobligation.Done{PlanClear: true}, params); ok {
				t.Fatal("kernel allowed completion despite the revived recovery debt")
			}
			refines(t, root, ids, kernelSteps{}, true, func() error {
				err := deadEndDone(t, root, id)
				if err != nil && !strings.Contains(err.Error(), "recovery-review:"+id) {
					t.Fatalf("done refused for another reason: %v", err)
				}
				return err
			})
		})
	}
}

// A resolution of an unplanned batch and a target sync change no kernel state;
// a repeated resolution and a drifted advance are refused.
func TestResolutionAndSyncRefineKernels(t *testing.T) {
	s := defaultStrayBoard(t, "kernel-resolve")
	refines(t, s.root, s.ids, kernelSteps{plan: []reviewplan.Event{reviewplan.Resolve{Cas: true, Fresh: true, Covered: true, Published: true, Git: true}}}, false, func() error {
		return s.resolve(s.request(t, s.fixed()))
	})
	refines(t, s.root, s.ids, kernelSteps{}, true, func() error { return s.resolve(s.request(t, s.fixed())) })

	root := tempBoard(t)
	id := gateCard(t, root, "kernel-sync")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	final, next := strings.Repeat("c", 40), strings.Repeat("d", 40)
	driftBatchTarget(t, root, "batch", final)
	refines(t, root, []string{id}, kernelSteps{}, true, func() error {
		return AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 2, Advance: ReviewAdvance{PreviousTarget: final, Target: next, Reason: "silent repair attempt", Deliveries: map[string]string{next: id}}})
	})
	plan := readPlanCopy(t, root, id)
	refines(t, root, []string{id}, kernelSteps{
		plan: extendSteps(reviewplan.Extend{Sync: true}, reviewplan.Admit{Cas: true, OtherOp: true}, reviewplan.Sync{Cas: true, CyclesCurrent: true, Targets: true}),
	}, false, func() error {
		return ExtendReviewPlan(root, ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: plan.Revision, SyncTargets: map[string]string{"batch": final}, Author: "coordinator", Basis: "align"})
	})
}
