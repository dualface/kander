package remote

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func (s *Session) AcquireCoordinator(ctx context.Context, request board.CoordinatorClaim, decision string) (api.GroupGrant, error) {
	view, err := s.Project.Group(ctx, request.GroupID)
	if err != nil {
		return api.GroupGrant{}, err
	}
	credential, found, err := s.CoordinatorCredential(request.GroupID)
	if err != nil {
		return api.GroupGrant{}, err
	}
	execution := os.Getenv(EnvCoordinatorExecutionID)
	if execution == "" {
		execution = os.Getenv(EnvExecutionID)
	}
	bound := execution != ""
	if execution == "" {
		execution = NewExecutionID()
	}
	var proof *client.Claim
	if found {
		if credential.ResourceKind != "group" {
			return api.GroupGrant{}, errors.New("group credential has a different resource kind")
		}
		if credential.Lost && bound {
			return api.GroupGrant{}, &api.Error{Code: api.CodeLeaseInvalid, Message: "this coordinator execution already lost its lease"}
		}
		if !credential.Lost && view.Claim != nil && view.Claim.Status == api.ClaimActive && credential.Generation == view.Claim.Generation {
			execution, proof = credential.ExecutionID, credential.Claim()
		}
	}
	input := api.GroupClaim{Request: request, ExecutionID: execution, Decision: decision}
	if view.Claim != nil {
		input.ExpectedGeneration = view.Claim.Generation
		if view.Claim.ExecutionID == execution && view.Claim.Status != api.ClaimActive {
			return api.GroupGrant{}, &api.Error{Code: api.CodeLeaseInvalid, Message: "a lost coordinator execution cannot acquire a later generation"}
		}
	}
	grant, err := s.Project.ClaimGroup(ctx, client.NewKey(), request.GroupID, input, proof)
	if err != nil {
		return grant, err
	}
	credential = Credential{ResourceKind: "group", ServerURL: s.ServerURL, ClientID: s.Client.ClientID(), ProjectID: s.Info.ID, TaskID: request.GroupID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: time.Now().UTC()}
	if err := RefreshExecutionContext(s.Home, &credential); err != nil {
		return grant, err
	}
	if err := s.SaveCredential(credential); err != nil {
		return grant, err
	}
	return grant, nil
}
func (s *Session) CoordinatorAuthority(group string, tasks []string) (client.Claim, error) {
	credential, found, err := s.CoordinatorCredential(group)
	if err != nil {
		return client.Claim{}, err
	}
	if !found || credential.Lost || credential.ResourceKind != "group" {
		return client.Claim{}, &api.Error{Code: api.CodeLeaseInvalid, Message: "this process has no live coordinator lease"}
	}
	proof, err := s.ClaimsFor(tasks)
	if err != nil {
		return client.Claim{}, err
	}
	if proof == nil {
		proof = &client.Claim{}
	}
	proof.Token, proof.Generation = credential.Token, credential.Generation
	return *proof, nil
}
