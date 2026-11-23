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
		if task.Claim == nil || !api.ClaimStands(task.Claim.Status) || task.Delivery == nil || task.Delivery.Purpose == "baseline" || task.Delivery.Commit != d.Commit || task.Delivery.Generation != task.Claim.Generation || task.Delivery.ExecutionID != task.Claim.ExecutionID || board.MetadataFrom(task.Text, board.FieldTaskBranch) != d.Branch {
			return launchError("board.coordinator_conflict", "delivery differs from the service execution")
		}
		_, err = s.FetchDelivery(ctx, cwd, d.TaskID, d.Commit)
		return err
	}
	if err := verifyPortableWrapUpTarget(cwd, facts.IntegrationTarget, facts.Integration.TargetRef); err != nil {
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

// coordinatorGitCheck is the verdict sent to the server for one fact. The text
// is the server text: it carries no Git diagnostic, which stays on this
// machine, and the server compares verdict texts of equal facts byte for byte.
func coordinatorGitCheck(facts board.CoordinatorGitFacts, err error) api.CoordinatorGitCheck {
	check := api.CoordinatorGitCheck{Facts: facts}
	if err != nil {
		check.Error = remote.ServerText(err)
	}
	return check
}
