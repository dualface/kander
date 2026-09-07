package board

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

type ReviewPlanBatch struct {
	BatchID         string            `json:"batch_id"`
	PreviousBatchID string            `json:"previous_batch_id,omitempty"`
	TaskIDs         []string          `json:"task_ids"`
	Base            string            `json:"base"`
	TargetCommit    string            `json:"target_commit"`
	Requirements    map[string]string `json:"requirements"`
}

// ReviewPlan freezes the execution cycle and every intended batch before closure.
// Each requirement is required or N/A with its reason and applicable rule basis.
type ReviewPlan struct {
	Revision       uint64            `json:"revision"`
	Sealed         bool              `json:"sealed"`
	Schema         int               `json:"schema"`
	PlanID         string            `json:"plan_id"`
	Author         string            `json:"author"`
	Basis          string            `json:"basis"`
	CWD            string            `json:"cwd"`
	ReportLanguage string            `json:"report_language"`
	TaskIDs        []string          `json:"task_ids"`
	Cycles         map[string]string `json:"cycles"`
	Batches        []ReviewPlanBatch `json:"batches"`
	RecordedAt     string            `json:"recorded_at"`
}

func planName(id string) string     { return "plans/" + id + ".json" }
func taskPlanName(id string) string { return "task-plans/" + id + ".json" }
func closureName(id string) string  { return "closures/" + id + ".json" }
func validateRequirements(r map[string]string) error {
	if len(r) != 4 {
		return reviewError("all four role requirements required")
	}
	for _, role := range []string{"PM", "QA", "CSA", "Hacker"} {
		value := r[role]
		if value != "required" && (!strings.HasPrefix(value, "N/A: ") || strings.TrimSpace(strings.TrimPrefix(value, "N/A: ")) == "") {
			return reviewError("requirement reason and rule basis: " + role)
		}
	}
	return nil
}
func planCycle(s Snapshot) string {
	return ReviewDigest([]byte(s.Entry.TaskID + "\n" + MetadataFrom(s.Text, FieldStartedAt)))
}
func CreateReviewPlan(root string, p ReviewPlan) error {
	if p.Schema != 1 || !ValidReviewID(p.PlanID) || strings.TrimSpace(p.Author) == "" || strings.TrimSpace(p.Basis) == "" || p.CWD == "" || len(p.Batches) == 0 {
		return reviewError("review plan identity/provenance")
	}
	ids, err := normalizedReviewTasks(p.TaskIDs)
	if err != nil {
		return err
	}
	p.TaskIDs = ids
	return WithTransaction(root, reviewScope(ids, false), func(tx *Transaction) error {
		language, err := reviewCards(tx, ids, p.ReportLanguage, "")
		if err != nil {
			return err
		}
		p.ReportLanguage = language
		p.Cycles = map[string]string{}
		for _, id := range ids {
			s, err := tx.Snapshot(id)
			if err != nil {
				return err
			}
			p.Cycles[id] = planCycle(s)
		}
		seen, covered := map[string]bool{}, map[string]bool{}
		for i, b := range p.Batches {
			if !ValidReviewID(b.BatchID) || seen[b.BatchID] || !validReviewTargets(b.Base, b.TargetCommit, b.Requirements) {
				return reviewError("plan batch identity/version")
			}
			seen[b.BatchID] = true
			if i == 0 && b.PreviousBatchID != "" || i > 0 && b.PreviousBatchID != p.Batches[i-1].BatchID {
				return reviewError("explicit previous batch chain required")
			}
			members, err := normalizedReviewTasks(b.TaskIDs)
			if err != nil {
				return err
			}
			p.Batches[i].TaskIDs = members
			for _, id := range members {
				if !containsID(ids, id) {
					return reviewError("foreign plan member")
				}
				covered[id] = true
			}
			if err = validateRequirements(b.Requirements); err != nil {
				return err
			}
		}
		if p.Sealed && len(covered) != len(ids) {
			return reviewError("plan must cover every member")
		}
		var old ReviewPlan
		exists, err := readReviewJSON(tx, planName(p.PlanID), &old)
		if err != nil {
			return err
		}
		if exists {
			p.RecordedAt = old.RecordedAt
			p.Revision = old.Revision
			if !reflect.DeepEqual(old, p) {
				return reviewError("immutable review plan conflict")
			}
			return verifyPlanCopies(tx, p)
		}
		p.Revision = 1
		p.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		for _, id := range ids {
			var pointer struct {
				PlanID string `json:"plan_id"`
			}
			exists, err := readReviewJSON(tx, taskPlanName(id), &pointer)
			if err != nil {
				return err
			}
			if exists {
				return reviewError("execution already has a review plan: " + id)
			}
		}
		for _, b := range p.Batches {
			var existing ReviewBatch
			exists, err := readReviewJSON(tx, reviewBatchName(b.BatchID), &existing)
			if err != nil {
				return err
			}
			if exists && (existing.Base != b.Base || existing.TargetCommit != b.TargetCommit || !reflect.DeepEqual(existing.TaskIDs, b.TaskIDs) || !reflect.DeepEqual(existing.Requirements, b.Requirements) || existing.ReportLanguage != p.ReportLanguage) {
				return reviewError("existing batch/plan mismatch")
			}
			if !exists {
				existing = ReviewBatch{Schema: 1, BatchID: b.BatchID, TaskIDs: b.TaskIDs, Base: b.Base, TargetCommit: b.TargetCommit, Requirements: b.Requirements, ReportLanguage: p.ReportLanguage, Revision: 1}
			}
			if existing.PlanID != "" {
				return reviewError("batch already planned")
			}
			existing.PlanID = p.PlanID
			existing.PreviousBatchID = b.PreviousBatchID
			if err = tx.PutGroup(reviewControlGroup, reviewBatchName(b.BatchID), reviewJSON(existing)); err != nil {
				return err
			}
		}
		for _, id := range ids {
			if err = tx.PutGroup(reviewControlGroup, "tracked-cycles/"+id+".json", reviewJSON(map[string]string{"cycle": p.Cycles[id]})); err != nil {
				return err
			}
			if err = tx.PutGroup(reviewControlGroup, taskPlanName(id), reviewJSON(map[string]string{"plan_id": p.PlanID})); err != nil {
				return err
			}
			if err = tx.Put(id, "reviews/plan.json", reviewJSON(p)); err != nil {
				return err
			}
		}
		return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
	})
}
func readTaskPlan(tx *Transaction, id string) (p ReviewPlan, exists bool, err error) {
	var pointer struct {
		PlanID string `json:"plan_id"`
	}
	exists, err = readReviewJSON(tx, taskPlanName(id), &pointer)
	if err != nil || !exists {
		return
	}
	if !ValidReviewID(pointer.PlanID) {
		return p, true, reviewError("plan pointer")
	}
	found, e := readReviewJSON(tx, planName(pointer.PlanID), &p)
	if e != nil {
		return p, true, e
	}
	if !found || p.Schema != 1 || p.PlanID != pointer.PlanID || !containsID(p.TaskIDs, id) {
		return p, true, reviewError("missing/invalid plan")
	}
	return
}
func verifyPlanCopies(tx *Transaction, p ReviewPlan) error {
	if p.Schema != 1 || !ValidReviewID(p.PlanID) || len(p.Batches) == 0 {
		return reviewError("plan schema")
	}
	for _, id := range p.TaskIDs {
		s, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		if p.Cycles[id] != planCycle(s) {
			return reviewError("plan execution cycle mismatch")
		}
		text, err := tx.Read(id, "reviews/plan.json")
		if err != nil {
			return err
		}
		if text != reviewJSON(p) {
			return reviewError("plan copy mismatch: " + id)
		}
		current, exists, err := readTaskPlan(tx, id)
		if err != nil {
			return err
		}
		if !exists || !reflect.DeepEqual(current, p) {
			return reviewError("plan membership pointer")
		}
		l := MetadataFrom(s.Text, FieldLanguage)
		if l != "" && l != p.ReportLanguage {
			return reviewError("plan language mismatch")
		}
	}
	return nil
}
func ReadReviewBatch(root, id string) (b ReviewBatch, err error) {
	if !ValidReviewID(id) {
		return b, reviewError("batch_id")
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, reviewBatchName(id), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		return nil
	})
	return
}
func batchPlan(tx *Transaction, b ReviewBatch) (ReviewPlan, error) {
	var p ReviewPlan
	ok, err := readReviewJSON(tx, planName(b.PlanID), &p)
	if err != nil {
		return p, err
	}
	if !ok || p.PlanID != b.PlanID {
		return p, reviewError("batch review plan required")
	}
	for _, pb := range p.Batches {
		if pb.BatchID == b.BatchID {
			if pb.Base != b.Base || !slices.Equal(pb.TaskIDs, b.TaskIDs) || !reflect.DeepEqual(pb.Requirements, b.Requirements) || pb.PreviousBatchID != b.PreviousBatchID || p.ReportLanguage != b.ReportLanguage {
				return p, reviewError("batch plan binding")
			}
			return p, nil
		}
	}
	return p, reviewError("unplanned batch")
}
func validatePreviousClosure(tx *Transaction, b ReviewBatch) error {
	seen := map[string]bool{b.BatchID: true}
	for b.PreviousBatchID != "" {
		if seen[b.PreviousBatchID] {
			return reviewError("closed batch cycle")
		}
		seen[b.PreviousBatchID] = true
		var previous ReviewClosure
		ok, err := readReviewJSON(tx, closureName(b.PreviousBatchID), &previous)
		if err != nil {
			return err
		}
		if !ok || previous.BatchID != b.PreviousBatchID || previous.PlanID != b.PlanID || previous.TargetCommit != b.Base {
			return reviewError(fmt.Sprintf("previous closed batch required: %s", b.PreviousBatchID))
		}
		if err = verifyClosureIntegrity(tx, previous); err != nil {
			return err
		}
		b = previous.View.Batch
	}
	return nil
}

func noGitReviewBatch(base, target string, requirements map[string]string) bool {
	if base != "N/A" || target != "N/A" || validateRequirements(requirements) != nil {
		return false
	}
	for _, value := range requirements {
		if value == "required" {
			return false
		}
	}
	return true
}
func validReviewTargets(base, target string, requirements map[string]string) bool {
	return validCommit(base) && validCommit(target) || noGitReviewBatch(base, target, requirements)
}

// ReviewNeedsGit separates explicit non-Git N/A plans from commit-bound batches.
func ReviewNeedsGit(b ReviewPlanBatch) bool {
	return !noGitReviewBatch(b.Base, b.TargetCommit, b.Requirements)
}
