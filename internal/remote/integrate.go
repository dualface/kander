package remote

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

func (s *Session) prepareIntegration(ctx context.Context, dir string, options IntegrationOptions, actor Credential) (api.IntegrationRequest, error) {
	request := api.IntegrationRequest{Purpose: options.Purpose, TargetRef: options.TargetRef, Commit: options.Commit, GroupID: options.GroupID, ExecutionID: NewExecutionID(), ActorExecutionID: actor.ExecutionID, Decision: options.Decision}
	members, err := s.integrationMembers(ctx, options)
	if err != nil {
		return request, err
	}
	request.Members = members
	if _, err := gitCommand(ctx, dir, "cat-file", "-e", options.Commit+"^{commit}"); err != nil {
		if err := s.FetchCommit(ctx, dir, options.Commit); err != nil {
			return request, err
		}
	}
	for _, member := range members {
		if member.DeliveryCommit != "" {
			if _, err := s.FetchDelivery(ctx, dir, member.TaskID, member.DeliveryCommit); err != nil {
				return request, err
			}
		}
	}
	target, err := s.integrationTarget(ctx, dir, options.TargetRef)
	if err != nil {
		return request, err
	}
	request.ExpectedTarget, request.BaseCommit = target, target
	if options.Purpose == "initialize" {
		request.BaseCommit = options.Commit
		if target != "" && target != options.Commit {
			return request, integrationFailure("remote.git_ref_conflict")
		}
	}
	if request.BaseCommit == "" {
		return request, integrationFailure("remote.integration_input_required")
	}
	contained, err := s.integrationContains(ctx, dir, options.Commit, target)
	if err != nil {
		return request, err
	}
	request.AlreadyContained = contained
	request.Edges = []api.IntegrationEdge{{Ancestor: request.BaseCommit, Descendant: options.Commit}}
	if contained {
		request.Edges = []api.IntegrationEdge{{Ancestor: options.Commit, Descendant: target}}
	}
	for _, member := range members {
		if member.DeliveryCommit != "" {
			request.Edges = append(request.Edges, api.IntegrationEdge{Ancestor: member.DeliveryCommit, Descendant: options.Commit})
		}
	}
	if err := s.integrationEdges(ctx, dir, request); err != nil {
		return request, err
	}
	observation, err := s.Project.IntegrationObservation(ctx, api.IntegrationID(options.TargetRef))
	if err != nil {
		return request, err
	}
	current, err := s.integrationTarget(ctx, dir, options.TargetRef)
	if err != nil {
		return request, err
	}
	if current != target {
		return request, integrationFailure("remote.git_ref_conflict")
	}
	request.ObservationTicket = observation.Ticket
	if observation.Integration != nil {
		request.ExpectedGeneration, request.ExpectedRevision = observation.Integration.Generation, observation.Integration.Revision
	}
	return request, nil
}
func (s *Session) integrationScopeCurrent(ctx context.Context, input api.IntegrationRequest) (bool, error) {
	for _, member := range input.Members {
		if input.Purpose != "deliver" && member.DeliveryCommit == "" {
			continue
		}
		current, err := s.Project.Show(ctx, member.TaskID)
		if err != nil {
			return false, err
		}
		generation := uint64(0)
		if current.Claim != nil {
			generation = current.Claim.Generation
		}
		if current.Revision != member.Revision || generation != member.Generation {
			return false, nil
		}
	}
	return true, nil
}

