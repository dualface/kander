package launch

import (
	"context"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// The service owns the closed plan. The candidate and observed ref already
// exist in the integration checkout; a preflight never registers or publishes them.
func runIntegrationPreflight(ctx context.Context, cfg *config.Config, path string, input board.DispatchInput) int {
	if !filepath.IsAbs(path) {
		return fail(launchError("board.dispatch_evidence_invalid", config.Text("launch.integration_preflight_path")))
	}
	if input.Evidence.WrapUp == nil || input.Evidence.WrapUp.Git == (board.DispatchIntegration{}) {
		return fail(launchError("board.dispatch_evidence_invalid", config.Text("launch.integration_preflight_git")))
	}
	g := input.Evidence.WrapUp.Git
	if !filepath.IsAbs(g.CWD) {
		return fail(launchError("board.dispatch_evidence_invalid", config.Text("launch.integration_preflight_cwd")))
	}
	session, err := remote.OpenReadOnly(ctx, cfg, g.CWD)
	if err != nil {
		return fail(err)
	}
	card, err := session.Project.Show(ctx, input.TaskID)
	if err != nil {
		return fail(err)
	}
	target, err := board.IntegrationTargetOf(card.Text)
	if err != nil {
		return fail(err)
	}
	span, err := session.Project.DispatchRange(ctx, input.TaskID)
	if err != nil {
		return fail(err)
	}
	if (g.ReviewBase != "" && g.ReviewBase != span.Ancestor) || (g.ReviewTarget != "" && g.ReviewTarget != span.Descendant) {
		return fail(launchError("board.dispatch_evidence_invalid", config.Text("launch.integration_preflight_range")))
	}
	g.ReviewBase, g.ReviewTarget = span.Ancestor, span.Descendant
	gitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	snapshot, err := branchSnapshotOf(ctx, session, card)
	if err != nil {
		return fail(err)
	}
	if err := checkIntegrationTarget(gitCtx, g, target, snapshot, true); err != nil {
		return fail(err)
	}
	result := map[string]string{
		"task_id": input.TaskID, "review_base": g.ReviewBase, "review_target": g.ReviewTarget,
		"source_commit": g.SourceCommit, "target_commit": g.TargetCommit, "target_ref": g.TargetRef,
	}
	if g.RebasedBase != "" {
		result["rebased_base"] = g.RebasedBase
	}
	return command.Object(result)
}
