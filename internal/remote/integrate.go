package remote

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// errTargetMoved reports that the target changed while a new attempt was being
// prepared. Integrate starts the attempt over from a fresh observation.
var errTargetMoved = errors.New("integration target moved during preparation")

// Reasons an attempt is retired. The journal keeps the reason of each one.
const (
	retireTargetMoved   = "target_moved"
	retireClaimEnded    = "claim_ended"
	retireIdentity      = "identity_changed"
	retireScope         = "scope_invalid"
	retireAutoMergeLost = "auto_merge_lost"
)

// prepareIntegration observes the target and decides how the reviewed commit
// reaches it. A generated merge exists only once the decision is made; a later
// failure of the preparation deletes its private ref again.
func (s *Session) prepareIntegration(ctx context.Context, dir string, options IntegrationOptions, actor Credential, carried *api.IntegrationRequest) (request api.IntegrationRequest, err error) {
	request = api.IntegrationRequest{Purpose: options.Purpose, TargetRef: options.TargetRef, Commit: options.Commit, GroupID: options.GroupID, ExecutionID: NewExecutionID(), ActorExecutionID: actor.ExecutionID, Decision: options.Decision}
	defer func() {
		if err == nil {
			return
		}
		if dropped := dropAutoMerge(ctx, dir, request); dropped != nil {
			err = errors.Join(err, dropped)
		}
	}()
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
	if err := s.integrationContainment(ctx, dir, options, &request, carried); err != nil {
		return request, err
	}
	request.Edges = request.ContainmentEdges()
	for _, member := range members {
		if member.DeliveryCommit != "" {
			edges, err := s.integrationMemberEdges(ctx, dir, request, member.DeliveryCommit)
			if err != nil {
				return request, err
			}
			request.Edges = append(request.Edges, edges...)
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
		return request, errTargetMoved
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
		// A completion whose cleanup failed is finished again by the next call.
		return *state.Result, dropAutoMerges(ctx, dir, &state)
	}
	if err := storeIntegrationOperation(journal, &state); err != nil {
		return api.Integration{}, err
	}
	// A completed execution remains readable even after a newer target lease or
	// a task handoff. This is receipt recovery, never renewed write authority.
	for _, attempt := range slices.Backward(state.Attempts) {
		previous, err := s.Project.IntegrationExecution(ctx, api.IntegrationID(options.TargetRef), attempt.Request.ExecutionID)
		if err == nil && previous.Phase == "integrated" {
			return s.finishIntegration(ctx, dir, journal, &state, previous)
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
	// last is why the previous pass gave up its attempt. A command that runs out
	// of passes while the target keeps moving says so; any other reason leaves
	// the unresolved-integration error.
	last := ""
	for range 3 {
		var attempt *integrationAttempt
		if len(state.Attempts) > 0 {
			attempt = state.Attempts[len(state.Attempts)-1]
		}
		if attempt == nil || attempt.Retired || attempt.Failed {
			if len(state.Attempts) >= 32 {
				return api.Integration{}, integrationFailure("remote.integration_recovery_required")
			}
			carried := carriedAutoMerge(&state)
			request, err := s.prepareIntegration(ctx, dir, options, actor, carried)
			if errors.Is(err, errTargetMoved) {
				last = retireTargetMoved
				if carried != nil {
					if err := dropAutoMerge(ctx, dir, *carried); err != nil {
						return api.Integration{}, err
					}
				}
				continue
			}
			if err != nil {
				return api.Integration{}, err
			}
			attempt = &integrationAttempt{Request: request, Key: client.NewKey(), Actions: map[string]*integrationActionCall{}}
			state.Attempts = append(state.Attempts, attempt)
			if err := storeIntegrationOperation(journal, &state); err != nil {
				return api.Integration{}, err
			}
			// The carried merge is adopted or stale by now. The new attempt is in the
			// journal first, so a failed deletion leaves only refs a completion drops.
			if carried != nil {
				if err := dropAutoMerge(ctx, dir, *carried); err != nil {
					return api.Integration{}, err
				}
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
			return s.finishIntegration(ctx, dir, journal, &state, current)
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
			if err := s.retireIntegration(ctx, dir, journal, &state, attempt, current, retireClaimEnded); err != nil {
				return api.Integration{}, err
			}
			last = retireClaimEnded
			continue
		}
		if !api.ClaimStands(current.Status) {
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
		reason := ""
		moved := head != attempt.Request.ExpectedTarget
		switch {
		case knownClaim && (!found || credential.Lost || credential.AgentPID != facts.PID || credential.AgentStart != facts.Start || credential.BootID != boot):
			reason = retireIdentity
		case !valid:
			reason = retireScope
		case current.Phase == "reserved" && moved:
			reason = retireTargetMoved
		case current.Phase == "pushing" && moved:
			contained, err := s.integrationContains(ctx, dir, attempt.Request.PushCommit(), head)
			if err != nil {
				return api.Integration{}, err
			}
			if !contained {
				reason = retireTargetMoved
			}
		}
		if reason == "" && attempt.Request.ViaAutoMerge {
			kept, err := s.autoMergeKept(ctx, dir, attempt.Request, head)
			if err != nil {
				return api.Integration{}, err
			}
			if !kept {
				reason = retireAutoMergeLost
			}
		}
		if reason != "" {
			if err := s.rotateIntegration(ctx, dir, journal, &state, attempt, current, proof, reason); err != nil {
				return api.Integration{}, err
			}
			last = reason
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
				pushing, err := s.integrationActionCall(ctx, dir, journal, &state, attempt, "pushing", proof)
				if err != nil {
					// The service refuses the push intent when the target is no longer
					// the one this attempt observed; the attempt starts over.
					moved, observeErr := s.integrationMoved(ctx, dir, attempt, err)
					if observeErr != nil || !moved {
						return api.Integration{}, errors.Join(err, observeErr)
					}
					if err := s.rotateIntegration(ctx, dir, journal, &state, attempt, current, proof, retireTargetMoved); err != nil {
						return api.Integration{}, err
					}
					last = retireTargetMoved
					continue
				}
				current = pushing
			}
			if _, err := s.Project.RenewIntegration(ctx, current.ID, proof); err != nil {
				return api.Integration{}, err
			}
			head, err = s.integrationTarget(ctx, dir, options.TargetRef)
			if err != nil {
				return api.Integration{}, err
			}
			contains, err := s.integrationContains(ctx, dir, attempt.Request.PushCommit(), head)
			if err != nil {
				return api.Integration{}, err
			}
			if !contains {
				if head != attempt.Request.ExpectedTarget {
					if err := s.rotateIntegration(ctx, dir, journal, &state, attempt, current, proof, retireTargetMoved); err != nil {
						return api.Integration{}, err
					}
					last = retireTargetMoved
					continue
				}
				attempt.PushAttempts++
				if err := storeIntegrationOperation(journal, &state); err != nil {
					return api.Integration{}, err
				}
				if _, err := s.Project.RenewIntegration(ctx, current.ID, proof); err != nil {
					return api.Integration{}, err
				}
				_, pushErr := gitCommand(ctx, dir, "push", "--porcelain", "--force-with-lease="+options.TargetRef+":"+attempt.Request.ExpectedTarget, "origin", attempt.Request.PushCommit()+":"+options.TargetRef)
				after, observeErr := s.integrationTarget(ctx, dir, options.TargetRef)
				if observeErr != nil {
					return api.Integration{}, errors.Join(pushErr, observeErr)
				}
				contains, verifyErr := s.integrationContains(ctx, dir, attempt.Request.PushCommit(), after)
				if verifyErr == nil && !contains && after != attempt.Request.ExpectedTarget {
					// The lease held the push back because the target moved in between;
					// nothing of this attempt reached the target.
					if err := s.rotateIntegration(ctx, dir, journal, &state, attempt, current, proof, retireTargetMoved); err != nil {
						return api.Integration{}, err
					}
					last = retireTargetMoved
					continue
				}
				if verifyErr != nil || !contains {
					return api.Integration{}, errors.Join(pushErr, verifyErr, integrationFailure("remote.integration_recovery_required"))
				}
			}
		}
		confirmed, err := s.integrationActionCall(ctx, dir, journal, &state, attempt, "confirm", proof)
		if err != nil {
			return api.Integration{}, err
		}
		return s.finishIntegration(ctx, dir, journal, &state, confirmed)
	}
	if last == retireTargetMoved {
		return api.Integration{}, integrationFailure("remote.integration_target_moving")
	}
	return api.Integration{}, integrationFailure("remote.integration_recovery_required")
}

// integrationMoved reports whether a refused action followed a move of the
// target: the refusal is a known one, and the target now differs from the one
// the attempt observed. Any other refusal keeps its own error.
func (s *Session) integrationMoved(ctx context.Context, dir string, attempt *integrationAttempt, refusal error) (bool, error) {
	if !requestFailureKnown(refusal) {
		return false, nil
	}
	head, err := s.integrationTarget(ctx, dir, attempt.Request.TargetRef)
	if err != nil {
		return false, err
	}
	return head != attempt.Request.ExpectedTarget, nil
}
