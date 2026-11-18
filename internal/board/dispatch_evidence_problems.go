package board

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
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

// plain makes an identifier of the evidence safe to show in one line of the
// list: a control character would split the line or reach the terminal of the
// reader, so such an identifier is shown quoted.
func plain(s string) string {
	if strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) {
		return strconv.Quote(s)
	}
	return s
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
	next := t("board.dispatch_problem_next_step")
	if in.Evidence.Fix == nil || in.Evidence.WrapUp != nil {
		return dispatchProblemsError([]string{t("board.dispatch_problem_fix_binding")}, next)
	}
	_, problems, err := collectDispatchFix(tx, in, true, false)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return dispatchProblemsError(problems, next)
	}
	return nil
}

// readFixBatchOutside reads the batch a fix binding names before any
// transaction holds the cards of its members. It returns the problem line of a
// batch the check cannot go on with, and an error for a failure of the store.
func readFixBatchOutside(root, batchID string) (batch ReviewBatch, readable bool, problem string, err error) {
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) (e error) {
		batch, readable, problem, e = readFixBatch(tx, batchID)
		return e
	})
	return batch, readable, problem, err
}

// FixProposalScope is the lock scope CheckDispatchFixProposal reads under: the
// review control group and the member cards of the batch. The caller adds its
// own groups and cards. A batch the check cannot go on with, unreadable or
// stored under another batch_id, adds no member: the check inside the transaction
// reads it again and reports the problem or the failure at its place in the order
// of the checks of the creation, and the members such a record names may not be
// cards at all.
func FixProposalScope(root string, fix *DispatchFixBinding) LockScope {
	scope := LockScope{Groups: []string{reviewControlGroup}}
	if batch, readable, problem, err := readFixBatchOutside(root, fix.BatchID); readable && problem == "" && err == nil {
		scope.Tasks = slices.Clone(batch.TaskIDs)
	}
	return scope
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
	batch, _, problem, err := readFixBatch(tx, binding.BatchID)
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
	check := fixRunCheck{in: in, targets: targets, runs: runs, completed: completed}
	expectedAuthors := map[string]DispatchAuthorReference{}
	seen := map[string]bool{}
	stamped = slices.Clone(binding.Findings)
	failed := 0
	for i, ref := range stamped {
		before := len(problems)
		label := ref.key()
		if seen[label] {
			problems = append(problems, t("board.dispatch_problem_finding_duplicate", plain(label)))
			failed++
			continue
		}
		seen[label] = true
		// The run of another batch has members this transaction does not hold, so
		// its batch is read from the record alone, before the originals are.
		if line, err := check.earlyProblem(tx, ref); err != nil || line != "" {
			if err != nil {
				return nil, nil, err
			}
			problems = append(problems, line)
			failed++
			continue
		}
		run, err := readRun(tx, ref.RunID)
		if err != nil {
			line, ok := evidenceProblem(err)
			if !ok {
				return nil, nil, err
			}
			problems = append(problems, plain(label)+": "+line)
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
			problems = append(problems, t("board.dispatch_problem_section_mismatch", plain(label), ref.Section, section))
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
// carry the rest of the check returns a problem line. readable tells that the
// record exists and parses, also when its own batch_id is not the name it is
// stored under.
func readFixBatch(tx *Transaction, batchID string) (batch ReviewBatch, readable bool, problem string, err error) {
	if ValidReviewID(batchID) {
		if readable, err = readReviewJSON(tx, reviewBatchName(batchID), &batch); err != nil {
			if line, ok := evidenceProblem(err); ok {
				return batch, false, line, nil
			}
			return batch, false, "", err
		}
	}
	switch {
	case !readable:
		return batch, false, t("board.dispatch_problem_batch_missing", batchID), nil
	case batch.BatchID != batchID:
		return batch, true, t("board.dispatch_problem_batch_id_mismatch", batchID, batch.BatchID), nil
	}
	return batch, true, "", nil
}

// fixRunCheck holds what the check of every finding's run shares: the chain of
// targets the batch advanced through (empty when it could not be read, which the
// caller already reported) and the runs of the batch.
type fixRunCheck struct {
	in        DispatchInput
	targets   []string
	runs      []ReviewRun
	completed bool
}

// earlyProblem finds what makes the run a finding names unfit to be read like
// the others: an identifier that is no run ID, or a run of another batch, whose
// members this transaction does not hold. A run that does not exist or cannot be
// read is for the check of the run itself.
func (c fixRunCheck) earlyProblem(tx *Transaction, ref DispatchFindingReference) (string, error) {
	if !ValidReviewID(ref.RunID) {
		return plain(ref.key()) + ": " + reviewError("run_id").Error(), nil
	}
	var run ReviewRun
	found, err := readReviewJSON(tx, reviewRunName(ref.RunID), &run)
	if err != nil {
		if _, ok := evidenceProblem(err); ok {
			return "", nil
		}
		return "", err
	}
	batchID := c.in.Evidence.Fix.BatchID
	if found && run.BatchID != batchID {
		return t("board.dispatch_problem_run_other_batch", plain(ref.key()), run.BatchID, batchID), nil
	}
	return "", nil
}

// problems checks one finding's run against the batch chain, the predecessor
// and the task.
func (c fixRunCheck) problems(run ReviewRun, ref DispatchFindingReference) (problems []string) {
	label := plain(ref.key())
	// A batch advanced before this round leaves the run behind the base; the
	// recorded chain still ties the two together, also on historical replay.
	if len(c.targets) > 0 {
		if commitAt := slices.Index(c.targets, run.Commit); commitAt < 0 {
			problems = append(problems, t("board.dispatch_problem_run_off_chain", label, run.Commit, strings.Join(c.targets, ", ")))
		} else if !slices.Contains(c.targets[commitAt:], c.in.Base) {
			problems = append(problems, t("board.dispatch_problem_base_before_run", label, plain(c.in.Base), run.Commit))
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
		if !c.completed && other.PreviousRunID == run.RunID && !reviewRunAbandoned(other) {
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
		shown := plain(key)
		if mentioned[key] {
			problems = append(problems, t("board.dispatch_problem_author_duplicate", shown))
		}
		mentioned[key] = true
		if !validReference(ref.Artifact) {
			problems = append(problems, t("board.dispatch_problem_author_artifact_invalid", shown, ref.Artifact.TaskID, ref.Artifact.Path))
		}
		if failed > 0 {
			continue
		}
		want, ok := expected[key]
		if !ok {
			problems = append(problems, t("board.dispatch_problem_author_not_in_lineage", shown))
			continue
		}
		for _, field := range []struct{ name, got, want string }{
			{"author", ref.Author, want.Author},
			{"artifact.task_id", ref.Artifact.TaskID, want.Artifact.TaskID},
			{"artifact.path", ref.Artifact.Path, want.Artifact.Path},
		} {
			if field.got != field.want {
				problems = append(problems, t("board.dispatch_problem_author_field", shown, field.name, field.got, field.want))
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
