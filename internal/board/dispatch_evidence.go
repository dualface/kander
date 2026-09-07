package board

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// DispatchAuthorReference preserves the original author and producer-owned ID.
type DispatchAuthorReference struct {
	Finding  FindingRef        `json:"finding"`
	RecordID string            `json:"record_id"`
	Author   string            `json:"author"`
	Artifact ArtifactReference `json:"artifact"`
}

type DispatchFindingReference struct {
	FindingRef
	PreviousRunID string `json:"previous_run_id,omitempty"`
}

type DispatchFixBinding struct {
	BatchID  string                     `json:"batch_id"`
	Findings []DispatchFindingReference `json:"findings"`
	Authors  []DispatchAuthorReference  `json:"authors,omitempty"`
}

// DispatchEvidence travels with the immutable intent. Paths always name artifacts
// relative to task IDs; moving a card never changes an evidence identity.
type DispatchEvidence struct {
	Fix    *DispatchFixBinding    `json:"fix,omitempty"`
	WrapUp *DispatchWrapUpBinding `json:"wrap_up,omitempty"`
}

func dispatchEvidenceError(detail string) error {
	return kanbanError("board.dispatch_evidence_invalid", detail)
}

func dispatchEvidenceScope(root string, in DispatchInput) (LockScope, error) {
	scope := LockScope{Tasks: []string{in.TaskID}, Groups: []string{dispatchRegistry, reviewControlGroup}}
	if in.Evidence.Fix != nil {
		b, err := ReadReviewBatch(root, in.Evidence.Fix.BatchID)
		if err != nil {
			return scope, err
		}
		scope.Tasks = append(scope.Tasks, b.TaskIDs...)
	}
	if in.Evidence.WrapUp != nil {
		review, err := reviewGateScope(root, in.TaskID, false)
		if err != nil {
			return scope, err
		}
		scope.Tasks = append(scope.Tasks, review.Tasks...)
	}
	slices.Sort(scope.Tasks)
	scope.Tasks = slices.Compact(scope.Tasks)
	return scope, nil
}

func validateDispatchEvidence(tx *Transaction, in DispatchInput, initial bool) error {
	switch in.Kind {
	case "fix":
		if in.Evidence.Fix == nil || in.Evidence.WrapUp != nil {
			return dispatchEvidenceError("fix binding required")
		}
		return validateDispatchFix(tx, in, initial)
	case "wrap-up":
		if in.Evidence.WrapUp == nil || in.Evidence.Fix != nil {
			return dispatchEvidenceError("wrap-up binding required")
		}
		return validateDispatchWrapUp(tx, in, !initial)
	case "sync":
		if in.Evidence != (DispatchEvidence{}) {
			return dispatchEvidenceError("sync cannot carry fix/wrap-up evidence")
		}
	}
	return nil
}

func validateDispatchFix(tx *Transaction, in DispatchInput, initial bool, historical ...bool) error {
	binding := in.Evidence.Fix
	if binding == nil {
		return dispatchEvidenceError("fix binding required")
	}
	completed := len(historical) > 0 && historical[0]
	var batch ReviewBatch
	ok, err := readReviewJSON(tx, reviewBatchName(binding.BatchID), &batch)
	if err != nil {
		return err
	}
	if !ok || batch.BatchID != binding.BatchID || !completed && batch.TargetCommit != in.Base || !containsID(batch.TaskIDs, in.TaskID) || len(binding.Findings) == 0 {
		return dispatchEvidenceError("batch, target or membership")
	}
	runs, err := batchRuns(tx, batch)
	if err != nil {
		return err
	}
	expectedAuthors := map[string]DispatchAuthorReference{}
	seen := map[string]bool{}
	for _, ref := range binding.Findings {
		if seen[ref.key()] {
			return dispatchEvidenceError("duplicate finding")
		}
		seen[ref.key()] = true
		readRun := reviewRunForMutation
		if completed {
			readRun = reviewRunForConsumption
		}
		run, err := readRun(tx, ref.RunID)
		if err != nil {
			return err
		}
		if run.BatchID != binding.BatchID || run.Commit != in.Base || run.PreviousRunID != ref.PreviousRunID || !containsID(run.TaskIDs, in.TaskID) {
			return dispatchEvidenceError("run, predecessor or task binding")
		}
		for _, other := range runs {
			if !completed && other.PreviousRunID == run.RunID {
				return dispatchEvidenceError("superseded review round")
			}
		}
		if err = collectDispatchAuthors(tx, run, ref.FindingID, in.TaskID, expectedAuthors, map[string]bool{}); err != nil {
			return err
		}
	}
	supplied := map[string]bool{}
	for _, ref := range binding.Authors {
		key := ref.Finding.key() + "/" + ref.RecordID
		if supplied[key] || !validReference(ref.Artifact) {
			return dispatchEvidenceError("author reference")
		}
		supplied[key] = true
		expected, ok := expectedAuthors[key]
		if !ok || !reflect.DeepEqual(ref, expected) {
			return dispatchEvidenceError("author identity, artifact or lineage")
		}
	}
	// Later author revisions must not rewrite a dispatch's original binding.
	// On creation, every existing original on this finding lineage is included.
	if initial && len(supplied) != len(expectedAuthors) {
		return dispatchEvidenceError("existing author originals must be referenced")
	}
	return nil
}

