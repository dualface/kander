package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
)

func commandRemoteAdopt(cfg *config.Config, task, decision string) error {
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
	snapshot := board.Snapshot{Entry: board.Entry{TaskID: task, State: state.Task.State}, Text: state.Task.Text, Revision: state.Task.Revision}
	if err := board.CheckAdoptable(snapshot, state.Record); err != nil {
		return err
	}
	if !state.Record.StartUnfinished() {
		return launchError("board.adopt_needs_unfinished_start", task, state.Record.Status)
	}
	if credential.Session == "" || credential.Session != board.MetadataFrom(state.Task.Text, board.FieldSession) || credential.Window != board.MetadataFrom(state.Task.Text, board.FieldWindow) {
		return launchError("launch.start_identity_changed")
	}
	presence := observeLocalExecution(ctx, credential)
	if presence.Status != "alive" {
		return launchError("launch.adopt_liveness_unconfirmed", task, presence.Status, presence.Reason)
	}
	_, err = session.Project.Execution(ctx, client.NewKey(), task, api.ExecutionUpdate{Action: api.ExecutionAdopt, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Presence: &presence, Decision: decision}, *credential.Claim())
	if err != nil {
		return err
	}
	fmt.Println(t("launch.adopted", task, orNA(credential.Window)))
	return nil
}

// observeLocalExecution never reads a card path or searches a foreign pane.
func observeLocalExecution(ctx context.Context, credential remote.Credential) api.AgentObservation {
	result := api.AgentObservation{Status: "unknown", ObservedAt: time.Now().UTC()}
	if credential.AgentPID > 0 {
		if credential.AgentStart == "" || credential.BootID == "" {
			return result
		}
		boot, err := process.BootIdentity()
		if err != nil {
			return result
		}
		if !process.SameBoot(credential.BootID, boot) {
			result.Status = "stopped"
			return result
		}
		facts, err := process.ObserveProcess(ctx, credential.AgentPID)
		if errors.Is(err, process.ErrProcessGone) || err == nil && facts.Start != credential.AgentStart {
			result.Status = "stopped"
		} else if err == nil {
			result.Status = "alive"
		}
		return result
	}
	reports := liveness.ObserveAgentStatus(ctx, []liveness.AgentStatusInput{{TaskID: credential.TaskID, Session: credential.Session, Window: credential.Window}}, liveness.BatchOptions{})
	if len(reports) == 1 {
		report := reports[0]
		result.Reason, result.RuntimeState = report.Detail, report.RuntimeState
		if report.Status == liveness.Alive || report.Status == liveness.AliveOrphaned {
			result.Status = "alive"
		}
		if report.Status == liveness.Stopped || report.Status == liveness.IdentityMismatch {
			result.Status = "stopped"
		}
	}
	return result
}
