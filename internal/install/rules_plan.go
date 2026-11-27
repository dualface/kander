package install

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
)

// RulesChange is the change an install would make to one agent file it writes.
type RulesChange int

const (
	// RulesChangeCreate means the file does not exist and would be created.
	RulesChangeCreate RulesChange = iota
	// RulesChangeUpdate means a previously installed extension or skill document would be updated.
	RulesChangeUpdate
	// RulesChangeNone means the file would stay as it is.
	RulesChangeNone
)

// PlannedFile is one file of a planned rules injection.
type PlannedFile struct {
	Target string
	Change RulesChange
	// Err reports a file the install could not inspect; the install reports it again.
	Err error
}

// PlanGroup is one covered agent with every file its install_rules switch controls in the
// global install: the rules extension and bundled skill documents. The switch also decides
// the project rules file kander init writes, which the install does not touch.
type PlanGroup struct {
	Agents []string
	Files  []PlannedFile
}

// PlanRulesInjection lists, without writing, the files an install would write for every
// covered agent. It reuses ExtensionAgents and the leader skill destinations so the plan
// covers what Perform writes.
func PlanRulesInjection(paths config.InstallPaths) []PlanGroup {
	return planRulesInjection(paths, leaderDestinations(paths))
}

func planRulesInjection(paths config.InstallPaths, targets []leaderDestination) []PlanGroup {
	var groups []PlanGroup
	byAgent := map[string]int{}
	for _, agent := range ExtensionAgents(paths) {
		file := planExtension(agent, paths)
		if index, ok := byAgent[agent]; ok {
			groups[index].Files = append(groups[index].Files, file)
			continue
		}
		byAgent[agent] = len(groups)
		groups = append(groups, PlanGroup{Agents: []string{agent}, Files: []PlannedFile{file}})
	}
	for _, target := range targets {
		agent := target.agent
		files := leaderPreview(target, paths)
		if index, ok := byAgent[agent]; ok {
			groups[index].Files = append(groups[index].Files, files...)
		} else {
			groups = append(groups, PlanGroup{Agents: []string{agent}, Files: files})
		}
	}
	return groups
}

func planExtension(agent string, paths config.InstallPaths) PlannedFile {
	outcome, err := InspectExtension(agent, paths)
	file := PlannedFile{Target: outcome.Target, Err: err}
	switch outcome.Status {
	case ExtensionMissing:
		file.Change = RulesChangeCreate
	case ExtensionOutdated:
		file.Change = RulesChangeUpdate
	default:
		// A locally modified extension is kept as it is.
		file.Change = RulesChangeNone
	}
	return file
}

// groupEnabled reports whether every agent of the group has its switch on.
func groupEnabled(group PlanGroup, switches config.InstallRules) bool {
	return !slices.ContainsFunc(group.Agents, func(agent string) bool { return !switches.Enabled(agent) })
}

// rulesChoice maps the selected groups to the agent switches to save: every agent of an
// unselected group gets its own false, so a later global change cannot turn it back on, and an
// agent of a selected group that was off turns on.
func rulesChoice(groups []PlanGroup, selected []int, switches config.InstallRules) map[string]bool {
	choice := map[string]bool{}
	for index, group := range groups {
		want := slices.Contains(selected, index)
		for _, agent := range group.Agents {
			if !want || !switches.Enabled(agent) {
				choice[agent] = want
			}
		}
	}
	return choice
}

// saveRulesChoice stores the wizard's per-agent choice in the scope config file before any
// rules file is written. A missing config file is created with the wizard's language, and
// welcome_complete stays false so the first-run setup still runs.
func saveRulesChoice(paths config.InstallPaths, choice map[string]bool, lang string) error {
	if len(choice) == 0 {
		return nil
	}
	file := config.ScopeConfigFile(paths)
	_, statErr := os.Lstat(file)
	created := os.IsNotExist(statErr)
	_, err := config.UpdateAt(file, func(cfg *config.Config) error {
		for agent, value := range choice {
			config.SetAgentInstallRules(cfg, agent, &value)
		}
		if created && lang != "" {
			cfg.Language = lang
			cfg.AgentLanguage = config.DefaultAgentLanguage(lang)
		}
		return nil
	})
	return err
}

// AgentLabelList joins the display names of agents.
func AgentLabelList(agents []string) string {
	labels := make([]string, len(agents))
	for i, agent := range agents {
		labels[i] = config.AgentDisplayName(agent)
	}
	return strings.Join(labels, ", ")
}

// displayTarget shortens a planned path below the home directory to its ~/ form.
func displayTarget(target string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, target); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return target
}
