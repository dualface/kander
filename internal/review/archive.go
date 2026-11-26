package review

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/version"
)

type archiveOptions struct {
	tasks                                                     []string
	runID, batchID, previousID, requirementsFile, advanceFile string
}
type archiveExecution struct {
	store        reviewStore
	root         string
	run          board.ReviewRun
	staging      string
	report       []byte
	taskSnapshot []byte
	reason       string
	// processEvidence is true once this process stored the evidence in staging.
	processEvidence bool
}

func archiveError(detail string) error { return newGate(2, "review.archive_invalid", detail) }

// archiveFlags are the flags that bind a review to board cards: --task may repeat, the others
// are given once.
type archiveFlags struct {
	tasks                                                     []string
	runID, batchID, previousID, requirementsFile, advanceFile string
}

func (f *archiveFlags) register(c *cobra.Command) {
	c.Flags().StringArrayVar(&f.tasks, "task", nil, "")
	command.OnceNonEmpty(c.Flags(), &f.runID, "run-id", "")
	command.OnceNonEmpty(c.Flags(), &f.batchID, "batch-id", "")
	command.OnceNonEmpty(c.Flags(), &f.previousID, "previous-run-id", "")
	command.OnceNonEmpty(c.Flags(), &f.requirementsFile, "requirements-file", "")
	command.OnceNonEmpty(c.Flags(), &f.advanceFile, "advance-file", "")
}

// options checks the flags against each other and returns the archive options: any archive flag
// needs --task, and a task-bound review needs a batch and gets a run ID when it names none.
func (f archiveFlags) options(c *cobra.Command) (archiveOptions, error) {
	options := archiveOptions{runID: f.runID, batchID: f.batchID, previousID: f.previousID, requirementsFile: f.requirementsFile, advanceFile: f.advanceFile}
	for _, value := range f.tasks {
		id, err := board.NormalizeTaskID(value)
		if err != nil {
			return options, command.UsageText(err.Error())
		}
		if !slices.Contains(options.tasks, id) {
			options.tasks = append(options.tasks, id)
		}
	}
	if len(options.tasks) == 0 {
		for _, name := range []string{"run-id", "batch-id", "previous-run-id", "requirements-file", "advance-file"} {
			if c.Flags().Changed(name) {
				return options, command.UsageText(archiveError("--task required").Error())
			}
		}
		return options, nil
	}
	if options.batchID == "" {
		return options, command.UsageText(archiveError("--batch-id required").Error())
	}
	if options.runID == "" {
		var err error
		if options.runID, err = board.NewReviewID(); err != nil {
			return options, err
		}
	}
	for _, id := range []string{options.runID, options.batchID} {
		if !board.ValidReviewID(id) {
			return options, command.UsageText(archiveError("run/batch ID").Error())
		}
	}
	if options.previousID != "" && !board.ValidReviewID(options.previousID) {
		return options, command.UsageText(archiveError("previous run ID").Error())
	}
	sort.Strings(options.tasks)
	return options, nil
}

