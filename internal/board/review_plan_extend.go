package board

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/internal/statemachine/reviewobligation"
	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

// ReviewPlanExtension appends one batch after the prior closure, seals the
// cycle, rebinds execution cycles, syncs a drifted plan target to the runtime
// batch, drops trailing batches no run has reached, releases an obligation on a user
// decision, or resolves an unplanned batch (ResolveReviewBatch). Existing
// requirements, members and evidence are never replaced. A sealed plan takes a batch
// only for a rebound cycle that has none, or for a recovery that still owes a review.
type ReviewPlanExtension struct {
	RebindCycles      map[string]string             `json:"rebind_cycles,omitempty"`
	SyncTargets       map[string]string             `json:"sync_targets,omitempty"`
	DropBatches       []string                      `json:"drop_batches,omitempty"`
	ReleaseObligation *ReviewObligationRelease      `json:"release_obligation,omitempty"`
	Decision          string                        `json:"decision,omitempty"`
	ResolveBatch      *ReviewBatchResolutionRequest `json:"resolve_batch,omitempty"`
	PlanID            string                        `json:"plan_id"`
	ExpectedRevision  uint64                        `json:"expected_revision"`
	Batch             *ReviewPlanBatch              `json:"batch,omitempty"`
	Seal              bool                          `json:"seal"`
	Author            string                        `json:"author"`
	Basis             string                        `json:"basis"`
}

func ExtendReviewPlanWithTransaction(root string, x ReviewPlanExtension, extensions ...TransactionExtension) error {
	if x.ResolveBatch != nil {
		return reviewError("resolve_batch needs the Git evidence of ResolveReviewBatch")
	}
	// A decision of only white space is no decision, also in plan-history.
	x.Decision = strings.TrimSpace(x.Decision)
	if err := extensionShape(x); err != nil {
		return err
	}
	var p ReviewPlan
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, planName(x.PlanID), &p)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing plan")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(p.TaskIDs, false), func(tx *Transaction) error {
		ok, err := readReviewJSON(tx, planName(x.PlanID), &p)
		if err != nil {
			return err
		}
		if !ok {
			return reviewError("plan extension CAS/sealed conflict")
		}
		pk, err := projectPlan(tx, p, kernelBound(p))
		if err != nil {
			return err
		}
		cycleBatch := false
		if x.Batch != nil && !x.Seal {
			if cycleBatch, err = cycleBatchOwed(tx, p, x.Batch.TaskIDs); err != nil {
				return err
			}
		}
		otherOp := len(x.RebindCycles) > 0 || len(x.SyncTargets) > 0 || len(x.DropBatches) > 0 || x.ReleaseObligation != nil
		if !pk.step(reviewplan.Admit{Cas: p.Revision == x.ExpectedRevision, OtherOp: otherOp, BatchOnly: x.Batch != nil && !x.Seal, Owed: cycleBatch}) {
			return reviewError("plan extension CAS/sealed conflict")
		}
		if err = verifyPlanCopiesFor(tx, p, len(x.RebindCycles) == 0 && len(x.SyncTargets) == 0); err != nil {
			return err
		}
		previous := p
		p.Cycles = maps.Clone(p.Cycles)
		p.CycleBatchFrom = maps.Clone(p.CycleBatchFrom)
		changed := map[string]string{}
		for _, id := range p.TaskIDs {
			excluded, e := memberExcluded(tx, id)
			if e != nil {
				return e
			}
			if excluded {
				continue
			}
			s, e := tx.Snapshot(id)
			if e != nil {
				return e
			}
			if p.Cycles[id] == planCycle(s) {
				continue
			}
			// A terminal card keeps the cycle it ended with, so a drift is damage.
			if s.Entry.State != "working" && s.Entry.State != "review" {
				return reviewError("plan member is terminal")
			}
			changed[id] = planCycle(s)
		}
		if len(x.RebindCycles) > 0 {
			if err = rebindPlanCycles(tx, pk, &p, changed, x.RebindCycles); err != nil {
				return err
			}
		}
		if len(x.DropBatches) > 0 {
			if err = dropPlanBatches(tx, pk, &p, x.DropBatches, x.Decision); err != nil {
				return err
			}
		}
		if x.ReleaseObligation != nil {
			f, e := releaseObligation(tx, p, *x.ReleaseObligation, x.Decision)
			if e != nil {
				return e
			}
			if !pk.step(reviewplan.Release{Cas: true, Decision: f.decision, Members: f.members, Accepted: f.accepted}) {
				return firstError(f.err, reviewError("release_obligation refused"))
			}
			if err = persistRelease(tx, &p, x.ReleaseObligation.Kind, x.Decision, f.kernels); err != nil {
				return err
			}
		}
		if len(x.SyncTargets) > 0 {
			return syncPlanTargets(tx, pk, p, x, len(changed) == 0)
		}
		if x.Batch != nil {
			if err = appendPlanBatch(tx, pk, &p, *x.Batch, x.Seal, cycleBatch); err != nil {
				return err
			}
		} else if x.Seal {
			assigned, noDropOwed, refusal, e := sealFacts(tx, p)
			if e != nil {
				return e
			}
			if !pk.step(reviewplan.Seal{Cas: true, AllAssigned: assigned, NoDropOwed: noDropOwed}) {
				return firstError(refusal, reviewError("plan extension CAS/sealed conflict"))
			}
			p.Sealed = pk.state.Sealed
		}
		p.Revision++
		for _, id := range p.TaskIDs {
			if err = tx.PutGroup(reviewControlGroup, "tracked-cycles/"+id+".json", reviewJSON(map[string]string{"cycle": p.Cycles[id]})); err != nil {
				return err
			}
		}
		if err = putPlanCopies(tx, p); err != nil {
			return err
		}
		if err = tx.PutGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", p.PlanID, p.Revision), reviewJSON(struct {
			Previous   ReviewPlan          `json:"previous"`
			Request    ReviewPlanExtension `json:"request"`
			RecordedAt string              `json:"recorded_at"`
		}{previous, x, time.Now().UTC().Format(time.RFC3339Nano)})); err != nil {
			return err
		}
		return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
	}, extensions...)
}