func collectDispatchAuthors(tx *Transaction, run ReviewRun, findingID, task string, refs map[string]DispatchAuthorReference, seen map[string]bool) error {
	key := run.RunID + "/" + findingID
	if seen[key] {
		return dispatchEvidenceError("finding lineage cycle")
	}
	seen[key] = true
	if err := verifyPublishedReview(tx, run); err != nil {
		return err
	}
	findings, err := runFindings(tx, run)
	if err != nil {
		return err
	}
	if err = validateFindingLineage(tx, run, findings); err != nil {
		return err
	}
	ledger, err := readDispositionLedger(tx, run, false)
	if err != nil {
		return err
	}
	if !containsID(ledger.Assignment.Items[findingID], task) {
		return dispatchEvidenceError("finding not assigned to task")
	}
	var item *ReviewFinding
	for _, f := range findings.Findings {
		if f.ID == findingID {
			copy := f
			item = &copy
		}
	}
	if item == nil {
		return dispatchEvidenceError("missing or non-blocking finding")
	}
	for _, record := range ledger.Records {
		if record.TaskID != task || record.FindingID != findingID {
			continue
		}
		refs[key+"/"+record.RecordID] = DispatchAuthorReference{Finding: FindingRef{run.RunID, findingID}, RecordID: record.RecordID, Author: record.Author, Artifact: ArtifactReference{task, dispositionPath(record)}}
	}
	if item.Lineage != nil {
		var previous ReviewRun
		ok, e := readReviewJSON(tx, reviewRunName(item.Lineage.RunID), &previous)
		if e != nil {
			return e
		}
		if !ok || previous.BatchID != run.BatchID || previous.Role != run.Role || !slices.Equal(previous.TaskIDs, run.TaskIDs) {
			return dispatchEvidenceError("foreign predecessor")
		}
		return collectDispatchAuthors(tx, previous, item.Lineage.FindingID, task, refs, seen)
	}
	return nil
}

// ValidateDispatchEvidence re-resolves every producer original before transport.
// ReadDispatch alone remains usable for historical unbound intents and receipts.
func ValidateDispatchEvidence(root, task, id string) error {
	d, err := ReadDispatch(root, task, id)
	if err != nil {
		return err
	}
	scope, err := dispatchEvidenceScope(root, d.Input)
	if err != nil {
		return err
	}
	scope.ReadOnly = true
	return WithTransaction(root, scope, func(tx *Transaction) error {
		current, err := readDispatch(tx, task, id)
		if err != nil {
			return err
		}
		return validateDispatchEvidence(tx, current.Input, false)
	})
}

// DispatchEvidenceInstruction renders identifiers only, never substitutes report prose.
func DispatchEvidenceInstruction(d Dispatch) string {
	var lines []string
	if f := d.Input.Evidence.Fix; f != nil {
		lines = append(lines, "BATCH_ID: "+f.BatchID)
		for _, item := range f.Findings {
			lines = append(lines, fmt.Sprintf("FINDING: %s/%s; TASK_ID: %s; REPORT: reviews/%s/report.md", item.RunID, item.FindingID, d.Input.TaskID, item.RunID))
		}
		for _, a := range f.Authors {
			lines = append(lines, fmt.Sprintf("AUTHOR: %s; TASK_ID: %s; ARTIFACT: %s", a.Author, a.Artifact.TaskID, a.Artifact.Path))
		}
	}
	if w := d.Input.Evidence.WrapUp; w != nil {
		lines = append(lines, "INTEGRATION: "+w.Artifact.Path)
	}
	return strings.Join(lines, "\n")
}
