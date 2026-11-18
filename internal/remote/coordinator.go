package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
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
	if found && credential.ResourceKind != "group" {
		return api.GroupGrant{}, errors.New("group credential has a different resource kind")
	}
	if bound {
		if err := s.refuseEndedCoordinator(request.GroupID, execution, view, credential, found); err != nil {
			return api.GroupGrant{}, err
		}
	}
	var proof *client.Claim
	if found && !credential.Lost && view.Claim != nil && api.ClaimStands(view.Claim.Status) && credential.Generation == view.Claim.Generation {
		execution, proof = credential.ExecutionID, credential.Claim()
	}
	input := api.GroupClaim{Request: request, ExecutionID: execution, Decision: decision}
	if view.Claim != nil {
		input.ExpectedGeneration = view.Claim.Generation
	}
	grant, err := s.Project.ClaimGroup(ctx, client.NewKey(), request.GroupID, input, proof)
	if err != nil {
		return grant, heldClaimError(err, proof, request.GroupID, execution)
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

// maxLostReason bounds the reason of a lost lease quoted in a refusal; the
// server accepts up to 4096 bytes.
const maxLostReason = 200

// refuseEndedCoordinator refuses a claim of a bound coordinator execution
// whose own lease ended, after the group view was read and before a claim
// request is sent. The service refuses the
// same execution anyway, and a grant of a new generation could not be stored:
// a released execution has a tombstone, and a credential never changes its
// generation. Each ending gets its own text and next step.
func (s *Session) refuseEndedCoordinator(group, execution string, view api.GroupView, credential Credential, found bool) error {
	refuse := func(id string, args ...any) error {
		return &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text(id, args...)}
	}
	own := view.Claim != nil && view.Claim.ExecutionID == execution
	released, err := fs.RegularFileExists(s.Home, filepath.Join(credentialDir(s.Home, s.ServerURL, s.Info.ID, group), execution+".released"))
	if err != nil {
		return err
	}
	switch {
	case released, own && view.Claim.Status == api.ClaimReleased:
		return refuse("remote.coordinator_execution_released", execution, group)
	case found && !own && view.Claim != nil && view.Claim.Generation > credential.Generation:
		return refuse("remote.coordinator_execution_taken_over", execution, group, credential.Generation, view.Claim.Generation)
	case own && view.Claim.Status == api.ClaimLost, found && credential.Lost:
		generation, reason := credential.Generation, credential.LostReason
		if own {
			generation = view.Claim.Generation
			if view.Claim.Observation != nil && view.Claim.Observation.Reason != "" {
				reason = view.Claim.Observation.Reason
			}
		}
		if reason = lostReasonText(reason); reason == "" {
			return refuse("remote.coordinator_execution_lost", execution, group, generation)
		}
		return refuse("remote.coordinator_execution_lost_reason", execution, group, generation, reason)
	}
	return nil
}

// lostReasonText folds a reason onto one line and cuts it to a length that
// fits a refusal.
func lostReasonText(reason string) string {
	reason = strings.Join(strings.Fields(reason), " ")
	if runes := []rune(reason); len(runes) > maxLostReason {
		reason = string(runes[:maxLostReason]) + "..."
	}
	return reason
}

// heldClaimError explains a refused claim of an execution that holds its lease.
// The checkpoint kernel refuses a changed request because the lease token of
// this generation is the same, so the holder needs no claim at all. The code
// stays as the service sent it.
func heldClaimError(err error, proof *client.Claim, group, execution string) error {
	var refused *api.Error
	if proof == nil || !errors.As(err, &refused) || refused.Code != api.CodeBoardRejected || refused.BoardCode != "board.coordinator_conflict" {
		return err
	}
	held := *refused
	held.Message = config.Text("remote.coordinator_claim_already_held", execution, group, proof.Generation, refused.Message)
	return &held
}