// extensionShape runs the request shape through the plan kernel: at least one
// operation, no two exclusive ones, and a decision only with a drop or a release.
func extensionShape(x ReviewPlanExtension) error {
	shape := reviewplan.Extend{
		Rebind:   len(x.RebindCycles) > 0,
		Sync:     len(x.SyncTargets) > 0,
		Batch:    x.Batch != nil || x.Seal,
		Drop:     len(x.DropBatches) > 0,
		Release:  x.ReleaseObligation != nil,
		Decision: x.Decision != "",
	}
	initial := reviewplan.Initial(reviewplan.Params{})
	_, accepted := reviewplan.Step(initial, shape, reviewplan.Params{})
	if !ValidReviewID(x.PlanID) || strings.TrimSpace(x.Author) == "" || strings.TrimSpace(x.Basis) == "" {
		return reviewError("plan extension provenance")
	}
	switch {
	case accepted:
		return nil
	case !shape.Rebind && !shape.Sync && !shape.Batch && !shape.Drop && !shape.Release:
		return reviewError("plan extension provenance")
	case shape.Decision && !shape.Drop && !shape.Release:
		return reviewError(t("board.review_decision_unused"))
	default:
		return reviewError(t("board.review_extend_exclusive"))
	}
}

// rebindPlanCycles records the new cycle of every changed member. The plan kernel
// requires the request to name exactly the changed members; each member's kernel
// sets where a batch reviewing its new cycle starts.
func rebindPlanCycles(tx *Transaction, pk *planKernel, p *ReviewPlan, changed, requested map[string]string) error {
	if !pk.step(reviewplan.Rebind{Cas: true, Exact: maps.Equal(changed, requested), MembersActive: true}) {
		return reviewError("cycle rebind CAS requires exactly all changed members")
	}
	kernels := make([]*memberKernel, 0, len(changed))
	for _, id := range slices.Sorted(maps.Keys(changed)) {
		k, err := loadMemberKernel(tx, *p, true, id, kernelBound(*p))
		if err != nil {
			return err
		}
		if !k.step(reviewobligation.Rebind{}) {
			return errKernelState(id)
		}
		kernels = append(kernels, k)
	}
	for _, k := range kernels {
		p.Cycles[k.id] = changed[k.id]
		if k.state.CycNeed {
			if p.CycleBatchFrom == nil {
				p.CycleBatchFrom = map[string]int{}
			}
			p.CycleBatchFrom[k.id] = int(k.state.CycFrom)
		}
	}
	return nil
}

