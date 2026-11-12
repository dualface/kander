package remote

import (
	"context"
	"errors"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

func (s *Session) publishIntegrationSource(ctx context.Context, dir string, journal *ActionJournal, state *integrationOperation, attempt *integrationAttempt, current api.Integration, proof client.Claim) error {
	ref := api.IntegrationSourceRef(s.Info.ID, attempt.Request.ExecutionID, attempt.Request.Commit)
	if current.SourceRef != ref {
		return integrationFailure("remote.integration_members_changed")
	}
	if _, err := s.Project.RenewIntegration(ctx, current.ID, proof); err != nil {
		return err
	}
	if err := verifiedRemote(ctx, dir, s.Info.Remote); err != nil {
		return err
	}
	exists, err := observeDeliveryRef(ctx, dir, ref, attempt.Request.Commit)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	// The immutable effect has one deterministic ref, tied to this granted
	// execution. Record intent first; a retry observes it before another push.
	attempt.SourceAttempts++
	if err := storeIntegrationOperation(journal, state); err != nil {
		return err
	}
	if _, err := s.Project.RenewIntegration(ctx, current.ID, proof); err != nil {
		return err
	}
	_, pushErr := gitCommand(ctx, dir, "push", "--porcelain", "--force-with-lease="+ref+":", "origin", attempt.Request.Commit+":"+ref)
	exists, err = observeDeliveryRef(ctx, dir, ref, attempt.Request.Commit)
	if err != nil || !exists {
		return errors.Join(pushErr, err, integrationFailure("remote.integration_recovery_required"))
	}
	return nil
}
func (s *Session) fetchIntegrationSource(ctx context.Context, dir string, value api.Integration) error {
	ref := api.IntegrationSourceRef(s.Info.ID, value.Request.ExecutionID, value.Request.Commit)
	if value.SourceRef != ref {
		return integrationFailure("remote.integration_members_changed")
	}
	if err := verifiedRemote(ctx, dir, s.Info.Remote); err != nil {
		return err
	}
	exists, err := observeDeliveryRef(ctx, dir, ref, value.Request.Commit)
	if err != nil {
		return err
	}
	if !exists {
		return integrationFailure("remote.git_commit_unavailable")
	}
	_, err = gitCommand(ctx, dir, "fetch", "--no-tags", "origin", ref)
	return err
}
