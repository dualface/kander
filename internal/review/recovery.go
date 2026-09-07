package review

import (
	"context"
	"reflect"

	"github.com/dualface/kander/internal/board"
)

// VerifyClosedReviewGit rechecks historical closure edges and mechanical diffs
// in a surviving repository worktree. It does not require HEAD to remain at an
// old batch target, change a ref, reopen a review or replace any original.
// The caller first validates the closure and all its copies through board.
func VerifyClosedReviewGit(ctx context.Context, cwd string, c board.ReviewClosure) error {
	edges, statuses, err := board.ReviewClosureEdges(c.View, c.Request)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(edges, c.Git.Edges) || !reflect.DeepEqual(statuses, c.RoleStatuses) {
		return archiveError("closed review binding")
	}
	if c.Git.NotApplicable != "" {
		if board.ReviewNeedsGit(board.ReviewPlanBatch{Base: c.View.Batch.Base, TargetCommit: c.TargetCommit, Requirements: c.View.Batch.Requirements}) {
			return archiveError("invalid Git N/A")
		}
		return ctx.Err()
	}
	for _, edge := range edges {
		if err = verifyGitEdgeContext(ctx, cwd, edge); err != nil {
			return err
		}
	}
	mechanical, err := verifyMechanicalGitContext(ctx, cwd, c.View, c.Request)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(mechanical, c.Git.Mechanical) {
		return archiveError("closed mechanical evidence changed")
	}
	return ctx.Err()
}