// sealFacts computes the seal conditions on the plan as it stands: every member
// is in a batch, and every drop obligation has a planned batch.
func sealFacts(tx *Transaction, p ReviewPlan) (assigned, noDropOwed bool, refusal error, err error) {
	assigned = true
	for _, id := range p.TaskIDs {
		k, e := loadMemberKernel(tx, p, true, id, kernelBound(p))
		if e != nil {
			return false, false, nil, e
		}
		in, e := queryKernel(k, reviewobligation.Assigned)
		if e != nil {
			return false, false, nil, e
		}
		if !in {
			return false, false, reviewError("seal requires every member assigned"), nil
		}
	}
	owed, err := dropObligationsOwed(tx, p, false)
	if err != nil {
		return false, false, nil, err
	}
	if len(owed) > 0 {
		return assigned, false, reviewError(t("board.review_seal_dropped", strings.Join(owed, ", "))), nil
	}
	return assigned, true, nil, nil
}

// syncPlanTargets moves recorded plan targets to the runtime batches and
// publishes the plan.
func syncPlanTargets(tx *Transaction, pk *planKernel, p ReviewPlan, x ReviewPlanExtension, cyclesCurrent bool) error {
	var refusal error
	if !cyclesCurrent {
		refusal = reviewError("target sync requires current execution cycles")
	}
	previous := p
	previous.Batches = slices.Clone(p.Batches)
	p.Batches = slices.Clone(p.Batches)
	targets := true
	synced := map[string]reviewPlanTargetSyncRequest{}
	for batchID, expected := range x.SyncTargets {
		if refusal != nil {
			break
		}
		var b ReviewBatch
		found, e := readReviewJSON(tx, reviewBatchName(batchID), &b)
		if e != nil {
			return e
		}
		index := slices.IndexFunc(p.Batches, func(pb ReviewPlanBatch) bool { return pb.BatchID == batchID })
		switch {
		case !found || b.PlanID != p.PlanID || b.BatchID != batchID:
			refusal = reviewError("sync target batch missing")
		case b.TargetCommit != expected:
			refusal = reviewError("sync target CAS requires the current batch target")
		case index < 0:
			refusal = reviewError("unplanned batch")
		case p.Batches[index].TargetCommit == b.TargetCommit:
			refusal = reviewError("sync target already matches batch")
		default:
			synced[batchID] = reviewPlanTargetSyncRequest{
				Kind:            "extend-plan",
				BatchID:         batchID,
				PreviousTarget:  p.Batches[index].TargetCommit,
				Target:          b.TargetCommit,
				Author:          x.Author,
				Basis:           x.Basis,
				ExpectedPlanRev: x.ExpectedRevision,
			}
			p.Batches[index].TargetCommit = b.TargetCommit
		}
		targets = refusal == nil
	}
	if !pk.step(reviewplan.Sync{Cas: true, CyclesCurrent: cyclesCurrent, Targets: targets}) {
		return firstError(refusal, reviewError("sync_targets refused"))
	}
	p.Revision++
	if err := putPlanCopies(tx, p); err != nil {
		return err
	}
	if err := tx.PutGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", p.PlanID, p.Revision), reviewJSON(struct {
		Previous   ReviewPlan                             `json:"previous"`
		Request    ReviewPlanExtension                    `json:"request"`
		Syncs      map[string]reviewPlanTargetSyncRequest `json:"syncs"`
		RecordedAt string                                 `json:"recorded_at"`
	}{previous, x, synced, time.Now().UTC().Format(time.RFC3339Nano)})); err != nil {
		return err
	}
	return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
}

