package remote

import (
	"context"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// VerifyIntegration rechecks a service receipt through the registered common
// remote. A rewound or unavailable target never passes on cached Git objects.
func (s *Session) VerifyIntegration(ctx context.Context, dir string, value api.Integration) error {
	if value.Phase != "integrated" || value.Status != api.ClaimReleased || value.Request.Purpose != "deliver" || value.ID != api.IntegrationID(value.Request.TargetRef) || !api.CommitSHA(value.Request.Commit) || !api.CommitSHA(value.ObservedTarget) {
		return integrationFailure("remote.integration_recovery_required")
	}
	if err := s.fetchIntegrationSource(ctx, dir, value); err != nil {
		return err
	}
	for _, member := range value.Request.Members {
		if member.DeliveryCommit != "" {
			if _, err := s.FetchDelivery(ctx, dir, member.TaskID, member.DeliveryCommit); err != nil {
				return err
			}
		}
	}
	for _, commit := range []string{value.Request.Commit, value.ObservedTarget} {
		if err := s.FetchCommit(ctx, dir, commit); err != nil {
			return err
		}
	}
	head, err := s.integrationTarget(ctx, dir, value.Request.TargetRef)
	if err != nil {
		return err
	}
	for _, source := range []string{value.Request.Commit, value.ObservedTarget} {
		contained, err := s.integrationContains(ctx, dir, source, head)
		if err != nil {
			return err
		}
		if !contained {
			return integrationFailure("remote.integration_recovery_required")
		}
	}
	return nil
}

// VerifyWrapUpIntegration binds portable dispatch evidence to the immutable
// service execution, then checks actual Git facts in this caller's clone.
func (s *Session) VerifyWrapUpIntegration(ctx context.Context, dir string, g board.DispatchIntegration) error {
	if g.IntegrationID == "" || g.IntegrationExecution == "" || g.IntegrationGeneration == 0 || g.CWD != "project:"+s.Info.ID {
		return integrationFailure("remote.integration_recovery_required")
	}
	value, err := s.Project.IntegrationExecution(ctx, g.IntegrationID, g.IntegrationExecution)
	if err != nil {
		return err
	}
	if value.Generation != g.IntegrationGeneration || value.Request.TargetRef != g.TargetRef || value.Request.Commit != g.SourceCommit || value.ObservedTarget != g.TargetCommit || g.ReviewTarget != g.SourceCommit || g.RebasedBase != "" {
		return integrationFailure("remote.integration_members_changed")
	}
	if err := s.VerifyIntegration(ctx, dir, value); err != nil {
		return err
	}
	if err := s.FetchCommit(ctx, dir, g.ReviewBase); err != nil {
		return err
	}
	ancestor, err := gitAncestor(ctx, dir, g.ReviewBase, g.ReviewTarget)
	if err != nil {
		return err
	}
	if !ancestor {
		return integrationFailure("remote.integration_members_changed")
	}
	return nil
}
