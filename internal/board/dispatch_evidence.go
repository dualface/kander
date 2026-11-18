package board

import (
	"fmt"
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
	// Section is a read-only stamp of FINDINGS or NON_BLOCKING from the
	// published report. Callers may omit it; prepare fills it in.
	Section string `json:"section,omitempty"`
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
	scope := LockScope{Tasks: []string{in.TaskID}, Groups: []string{dispatchRegistry, reviewControlGroup, taskStartGroup}}
	if in.Evidence.Fix != nil {
		members, err := fixBatchMembers(root, in.Evidence.Fix.BatchID)
		if err != nil {
			return scope, err
		}
		scope.Tasks = append(scope.Tasks, members...)
	}
	if in.Evidence.WrapUp != nil {
		review, err := reviewGateScope(root, in.TaskID, false)
		if err != nil {
			return scope, err
		}
		scope.Tasks = append(scope.Tasks, review.Tasks...)
		card, err := ReadSnapshot(root, in.TaskID)
		if err != nil {
			return scope, err
		}
		if group := TaskGroupFrom(card.Text); group != "" {
			scope.Groups = append(scope.Groups, group)
		}
	}
	slices.Sort(scope.Tasks)
	scope.Tasks = slices.Compact(scope.Tasks)
	return scope, nil
}

// refuseBlockedWrapUp keeps a group from ending while the coordinator has not
// accepted one of its members. dispatchEvidenceScope locks the group of a wrap-up.
func refuseBlockedWrapUp(tx *Transaction, in DispatchInput) error {
	if in.Evidence.WrapUp == nil {
		return nil
	}
	s, err := tx.Snapshot(in.TaskID)
	if err != nil {
		return err
	}
	group := TaskGroupFrom(s.Text)
	if group == "" {
		return nil
	}
	c, exists, err := readCheckpoint(tx, group)
	if err != nil || !exists {
		return err
	}
	if wrapUpBlocked(c) {
		return kanbanError("board.wrap_up_blocked_members", group, strings.Join(c.BlockedMembers(), ", "))
	}
	return nil
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
		return validateDispatchWrapUp(tx, in, !initial, false)
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
	stamped, problems, err := collectDispatchFix(tx, in, initial, completed)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return dispatchProblemsError(problems, "")
	}
	if initial {
		binding.Findings = stamped
	}
	return nil
}

func findingSection(findings ReviewFindings, id string) (ReviewFinding, string, bool) {
	for _, f := range findings.Findings {
		if f.ID == id {
			return f, "FINDINGS", true
		}
	}
	for _, f := range findings.NonBlocking {
		if f.ID == id {
			return f, "NON_BLOCKING", true
		}
	}
	return ReviewFinding{}, "", false
}

// collectDispatchAuthors gathers the author originals of one finding and its
// lineage. A failure of the evidence returns as the problem line of the first
// lineage step that fails, since the later steps depend on it. origin is the
// finding as the evidence names it; a lineage step names itself next to it.
func collectDispatchAuthors(tx *Transaction, run ReviewRun, findingID, task, origin string, refs map[string]DispatchAuthorReference, seen map[string]bool) (section, problem string, err error) {
	key := run.RunID + "/" + findingID
	label := origin
	if key != origin {
		label = t("board.dispatch_problem_lineage_label", origin, key)
	}
	fail := func(e error) (string, string, error) {
		if line, ok := evidenceProblem(e); ok {
			return "", label + ": " + line, nil
		}
		return "", "", e
	}
	if seen[key] {
		return "", t("board.dispatch_problem_lineage_cycle", label), nil
	}
	seen[key] = true
	if err = verifyPublishedReview(tx, run); err != nil {
		return fail(err)
	}
	findings, err := runFindings(tx, run)
	if err != nil {
		return fail(err)
	}
	if err = validateFindingLineage(tx, run, findings); err != nil {
		return fail(err)
	}
	item, section, ok := findingSection(findings, findingID)
	if !ok {
		return "", t("board.dispatch_problem_finding_missing", label), nil
	}
	ledger, err := readDispositionLedger(tx, run, false)
	if err != nil {
		return fail(err)
	}
	if assigned := ledger.Assignment.Items[findingID]; !containsID(assigned, task) {
		who := t("board.dispatch_problem_nobody")
		if len(assigned) > 0 {
			who = strings.Join(assigned, ", ")
		}
		return "", t("board.dispatch_problem_finding_unassigned", label, task, who), nil
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
			return fail(e)
		}
		if !ok {
			return "", t("board.dispatch_problem_lineage_run_missing", label, item.Lineage.key()), nil
		}
		if previous.BatchID != run.BatchID || previous.Role != run.Role || !slices.Equal(previous.TaskIDs, run.TaskIDs) {
			return "", t("board.dispatch_problem_lineage_foreign", label, item.Lineage.key()), nil
		}
		if _, problem, e = collectDispatchAuthors(tx, previous, item.Lineage.FindingID, task, origin, refs, seen); e != nil || problem != "" {
			return "", problem, e
		}
	}
	return section, "", nil
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
			label := "FINDING"
			if item.Section == "NON_BLOCKING" {
				label = "NON_BLOCKING"
			}
			lines = append(lines, fmt.Sprintf("%s: %s/%s; TASK_ID: %s; REPORT: reviews/%s/report.md", label, item.RunID, item.FindingID, d.Input.TaskID, item.RunID))
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