// Integrate coordinates an explicit Git ref update through a private operation
// journal and the service's exclusive target lease. No local card is created.
func (s *Session) Integrate(ctx context.Context, dir string, options IntegrationOptions) (api.Integration, error) {
	if err := validateIntegrationOptions(options); err != nil {
		return api.Integration{}, err
	}
	options.Contributors = slices.Sorted(slices.Values(options.Contributors))
	journal, fresh, err := s.BeginAction("integration", options.OperationID)
	if err != nil {
		return api.Integration{}, err
	}
	defer journal.Close()
	state := integrationOperation{Schema: 1, Options: options}
	if !fresh {
		if json.Unmarshal(journal.Value.Result, &state) != nil || state.Schema != 1 || !sameIntegrationOptions(state.Options, options) {
			return api.Integration{}, integrationFailure("remote.integration_operation_changed")
		}
	}
	if state.Result != nil {
		return *state.Result, nil
	}
	if err := storeIntegrationOperation(journal, &state); err != nil {
		return api.Integration{}, err
	}
	// A completed execution remains readable even after a newer target lease or
	// a task handoff. This is receipt recovery, never renewed write authority.
	for _, attempt := range slices.Backward(state.Attempts) {
		previous, err := s.Project.IntegrationExecution(ctx, api.IntegrationID(options.TargetRef), attempt.Request.ExecutionID)
		if err == nil && previous.Phase == "integrated" {
			return s.finishIntegration(journal, &state, previous)
		}
		if err != nil && !api.IsCode(err, api.CodeNotFound) {
			return api.Integration{}, err
		}
	}
	actor, err := s.integrationRole(options)
	if err != nil {
		return api.Integration{}, err
	}
	if state.Actor != "" && state.Actor != actor.ExecutionID {
		return api.Integration{}, integrationFailure("remote.integration_members_changed")
	}
	state.Actor = actor.ExecutionID
	facts, boot, err := integrationProcess(ctx)
	if err != nil {
		return api.Integration{}, err
	}
	for range 3 {
		var attempt *integrationAttempt
		if len(state.Attempts) > 0 {
			attempt = state.Attempts[len(state.Attempts)-1]
		}
		if attempt == nil || attempt.Retired || attempt.Failed {
			if len(state.Attempts) >= 32 {
				return api.Integration{}, integrationFailure("remote.integration_recovery_required")
			}
			request, err := s.prepareIntegration(ctx, dir, options, actor)
			if err != nil {
				return api.Integration{}, err
			}
			attempt = &integrationAttempt{Request: request, Key: client.NewKey(), Actions: map[string]*integrationActionCall{}}
			state.Attempts = append(state.Attempts, attempt)
			if err := storeIntegrationOperation(journal, &state); err != nil {
				return api.Integration{}, err
			}
		}
		knownClaim := attempt.Claimed
		grant, err := s.Project.ClaimIntegration(ctx, attempt.Key, attempt.Request, *actor.Claim())
		if err != nil {
			if requestFailureKnown(err) {
				attempt.Failed = true
				_ = storeIntegrationOperation(journal, &state)
			}
			return api.Integration{}, err
		}
		attempt.Claimed = true
		if err := storeIntegrationOperation(journal, &state); err != nil {
			return api.Integration{}, err
		}
		current, err := s.Project.Integration(ctx, grant.Integration.ID)
		if err != nil {
			return api.Integration{}, err
		}
		if current.Request.ExecutionID != attempt.Request.ExecutionID || current.Generation != grant.Integration.Generation {
			return api.Integration{}, integrationFailure("remote.integration_members_changed")
		}
		if current.Phase == "integrated" {
			return s.finishIntegration(journal, &state, current)
		}
		proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
		if current.Status == api.ClaimReleased {
			call := attempt.Actions["release"]
			if call == nil || call.Failed {
				return api.Integration{}, integrationFailure("remote.integration_recovery_required")
			}
			if _, err := s.integrationActionCall(ctx, dir, journal, &state, attempt, "release", proof); err != nil {
				return api.Integration{}, err
			}
		}
		if current.Status == api.ClaimReleased || current.Status == api.ClaimLost {
			if err := s.retireIntegration(journal, &state, attempt, current); err != nil {
				return api.Integration{}, err
			}
			continue
		}
		if current.Status != api.ClaimActive {
			return api.Integration{}, integrationFailure("remote.integration_recovery_required")
		}
		credential, found, err := s.credentialFor(current.ID, attempt.Request.ExecutionID)
		if err != nil {
			return api.Integration{}, err
		}
		valid, err := s.integrationScopeCurrent(ctx, attempt.Request)
		if err != nil {
			return api.Integration{}, err
		}
		head, err := s.integrationTarget(ctx, dir, options.TargetRef)
		if err != nil {
			return api.Integration{}, err
		}
		rotate := knownClaim && (!found || credential.Lost || credential.AgentPID != facts.PID || credential.AgentStart != facts.Start || credential.BootID != boot)
		rotate = rotate || !valid || current.Phase == "reserved" && head != attempt.Request.ExpectedTarget
		if current.Phase == "pushing" && head != attempt.Request.ExpectedTarget {
			contained, err := s.integrationContains(ctx, dir, options.Commit, head)
			if err != nil {
				return api.Integration{}, err
			}
			rotate = rotate || !contained
		}
		if rotate {
			if _, err := s.integrationActionCall(ctx, dir, journal, &state, attempt, "release", proof); err != nil {
				return api.Integration{}, err
			}
			if err := s.retireIntegration(journal, &state, attempt, current); err != nil {
				return api.Integration{}, err
			}
			continue
		}
		credential = Credential{ResourceKind: "integration", AuthorityExecutionID: actor.ExecutionID, ServerURL: s.ServerURL, ClientID: s.Client.ClientID(), ProjectID: s.Info.ID, TaskID: current.ID, ExecutionID: attempt.Request.ExecutionID, Generation: current.Generation, Token: grant.Token, RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: time.Now().UTC(), AgentPID: facts.PID, AgentStart: facts.Start, BootID: boot, Branch: options.TargetRef, BaseCommit: attempt.Request.BaseCommit}
		if err := s.SaveCredential(credential); err != nil {
			return api.Integration{}, err
		}
		starter := s.WorkerStarter
		if starter == nil {
			starter = EnsureWorker
		}
		if err := starter(s.Home); err != nil {
			return api.Integration{}, err
		}
		if err := s.publishIntegrationSource(ctx, dir, journal, &state, attempt, current, proof); err != nil {
			return api.Integration{}, err
		}
		if !attempt.Request.AlreadyContained {
			if current.Phase == "reserved" {
				current, err = s.integrationActionCall(ctx, dir, journal, &state, attempt, "pushing", proof)
				if err != nil {
					return api.Integration{}, err
				}
			}
			if _, err := s.Project.RenewIntegration(ctx, current.ID, proof); err != nil {
				return api.Integration{}, err
			}
			head, err = s.integrationTarget(ctx, dir, options.TargetRef)
			if err != nil {
				return api.Integration{}, err
			}
			contains, err := s.integrationContains(ctx, dir, options.Commit, head)
			if err != nil {
				return api.Integration{}, err
			}
			if !contains {
				if head != attempt.Request.ExpectedTarget {
					return api.Integration{}, integrationFailure("remote.git_ref_conflict")
				}
				attempt.PushAttempts++
				if err := storeIntegrationOperation(journal, &state); err != nil {
					return api.Integration{}, err
				}
				if _, err := s.Project.RenewIntegration(ctx, current.ID, proof); err != nil {
					return api.Integration{}, err
				}
				_, pushErr := gitCommand(ctx, dir, "push", "--porcelain", "--force-with-lease="+options.TargetRef+":"+attempt.Request.ExpectedTarget, "origin", options.Commit+":"+options.TargetRef)
				after, observeErr := s.integrationTarget(ctx, dir, options.TargetRef)
				if observeErr != nil {
					return api.Integration{}, errors.Join(pushErr, observeErr)
				}
				contains, verifyErr := s.integrationContains(ctx, dir, options.Commit, after)
				if verifyErr != nil || !contains {
					return api.Integration{}, errors.Join(pushErr, verifyErr, integrationFailure("remote.integration_recovery_required"))
				}
			}
		}
		confirmed, err := s.integrationActionCall(ctx, dir, journal, &state, attempt, "confirm", proof)
		if err != nil {
			return api.Integration{}, err
		}
		return s.finishIntegration(journal, &state, confirmed)
	}
	return api.Integration{}, integrationFailure("remote.integration_recovery_required")
}
