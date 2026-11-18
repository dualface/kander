package menu

import (
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
	"github.com/dualface/kander/internal/version"
)

// confirmationsOf is the seam of doctorJSON: tests replace it to leave the probing of the
// machine's agents and terminal tools out.
var confirmationsOf = doctorConfirmations

// doctorConfirmations lists, without writing or printing anything, what an interactive doctor
// would ask the person about before it repairs: the backup and replacement of modified rule
// files, the replacement of an unusable agent, the storing of a session file, the herdr install,
// and the upgrade of an outdated Homebrew copy. With --json none of these can be answered, so a
// non-empty list refuses the whole run before any repair.
func doctorConfirmations(tools TerminalTools) []string {
	var reasons []string
	if paths, err := currentPaths(); err == nil {
		if report, err := install.InspectRules(paths); err == nil && len(report.Modified) > 0 {
			reasons = append(reasons, config.Text("menu.doctor_confirm_modified_rules", len(report.Modified)))
		}
	}
	if scope, raw, err := config.LoadScopeRaw(true); err == nil {
		chat, _ := raw["chat_agent"].(string)
		agents := findAgents(scope)
		for _, plan := range planToolReplacements(scope, agents, chat != "") {
			if len(plan.candidates) > 0 {
				reasons = append(reasons, config.Text("menu.doctor_confirm_agent_replacement", plan.field, plan.current))
			}
		}
		for _, name := range config.AgentNames(scope) {
			if _, _, ok := sessionFileInheritSpec(scope, name); ok {
				reasons = append(reasons, config.Text("menu.doctor_confirm_session_file", name))
			}
		}
	}
	if tools.NeedsHerdrInstall() {
		reasons = append(reasons, config.Text("menu.doctor_confirm_herdr_install"))
	}
	if brewKanderOutdated(pathKanderBinaries()) {
		reasons = append(reasons, config.Text("menu.doctor_confirm_brew_upgrade"))
	}
	return reasons
}

// brewKanderOutdated is the condition under which reportPathKanders offers the Homebrew upgrade.
func brewKanderOutdated(binaries []install.PathKanderBinary) bool {
	if len(binaries) <= 1 {
		return false
	}
	latest := 0
	for i := range binaries {
		if version.Compare(binaries[i].Version, binaries[latest].Version) > 0 {
			latest = i
		}
	}
	for i, bin := range binaries {
		if bin.Brew && i != latest && version.Compare(bin.Version, binaries[latest].Version) < 0 {
			return true
		}
	}
	return false
}

// refuseDoctorConfirmations is the error of a doctor run that has something to ask: the
// interactive_required refusal, with the first thing it would have asked in its text.
func refuseDoctorConfirmations(reasons []string) error {
	return command.Interactive(config.Text("menu.doctor_confirm_required", reasons[0], len(reasons)))
}
