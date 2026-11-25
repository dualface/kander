package liveness

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// livenessEntry is one working card in the result of kander check.
type livenessEntry struct {
	TaskID           string    `json:"task_id"`
	Agent            string    `json:"agent"`
	Status           string    `json:"status"`
	Channel          string    `json:"channel"`
	Container        string    `json:"container"`
	Detail           string    `json:"detail,omitempty"`
	NewWindow        string    `json:"new_window,omitempty"`
	ObservedAt       time.Time `json:"observed_at"`
	RuntimeState     string    `json:"runtime_state"`
	ObservationValid bool      `json:"observation_valid"`
}

// checkReport is the result object of kander check: the service's structural report with its
// exit status, and the liveness of every working card.
type checkReport struct {
	ExitCode int             `json:"exit_code"`
	Output   string          `json:"output"`
	Liveness []livenessEntry `json:"liveness"`
}

func runRemoteCheck(cfg *config.Config, tasks []string, all bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cwd, err := os.Getwd()
	if err != nil {
		return command.Fail(err)
	}
	service, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return command.Fail(err)
	}
	operationCtx, finishOperation := service.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	result, err := service.Project.Check(ctx, api.CheckQuery{Tasks: tasks, All: all})
	if err != nil {
		return command.Fail(err)
	}
	for _, line := range result.Diagnostics {
		command.Notify("check_diagnostic", line)
	}
	query := api.SnapshotQuery{}
	if len(tasks) > 0 {
		query.Tasks = tasks
	}
	snapshot, err := service.Project.Snapshot(ctx, query)
	if err != nil {
		return command.Fail(err)
	}
	scanned, err := board.RestoreView(snapshot.View)
	if err != nil {
		return command.Fail(err)
	}
	facts := subscriptionFacts{scanned: scanned, claims: snapshot.Claims, observedAt: time.Now().UTC(), serverNow: snapshot.ObservedAt}
	report := checkReport{ExitCode: result.Code, Output: result.Output, Liveness: []livenessEntry{}}
	for _, card := range snapshot.View.Cards {
		if card.State != "working" {
			continue
		}
		observation := remoteObservation(card.TaskID, facts)
		if command.JSON() {
			report.Liveness = append(report.Liveness, livenessEntry{
				TaskID: observation.TaskID, Agent: observation.Agent, Status: observation.Status, Channel: observation.Channel,
				Container: observation.Container, Detail: observation.Detail, NewWindow: observation.NewWindow,
				ObservedAt: observation.ObservedAt, RuntimeState: observation.RuntimeState, ObservationValid: observation.ObservationValid,
			})
			continue
		}
		fmt.Println(formatReport(observation))
	}
	if command.JSON() {
		return command.ResultStatus(report, result.Code)
	}
	_, _ = os.Stdout.WriteString(result.Output)
	return result.Code
}
