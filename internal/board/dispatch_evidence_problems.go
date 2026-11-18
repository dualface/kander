package board

import (
	"cmp"
	"errors"
	"slices"
	"strings"
)

// evidenceProblem tells a failure of the evidence itself from any other error.
// Only the first becomes a line of the problem list; I/O, lock conflicts and
// cancellation stay errors, so a retry or an internal failure is never stored
// as a refusal of the evidence.
func evidenceProblem(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) && (e.Code == "board.review_evidence_invalid" || e.Code == "board.dispatch_evidence_invalid") {
		return err.Error(), true
	}
	return "", false
}

// dispatchProblemsError lists every problem of one fix binding, one per line.
// next, when set, is the closing line that tells the sender how to send again.
func dispatchProblemsError(problems []string, next string) error {
	var b strings.Builder
	b.WriteString(t("board.dispatch_problem_header", len(problems)))
	for _, line := range problems {
		b.WriteString("\n- " + line)
	}
	if next != "" {
		b.WriteString("\n" + next)
	}
	return dispatchEvidenceError(b.String())
}

// CheckDispatchFixProposal runs the semantic check of a fix binding when the
// server creates the proposal that carries it, so the sender sees every problem
// at once. The binding is not stamped. The caller holds the scope of
// FixProposalScope. The prepare of the holder runs the same check again.
func CheckDispatchFixProposal(tx *Transaction, in DispatchInput) error {
	if in.Evidence.Fix == nil || in.Evidence.WrapUp != nil {
		return dispatchEvidenceError("fix binding required")
	}
	_, problems, err := collectDispatchFix(tx, in, true, false)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return dispatchProblemsError(problems, t("board.dispatch_problem_next_step"))
	}
	return nil
}

// fixBatchMembers lists the member cards of the batch a fix binding names. A
// batch that does not exist or cannot be read contributes no members: the
// check inside the transaction reports it as a problem.
func fixBatchMembers(root, batchID string) ([]string, error) {
	batch, err := ReadReviewBatch(root, batchID)
	if err != nil {
		if _, ok := evidenceProblem(err); ok {
			return nil, nil
		}
		return nil, err
	}
	return batch.TaskIDs, nil
}

// FixProposalScope is the lock scope CheckDispatchFixProposal reads under: the
// review control group and the member cards of the batch. The caller adds its
// own groups and cards.
func FixProposalScope(root string, fix *DispatchFixBinding) (LockScope, error) {
	scope := LockScope{Groups: []string{reviewControlGroup}}
	members, err := fixBatchMembers(root, fix.BatchID)
	scope.Tasks = slices.Clone(members)
	return scope, err
}

// collectDispatchFix checks a fix binding and returns every problem it finds,
// in a fixed order: the batch, each finding in the order given, then the author
// references in the order given, then the author originals that are missing. The
// stamped findings carry the section of the published report; they are only
// meaningful when there is no problem.
//
// A problem of the evidence is a line of the list. Any other error aborts.
func collectDispatchFix(tx *Transaction, in DispatchInput, initial, completed bool) (stamped []DispatchFindingReference, problems []string, err error) {
	binding := in.Evidence.Fix
	batch, problem, err := readFixBatch(tx, binding.BatchID)
	if err != nil || problem != "" {
		if problem != "" {
			problems = []string{problem}
		}
		return nil, problems, err
	}
	if !completed && batch.TargetCommit != in.Base {
		problems = append(problems, t("board.dispatch_problem_base_not_target", in.Base, batch.TargetCommit, batch.BatchID))
	}
	if !containsID(batch.TaskIDs, in.TaskID) {
		problems = append(problems, t("board.dispatch_problem_not_member", in.TaskID, batch.BatchID, strings.Join(batch.TaskIDs, ", ")))
	}
	if len(binding.Findings) == 0 {
		problems = append(problems, t("board.dispatch_problem_findings_empty"))
	}
	targets, err := reviewBatchTargets(batch)
	if err != nil {
		line, ok := evidenceProblem(err)
		if !ok {
			return nil, nil, err
		}
		problems = append(problems, line)
	}
	runs, err := batchRuns(tx, batch)
	if err != nil {
		line, ok := evidenceProblem(err)
		if !ok {
			return nil, nil, err
		}
		problems = append(problems, line)
	}
	readRun := reviewRunForMutation
	if completed {
		readRun = reviewRunForConsumption
	}
	check := fixRunCheck{in: in, targets: targets, runs: runs, completed: completed, baseOff: !completed && batch.TargetCommit != in.Base}
	expectedAuthors := map[string]DispatchAuthorReference{}
	seen := map[string]bool{}
	stamped = slices.Clone(binding.Findings)
	failed := 0
	for i, ref := range stamped {
		before := len(problems)
		label := ref.key()
		if seen[label] {
			problems = append(problems, t("board.dispatch_problem_finding_duplicate", label))
			failed++
			continue
		}
		seen[label] = true
		run, err := readRun(tx, ref.RunID)
		if err != nil {
			line, ok := evidenceProblem(err)
			if !ok {
				return nil, nil, err
			}
			problems = append(problems, label+": "+line)
			failed++
			continue
		}
		problems = append(problems, check.problems(run, ref)...)
		section, problem, err := collectDispatchAuthors(tx, run, ref.FindingID, in.TaskID, label, expectedAuthors, map[string]bool{})
		switch {
		case err != nil:
			return nil, nil, err
		case problem != "":
			problems = append(problems, problem)
		case ref.Section != "" && ref.Section != section:
			problems = append(problems, t("board.dispatch_problem_section_mismatch", label, ref.Section, section))
		default:
			stamped[i].Section = section
		}
		if len(problems) > before {
			failed++
		}
	}
	return stamped, append(problems, authorProblems(binding.Authors, expectedAuthors, failed, initial)...), nil
}

