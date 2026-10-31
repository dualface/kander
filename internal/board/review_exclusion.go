package board

import (
	"fmt"
	"slices"
	"strings"
)

// ReviewExclusion is the producer record of a plan or task group member that left
// the flow. Only a move into archived or trash writes it, in the transaction of
// the move, and neither state has a way back, so it cannot be forged to escape a
// review. A card that reached done is not excluded when it is archived later.
type ReviewExclusion struct {
	Schema      int    `json:"schema"`
	ExclusionID string `json:"exclusion_id"`
	TaskID      string `json:"task_id"`
	PlanID      string `json:"plan_id,omitempty"`
	TaskGroup   string `json:"task_group,omitempty"`
	FromState   string `json:"from_state"`
	State       string `json:"state"`
	Result      string `json:"result"`
	Cycle       string `json:"cycle"`
	Reason      string `json:"reason"`
	Decision    string `json:"decision"`
	RecordedAt  string `json:"recorded_at"`
}

func exclusionName(id string) string { return "exclusions/" + id + ".json" }

func readMemberExclusion(tx *Transaction, id string) (e ReviewExclusion, exists bool, err error) {
	exists, err = readReviewJSON(tx, exclusionName(id), &e)
	if err != nil || !exists {
		return e, exists, err
	}
	if e.Schema != 1 || e.TaskID != id || e.ExclusionID == "" {
		return e, true, reviewError("invalid member exclusion: " + id)
	}
	return e, true, nil
}

func memberExcluded(tx *Transaction, id string) (bool, error) {
	_, exists, err := readMemberExclusion(tx, id)
	return exists, err
}

// stageMemberExclusion writes the record of a member that ends without done. The
// done-to-trash path writes one too: the card no longer counts as delivered.
func stageMemberExclusion(tx *Transaction, s Snapshot, target, text string, o MoveOptions) error {
	if target != "archived" && target != "trash" || target == "archived" && s.Entry.State == "done" {
		return nil
	}
	id := s.Entry.TaskID
	plan, planned, err := readTaskPlan(tx, id)
	if err != nil {
		return err
	}
	group := TaskGroupFrom(text)
	if !planned && group == "" {
		return nil
	}
	next := Snapshot{Entry: s.Entry, Text: text}
	return tx.PutGroup(reviewControlGroup, exclusionName(id), reviewJSON(ReviewExclusion{
		Schema: 1, ExclusionID: tx.record.ID, TaskID: id, PlanID: plan.PlanID, TaskGroup: group,
		FromState: s.Entry.State, State: target, Result: MetadataFrom(text, FieldResult), Cycle: planCycle(next),
		Reason: o.Reason, Decision: o.Decision, RecordedAt: nowStamp(),
	}))
}

// reviewCopyTargets lists the members that still take a card copy. An archived
// card is final, so it takes none even when it was never excluded; an excluded
// card takes none in trash either. Skipping a copy exempts a card from no gate.
// A later run names fewer members than its predecessor, so a caller may not hold
// the lock of an excluded member: the record is read first, from the control group.
func reviewCopyTargets(tx *Transaction, ids []string) ([]string, error) {
	targets := make([]string, 0, len(ids))
	for _, id := range ids {
		excluded, err := memberExcluded(tx, id)
		if err != nil {
			return nil, err
		}
		if excluded {
			continue
		}
		s, err := tx.Snapshot(id)
		if err != nil {
			return nil, err
		}
		if s.Entry.State != "archived" {
			targets = append(targets, id)
		}
	}
	return targets, nil
}

// voidReviewBatch reports a batch with no member left. No run can reach it, so it
// neither closes nor waits: the chain continues over it.
func voidReviewBatch(tx *Transaction, ids []string) (bool, error) {
	for _, id := range ids {
		excluded, err := memberExcluded(tx, id)
		if err != nil || !excluded {
			return false, err
		}
	}
	return len(ids) > 0, nil
}

// ReviewRemainingMembers returns ids without the members that left.
func ReviewRemainingMembers(root string, ids []string) (remaining []string, err error) {
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var e error
		remaining, e = withoutExcluded(tx, ids)
		return e
	})
	return remaining, err
}