func archiveInvocation(ctx *reviewContext, options archiveOptions, arguments []string, root string, findingsSchema int, stores ...reviewStore) (bool, error) {
	store := selectedReviewStore(root, stores)
	requirements := map[string]string{}
	if options.requirementsFile != "" {
		if err := readArchiveJSON(options.requirementsFile, &requirements); err != nil {
			return false, err
		}
	}
	var advance *board.ReviewAdvance
	if options.advanceFile != "" {
		advance = &board.ReviewAdvance{}
		if err := readArchiveJSON(options.advanceFile, advance); err != nil {
			return false, err
		}
		// The range may hold a delivery of a member that left the batch since.
		members := options.tasks
		if b, err := store.Batch(options.batchID); err == nil {
			members = b.TaskIDs
		}
		if err := validateReviewAdvance(*ctx, *advance, members); err != nil {
			return false, err
		}
	}
	task := []byte(arguments[4])
	if ctx.taskSpec != "" {
		var err error
		task, err = os.ReadFile(ctx.taskSpec)
		if err != nil {
			return false, err
		}
		// *_RESOLVED records are rewritten by launches and review runs; keeping them
		// out of the snapshot keeps the batch's frozen task context stable.
		var frozen string
		if b, err := store.Batch(options.batchID); err == nil {
			frozen = b.TaskContextHash
		}
		task = taskContextBytes(string(task), frozen)
	}
	reviewContext := []byte{}
	if len(arguments) >= 6 {
		reviewContext = []byte(arguments[5])
	}
	input := board.ReviewInput{FindingsSchema: findingsSchema, RunID: options.runID, BatchID: options.batchID, PreviousRunID: options.previousID, TaskIDs: options.tasks, Role: ctx.role, Reviewer: ctx.agent, Model: ctx.settings.model, Effort: ctx.settings.effort, CWD: ctx.root, Base: ctx.base, Commit: ctx.commit, ReviewedCommit: ctx.reviewed, ReportLanguage: ctx.reportLanguage, Resolved: ctx.resolved}
	run, fresh, err := store.Prepare(input, requirements, advance, map[string][]byte{"task-context.md": task, "review-context.md": reviewContext}, version.String())
	if err != nil {
		return false, err
	}
	staging, err := store.Staging(run.RunID)
	if err != nil {
		return false, err
	}
	ctx.reportLanguage = run.ReportLanguage
	ctx.archive = &archiveExecution{store: store, root: root, run: run, staging: staging, taskSnapshot: task}
	recordRun(run)
	command.Notify("review_archive_identity", config.Text("review.archive_identity", run.RunID, run.BatchID))
	return fresh, nil
}
func readArchiveJSON(path string, value any) error {
	if !filepath.IsAbs(path) {
		return archiveError("absolute JSON file path required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return board.DecodeReviewJSON(data, value)
}
func validateReviewAdvance(ctx reviewContext, advance board.ReviewAdvance, tasks []string) error {
	if advance.Target != ctx.commit || advance.PreviousTarget == "" || strings.TrimSpace(advance.Reason) == "" {
		return archiveError("advance target/reason")
	}
	_, _, code, err := gitCommand([]string{"merge-base", "--is-ancestor", advance.PreviousTarget, ctx.commit}, ctx.root, "")
	if err != nil {
		return err
	}
	if code != 0 {
		return archiveError("advance ancestry")
	}
	output, _, code, err := gitCommand([]string{"rev-list", advance.PreviousTarget + ".." + ctx.commit}, ctx.root, "")
	if err != nil {
		return err
	}
	if code != 0 {
		return archiveError("advance range")
	}
	commits := strings.Fields(output)
	rangeCommits := make(map[string]bool, len(commits))
	for _, commit := range commits {
		rangeCommits[commit] = true
	}
	members := map[string]bool{}
	for _, id := range tasks {
		members[id] = true
	}
	for commit, taskID := range advance.Deliveries {
		if !rangeCommits[commit] {
			return archiveError("advance attribution outside range: " + commit)
		}
		if !members[taskID] {
			return archiveError("unattributed or foreign delivery: " + commit)
		}
	}
	for commit, reason := range advance.ForeignCommits {
		if !rangeCommits[commit] {
			return archiveError("advance attribution outside range: " + commit)
		}
		if strings.TrimSpace(reason) == "" {
			return archiveError("foreign commit reason required: " + commit)
		}
		if _, exists := advance.Deliveries[commit]; exists {
			return archiveError("duplicate advance attribution: " + commit)
		}
	}
	if len(commits) == 0 || len(commits) != len(advance.Deliveries)+len(advance.ForeignCommits) {
		return archiveError("advance must attribute every commit")
	}
	return nil
}
func (a *archiveExecution) phase(phase, launch string) error {
	a.run.Phase = phase
	a.run.LaunchStatus = launch
	return a.backend().Update(a.run)
}
func (a *archiveExecution) finish(code int) int {
	a.run.ExitCode = code
	switch {
	case code == 0:
		a.run.ExecutionStatus = "ok"
	case a.run.LaunchStatus == "not_started":
		a.run.ExecutionStatus = "not_started"
	default:
		a.run.ExecutionStatus = "failed"
	}
	a.run.FailureReason = a.reason
	if code != 0 && a.run.FailureReason == "" {
		a.run.FailureReason = config.Text("review.archive_execution_failed", code)
	}
	if code != 0 && a.processEvidence {
		note := processEvidenceNote(a.run.RunID)
		a.run.FailureReason += "; " + note
		userError(note)
	}
	run, err := a.backend().Finalize(a.run, a.report)
	if err != nil {
		userError(config.Text("review.archive_publish_failed", a.run.RunID, err.Error()))
		return 2
	}
	if code == 0 && run.ExitCode != 0 {
		userError(run.FailureReason)
	}
	a.run = run
	return a.publish()
}
func (a *archiveExecution) recover() int {
	if a.run.Phase != "finalized" {
		a.run.ExecutionStatus = "interrupted"
		a.run.ExitCode = 2
		a.run.FailureReason = config.Text("review.archive_interrupted")
		// A gate killed after its first evidence write still left the record in staging.
		if _, err := os.Lstat(filepath.Join(a.staging, board.ReviewProcessEvidence)); err == nil {
			note := processEvidenceNote(a.run.RunID)
			a.run.FailureReason += "; " + note
			userError(note)
		}
		var err error
		a.run, err = a.backend().Finalize(a.run, nil)
		if err != nil {
			userError(err.Error())
			return 2
		}
	}
	// Replay exactly the saved semantic report, or the raw partial output when no
	// semantic report exists. This path never launches a reviewer.
	if data, err := a.backend().Original(a.run.RunID, "report.md"); err == nil {
		_, _ = reportStream().Write(data)
	} else if data, err := a.backend().Original(a.run.RunID, "output.raw"); err == nil {
		_, _ = reportStream().Write(data)
	}
	return a.publish()
}
func (a *archiveExecution) publish() int {
	run, failures, err := a.backend().Publish(a.run.RunID)
	if err == nil {
		recordRun(run)
	}
	if err != nil {
		userError(config.Text("review.archive_publish_failed", a.run.RunID, err.Error()))
		return 2
	}
	ids := append([]string(nil), run.TaskIDs...)
	sort.Strings(ids)
	for _, id := range ids {
		if err := failures[id]; err != nil {
			userError(config.Text("review.archive_card_failed", id, err.Error()))
		} else {
			command.Notify("review_card_published", config.Text("review.archive_card_published", id, run.RunID, run.ExecutionStatus))
		}
	}
	if len(failures) > 0 {
		return 2
	}
	markPublished()
	if err = a.backend().Complete(run.RunID); err != nil {
		userError(err.Error())
		return 2
	}
	pending, err := a.backend().NeedsInterpretation(run.RunID)
	if err != nil {
		userError(err.Error())
		return 2
	}
	if pending {
		command.Notify("review_interpretation_pending", config.Text("review.interpretation_pending", run.RunID))
	}
	return run.ExitCode
}
func (a *archiveExecution) snapshotRuntime(runtime, largestEvidenceSection string) error {
	for _, name := range []string{"prompt.txt", config.ReviewContractFilename, "evidence.txt"} {
		data, err := fs.ReadRegularFile(runtime, filepath.Join(runtime, name))
		if err != nil {
			return err
		}
		if err = a.backend().Artifact(a.run.RunID, name, data); err != nil {
			var size *artifactTooLargeError
			if errors.As(err, &size) {
				source := ""
				if name == "evidence.txt" {
					source = largestEvidenceSection
				}
				return newGate(2, "review.artifact_request_too_large", size.name, size.rawBytes, size.requestBytes, size.limit, source)
			}
			return err
		}
	}
	return nil
}

// sameReplayTarget prevents an explicit existing ID from bypassing immutable
// invocation identity even when the worktree or CLI availability has changed.
func sameReplayTarget(run board.ReviewRun, options archiveOptions) bool {
	return run.BatchID == options.batchID && reflect.DeepEqual(run.TaskIDs, options.tasks)
}

func (a *archiveExecution) backend() reviewStore {
	if a.store != nil {
		return a.store
	}
	return localReviewStore{root: a.root}
}

// taskContextBytes is the task context a run freezes: the card with English
// schema names and without *_RESOLVED records. A batch frozen before the schema
// rename holds the Chinese bytes of a legacy card, and its hash is the identity
// every later run must match, so the spelling that hashes to it is kept.
func taskContextBytes(text, frozenHash string) []byte {
	original := []byte(board.WithoutResolvedRecords(text))
	if frozenHash != "" && board.ReviewDigest(original) == frozenHash {
		return original
	}
	return []byte(board.WithoutResolvedRecords(board.NormalizeLegacyCard(text)))
}
