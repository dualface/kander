package liveness

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func runRemoteCheck(cfg *config.Config, tasks []string, all bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	service, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	operationCtx, finishOperation := service.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	result, err := service.Project.Check(ctx, api.CheckQuery{Tasks: tasks, All: all})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, line := range result.Diagnostics {
		fmt.Fprintln(os.Stderr, line)
	}
	query := api.SnapshotQuery{}
	if len(tasks) > 0 {
		query.Tasks = tasks
	}
	snapshot, err := service.Project.Snapshot(ctx, query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	scanned, err := board.RestoreView(snapshot.View)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	facts := subscriptionFacts{scanned: scanned, claims: snapshot.Claims, observedAt: time.Now().UTC(), serverNow: snapshot.ObservedAt}
	for _, card := range snapshot.View.Cards {
		if card.State == "working" {
			fmt.Println(formatReport(remoteObservation(card.TaskID, facts)))
		}
	}
	_, _ = os.Stdout.WriteString(result.Output)
	return result.Code
}
