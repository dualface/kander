package review

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

// jsonRun holds what one review run reports with --json: the reviewer's report, the messages
// userError collects, and the run published to the archive. runReview resets it.
var jsonRun struct {
	report   bytes.Buffer
	messages []string
	run      *board.ReviewRun
	// published is set once the evidence of run is published to every card. A run that was only
	// prepared (recordRun) is a diagnostic of a failure, never a result.
	published bool
}

func resetJSONRun() {
	jsonRun.report.Reset()
	jsonRun.messages, jsonRun.run, jsonRun.published = nil, nil, false
}

// reportStream is where the report of a review goes: stdout, or with --json the report field
// of the result object.
func reportStream() io.Writer {
	if command.JSON() {
		return &jsonRun.report
	}
	return os.Stdout
}

// remarkStream is where the remarks of a review run go: stderr, or with --json notices.
func remarkStream() io.Writer {
	if command.JSON() {
		return command.NoticeWriter("review_output")
	}
	return os.Stderr
}

// recordRun keeps the run of the archive for the result object, or for the diagnostic of a
// failure before the evidence was published.
func recordRun(run board.ReviewRun) { jsonRun.run = &run }

// markPublished records that the evidence of the run reached every card.
func markPublished() { jsonRun.published = true }

// reviewResult is the result object of kander review. The run fields appear when the review
// was bound to cards with --task.
type reviewResult struct {
	ExitCode        int      `json:"exit_code"`
	RunID           string   `json:"run_id,omitempty"`
	BatchID         string   `json:"batch_id,omitempty"`
	ExecutionStatus string   `json:"execution_status,omitempty"`
	FailureReason   string   `json:"failure_reason,omitempty"`
	TaskIDs         []string `json:"task_ids,omitempty"`
	Report          string   `json:"report"`
}

// reviewExit ends a review with its exit status. With --json a review that published a run, or
// that succeeded, is the result object with that status; any other failure is the error object
// with the messages the run reported.
func reviewExit(code int) int {
	if !command.JSON() {
		return code
	}
	run := jsonRun.run
	if code != 0 && !jsonRun.published {
		messages := jsonRun.messages
		if run != nil {
			messages = append(messages[:len(messages):len(messages)], config.Text("review.json_prepared_not_published", run.RunID, run.BatchID))
		}
		return command.FailStatus(code, command.Coded("review_failed", errors.New(strings.Join(messages, "\n"))))
	}
	result := reviewResult{ExitCode: code, Report: jsonRun.report.String()}
	if run != nil {
		result.RunID, result.BatchID, result.ExecutionStatus = run.RunID, run.BatchID, run.ExecutionStatus
		result.FailureReason, result.TaskIDs = run.FailureReason, run.TaskIDs
	}
	for _, message := range jsonRun.messages {
		command.Notify("review_error", message)
	}
	return command.ResultStatus(result, code)
}
