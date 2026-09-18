package menu

import (
	"slices"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/terminal/builtin"
	"github.com/dualface/kander/internal/terminal/direct"
)

// Seam for tests: the agent-replacement prompt is injected so the interactive
// repair never reads a real terminal under test.
var askDoctorAgentChoice = askChoice

func repairDoctorConfig(agents map[string]agentState, tools TerminalTools, interactive bool) (*config.Config, bool) {
	var changes, warnings []string
	language := "en"
	// Config result messages only use the language of the on-disk config, unaffected by the UI or process language override.
	text := func(id string, args ...any) string {
		return i18n.Text(language, id, args...)
	}
	_, overlay, overlayErr := config.ReadOverlay("")
	if overlayErr != nil {
		warning(overlayErr.Error())
	}

	// Replacement choices are collected before config.Repair: its adjust
	// closure runs under the config lock and must never read the terminal.
	// When the scope file cannot be loaded, nothing is collected and the
	// repair keeps the automatic first-usable choice.
	decisions := map[string]string{}
	if interactive {
		if scope, loadErr := config.LoadScope(true); loadErr == nil {
			language = scope.Language
			decisions = collectToolReplacements(planToolReplacements(scope, agents), text)
		}
	}

	cfg, result, err := config.Repair(func(cfg *config.Config) {
		language = cfg.Language
		// Derive policy from the repaired scope so missing or malformed scope
		// files can still honor project activation. Never save this merged view.
		policy := cfg
		if overlayErr == nil && overlay != nil {
			merged, mergeErr := config.ApplyOverlay(cfg, overlay)
			if mergeErr != nil {
				// Keep scope repair available; doctor's final Load reports the
				// invalid overlay as unhealthy after repairing the scope file.
				warning(mergeErr.Error())
			} else {
				policy = merged
			}
		}
		changes, warnings = repairConfiguredTools(cfg, policy, agents, tools, decisions, text)
	})
	if result.BackupPath != "" {
		hint(text("menu.original_config_backed_up") + result.BackupPath)
	}
	if err != nil {
		warning(text("menu.config_repair_failed") + err.Error())
		return nil, false
	}
	if result.Created {
		success(text("menu.created_and_saved_config_json") + result.Path)
	} else if result.Changed {
		success(text("menu.updated_and_saved_config_json") + result.Path)
	} else {
		success(text("menu.config_json_is_unchanged") + result.Path)
	}
	for _, change := range changes {
		hint(change)
	}
	for _, item := range warnings {
		warning(item)
	}
	return cfg, true
}

// toolReplacement plans the repair of one field whose configured Agent probed
// unavailable: candidates holds the usable choices in offer order, defaultFrom
// names an earlier field whose collected decision supplies the prompt default,
// and fallback is the preferred default when no such decision exists.
type toolReplacement struct {
	field       string
	current     string
	candidates  []string
	defaultFrom string
	fallback    string
}

// planToolReplacements mirrors the field scan of repairConfiguredTools on a
// config loaded outside the repair lock, so the interactive path can collect
// every decision before any write begins.
func planToolReplacements(cfg *config.Config, agents map[string]agentState) []toolReplacement {
	execution := usableAgentNames(config.AgentNames(cfg), agents, false)
	reviewers := usableAgentNames(config.ReviewAgentNames(cfg), agents, true)
	var plans []toolReplacement
	if !agentUsable(agents[cfg.KanbanAgent]) {
		plans = append(plans, toolReplacement{field: "kanban_agent", current: cfg.KanbanAgent, candidates: execution})
	}
	for _, scale := range config.TaskScales {
		if selected := cfg.KanbanAgents[scale]; !agentUsable(agents[selected]) {
			plans = append(plans, toolReplacement{
				field: "kanban_agents." + scale, current: selected, candidates: execution,
				defaultFrom: "kanban_agent", fallback: cfg.KanbanAgent,
			})
		}
	}
	// An empty chat_agent inherits the repaired large-scale agent at apply
	// time, so only an explicitly configured value can need a prompt.
	if cfg.ChatAgent != "" && !agentUsable(agents[cfg.ChatAgent]) {
		plans = append(plans, toolReplacement{
			field: "chat_agent", current: cfg.ChatAgent, candidates: execution,
			defaultFrom: "kanban_agents.large",
		})
	}
	for _, scale := range config.TaskScales {
		for _, role := range config.ReviewRoles {
			if selected := cfg.Reviewers[scale][role]; !reviewerUsable(agents[selected]) {
				plans = append(plans, toolReplacement{
					field: "reviewers." + scale + "." + role, current: selected, candidates: reviewers,
				})
			}
		}
	}
	return plans
}

// collectToolReplacements asks once per broken field and returns the chosen
// replacements keyed by field. An input error stops the collection; fields
// left without a decision keep the automatic first-usable choice.
func collectToolReplacements(plans []toolReplacement, text func(string, ...any) string) map[string]string {
	decisions := map[string]string{}
	labels := agentLabels()
	for _, plan := range plans {
		if len(plan.candidates) == 0 {
			continue
		}
		def := decisions[plan.defaultFrom]
		if def == "" {
			def = plan.fallback
		}
		if !slices.Contains(plan.candidates, def) {
			def = plan.candidates[0]
		}
		choices := make([]choice, 0, len(plan.candidates))
		for _, name := range plan.candidates {
			label := labels[name]
			if label == "" {
				label = name
			}
			choices = append(choices, choice{Value: name, Label: label})
		}
		selected, err := askDoctorAgentChoice(
			text("menu.configured_agent_for_is_unavailable_choose_a_replacement", plan.current, plan.field),
			choices, def,
		)
		if err != nil {
			break
		}
		decisions[plan.field] = selected
	}
	return decisions
}

