package review

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/version"
)

type archiveOptions struct {
	tasks                                                     []string
	runID, batchID, previousID, requirementsFile, advanceFile string
}
type archiveExecution struct {
	root         string
	run          board.ReviewRun
	staging      string
	report       []byte
	taskSnapshot []byte
	reason       string
}

func archiveError(detail string) error { return newGate(2, "review.archive_invalid", detail) }

func parseArchiveOptions(args []string) (archiveOptions, []string, error) {
	var options archiveOptions
	var prefix []string
	if len(args) > 0 && containsAgent(args[0]) {
		prefix = append(prefix, args[0])
		args = args[1:]
	}
	seen := map[string]bool{}
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		flag := args[0]
		if len(args) < 2 || args[1] == "" {
			return options, nil, archiveError(flag)
		}
		value := args[1]
		args = args[2:]
		if flag != "--task" && seen[flag] {
			return options, nil, archiveError("duplicate " + flag)
		}
		seen[flag] = true
		switch flag {
		case "--task":
			id, err := board.NormalizeTaskID(value)
			if err != nil {
				return options, nil, err
			}
			duplicate := false
			for _, old := range options.tasks {
				if old == id {
					duplicate = true
				}
			}
			if !duplicate {
				options.tasks = append(options.tasks, id)
			}
		case "--run-id":
			options.runID = value
		case "--batch-id":
			options.batchID = value
		case "--previous-run-id":
			options.previousID = value
		case "--requirements-file":
			options.requirementsFile = value
		case "--advance-file":
			options.advanceFile = value
		default:
			return options, nil, archiveError(flag)
		}
	}
	if len(options.tasks) == 0 && len(seen) > 0 {
		return options, nil, archiveError("--task required")
	}
	if len(options.tasks) > 0 {
		if options.batchID == "" {
			return options, nil, archiveError("--batch-id required")
		}
		if options.runID == "" {
			var err error
			options.runID, err = board.NewReviewID()
			if err != nil {
				return options, nil, err
			}
		}
		for _, id := range []string{options.runID, options.batchID} {
			if !board.ValidReviewID(id) {
				return options, nil, archiveError("run/batch ID")
			}
		}
		if options.previousID != "" && !board.ValidReviewID(options.previousID) {
			return options, nil, archiveError("previous run ID")
		}
	}
	sort.Strings(options.tasks)
	return options, append(prefix, args...), nil
}

func archiveInvocation(ctx *reviewContext, options archiveOptions, arguments []string, root string) (bool, error) {
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
		if err := validateReviewAdvance(*ctx, *advance, options.tasks); err != nil {
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
	}
	reviewContext := []byte{}
	if len(arguments) >= 6 {
		reviewContext = []byte(arguments[5])
	}
	input := board.ReviewInput{RunID: options.runID, BatchID: options.batchID, PreviousRunID: options.previousID, TaskIDs: options.tasks, Role: ctx.role, Reviewer: ctx.agent, Model: ctx.settings.model, Effort: ctx.settings.effort, CWD: ctx.root, Base: ctx.base, Commit: ctx.commit, ReviewedCommit: ctx.reviewed, ReportLanguage: ctx.reportLanguage}
	run, fresh, err := board.PrepareReviewRun(root, input, requirements, advance, map[string][]byte{"task-context.md": task, "review-context.md": reviewContext}, version.String())
	if err != nil {
		return false, err
	}
	staging, err := board.ReviewStaging(root, run.RunID)
	if err != nil {
		return false, err
	}
	ctx.reportLanguage = run.ReportLanguage
	ctx.archive = &archiveExecution{root: root, run: run, staging: staging, taskSnapshot: task}
	fmt.Fprintln(os.Stderr, config.Text("review.archive_identity", run.RunID, run.BatchID))
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
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return archiveError(err.Error())
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return archiveError("trailing JSON")
	}
	return nil
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
	if len(commits) == 0 || len(commits) != len(advance.Deliveries) {
		return archiveError("advance must attribute every commit")
	}
	members := map[string]bool{}
	for _, id := range tasks {
		members[id] = true
	}
	for _, commit := range commits {
		if !members[advance.Deliveries[commit]] {
			return archiveError("unattributed or foreign delivery: " + commit)
		}
	}
	return nil
}
func (a *archiveExecution) phase(phase, launch string) error {
	a.run.Phase = phase
	a.run.LaunchStatus = launch
	return board.UpdateReviewRun(a.root, a.run)
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
	run, err := board.FinalizeReviewRun(a.root, a.run, a.report)
	if err != nil {
		userError(config.Text("review.archive_publish_failed", a.run.RunID, err.Error()))
		return 2
	}
	a.run = run
	return a.publish()
}
func (a *archiveExecution) recover() int {
	if a.run.Phase != "finalized" {
		a.run.ExecutionStatus = "interrupted"
		a.run.ExitCode = 2
		a.run.FailureReason = config.Text("review.archive_interrupted")
		var err error
		a.run, err = board.FinalizeReviewRun(a.root, a.run, nil)
		if err != nil {
			userError(err.Error())
			return 2
		}
	}
	// Replay exactly the saved semantic report, or the raw partial output when no
	// semantic report exists. This path never launches a reviewer.
	if data, err := board.ReadReviewOriginal(a.root, a.run.RunID, "report.md"); err == nil {
		_, _ = os.Stdout.Write(data)
	} else if data, err := board.ReadReviewOriginal(a.root, a.run.RunID, "output.raw"); err == nil {
		_, _ = os.Stdout.Write(data)
	}
	return a.publish()
}
func (a *archiveExecution) publish() int {
	run, failures, err := board.PublishReviewRun(a.root, a.run.RunID)
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
			fmt.Fprintln(os.Stderr, config.Text("review.archive_card_published", id, run.RunID, run.ExecutionStatus))
		}
	}
	if len(failures) > 0 {
		return 2
	}
	if err = board.ReviewPublicationComplete(a.root, run.RunID); err != nil {
		userError(err.Error())
		return 2
	}
	return run.ExitCode
}
func (a *archiveExecution) snapshotRuntime(runtime string) error {
	for _, name := range []string{"prompt.txt", "evidence.txt"} {
		data, err := fs.ReadRegularFile(runtime, filepath.Join(runtime, name))
		if err != nil {
			return err
		}
		if err = board.StoreReviewArtifact(a.root, a.run.RunID, name, data); err != nil {
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