func withoutExcluded(tx *Transaction, ids []string) ([]string, error) {
	remaining := make([]string, 0, len(ids))
	for _, id := range ids {
		excluded, err := memberExcluded(tx, id)
		if err != nil {
			return nil, err
		}
		if !excluded {
			remaining = append(remaining, id)
		}
	}
	return remaining, nil
}

// requireRunMembers keeps the batch member set fixed: a run names every member
// except the excluded ones, which can no longer take a review.
func requireRunMembers(tx *Transaction, batch ReviewBatch, ids []string) error {
	for _, id := range ids {
		if !containsID(batch.TaskIDs, id) {
			return reviewError("batch binding conflict: " + id + " is not a batch member")
		}
	}
	for _, id := range batch.TaskIDs {
		if containsID(ids, id) {
			continue
		}
		excluded, err := memberExcluded(tx, id)
		if err != nil {
			return err
		}
		if !excluded {
			return reviewError("batch binding conflict: the run must name member " + id)
		}
	}
	return nil
}

// requireNoStrayBatch refuses a batch outside the plan of a member before the batch
// exists. The done gate rejects such a run until extend-plan resolves its batch, and
// the batch can never join the chain of the plan.
func requireNoStrayBatch(tx *Transaction, ids []string, batchID string) error {
	for _, id := range ids {
		p, exists, err := readTaskPlan(tx, id)
		if err != nil {
			return err
		}
		if exists {
			return reviewError(t("board.review_batch_outside_plan", batchID, id, p.PlanID))
		}
	}
	return nil
}

// requireNoSuccessorBatch refuses an advance that a planned successor cannot
// follow: the base of a batch is immutable and must equal the closed target of its
// predecessor.
func requireNoSuccessorBatch(tx *Transaction, b ReviewBatch) error {
	if b.PlanID == "" {
		return nil
	}
	var p ReviewPlan
	ok, err := readReviewJSON(tx, planName(b.PlanID), &p)
	if err != nil {
		return err
	}
	if !ok {
		return reviewError("batch review plan required")
	}
	at := slices.IndexFunc(p.Batches, func(pb ReviewPlanBatch) bool { return pb.BatchID == b.BatchID })
	if at < 0 || at == len(p.Batches)-1 {
		return nil
	}
	successors := make([]string, 0, len(p.Batches)-at-1)
	for _, pb := range p.Batches[at+1:] {
		successors = append(successors, pb.BatchID)
	}
	return reviewError(t("board.review_advance_has_successor", b.BatchID, strings.Join(successors, ", ")))
}

// withdrawalsOwed lists the assigned findings of a batch that wait for a withdrawn
// record of an excluded member, as `withdrawal:<run>/<finding>/<task>`.
func withdrawalsOwed(tx *Transaction, b ReviewBatch) ([]string, error) {
	runs, err := batchRuns(tx, b)
	if err != nil {
		return nil, err
	}
	var owed []string
	for _, run := range runs {
		var ledger dispositionLedger
		exists, err := readReviewJSON(tx, ledgerName(run.RunID), &ledger)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		recorded := map[string]bool{}
		for _, d := range ledger.Records {
			recorded[dispositionKey(d)] = true
		}
		for _, finding := range sortedKeys(ledger.Assignment.Items) {
			for _, id := range ledger.Assignment.Items[finding] {
				excluded, err := memberExcluded(tx, id)
				if err != nil {
					return nil, err
				}
				if excluded && !recorded[finding+"/"+id] {
					owed = append(owed, fmt.Sprintf("withdrawal:%s/%s/%s", run.RunID, finding, id))
				}
			}
		}
	}
	return owed, nil
}

// withdrawnRecords returns the latest records that withdrew a finding. The view
// shows them apart from the author dispositions: a withdrawal is not a fix.
func withdrawnRecords(l dispositionLedger) []ReviewDisposition {
	var out []ReviewDisposition
	for _, d := range latestDispositions(l) {
		if d.Status == "withdrawn" {
			out = append(out, d)
		}
	}
	return out
}

