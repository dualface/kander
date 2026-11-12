package launch

import (
	"context"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/review"
)

func verifyRemoteCoordinatorGitFacts(ctx context.Context, s *remote.Session, cwd string, facts board.CoordinatorGitFacts) error {
	if d := facts.Delivery; d != nil {
		task, err := s.Project.Show(ctx, d.TaskID)
		if err != nil {
			return err
		}
		if task.Claim == nil || task.Claim.Status != api.ClaimActive || task.Delivery == nil || task.Delivery.Purpose == "baseline" || task.Delivery.Commit != d.Commit || task.Delivery.Generation != task.Claim.Generation || task.Delivery.ExecutionID != task.Claim.ExecutionID || board.MetadataFrom(task.Text, board.FieldTaskBranch) != d.Branch {
			return launchError("board.coordinator_conflict", "delivery differs from the service execution")
		}
		_, err = s.FetchDelivery(ctx, cwd, d.TaskID, d.Commit)
		return err
	}
	if err := s.VerifyWrapUpIntegration(ctx, cwd, facts.Integration); err != nil {
		return err
	}
	for _, closure := range facts.Closures {
		if err := review.VerifyClosedReviewGit(ctx, cwd, closure); err != nil {
			return err
		}
	}
	return s.VerifyWrapUpIntegration(ctx, cwd, facts.Integration)
}