// usableAgentNames filters the candidate list down to the agents that probed
// usable, keeping the declaration order for the prompt and the automatic pick.
func usableAgentNames(names []string, agents map[string]agentState, review bool) []string {
	usable := make([]string, 0, len(names))
	for _, name := range names {
		if state := agents[name]; !review && agentUsable(state) || review && reviewerUsable(state) {
			usable = append(usable, name)
		}
	}
	return usable
}

func repairConfiguredTools(cfg, policy *config.Config, agents map[string]agentState, tools TerminalTools, decisions map[string]string, text func(string, ...any) string) (changes, warnings []string) {
	if text == nil {
		text = func(id string, args ...any) string { return i18n.Text("en", id, args...) }
	}
	execution := usableAgentNames(config.AgentNames(cfg), agents, false)
	reviewers := usableAgentNames(config.ReviewAgentNames(cfg), agents, true)
	// A collected decision wins; otherwise the preferred field value (the
	// already repaired kanban_agent for the scale fields), then the first
	// usable candidate, matching the previous automatic repair.
	pick := func(field string, candidates []string, preferred string) string {
		if chosen := decisions[field]; slices.Contains(candidates, chosen) {
			return chosen
		}
		if slices.Contains(candidates, preferred) {
			return preferred
		}
		if len(candidates) > 0 {
			return candidates[0]
		}
		return ""
	}
	set := func(field string, old *string, replacement string) {
		if replacement == "" || *old == replacement {
			return
		}
		changes = append(changes, field+": "+*old+" -> "+replacement)
		*old = replacement
	}
	noCandidate := func(field, current string) {
		warnings = append(warnings, text("menu.no_usable_agent_replacement_for_keeping", field, current))
	}
	if !agentUsable(agents[cfg.KanbanAgent]) {
		replacement := pick("kanban_agent", execution, "")
		if replacement == "" {
			noCandidate("kanban_agent", cfg.KanbanAgent)
		}
		set("kanban_agent", &cfg.KanbanAgent, replacement)
	}
	for _, scale := range config.TaskScales {
		selected := cfg.KanbanAgents[scale]
		if agentUsable(agents[selected]) {
			continue
		}
		replacement := pick("kanban_agents."+scale, execution, cfg.KanbanAgent)
		if replacement == "" {
			noCandidate("kanban_agents."+scale, selected)
			continue
		}
		set("kanban_agents."+scale, &selected, replacement)
		cfg.KanbanAgents[scale] = selected
	}
	if cfg.ChatAgent == "" {
		cfg.ChatAgent = cfg.KanbanAgents["large"]
	}
	if !agentUsable(agents[cfg.ChatAgent]) {
		replacement := pick("chat_agent", execution, "")
		if replacement == "" {
			noCandidate("chat_agent", cfg.ChatAgent)
		}
		set("chat_agent", &cfg.ChatAgent, replacement)
	}
	for _, scale := range config.TaskScales {
		if cfg.Reviewers[scale] == nil {
			cfg.Reviewers[scale] = map[string]string{}
		}
		for _, role := range config.ReviewRoles {
			selected := cfg.Reviewers[scale][role]
			if reviewerUsable(agents[selected]) {
				continue
			}
			replacement := pick("reviewers."+scale+"."+role, reviewers, "")
			if replacement == "" {
				noCandidate("reviewers."+scale+"."+role, selected)
				continue
			}
			set("reviewers."+scale+"."+role, &selected, replacement)
			cfg.Reviewers[scale][role] = selected
			// Models are bound to the reviewer; picking a new one adopts that reviewer's model settings.
			entry := cfg.Models.Review[selected]
			roleEntry := cfg.Models.ReviewRoles[role]
			if roleEntry == nil {
				roleEntry = map[string]string{}
				cfg.Models.ReviewRoles[role] = roleEntry
			}
			roleEntry[scale+"_model"] = entry["model"]
			roleEntry[scale+"_effort"] = entry["effort"]
			roleEntry[scale+"_agent"] = selected
		}
	}
	if !doctorLauncherAvailable(cfg.Launcher, tools) {
		replacement := direct.Foreground
		switch {
		case isWindowsOS():
			replacement = direct.Console
		case tools.Herdr.Available():
			replacement = builtin.Herdr
		case tools.Tmux.Available():
			replacement = builtin.TmuxSession
		}
		set("launcher", &cfg.Launcher, replacement)
	}
	// Once an execution agent and a reviewer exist, the repaired choices should immediately become the effective config.
	if len(execution) > 0 && len(reviewers) > 0 {
		cfg.WelcomeComplete = true
	}
	return changes, warnings
}

// doctorLauncherAvailable only serves the "should we rewrite the user's
// launcher" decision, which is why it asks herdr for Installed() rather than
// Available(): when herdr is installed but not on PATH yet, telling the user to
// reopen the terminal beats silently switching the config to console. It does
// not mean the launcher can start right now — prepareLaunch still requires
// herdr to actually be on PATH.
func doctorLauncherAvailable(launcher string, tools TerminalTools) bool {
	if launcher == direct.Foreground {
		return true
	}
	if isWindowsOS() && (launcher == builtin.Tmux || launcher == builtin.TmuxSession) {
		return false
	}
	if launcher == direct.Console {
		return isWindowsOS()
	}
	switch launcher {
	case "auto":
		return tools.Herdr.Installed() || tools.Tmux.Available()
	case builtin.Herdr:
		return tools.Herdr.Installed()
	case builtin.Tmux, builtin.TmuxSession:
		return tools.Tmux.Available()
	}
	return definitionLauncherAvailable(launcher)
}
