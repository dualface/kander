package launch

import (
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// startBranch names the branch a new execution of the card starts from: the
// group branch group/<id> for a task group member, otherwise the card's
// integration target, which is the mainline branch when the card names none.
func startBranch(cfg *config.Config, text string) (string, error) {
	if group := taskGroupFrom(text); group != "" {
		// The group ID becomes part of a ref name, so it must be a valid ID.
		if !board.ValidTaskGroup(group) {
			return "", launchError("launch.start_base_group_invalid", group)
		}
		return "group/" + group, nil
	}
	target, err := board.IntegrationTargetOf(text)
	if err != nil {
		return "", err
	}
	mainline := config.MainlineBranch(cfg)
	if target == "" {
		return mainline, nil
	}
	// In main-develop mode main advances only from develop, so a card may not
	// name it as a target; the dispatch evidence check applies the same rule.
	if target == "main" && mainline == "develop" {
		return "", launchError("launch.start_base_main_in_develop_mode")
	}
	return target, nil
}
