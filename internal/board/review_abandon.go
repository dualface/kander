package board

import (
	"reflect"
	"strings"
	"time"

	"github.com/dualface/kander/internal/statemachine/reviewrun"
)

// ReviewAbandonRequest retires a finalized run that no process can publish any
// more. Decision is the user decision reference; the tool records it and does not
// verify it.
type ReviewAbandonRequest struct {
	RunID    string `json:"run_id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// ReviewAbandonment is the record an abandon writes into the run. omitempty on
// the run keeps runs written before the field byte-identical on re-encode.
type ReviewAbandonment struct {
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	AbandonedAt string `json:"abandoned_at"`
}

// ReviewAbandonedRun is the audit line of an abandoned run in a batch view and
// in the closure that keeps the view.
type ReviewAbandonedRun struct {
	RunID    string `json:"run_id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// reviewRunAbandoned reports that the run was abandoned. The kernel answers, so
// no reader decides on the record field itself.
func reviewRunAbandoned(run ReviewRun) bool {
	return reviewrun.Abandoned(projectReviewRun(run, true, ""), reviewrun.Params{})
}

// RequireReviewRunNotAbandoned refuses an operation that needs a run that is not
// abandoned, with the error that names the run.
func RequireReviewRunNotAbandoned(run ReviewRun) error {
	if reviewRunAbandoned(run) {
		return abandonedRunError(run.RunID)
	}
	return nil
}

// withoutAbandonedRuns drops the runs an abandon retired from a batch's runs: an
// abandoned run is no longer a run of its batch.
func withoutAbandonedRuns(runs []ReviewRun) []ReviewRun {
	kept := make([]ReviewRun, 0, len(runs))
	for _, run := range runs {
		if !reviewRunAbandoned(run) {
			kept = append(kept, run)
		}
	}
	return kept
}

// abandonedRunError is the refusal of an operation that needs a run that is not
// abandoned.
func abandonedRunError(runID string) error {
	return reviewError(runID + ": " + t("board.review_run_abandoned"))
}

// incompletePublication is the error of a run that holds members without a
// copy. A finalized run can be published again or abandoned, so the error says
// so; a run that is not finalized has neither way out.
func incompletePublication(run ReviewRun) error {
	detail := run.RunID + ": incomplete publication"
	if run.Phase == "finalized" {
		detail += "; " + t("board.review_abandon_hint")
	}
	return reviewError(detail)
}

// liveReviewMember reports that the member card is in working or review and has
// not left the batch.
func liveReviewMember(tx *Transaction, id string) (bool, error) {
	s, err := tx.Snapshot(id)
	if err != nil {
		return false, err
	}
	if s.Entry.State != "working" && s.Entry.State != "review" {
		return false, nil
	}
	excluded, err := memberExcluded(tx, id)
	return !excluded, err
}

// abandonFacts are the integrity facts the kernel takes for an abandon, computed
// from the records of one transaction.
func abandonFacts(tx *Transaction, request ReviewAbandonRequest, before, current ReviewRun) (reviewrun.Abandon, error) {
	facts := reviewrun.Abandon{
		SameInput: current.RunID == request.RunID && reflect.DeepEqual(before.ReviewInput, current.ReviewInput),
		GroupOk:   true,
		Decision:  strings.TrimSpace(request.Decision) != "" && strings.TrimSpace(request.Reason) != "",
		NoCopies:  true,
	}
	for _, id := range current.TaskIDs {
		s, err := tx.Snapshot(id)
		if err != nil {
			return facts, err
		}
		if TaskGroupFrom(s.Text) != current.TaskGroup {
			facts.GroupOk = false
		}
		live, err := liveReviewMember(tx, id)
		if err != nil {
			return facts, err
		}
		facts.Live = facts.Live || live
		if current.Published[id] && !containsID(current.SkippedMembers, id) {
			facts.NoCopies = false
		}
	}
	closed, err := reviewBatchClosed(tx, current)
	if err != nil {
		return facts, err
	}
	facts.BatchOpen = !closed
	runs, err := batchRuns(tx, ReviewBatch{BatchID: current.BatchID})
	if err != nil {
		return facts, err
	}
	facts.NoSuccessor = true
	for _, run := range runs {
		if run.PreviousRunID == current.RunID {
			facts.NoSuccessor = false
		}
	}
	return facts, nil
}

// abandonRefusal names the first thing that made the kernel refuse an abandon of
// the member state: the state guards first, then the facts in a fixed order, each
// judged with every other fact holding.
func abandonRefusal(state reviewrun.State, facts reviewrun.Abandon, runID string, successors []string) error {
	holds := reviewrun.Abandon{SameInput: true, GroupOk: true, Decision: true, Live: true, NoCopies: true, BatchOpen: true, NoSuccessor: true}
	allows := func(e reviewrun.Abandon) bool { _, ok := stepReviewRun(state, e); return ok }
	switch {
	case allows(holds):
		// The state allows the abandon; a fact decides below.
	case state.Phase != reviewrun.PhaseFinalized:
		return reviewError(runID + ": " + t("board.review_abandon_unfinished"))
	case state.Abandoned:
		return abandonedRunError(runID)
	default:
		return reviewError(runID + ": " + t("board.review_abandon_has_copies"))
	}
	probe := func(set func(*reviewrun.Abandon, bool), value bool, refusal error) error {
		e := holds
		set(&e, value)
		if !allows(e) {
			return refusal
		}
		return nil
	}
	for _, check := range []struct {
		set     func(*reviewrun.Abandon, bool)
		value   bool
		refusal error
	}{
		{func(e *reviewrun.Abandon, v bool) { e.SameInput = v }, facts.SameInput, reviewError(runID + ": " + t("board.review_abandon_input_changed"))},
		{func(e *reviewrun.Abandon, v bool) { e.GroupOk = v }, facts.GroupOk, reviewError(runID + ": " + t("board.review_abandon_group_mismatch"))},
		{func(e *reviewrun.Abandon, v bool) { e.Decision = v }, facts.Decision, reviewError(runID + ": " + t("board.review_abandon_decision_required"))},
		{func(e *reviewrun.Abandon, v bool) { e.NoCopies = v }, facts.NoCopies, reviewError(runID + ": " + t("board.review_abandon_has_copies"))},
		{func(e *reviewrun.Abandon, v bool) { e.BatchOpen = v }, facts.BatchOpen, reviewError(runID + ": " + t("board.review_abandon_batch_closed"))},
		{func(e *reviewrun.Abandon, v bool) { e.NoSuccessor = v }, facts.NoSuccessor, reviewError(runID + ": " + t("board.review_abandon_successor", strings.Join(successors, ", ")))},
		{func(e *reviewrun.Abandon, v bool) { e.Live = v }, facts.Live, reviewError(runID + ": " + t("board.review_abandon_no_live"))},
	} {
		if err := probe(check.set, check.value, check.refusal); err != nil {
			return err
		}
	}
	return reviewError(runID + ": " + t("board.review_abandon_input_changed"))
}

// AbandonReviewRunWithTransaction retires a finalized run that has members
// without a copy and that no process can publish any more. Every member of the
// run is marked published and skipped, so the run counts as settled; the run
// records the decision and the reason, leaves its batch, and can never be
// published again. Nothing is written into a member card: no copy, no index line.
// The same request again returns the first result and writes nothing.
func AbandonReviewRunWithTransaction(root string, request ReviewAbandonRequest, extensions ...TransactionExtension) error {
	before, exists, err := LookupReviewRun(root, request.RunID)
	if err != nil {
		return err
	}
	if !exists {
		return reviewError(request.RunID + ": " + t("board.review_abandon_missing_run"))
	}
	return WithTransaction(root, reviewScope(before.TaskIDs, false), func(tx *Transaction) error {
		var current ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(request.RunID), &current)
		if err != nil {
			return err
		}
		if !ok {
			return reviewError(request.RunID + ": " + t("board.review_abandon_missing_run"))
		}
		decision, reason := strings.TrimSpace(request.Decision), strings.TrimSpace(request.Reason)
		// The replay is judged before the facts, so a batch that closed or a member
		// that left after the abandon does not change the answer.
		if recorded := current.Abandonment; recorded != nil {
			if recorded.Decision != decision || recorded.Reason != reason {
				return reviewError(request.RunID + ": " + t("board.review_abandon_conflict"))
			}
			tx.SetOperationResult(map[string]string{"run_id": request.RunID})
			return nil
		}
		facts, err := abandonFacts(tx, request, before, current)
		if err != nil {
			return err
		}
		var successors []string
		if !facts.NoSuccessor {
			runs, err := batchRuns(tx, ReviewBatch{BatchID: current.BatchID})
			if err != nil {
				return err
			}
			for _, run := range runs {
				if run.PreviousRunID == current.RunID {
					successors = append(successors, run.RunID)
				}
			}
		}
		for _, id := range current.TaskIDs {
			state := projectReviewRun(current, true, id)
			if _, ok := stepReviewRun(state, facts); !ok {
				return abandonRefusal(state, facts, request.RunID, successors)
			}
		}
		if current.Published == nil {
			current.Published = map[string]bool{}
		}
		for _, id := range current.TaskIDs {
			current.Published[id] = true
			if !containsID(current.SkippedMembers, id) {
				current.SkippedMembers = append(current.SkippedMembers, id)
			}
		}
		current.Abandonment = &ReviewAbandonment{Decision: decision, Reason: reason, AbandonedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		tx.SetOperationResult(map[string]string{"run_id": request.RunID})
		return tx.PutGroup(reviewControlGroup, reviewRunName(request.RunID), reviewJSON(current))
	}, extensions...)
}
