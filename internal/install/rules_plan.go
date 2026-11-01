package install

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// RulesChange is the change an install would make to one file of the rules injection.
type RulesChange int

const (
	// RulesChangeCreate means the file does not exist and would be created.
	RulesChangeCreate RulesChange = iota
	// RulesChangeAppend means the reference would be appended to an existing file.
	RulesChangeAppend
	// RulesChangeRewrite means a reference to the previous entry would be rewritten, invalid or
	// duplicate load commands dropped, or the alwaysApply frontmatter added.
	RulesChangeRewrite
	// RulesChangeReplace means a symlink to the previous entry would be replaced by a file.
	RulesChangeReplace
	// RulesChangeUpdate means a previous release of the rules extension would be updated.
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

// PlanGroup is the covered agents that read the same rules file, with every file their
// install_rules switches control: the rules file, an existing AGENTS.override.md sibling, and
// the rules extension.
type PlanGroup struct {
	Agents []string
	Files  []PlannedFile
}

// PlanRulesInjection lists, without writing, the files an install of the scope would inject
// for every covered agent. It reuses IntegrationAgents and ExtensionAgents so the plan covers
// what Perform writes, and groups the agents that share one rules file, since one switch off
// keeps that file unwritten for all of them.
func PlanRulesInjection(paths config.InstallPaths) []PlanGroup {
	var groups []PlanGroup
	byTarget := map[string]int{}
	byAgent := map[string]int{}
	for _, agent := range IntegrationAgents(paths) {
		target, err := AgentRulesTarget(agent, paths)
		if err != nil {
			byAgent[agent] = len(groups)
			groups = append(groups, PlanGroup{Agents: []string{agent}, Files: []PlannedFile{{Err: err}}})
			continue
		}
		if target == "" {
			continue
		}
		key := rulesFileKey(target)
		if index, ok := byTarget[key]; ok {
			groups[index].Agents = append(groups[index].Agents, agent)
			byAgent[agent] = index
			// A rules file linked to another agent's still has its own override sibling,
			// which Perform edits through this agent's target.
			if override := rulesOverrideTarget(target); override != "" && !groupHasFile(groups[index], override) {
				groups[index].Files = append(groups[index].Files, planRulesFile(agent, override, false, paths, legacyEntryWillRetire(paths)))
			}
			continue
		}
		byTarget[key] = len(groups)
		byAgent[agent] = len(groups)
		groups = append(groups, PlanGroup{Agents: []string{agent}, Files: planRulesFiles(agent, target, paths)})
	}
	for _, agent := range ExtensionAgents(paths) {
		file := planExtension(agent, paths)
		if index, ok := byAgent[agent]; ok {
			groups[index].Files = append(groups[index].Files, file)
			continue
		}
		byAgent[agent] = len(groups)
		groups = append(groups, PlanGroup{Agents: []string{agent}, Files: []PlannedFile{file}})
	}
	return groups
}

// planRulesFiles plans the configured rules file and its existing AGENTS.override.md sibling
// against the state Perform leaves after migrateLegacyRules: once the previous entry retires,
// a symlink to it is replaced and references to it are rewritten.
func planRulesFiles(agent, target string, paths config.InstallPaths) []PlannedFile {
	retired := legacyEntryWillRetire(paths)
	var files []PlannedFile
	if retired && linksToLegacyEntry(target, paths) {
		files = append(files, PlannedFile{Target: target, Change: RulesChangeReplace})
	} else {
		files = append(files, planRulesFile(agent, target, true, paths, retired))
	}
	if override := rulesOverrideTarget(target); override != "" {
		files = append(files, planRulesFile(agent, override, false, paths, retired))
	}
	return files
}

func planRulesFile(agent, target string, create bool, paths config.InstallPaths, retired bool) PlannedFile {
	edit, err := planTargetIntegration(agent, target, create, paths, retired)
	file := PlannedFile{Target: target, Err: err}
	switch {
	case err != nil:
	case edit.outcome.Status == IntegrationCreated:
		file.Change = RulesChangeCreate
	case edit.appended:
		file.Change = RulesChangeAppend
	case edit.path != "":
		file.Change = RulesChangeRewrite
	default:
		file.Change = RulesChangeNone
	}
	return file
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

// groupHasFile reports whether the group already plans the file target resolves to.
func groupHasFile(group PlanGroup, target string) bool {
	key := rulesFileKey(target)
	return slices.ContainsFunc(group.Files, func(file PlannedFile) bool {
		return file.Target != "" && rulesFileKey(file.Target) == key
	})
}

// linksToLegacyEntry reports whether target is a symlink aimed at the previous rules entry,
// which removeLegacyEntrySymlinks replaces by a reference file.
func linksToLegacyEntry(target string, paths config.InstallPaths) bool {
	legacy := legacyRulesEntry(paths)
	if legacy == "" {
		return false
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	link, err := os.Readlink(target)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(target), link)
	}
	return filepath.Clean(link) == filepath.Clean(legacy)
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

// displayTarget shortens a planned path: relative to the main worktree for a project install,
// ~/ for a global path below the home directory.
func displayTarget(target string, paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject && paths.ProjectRoot != "" {
		if rel, err := filepath.Rel(paths.ProjectRoot, target); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		return target
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, target); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return target
}
