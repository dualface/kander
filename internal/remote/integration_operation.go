package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/process"
)

type integrationActionCall struct {
	Key    string                 `json:"key"`
	Input  api.IntegrationCommand `json:"input"`
	Failed bool                   `json:"failed,omitzero"`
	Result *api.Integration       `json:"result,omitempty"`
}
type integrationAttempt struct {
	Request        api.IntegrationRequest            `json:"request"`
	Key            string                            `json:"key"`
	Claimed        bool                              `json:"claimed,omitzero"`
	Retired        bool                              `json:"retired,omitzero"`
	Failed         bool                              `json:"failed,omitzero"`
	SourceAttempts int                               `json:"source_attempts,omitzero"`
	PushAttempts   int                               `json:"push_attempts,omitzero"`
	Actions        map[string]*integrationActionCall `json:"actions"`
}
type integrationOperation struct {
	Schema   int                   `json:"schema"`
	Options  IntegrationOptions    `json:"options"`
	Actor    string                `json:"actor,omitempty"`
	Attempts []*integrationAttempt `json:"attempts"`
	Result   *api.Integration      `json:"result,omitempty"`
}

func storeIntegrationOperation(journal *ActionJournal, value *integrationOperation) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	journal.Value.Result = data
	journal.Value.Phase = "pending"
	if value.Result != nil {
		journal.Value.Phase = "complete"
	}
	return journal.Save()
}
func sameIntegrationOptions(a, b IntegrationOptions) bool {
	first, _ := json.Marshal(a)
	second, _ := json.Marshal(b)
	return bytes.Equal(first, second)
}
func (s *Session) integrationActionCall(ctx context.Context, dir string, journal *ActionJournal, state *integrationOperation, attempt *integrationAttempt, action string, proof client.Claim) (api.Integration, error) {
	if call := attempt.Actions[action]; call != nil && !call.Failed {
		if call.Result != nil {
			return *call.Result, nil
		}
		value, err := s.Project.IntegrationAction(ctx, call.Key, api.IntegrationID(attempt.Request.TargetRef), call.Input, proof)
		if err != nil {
			if requestFailureKnown(err) {
				call.Failed = true
				_ = storeIntegrationOperation(journal, state)
			}
			return value, err
		}
		call.Result = &value
		return value, storeIntegrationOperation(journal, state)
	}
	id := api.IntegrationID(attempt.Request.TargetRef)
	observation, err := s.Project.IntegrationObservation(ctx, id)
	if err != nil {
		return api.Integration{}, err
	}
	if observation.Integration == nil || observation.Integration.Request.ExecutionID != attempt.Request.ExecutionID {
		return api.Integration{}, integrationFailure("remote.integration_recovery_required")
	}
	head, err := s.integrationTarget(ctx, dir, attempt.Request.TargetRef)
	if err != nil {
		return api.Integration{}, err
	}
	input := api.IntegrationCommand{Action: action, ExpectedRevision: observation.Integration.Revision, ObservationTicket: observation.Ticket, ObservedTarget: head, ObservedAt: time.Now().UTC()}
	switch action {
	case "confirm":
		contains, err := s.integrationContains(ctx, dir, attempt.Request.Commit, head)
		if err != nil {
			return api.Integration{}, err
		}
		if !contains {
			return api.Integration{}, integrationFailure("remote.integration_recovery_required")
		}
		// Fetch/ancestry checks may take longer than the ticket. Re-observe the
		// remote under a new service ticket after those immutable checks finish.
		observation, err = s.Project.IntegrationObservation(ctx, id)
		if err != nil {
			return api.Integration{}, err
		}
		again, err := s.integrationTarget(ctx, dir, attempt.Request.TargetRef)
		if err != nil {
			return api.Integration{}, err
		}
		if again != head || observation.Integration == nil || observation.Integration.Request.ExecutionID != attempt.Request.ExecutionID {
			return api.Integration{}, integrationFailure("remote.integration_recovery_required")
		}
		input.ExpectedRevision, input.ObservationTicket = observation.Integration.Revision, observation.Ticket
		input.Edges = []api.IntegrationEdge{{Ancestor: attempt.Request.Commit, Descendant: head}}
	case "release":
		input.Decision = state.Options.Decision + "; recover completed local invocation " + attempt.Request.ExecutionID
	}
	call := &integrationActionCall{Key: client.NewKey(), Input: input}
	attempt.Actions[action] = call
	if err := storeIntegrationOperation(journal, state); err != nil {
		return api.Integration{}, err
	}
	return s.integrationActionCall(ctx, dir, journal, state, attempt, action, proof)
}
func integrationProcess(ctx context.Context) (process.ProcessFacts, string, error) {
	facts, err := process.ObserveProcess(ctx, os.Getpid())
	if err != nil {
		return facts, "", errors.Join(err, integrationFailure("remote.integration_process_required"))
	}
	boot, err := process.BootIdentity()
	if err != nil {
		return facts, "", err
	}
	return facts, boot, nil
}
func (s *Session) finishIntegration(journal *ActionJournal, state *integrationOperation, value api.Integration) (api.Integration, error) {
	state.Result = &value
	if err := storeIntegrationOperation(journal, state); err != nil {
		return value, err
	}
	credential := Credential{ResourceKind: "integration", ServerURL: s.ServerURL, ClientID: s.Client.ClientID(), ProjectID: s.Info.ID, TaskID: value.ID, ExecutionID: value.Request.ExecutionID, Generation: value.Generation}
	return value, RemoveCredential(s.Home, credential)
}

func (s *Session) retireIntegration(journal *ActionJournal, state *integrationOperation, attempt *integrationAttempt, current api.Integration) error {
	if err := RemoveCredential(s.Home, Credential{ResourceKind: "integration", ProjectID: s.Info.ID, TaskID: current.ID, ExecutionID: attempt.Request.ExecutionID, Generation: current.Generation}); err != nil {
		return err
	}
	attempt.Retired = true
	return storeIntegrationOperation(journal, state)
}