// appendPlanBatch appends b, sealing the plan with it when seal. The plan kernel
// decides the append from the batch facts and, under a seal, the seal facts of
// the plan with the batch; each member's kernel takes the batch.
func appendPlanBatch(tx *Transaction, pk *planKernel, p *ReviewPlan, b ReviewPlanBatch, seal, owed bool) error {
	binding := ValidReviewID(b.BatchID) && validReviewTargets(b.Base, b.TargetCommit, b.Requirements) && b.PreviousBatchID == p.Batches[len(p.Batches)-1].BatchID
	var refusal error
	if !binding {
		refusal = reviewError("extension batch binding")
	}
	if refusal == nil && slices.ContainsFunc(p.Batches, func(prior ReviewPlanBatch) bool { return prior.BatchID == b.BatchID }) {
		binding, refusal = false, reviewError("duplicate planned batch")
	}
	if refusal == nil {
		ids, e := normalizedReviewTasks(b.TaskIDs)
		if e != nil {
			binding, refusal = false, e
		}
		b.TaskIDs = ids
	}
	roles, err := batchRequiredSet(b)
	if refusal == nil && err != nil {
		binding, refusal = false, firstError(validateRequirements(b.Requirements), err)
	}
	membersActive := refusal == nil
	if refusal == nil {
		accepted := map[string]bool{}
		for _, id := range p.TaskIDs {
			k, e := loadMemberKernel(tx, *p, true, id, kernelBound(*p))
			if e != nil {
				return e
			}
			accepted[id] = k.step(reviewobligation.AddBatch{Member: containsID(b.TaskIDs, id), Roles: roles})
		}
		for _, id := range b.TaskIDs {
			if !containsID(p.TaskIDs, id) {
				binding, refusal = false, reviewError("foreign plan member")
			} else if !accepted[id] {
				membersActive, refusal = false, reviewError("batch member is terminal: "+id)
			}
			if refusal != nil {
				break
			}
		}
	}
	requirements := refusal == nil
	if requirements {
		if e := validateRequirements(b.Requirements); e != nil {
			requirements, refusal = false, e
		}
	}
	actual := ReviewBatch{Schema: 1, PlanID: p.PlanID, BatchID: b.BatchID, PreviousBatchID: b.PreviousBatchID, TaskIDs: b.TaskIDs, Base: b.Base, TargetCommit: b.TargetCommit, Requirements: b.Requirements, ReportLanguage: p.ReportLanguage, Revision: 1}
	previousClosed := refusal == nil
	if previousClosed {
		if e := validatePreviousClosure(tx, actual); e != nil {
			previousClosed, refusal = false, e
		}
	}
	if refusal == nil {
		var old ReviewBatch
		exists, e := readReviewJSON(tx, reviewBatchName(b.BatchID), &old)
		if e != nil {
			return e
		}
		switch {
		case !exists:
			if e = validateNewRequirements(b.Requirements); e != nil {
				requirements, refusal = false, e
			}
		case !reflect.DeepEqual(actual, old):
			binding, refusal = false, reviewError("extension batch already exists")
		}
	}
	assigned, noDropOwed := true, true
	if refusal == nil {
		p.Batches = append(p.Batches, b)
		if seal {
			var e error
			if assigned, noDropOwed, refusal, e = sealFacts(tx, *p); e != nil {
				return e
			}
		}
	}
	if !pk.step(reviewplan.Append{Seal: seal, Owed: owed, Cas: true, Binding: binding, MembersActive: membersActive, PreviousClosed: previousClosed, Requirements: requirements, AllAssigned: assigned, NoDropOwed: noDropOwed}) {
		return firstError(refusal, reviewError("plan extension CAS/sealed conflict"))
	}
	p.Sealed = pk.state.Sealed
	return tx.PutGroup(reviewControlGroup, reviewBatchName(b.BatchID), reviewJSON(actual))
}