// readFixBatch reads the batch record a fix binding names. A batch that cannot
// carry the rest of the check returns a problem line and no batch.
func readFixBatch(tx *Transaction, batchID string) (batch ReviewBatch, problem string, err error) {
	found := false
	if ValidReviewID(batchID) {
		if found, err = readReviewJSON(tx, reviewBatchName(batchID), &batch); err != nil {
			if line, ok := evidenceProblem(err); ok {
				return batch, line, nil
			}
			return batch, "", err
		}
	}
	switch {
	case !found:
		return batch, t("board.dispatch_problem_batch_missing", batchID), nil
	case batch.BatchID != batchID:
		return batch, t("board.dispatch_problem_batch_id_mismatch", batchID, batch.BatchID), nil
	}
	return batch, "", nil
}

// fixRunCheck holds what the check of every finding's run shares: the chain of
// targets the batch advanced through (empty when it could not be read, which the
// caller already reported) and the runs of the batch.
type fixRunCheck struct {
	in        DispatchInput
	targets   []string
	runs      []ReviewRun
	completed bool
	// baseOff is set when the base is not the target of the batch; the batch
	// problem says so once, and no finding repeats it.
	baseOff bool
}

// problems checks one finding's run against the batch chain, the predecessor
// and the task.
func (c fixRunCheck) problems(run ReviewRun, ref DispatchFindingReference) (problems []string) {
	label := ref.key()
	batchID := c.in.Evidence.Fix.BatchID
	if run.BatchID != batchID {
		problems = append(problems, t("board.dispatch_problem_run_other_batch", label, run.BatchID, batchID))
	}
	// A batch advanced before this round leaves the run behind the base; the
	// recorded chain still ties the two together, also on historical replay.
	if len(c.targets) > 0 {
		if commitAt := slices.Index(c.targets, run.Commit); commitAt < 0 {
			problems = append(problems, t("board.dispatch_problem_run_off_chain", label, run.Commit, strings.Join(c.targets, ", ")))
		} else if !c.baseOff && !slices.Contains(c.targets[commitAt:], c.in.Base) {
			problems = append(problems, t("board.dispatch_problem_base_before_run", label, c.in.Base, run.Commit))
		}
	}
	if run.PreviousRunID != ref.PreviousRunID {
		problems = append(problems, t("board.dispatch_problem_previous_run_mismatch", label, ref.PreviousRunID, run.PreviousRunID))
	}
	if !containsID(run.TaskIDs, c.in.TaskID) {
		problems = append(problems, t("board.dispatch_problem_task_not_in_run", label, c.in.TaskID, strings.Join(run.TaskIDs, ", ")))
	}
	var successors []string
	for _, other := range c.runs {
		if !c.completed && other.PreviousRunID == run.RunID {
			successors = append(successors, other.RunID)
		}
	}
	if len(successors) > 0 {
		problems = append(problems, t("board.dispatch_problem_superseded", label, strings.Join(successors, ", ")))
	}
	return problems
}

// authorProblems checks the supplied author references. Structural problems
// (a repeated key, a bad artifact) are always reported. The comparison with the
// originals the findings expect only makes sense once every finding passed; after
// a failed finding the expected set is incomplete, and one line says so. Later
// author revisions must not rewrite a dispatch's original binding, so on creation
// every existing original on the finding lineage must be referenced.
func authorProblems(authors []DispatchAuthorReference, expected map[string]DispatchAuthorReference, failed int, initial bool) (problems []string) {
	mentioned := map[string]bool{}
	for _, ref := range authors {
		key := ref.Finding.key() + "/" + ref.RecordID
		if mentioned[key] {
			problems = append(problems, t("board.dispatch_problem_author_duplicate", key))
			continue
		}
		mentioned[key] = true
		if !validReference(ref.Artifact) {
			problems = append(problems, t("board.dispatch_problem_author_artifact_invalid", key, ref.Artifact.TaskID, ref.Artifact.Path))
			continue
		}
		if failed > 0 {
			continue
		}
		want, ok := expected[key]
		if !ok {
			problems = append(problems, t("board.dispatch_problem_author_not_in_lineage", key))
			continue
		}
		for _, field := range []struct{ name, got, want string }{
			{"author", ref.Author, want.Author},
			{"artifact.task_id", ref.Artifact.TaskID, want.Artifact.TaskID},
			{"artifact.path", ref.Artifact.Path, want.Artifact.Path},
		} {
			if field.got != field.want {
				problems = append(problems, t("board.dispatch_problem_author_field", key, field.name, field.got, field.want))
			}
		}
	}
	if failed > 0 {
		return append(problems, t("board.dispatch_problem_authors_not_compared", failed))
	}
	if !initial {
		return problems
	}
	var missing []DispatchAuthorReference
	for key, want := range expected {
		if !mentioned[key] {
			missing = append(missing, want)
		}
	}
	// The expected originals come from a map; sorting keeps the text of one
	// board state identical between runs.
	slices.SortFunc(missing, func(a, b DispatchAuthorReference) int {
		return cmp.Or(
			strings.Compare(a.Finding.RunID, b.Finding.RunID),
			strings.Compare(a.Finding.FindingID, b.Finding.FindingID),
			strings.Compare(a.RecordID, b.RecordID),
		)
	})
	for _, want := range missing {
		problems = append(problems, t("board.dispatch_problem_author_missing", want.Finding.RunID, want.Finding.FindingID, want.RecordID, want.Author, want.Artifact.Path))
	}
	return problems
}
