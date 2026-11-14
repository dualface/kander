package launch

import (
	"context"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func commandRemoteRelease(cfg *config.Config, task, decision string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCall)
	defer cancel()
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return err
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	credential, found, err := session.Credential(task)
	if err != nil {
		return err
	}
	if !found || credential.Lost {
		return launchError("remote.execution_holder_required")
	}
	state, err := session.Project.ExecutionState(ctx, task, *credential.Claim())
	if err != nil {
		return err
	}
	if err := cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(state.Task.Text)); err != nil {
		return err
	}
	presence := observeLocalExecution(ctx, credential)
	if presence.Status != "stopped" {
		return launchError("remote.restart_needs_stopped")
	}
	if _, err := session.Project.Execution(ctx, client.NewKey(), task, api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Presence: &presence, Decision: decision}, *credential.Claim()); err != nil {
		return err
	}
	if err := remote.RemoveCredential(session.Home, credential); err != nil {
		return err
	}
	fmt.Println(t("remote.execution_released", task))
	return nil
}
