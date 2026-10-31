package board

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"
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

func ExtendReviewPlan(root string, x ReviewPlanExtension) error {
	if x.ResolveBatch != nil {
		return reviewError("resolve_batch needs the Git evidence of ResolveReviewBatch")
	}
	if !ValidReviewID(x.PlanID) || strings.TrimSpace(x.Author) == "" || strings.TrimSpace(x.Basis) == "" || x.Batch == nil && !x.Seal && len(x.RebindCycles) == 0 && len(x.SyncTargets) == 0 && len(x.DropBatches) == 0 && x.ReleaseObligation == nil {
		return reviewError("plan extension provenance")
	}
	// A decision of only white space is no decision, also in plan-history.
	x.Decision = strings.TrimSpace(x.Decision)
	if x.Decision != "" && len(x.DropBatches) == 0 && x.ReleaseObligation == nil {
		return reviewError(t("board.review_decision_unused"))
	}
	exclusiveOps := 0
	if len(x.RebindCycles) > 0 {
		exclusiveOps++
	}
	if len(x.SyncTargets) > 0 {
		exclusiveOps++
	}
	if x.Batch != nil || x.Seal {
		exclusiveOps++
	}
	if len(x.DropBatches) > 0 {
		exclusiveOps++
	}
	if x.ReleaseObligation != nil {
		exclusiveOps++
	}
	if exclusiveOps > 1 {
		return reviewError(t("board.review_extend_exclusive"))
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
		cycleBatch := false
		if x.Batch != nil && !x.Seal {
			if cycleBatch, err = cycleBatchOwed(tx, p, x.Batch.TaskIDs); err != nil {
				return err
			}
		}
		if !ok || p.Sealed && len(x.RebindCycles) == 0 && len(x.SyncTargets) == 0 && len(x.DropBatches) == 0 && x.ReleaseObligation == nil && !cycleBatch || p.Revision != x.ExpectedRevision {
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
			if s.Entry.State != "working" && s.Entry.State != "review" {
				return reviewError("plan member is terminal")
			}
			changed[id] = planCycle(s)
		}
		if len(x.RebindCycles) > 0 {
			if !maps.Equal(changed, x.RebindCycles) {
				return reviewError("cycle rebind CAS requires exactly all changed members")
			}
			for id, cycle := range changed {
				p.Cycles[id] = cycle
				start, found, e := cycleBatchStart(tx, p, id)
				if e != nil {
					return e
				}
				if found {
					if p.CycleBatchFrom == nil {
						p.CycleBatchFrom = map[string]int{}
					}
					p.CycleBatchFrom[id] = start
				}
			}
		}
		if len(x.DropBatches) > 0 {
			if err = dropPlanBatches(tx, &p, x.DropBatches, x.Decision); err != nil {
				return err
			}
		}
		if x.ReleaseObligation != nil {
			if err = releaseObligation(tx, &p, *x.ReleaseObligation, x.Decision); err != nil {
				return err
			}
		}
		if len(x.SyncTargets) > 0 {
			if len(changed) > 0 {
				return reviewError("target sync requires current execution cycles")
			}
			previous = p
			previous.Batches = slices.Clone(p.Batches)
			synced := map[string]reviewPlanTargetSyncRequest{}
			for batchID, expected := range x.SyncTargets {
				var b ReviewBatch
				found, e := readReviewJSON(tx, reviewBatchName(batchID), &b)
				if e != nil {
					return e
				}
				if !found || b.PlanID != p.PlanID || b.BatchID != batchID {
					return reviewError("sync target batch missing")
				}
				if b.TargetCommit != expected {
					return reviewError("sync target CAS requires the current batch target")
				}
				index := -1
				for i, pb := range p.Batches {
					if pb.BatchID == batchID {
						index = i
						break
					}
				}
				if index < 0 {
					return reviewError("unplanned batch")
				}
				if p.Batches[index].TargetCommit == b.TargetCommit {
					return reviewError("sync target already matches batch")
				}
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
			p.Revision++
			if err = putPlanCopies(tx, p); err != nil {
				return err
			}
			if err = tx.PutGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", p.PlanID, p.Revision), reviewJSON(struct {
				Previous   ReviewPlan                             `json:"previous"`
				Request    ReviewPlanExtension                    `json:"request"`
				Syncs      map[string]reviewPlanTargetSyncRequest `json:"syncs"`
				RecordedAt string                                 `json:"recorded_at"`
			}{previous, x, synced, time.Now().UTC().Format(time.RFC3339Nano)})); err != nil {
				return err
			}
			return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
		}
		if x.Batch != nil {
			b := *x.Batch
			if !ValidReviewID(b.BatchID) || !validReviewTargets(b.Base, b.TargetCommit, b.Requirements) || b.PreviousBatchID != p.Batches[len(p.Batches)-1].BatchID {
				return reviewError("extension batch binding")
			}
			for _, prior := range p.Batches {
				if b.BatchID == prior.BatchID {
					return reviewError("duplicate planned batch")
				}
			}
			ids, e := normalizedReviewTasks(b.TaskIDs)
			if e != nil {
				return e
			}
			b.TaskIDs = ids
			for _, id := range ids {
				if !containsID(p.TaskIDs, id) {
					return reviewError("foreign plan member")
				}
				s, e := tx.Snapshot(id)
				if e != nil {
					return e
				}
				if s.Entry.State != "working" && s.Entry.State != "review" {
					return reviewError("batch member is terminal: " + id)
				}
			}
			if err = validateRequirements(b.Requirements); err != nil {
				return err
			}
			actual := ReviewBatch{Schema: 1, PlanID: p.PlanID, BatchID: b.BatchID, PreviousBatchID: b.PreviousBatchID, TaskIDs: b.TaskIDs, Base: b.Base, TargetCommit: b.TargetCommit, Requirements: b.Requirements, ReportLanguage: p.ReportLanguage, Revision: 1}
			if err = validatePreviousClosure(tx, actual); err != nil {
				return err
			}
			var old ReviewBatch
			exists, e := readReviewJSON(tx, reviewBatchName(b.BatchID), &old)
			if e != nil {
				return e
			}
			if !exists {
				if err = validateNewRequirements(b.Requirements); err != nil {
					return err
				}
			} else if !reflect.DeepEqual(actual, old) {
				return reviewError("extension batch already exists")
			}
			if err = tx.PutGroup(reviewControlGroup, reviewBatchName(b.BatchID), reviewJSON(actual)); err != nil {
				return err
			}
			p.Batches = append(p.Batches, b)
		}
		if x.Seal {
			for _, id := range p.TaskIDs {
				found := false
				for _, b := range p.Batches {
					if containsID(b.TaskIDs, id) {
						found = true
					}
				}
				if !found {
					return reviewError("seal requires every member assigned")
				}
			}
			owed, e := dropObligationsOwed(tx, p, false)
			if e != nil {
				return e
			}
			if len(owed) > 0 {
				return reviewError(t("board.review_seal_dropped", strings.Join(owed, ", ")))
			}
			p.Sealed = true
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
	})
}
