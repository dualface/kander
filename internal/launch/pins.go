package launch

import (
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// execChoice is the agent, model and effort one launch passes to the executing
// agent, plus the EXEC_RESOLVED record value; record is empty for a card that
// pins nothing, so such cards are written exactly as before.
type execChoice struct {
	Agent  string
	Model  string
	Effort string
	record string
}

// execMode says where the agent of a launch comes from.
type execMode int

const (
	// execStart takes an optional --agent and otherwise the card pin or the configuration.
	execStart execMode = iota
	// execTakeover takes the explicit --agent of `resume --agent`.
	execTakeover
	// execContinue relaunches the agent recorded on the card (plain resume, notify recovery).
	execContinue
)

func kindScale(kind string) string {
	if kind == "large" {
		return "large"
	}
	return "small"
}

// resolveExecution applies the card's EXEC_* pins on top of the configuration.
// A forced value must agree with an explicit command-line value or the card
// owner, otherwise the launch fails before any side effect.
func resolveExecution(cfg *config.Config, text, kind, agent string, mode execMode) (execChoice, error) {
	pins, err := board.CardPinsOf(cfg, text)
	if err != nil {
		return execChoice{}, err
	}
	pin := pins.Exec
	scale := kindScale(kind)
	configSource := board.PinSourceConfig(scale)
	var agentItem board.ResolvedItem
	switch {
	case mode == execContinue:
		if pin.Agent != "" && pin.Agent != agent {
			return execChoice{}, launchError("launch.pin_owner_conflict", board.FieldExecAgent, pin.Agent, agent)
		}
		agentItem = board.ResolvedItem{Value: agent, Source: continuedAgentSource(cfg, pin, kind, agent)}
	case agent != "":
		if pin.Agent != "" && pin.Agent != agent {
			return execChoice{}, launchError("launch.pin_agent_conflict", board.FieldExecAgent, pin.Agent, agent)
		}
		agentItem = board.ResolvedItem{Value: agent, Source: board.PinSourceCLI}
		if pin.Agent != "" {
			agentItem.Source = board.PinSourceForced
		}
	case pin.Agent != "":
		agentItem = board.ResolvedItem{Value: pin.Agent, Source: board.PinSourceForced}
	default:
		configured, err := config.KanbanAgentFor(cfg, kind)
		if err != nil {
			return execChoice{}, err
		}
		agentItem = board.ResolvedItem{Value: configured, Source: configSource}
	}
	entry := cfg.Models.Kanban[agentItem.Value]
	modelItem := board.ResolvedItem{Value: config.KanbanModelFor(entry, scale), Source: configSource}
	if pin.Model != "" {
		modelItem = board.ResolvedItem{Value: pin.Model, Source: board.PinSourceForced}
	}
	effortItem := board.ResolvedItem{Value: entry[scale+"_effort"], Source: configSource}
	if pin.Effort != "" {
		effortItem = board.ResolvedItem{Value: pin.Effort, Source: board.PinSourceForced}
	}
	choice := execChoice{Agent: agentItem.Value, Model: modelItem.Value, Effort: effortItem.Value}
	if pins.Any() {
		recorded := effortItem
		recorded.NotApplicable = !board.ExecutionTakesEffort(cfg, agentItem.Value)
		choice.record = board.Resolution{Agent: agentItem, Model: modelItem, Effort: recorded}.Render()
	}
	return choice, nil
}

// continuedAgentSource labels the agent of a relaunch: the pin when there is
// one, the configuration when the owner is still the configured agent, and
// otherwise an earlier command-line choice.
func continuedAgentSource(cfg *config.Config, pin board.StagePin, kind, agent string) string {
	if pin.Agent != "" {
		return board.PinSourceForced
	}
	if configured, err := config.KanbanAgentFor(cfg, kind); err == nil && configured == agent {
		return board.PinSourceConfig(kindScale(kind))
	}
	return board.PinSourceCLI
}

// withExecRecord writes EXEC_RESOLVED when the choice carries a record.
func withExecRecord(text string, choice execChoice) (string, error) {
	if choice.record == "" {
		return text, nil
	}
	return board.WithResolvedRecord(text, board.FieldExecResolved, choice.record)
}

// execArguments renders the agent argv for a resolved choice.
func execArguments(choice execChoice, session AgentSession, resume bool, cfg *config.Config) ([]string, error) {
	return expandAgentInvocation(choice.Agent, choice.Model, choice.Effort, session, resume, cfg)
}
