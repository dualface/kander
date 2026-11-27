package launch

import (
	"context"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// startBranch names the branch a new execution of the card starts from: the
// group branch group/<id> for a task group member, otherwise the card's
// integration target, which is the mainline branch when the card names none.
// The mainline branch comes from the branch snapshot of the card or its group
// when there is one, otherwise from the configuration.
func startBranch(cfg *config.Config, text string, snapshot *api.BranchSnapshot) (string, error) {
	// The target is checked for every card, a group member included: a member
	// starts from the group branch, but its group still delivers to the target.
	target, err := board.IntegrationTargetOf(text)
	if err != nil {
		return "", err
	}
	mainline := mainlineFor(cfg, snapshot)
	// In main-develop mode main advances only from develop, so a card may not
	// name it as a target; the dispatch evidence check applies the same rule.
	if target == "main" && mainline == "develop" {
		return "", launchError("launch.start_base_main_in_develop_mode")
	}
	if group := taskGroupFrom(text); group != "" {
		// The group ID becomes part of a ref name, so it must be a valid ID.
		if !board.ValidTaskGroup(group) {
			return "", launchError("launch.start_base_group_invalid", group)
		}
		return "group/" + group, nil
	}
	if target == "" {
		return mainline, nil
	}
	return target, nil
}

// mainlineFor is the mainline branch of a card: that of its branch snapshot
// (its own, or its task group's) when it has one, otherwise that of the
// configuration. A snapshot keeps work in progress on the branch model it
// started with after the project policy changed.
func mainlineFor(cfg *config.Config, snapshot *api.BranchSnapshot) string {
	if snapshot != nil {
		return config.MainlineBranchFor(snapshot.BranchMode)
	}
	return config.MainlineBranch(cfg)
}

// branchSnapshotOf finds the branch snapshot a card delivers under. The
// server puts it in the claim of the card, its own or its group's; a task
// group member without a claim of its own (a card still in todo) reads the
// snapshot from the group lease. nil when there is none.
func branchSnapshotOf(ctx context.Context, session *remote.Session, task api.Task) (*api.BranchSnapshot, error) {
	if task.Claim != nil && task.Claim.BranchSnapshot != nil {
		return task.Claim.BranchSnapshot, nil
	}
	group := taskGroupFrom(task.Text)
	if group == "" || !board.ValidTaskGroup(group) {
		return nil, nil
	}
	view, err := session.Project.Group(ctx, group)
	if err != nil || view.Claim == nil {
		return nil, err
	}
	return view.Claim.BranchSnapshot, nil
}
