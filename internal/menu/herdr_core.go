package menu

import (
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal/builtin"
)

// validateHerdrCoreLauncher checks the configured herdr-core launcher: hc on
// PATH, a running core, and execution agents that report their status through
// OSC 7501, because herdr-core identifies agents only by those reports.
func validateHerdrCoreLauncher(cfg *config.Config, tools TerminalTools) bool {
	if isWindowsOS() {
		warning(config.Text("menu.the_configured_launcher_is_but_native_windows_does_not", builtin.HerdrCore))
		return false
	}
	if !tools.HerdrCore.Available() {
		warning(config.Text("herdr_core.doctor_not_in_path"))
		return false
	}
	healthy := true
	if tools.HerdrCoreDown != "" {
		healthy = false
		warning(config.Text("herdr_core.doctor_launcher_not_running", tools.HerdrCoreDown))
	}
	if missing := agentsWithoutProgramStatus(cfg); len(missing) > 0 {
		healthy = false
		warning(config.Text("herdr_core.doctor_agents_unreported", strings.Join(missing, ", ")))
	}
	if healthy {
		hint(config.Text("herdr_core.doctor_ready"))
	}
	return healthy
}

// agentsWithoutProgramStatus lists the configured execution agents (both task
// scales and the chat agent) that do not declare OSC 7501 reports.
func agentsWithoutProgramStatus(cfg *config.Config) []string {
	var missing []string
	for _, name := range config.ExecutionAgentsInUse(cfg) {
		if _, ok := config.ProgramStatusApp(config.AgentFor(cfg, name)); !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

// herdrCoreLauncherChoices offers herdr-core once hc is installed, or keeps a
// configured herdr-core visible with the not-installed note.
func herdrCoreLauncherChoices(cfg *config.Config) []Choice {
	installed := lookPath(builtin.HerdrCoreExecutable) != ""
	if !installed && cfg.Launcher != builtin.HerdrCore {
		return nil
	}
	label := config.Text("herdr_core.menu_choice")
	if !installed {
		label += config.Text("menu.not_currently_installed")
	}
	return []Choice{{Value: builtin.HerdrCore, Label: label}}
}
