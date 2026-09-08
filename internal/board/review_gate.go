package board

import (
	"errors"
	"github.com/dualface/kander/internal/fs"
	"os"
	"reflect"
	"strings"
	"time"
)

type ReviewProgress struct {
	TaskID       string            `json:"task_id"`
	PlanID       string            `json:"plan_id,omitempty"`
	Status       string            `json:"status"`
	Pending      []string          `json:"pending,omitempty"`
	RebindCycles map[string]string `json:"rebind_cycles,omitempty"`
}

func reviewGateScope(root, id string, exclusive bool, warnings ...*WarningLog) (LockScope, error) {
	ids := []string{id}
	readScope := reviewScope(nil, true)
	readScope.warnings = journalWarningLog(warnings)
	err := WithTransaction(root, readScope, func(tx *Transaction) error {
		p, exists, e := readTaskPlan(tx, id)
		if e != nil {
			return e
		}
		if exists {
			ids = p.TaskIDs
		}
		return nil
	})
	scope := reviewScope(ids, false)
	scope.ExclusiveBoard = exclusive
	scope.warnings = journalWarningLog(warnings)
	return scope, err
}

// validateTaskReview is shared by completion and check. Pending work is legitimate
// during execution; completion demands all planned batches and immutable copies.
func validateTaskReview(tx *Transaction, id string, completion bool) (progress ReviewProgress, err error) {
	progress.TaskID = id
	s, err := tx.Snapshot(id)
	if err != nil {
		return progress, err
	}
	p, exists, err := readTaskPlan(tx, id)
	if err != nil {
		return progress, err
	}
	if !exists {
		_, tracked, e := tx.ReadGroup(reviewControlGroup, "tracked-cycles/"+id+".json")
		if e != nil {
			return progress, e
		}
		if tracked {
			return progress, reviewError("tracked execution plan missing")
		}
		if !completion && (s.Entry.State == "done" || s.Entry.State == "archived") {
			progress.Status = "legacy-untracked"
			return progress, nil
		}
		progress.Status = "requirements-needed"
		if completion {
			return progress, reviewError("review plan required for execution cycle")
		}
		return progress, nil
	}
	progress.PlanID = p.PlanID
	if err = checkUnplannedReviewRuns(tx, p); err != nil {
		return progress, err
	}
	if err = verifyPlanCopiesFor(tx, p, false); err != nil {
		return progress, err
	}
	for _, member := range p.TaskIDs {
		card, e := tx.Snapshot(member)
		if e != nil {
			return progress, e
		}
		if p.Cycles[member] != planCycle(card) {
			if progress.RebindCycles == nil {
				progress.RebindCycles = map[string]string{}
			}
			progress.RebindCycles[member] = planCycle(card)
		}
	}
	if len(progress.RebindCycles) > 0 {
		progress.Status = "requirements-needed"
		if completion {
			return progress, reviewError("execution cycle changed; extend-plan with rebind_cycles retains all existing requirements and failures")
		}
		return progress, nil
	}
	if !p.Sealed {
		progress.Pending = append(progress.Pending, "unsealed-plan")
	}
	covered := false
	for _, b := range p.Batches {
		if containsID(b.TaskIDs, id) {
			covered = true
		}
	}
	if !covered {
		progress.Pending = append(progress.Pending, "unassigned-member")
	}
	previous := ""
	for i, pb := range p.Batches {
		var b ReviewBatch
		ok, e := readReviewJSON(tx, reviewBatchName(pb.BatchID), &b)
		if e != nil {
			return progress, e
		}
		if !ok {
			return progress, reviewError("planned batch missing")
		}
		if _, e = batchPlan(tx, b); e != nil {
			return progress, e
		}
		if i > 0 && (b.PreviousBatchID != p.Batches[i-1].BatchID || previous != "" && b.Base != previous) {
			return progress, reviewError("closed batch chain mismatch")
		}
		var c ReviewClosure
		closed, e := readReviewJSON(tx, closureName(b.BatchID), &c)
		if e != nil {
			return progress, e
		}
		if !closed {
			progress.Pending = append(progress.Pending, b.BatchID)
			if e = checkPendingDispositions(tx, b); e != nil {
				return progress, e
			}
			previous = ""
			continue
		}
		if i > 0 && previous == "" {
			return progress, reviewError("closed batch has unclosed predecessor")
		}
		if noGitReviewBatch(b.Base, b.TargetCommit, b.Requirements) != (strings.TrimSpace(c.Git.NotApplicable) != "") {
			return progress, reviewError("Git N/A evidence binding")
		}
		if c.Schema != 1 || c.PlanID != p.PlanID || c.BatchID != b.BatchID || c.TargetCommit != b.TargetCommit || c.Git.Head != b.TargetCommit || c.Git.CWD != p.CWD {
			return progress, reviewError("closed target binding")
		}
		v, e := aggregateReviewBatch(tx, b)
		if e != nil {
			return progress, e
		}
		if !reflect.DeepEqual(v, c.View) {
			return progress, reviewError("closure evidence changed")
		}
		edges, statuses, e := ReviewClosureEdges(v, c.Request)
		if e != nil {
			return progress, e
		}
		if e = verifyMechanicalEvidence(v, c.Request, c.Git); e != nil {
			return progress, e
		}
		if !reflect.DeepEqual(edges, c.Git.Edges) || !reflect.DeepEqual(statuses, c.RoleStatuses) {
			return progress, reviewError("closed conclusion mismatch")
		}
		if _, e = time.Parse(time.RFC3339Nano, c.ClosedAt); e != nil {
			return progress, reviewError("closed timestamp")
		}
		if _, e = time.Parse(time.RFC3339Nano, c.Git.VerifiedAt); e != nil {
			return progress, reviewError("Git timestamp")
		}
		if e = verifyClosureCopies(tx, c); e != nil {
			return progress, e
		}
		previous = b.TargetCommit
	}
	progress.Status = "closed"
	if len(progress.Pending) > 0 {
		progress.Status = "pending"
		if completion || s.Entry.State == "done" {
			return progress, reviewError("unclosed review batches: " + strings.Join(progress.Pending, ","))
		}
	}
	return progress, nil
}
func checkPendingDispositions(tx *Transaction, b ReviewBatch) error {
	runs, err := batchRuns(tx, b)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.Phase != "finalized" || !allPublished(run) {
			continue
		}
		if err = verifyPublishedReview(tx, run); err != nil {
			return err
		}
		if run.ExecutionStatus != "ok" {
			continue
		}
		f, err := runFindings(tx, run)
		if err != nil {
			return err
		}
		if err = validateFindingLineage(tx, run, f); err != nil {
			return err
		}
		if _, err = readDispositionLedger(tx, run, false); err != nil {
			return err
		}
	}
	return nil
}
func ReviewTaskProgress(root, id string) (p ReviewProgress, err error) {
	scope, err := reviewGateScope(root, id, false)
	if err != nil {
		return p, err
	}
	scope.ReadOnly = true
	err = WithTransaction(root, scope, func(tx *Transaction) error { var e error; p, e = validateTaskReview(tx, id, false); return e })
	return
}

// CheckReviewGate reports structural damage, while pending conclusions remain
// ordinary execution progress. The exact same validator enforces move done.
func CheckReviewGate(root string, ids []string) []Problem {
	var problems []Problem
	for _, id := range ids {
		if _, err := ReviewTaskProgress(root, id); err != nil {
			problems = append(problems, Problem{Path: id, Message: err.Error()})
		}
	}
	return problems
}

func checkUnplannedReviewRuns(tx *Transaction, p ReviewPlan) error {
	planned := map[string]bool{}
	for _, b := range p.Batches {
		planned[b.BatchID] = true
	}
	entries, err := fs.ListDirectory(tx.root, control(tx.root, "groups", reviewControlGroup, "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !ValidReviewID(entry.Name) || entry.Kind != fs.KindDirectory {
			return reviewError("invalid run entry")
		}
		var run ReviewRun
		ok, e := readReviewJSON(tx, reviewRunName(entry.Name), &run)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing run")
		}
		for _, id := range p.TaskIDs {
			if containsID(run.TaskIDs, id) && !planned[run.BatchID] {
				return reviewError("unplanned execution batch: " + run.BatchID)
			}
		}
	}
	return nil
}