// cycleBatchOwed reports a batch a sealed plan must still take: one of ids has a
// rebound cycle with no batch, or a recovery that needs an actual review.
func cycleBatchOwed(tx *Transaction, p ReviewPlan, ids []string) (bool, error) {
	for _, id := range ids {
		if cycleBatchMissing(p, id) {
			return true, nil
		}
		owed, err := recoveryReviewOwed(tx, p, id)
		if owed || err != nil {
			return owed, err
		}
	}
	return false, nil
}

// cycleBatchMissing reports a member whose rebound cycle has no batch. A batch from
// the recorded position on covers the cycle, so dropping it owes the batch again.
func cycleBatchMissing(p ReviewPlan, id string) bool {
	from, ok := p.CycleBatchFrom[id]
	if !ok {
		return false
	}
	for i, b := range p.Batches {
		if i >= from && containsID(b.TaskIDs, id) {
			return false
		}
	}
	return true
}

// cycleBatchStart returns the position from which a batch reviews the new cycle of
// a member: its first open batch, or the end of the plan when every batch of the
// member closed. A rebind alone must not pass done. A member of no batch has no
// closed review to reuse, and the seal covers it. An open batch may already hold a
// run of the old cycle; requireCurrentConclusions keeps it from closing on that run.
func cycleBatchStart(tx *Transaction, p ReviewPlan, id string) (int, bool, error) {
	found := false
	for i, b := range p.Batches {
		if !containsID(b.TaskIDs, id) {
			continue
		}
		found = true
		var c ReviewClosure
		closed, err := readReviewJSON(tx, closureName(b.BatchID), &c)
		if err != nil {
			return 0, false, err
		}
		if !closed {
			return i, true, nil
		}
	}
	return len(p.Batches), found, nil
}

// putPlanCopies publishes the plan to every member that still takes a copy.
func putPlanCopies(tx *Transaction, p ReviewPlan) error {
	targets, err := reviewCopyTargets(tx, p.TaskIDs)
	if err != nil {
		return err
	}
	for _, id := range targets {
		if err = tx.Put(id, "reviews/plan.json", reviewJSON(p)); err != nil {
			return err
		}
	}
	return nil
}

// dropPlanBatches removes a trailing run of batches no run has reached, so a
// predecessor can advance. The batch records stay, so a dropped ID cannot come
// back with another base, and the plan reopens: only a later seal ends it. A dropped
// required batch stays owed unless a user decision comes with the drop.
func dropPlanBatches(tx *Transaction, p *ReviewPlan, ids []string, decision string) error {
	if len(ids) >= len(p.Batches) {
		return reviewError("drop_batches must leave one batch")
	}
	tail := p.Batches[len(p.Batches)-len(ids):]
	for i, pb := range tail {
		if pb.BatchID != ids[i] {
			return reviewError("drop_batches must name the trailing batches in plan order")
		}
		var b ReviewBatch
		ok, err := readReviewJSON(tx, reviewBatchName(pb.BatchID), &b)
		if err != nil {
			return err
		}
		if !ok {
			return reviewError("planned batch missing")
		}
		runs, err := batchRuns(tx, b)
		if err != nil {
			return err
		}
		var c ReviewClosure
		closed, err := readReviewJSON(tx, closureName(pb.BatchID), &c)
		if err != nil {
			return err
		}
		if closed || len(runs) > 0 || len(b.Advances) > 0 {
			return reviewError("drop_batches accepts only a batch with no run: " + pb.BatchID)
		}
	}
	keep := len(p.Batches) - len(ids)
	if err := keepDropObligations(tx, p, tail, keep, decision); err != nil {
		return err
	}
	droppedRoles := planRequiredRoles(ReviewPlan{Batches: tail})
	p.Batches = slices.Clone(p.Batches[:keep])
	p.Sealed = false
	// The next batch lands at the new end, so a cycle owed past it must accept it.
	for id, from := range p.CycleBatchFrom {
		p.CycleBatchFrom[id] = min(from, len(p.Batches))
	}
	return moveRecoveryReviews(tx, *p, droppedRoles)
}
