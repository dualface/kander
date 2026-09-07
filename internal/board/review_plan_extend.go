package board

import (
	"fmt"
	"reflect"
	"strings"
)

// ReviewPlanExtension appends one batch after the prior closure, or seals the
// cycle. Existing requirements, members and evidence are never replaced.
type ReviewPlanExtension struct {
	PlanID           string           `json:"plan_id"`
	ExpectedRevision uint64           `json:"expected_revision"`
	Batch            *ReviewPlanBatch `json:"batch,omitempty"`
	Seal             bool             `json:"seal"`
	Author           string           `json:"author"`
	Basis            string           `json:"basis"`
}

func ExtendReviewPlan(root string, x ReviewPlanExtension) error {
	if !ValidReviewID(x.PlanID) || strings.TrimSpace(x.Author) == "" || strings.TrimSpace(x.Basis) == "" || x.Batch == nil && !x.Seal {
		return reviewError("plan extension provenance")
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
		if !ok || p.Sealed || p.Revision != x.ExpectedRevision {
			return reviewError("plan extension CAS/sealed conflict")
		}
		if err = verifyPlanCopies(tx, p); err != nil {
			return err
		}
		for _, id := range p.TaskIDs {
			s, e := tx.Snapshot(id)
			if e != nil {
				return e
			}
			if s.Entry.State != "working" && s.Entry.State != "review" {
				return reviewError("plan member is terminal")
			}
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
			if exists && !reflect.DeepEqual(actual, old) {
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
			p.Sealed = true
		}
		p.Revision++
		for _, id := range p.TaskIDs {
			if err = tx.Put(id, "reviews/plan.json", reviewJSON(p)); err != nil {
				return err
			}
		}
		if err = tx.PutGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", p.PlanID, p.Revision), reviewJSON(x)); err != nil {
			return err
		}
		return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
	})
}
