package launch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

type coordinatorRecoveryCall struct {
	Schema int                          `json:"schema"`
	Key    string                       `json:"key"`
	Input  api.GroupRecover             `json:"input"`
	Proof  client.Claim                 `json:"proof"`
	Result *board.CoordinatorCheckpoint `json:"result,omitempty"`
}

func prepareRemoteGroupRecovery(ctx context.Context, session *remote.Session, request board.CoordinatorRecover, execution string) (api.GroupRecover, client.Claim, error) {
	task, err := session.Project.Show(ctx, request.Member)
	if err != nil {
		return api.GroupRecover{}, client.Claim{}, err
	}
	input := api.GroupRecover{Request: request, ObservedRevision: task.Revision, Observation: api.AgentObservation{Status: "stopped", Reason: "finished member recovery"}}
	var tasks []string
	if task.State == "working" || task.State == "review" {
		if task.Claim == nil || task.Claim.Status == api.ClaimLost || task.Claim.Status == api.ClaimReleased {
			generation := uint64(0)
			if task.Claim != nil {
				generation = task.Claim.Generation
			}
			input.Lost = &api.RecoveryLease{ExecutionID: execution, ExpectedGeneration: generation}
			input.Observation = api.AgentObservation{Status: "unknown", Reason: "the service lease is lost or released; the predecessor host was not probed"}
		} else {
			if task.Claim.ClientID != session.Client.ClientID() {
				return input, client.Claim{}, launchError("remote.coordinator_holder_recovery_required")
			}
			credential, found, err := session.Credential(task.TaskID)
			if err != nil {
				return input, client.Claim{}, err
			}
			if !found || credential.Lost || credential.Generation != task.Claim.Generation || credential.ExecutionID != task.Claim.ExecutionID {
				return input, client.Claim{}, launchError("remote.coordinator_holder_recovery_required")
			}
			input.Observation = observeLocalExecution(ctx, credential)
			if input.Observation.Status != "stopped" {
				return input, client.Claim{}, launchError("remote.coordinator_stopped_required")
			}
			state, err := session.Project.ExecutionState(ctx, task.TaskID, *credential.Claim())
			if err != nil {
				return input, client.Claim{}, err
			}
			if state.Task.Revision != task.Revision {
				return input, client.Claim{}, &api.Error{Code: api.CodeRevisionConflict, Message: config.Text("remote.delivery_card_changed")}
			}
			input.ObservationTicket = state.ObservationTicket
			tasks = []string{task.TaskID}
		}
	}
	proof, err := session.CoordinatorAuthority(request.GroupID, tasks)
	return input, proof, err
}
func recoverRemoteCoordinatorMember(ctx context.Context, session *remote.Session, request board.CoordinatorRecover) (board.CoordinatorCheckpoint, error) {
	journal, fresh, err := session.BeginAction("coordinator-recover", request)
	if err != nil {
		return board.CoordinatorCheckpoint{}, err
	}
	defer journal.Close()
	call := coordinatorRecoveryCall{Schema: 1, Key: client.NewKey()}
	if fresh {
		call.Input, call.Proof, err = prepareRemoteGroupRecovery(ctx, session, request, journal.Value.ExecutionID)
		if err != nil {
			return board.CoordinatorCheckpoint{}, err
		}
	} else if len(journal.Value.Result) > 0 {
		if err := json.Unmarshal(journal.Value.Result, &call); err != nil {
			return board.CoordinatorCheckpoint{}, err
		}
		if call.Schema != 1 || !api.ValidIdentity(call.Key) || !reflect.DeepEqual(call.Input.Request, request) {
			return board.CoordinatorCheckpoint{}, launchError("remote.launch_receipt_damaged")
		}
		if call.Result != nil {
			return *call.Result, nil
		}
	} else {
		// A crash before freezing input crossed no domain boundary.
		call.Input, call.Proof, err = prepareRemoteGroupRecovery(ctx, session, request, journal.Value.ExecutionID)
		if err != nil {
			return board.CoordinatorCheckpoint{}, err
		}
	}
	persist := func() error {
		data, err := json.Marshal(call)
		if err != nil {
			return err
		}
		journal.Value.Result = data
		journal.Value.Phase = "pending"
		if call.Result != nil {
			journal.Value.Phase = "complete"
		}
		return journal.Save()
	}
	if err := persist(); err != nil {
		return board.CoordinatorCheckpoint{}, err
	}
	value, err := session.Project.CoordinatorAction(ctx, call.Key, request.GroupID, "recover", call.Input, call.Proof)
	if err != nil {
		var failure *api.Error
		if errors.As(err, &failure) && failure.Status >= 400 && failure.Status < 500 {
			journal.Value.Phase = "failed"
			err = errors.Join(err, journal.Save())
		}
		return value, err
	}
	call.Result = &value
	return value, persist()
}
