package review

import (
	"fmt"
	"os"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// reviewPinning is the REVIEW_<ROLE>_* pin shared by every card bound to one
// task-bound review run. scale is set only when a bound card pins anything:
// the common card size, or large for a mixed-size batch.
type reviewPinning struct {
	pin   board.StagePin
	any   bool
	scale string
}

// boundReviewPinning reads the bound cards and requires them to agree on the
// role's pin; one card pinning while another leaves the role unset is a conflict.
func boundReviewPinning(root string, taskIDs []string, role string) (reviewPinning, error) {
	cfg, err := config.Effective(nil)
	if err != nil {
		return reviewPinning{}, err
	}
	var result reviewPinning
	sizes := map[string]bool{}
	for i, id := range taskIDs {
		snapshot, err := board.ReadSnapshot(root, id)
		if err != nil {
			return reviewPinning{}, err
		}
		pins, err := board.CardPinsOf(cfg, snapshot.Text)
		if err != nil {
			return reviewPinning{}, err
		}
		pin := pins.Review[role]
		if i == 0 {
			result.pin = pin
		} else if pin != result.pin {
			return reviewPinning{}, newGate(2, "review.pin_batch_conflict", board.ReviewPinField(role, "*"), taskIDs[0], id)
		}
		result.any = result.any || pins.Any()
		sizes[kindOrSmall(snapshot.Entry.Kind)] = true
	}
	if result.any {
		result.scale = "large"
		if len(sizes) == 1 && sizes["small"] {
			result.scale = "small"
		}
	}
	return result, nil
}

func kindOrSmall(kind string) string {
	if kind == "large" {
		return "large"
	}
	return "small"
}

// pinnedReviewer applies the role's agent pin to the command-line reviewer.
// It returns the reviewer to use (empty when the configuration decides) and
// the agent source for the record.
func pinnedReviewer(pinning reviewPinning, role, cliAgent string) (string, string, error) {
	pinned := pinning.pin.Agent
	switch {
	case cliAgent != "" && pinned != "" && cliAgent != pinned:
		return "", "", newGate(2, "review.pin_agent_conflict", board.ReviewPinField(role, "AGENT"), pinned, cliAgent)
	case pinned != "":
		return pinned, board.PinSourceForced, nil
	case cliAgent != "":
		return cliAgent, board.PinSourceCLI, nil
	}
	return "", "", nil
}

// applyReviewPin overrides the configured model and effort with the role's
// pins and returns the REVIEW_<ROLE>_RESOLVED record, which is empty when no
// bound card pins anything. The <AGENT>_REVIEW_MODEL and
// <AGENT>_REVIEW_REASONING_EFFORT environment overrides act as command-line
// values: they must agree with a pin and otherwise are recorded as `cli`.
func applyReviewPin(ctx *reviewContext, pinning reviewPinning, role, agentSource, scale string) (string, error) {
	prefix := strings.ToUpper(ctx.agent)
	configSource := board.PinSourceConfig(scale)
	model, err := pinnedReviewValue(ctx.settings.model, pinning.pin.Model, os.Getenv(prefix+"_REVIEW_MODEL"), board.ReviewPinField(role, "MODEL"), configSource)
	if err != nil {
		return "", err
	}
	effort, err := pinnedReviewValue(ctx.settings.effort, pinning.pin.Effort, os.Getenv(prefix+"_REVIEW_REASONING_EFFORT"), board.ReviewPinField(role, "EFFORT"), configSource)
	if err != nil {
		return "", err
	}
	ctx.settings.model, ctx.settings.effort = model.Value, effort.Value
	if !pinning.any {
		return "", nil
	}
	if agentSource == "" {
		agentSource = configSource
	}
	cfg, err := config.Effective(nil)
	if err != nil {
		return "", err
	}
	effort.NotApplicable = !board.ReviewTakesEffort(cfg, ctx.agent)
	agent := board.ResolvedItem{Value: ctx.agent, Source: agentSource}
	return board.Resolution{Agent: agent, Model: model, Effort: effort}.Render(), nil
}

func pinnedReviewValue(configured, pinned, env, field, configSource string) (board.ResolvedItem, error) {
	switch {
	case pinned != "" && env != "" && env != pinned:
		return board.ResolvedItem{}, newGate(2, "review.pin_env_conflict", field, pinned, env)
	case pinned != "":
		return board.ResolvedItem{Value: pinned, Source: board.PinSourceForced}, nil
	case env != "":
		return board.ResolvedItem{Value: configured, Source: board.PinSourceCLI}, nil
	}
	return board.ResolvedItem{Value: configured, Source: configSource}, nil
}

// notePinnedNotApplicable prints one stderr notice for every planned role that
// is N/A while a card of its batch pins that role's reviewer: a pin never turns
// a skipped or N/A role on. Cards that cannot be read are left to the plan's
// own validation, so the notice never fails the command.
func notePinnedNotApplicable(root string, plan board.ReviewPlan) {
	cfg, err := config.Effective(nil)
	if err != nil {
		return
	}
	for _, batch := range plan.Batches {
		for _, role := range board.PinRoles {
			if !strings.HasPrefix(batch.Requirements[role], "N/A") {
				continue
			}
			for _, id := range batch.TaskIDs {
				snapshot, err := board.ReadSnapshot(root, id)
				if err != nil {
					continue
				}
				pins, err := board.CardPinsOf(cfg, snapshot.Text)
				if err == nil && pins.Review[role].Forced() {
					fmt.Fprintln(os.Stderr, config.Text("review.pin_role_not_applicable", id, role, batch.BatchID))
				}
			}
		}
	}
}
