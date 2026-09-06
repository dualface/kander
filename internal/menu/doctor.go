package menu

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
)

const tmuxSessionHint = "tmux new -A -s kander"

func printDoctor() bool {
	return printDoctorWithTools(CheckTerminalTools(), false)
}

func printDoctorWithTools(tools TerminalTools, repair bool) bool {
	healthy := true
	hint(config.Text("menu.kander_environment_check"))
	paths, err := currentPaths()
	if err != nil {
		warning(err.Error())
		return false
	}
	if paths.Mode == config.ModeProject {
		success(config.Text("menu.install_mode_project", paths.InstallRoot))
		success(config.Text("menu.rules_entry", rulesEntry(paths)))
	} else {
		success(config.Text("menu.install_mode_global"))
	}
	install.CleanupStaleBinary(paths)
	cfgLang := config.ResolveLanguage()
	if loaded, loadErr := config.Load(true); loadErr == nil && loaded.Language != "" {
		cfgLang = loaded.Language
	}
	if repair {
		if repairErr := install.RepairRules(paths, cfgLang); repairErr != nil {
			warning(repairErr.Error())
			healthy = false
		}
	}
	if report, inspectErr := install.InspectRules(paths, cfgLang); inspectErr != nil {
		warning(inspectErr.Error())
		healthy = false
	} else {
		for _, name := range report.Missing {
			healthy = false
			warning(config.Text("install.rule_missing", name))
		}
		for _, name := range report.Outdated {
			healthy = false
			warning(config.Text("install.rule_outdated", name))
		}
		for _, name := range report.Modified {
			hint(config.Text("install.rule_modified", name))
		}
		if report.LanguageDrift {
			note(config.Text("install.rule_language_drift", report.ConfigLanguage, report.InstalledLanguage))
		}
	}
	for name, path := range findCommands(paths) {
		if path != "" {
			success(name + ": " + path)
		} else {
			healthy = false
			warning(commandMissingMessage(name, paths))
		}
	}
	reportTerminalTools(tools)
	if tools.Herdr.Error != "" || tools.Tmux.Error != "" {
		healthy = false
	}
	agents := findAgents()
	if repair {
		if _, ok := repairDoctorConfig(agents, tools); !ok {
			healthy = false
		}
	}
	var configuredLauncher string
	if loaded, loadErr := config.Load(true); loadErr == nil {
		if effective, effErr := config.Effective(loaded); effErr == nil {
			configuredLauncher = effective.Launcher
		}
	}
	if isWindowsOS() {
		success(config.Text("menu.windows_console_launcher_available"))
	} else if tools.Tmux.Available() {
		if os.Getenv("TMUX") == "" && configuredLauncher != "tmux-session" && configuredLauncher != "auto" && configuredLauncher != "herdr" {
			hint(config.Text(
				"menu.tmux_installed_but_not_in_a_session_start_one", tmuxSessionHint,
			))
		}
	} else {
		hint(config.Text(
			"menu.tmux_unavailable_welcome_can_select_foreground_mode",
		))
	}
	// herdr works on Windows too, so these hints are not platform-specific.
	if tools.Herdr.Available() {
		if os.Getenv("HERDR_ENV") != "1" && configuredLauncher == "herdr" {
			hint(config.Text(
				"menu.herdr_installed_but_not_currently_in_herdr_the_herdr",
			))
		}
	} else if configuredLauncher == "herdr" {
		hint(config.Text(
			"menu.herdr_unavailable_welcome_can_select_another_launcher",
		))
	}

	labels := agentLabels()
	hint(config.Text("menu.agent_capabilities"))
	anyExec := false
	anyReview := false
	for _, name := range config.ExecutionAgents {
		state := agents[name]
		if agentUsable(state) {
			anyExec = true
			if state.Review {
				anyReview = true
			}
			caps := []string{config.Text("menu.execution")}
			if state.Review {
				caps = append(caps, config.Text("menu.review"))
			}
			joined := caps[0]
			for _, c := range caps[1:] {
				joined += ", " + c
			}
			success(labels[name] + ": " + state.Version + " (" + joined + "; " + config.Text("menu.authentication_not_verified") + ")")
		} else if state.Path != "" {
			healthy = false
			warning(config.Text(
				"menu.found_at_but_version_failed_it_is_unavailable_and", labels[name], state.Path,
			))
		} else {
			hint(config.Text("menu.not_installed", labels[name]))
		}
	}
	if !anyExec {
		healthy = false
		warning(config.Text("menu.no_executable_agent_found_kanban_cannot_start_tasks"))
	}
	if !anyReview {
		healthy = false
		warning(config.Text("menu.no_reviewer_found_reviews_cannot_run"))
	}

	loaded, loadErr := config.Load(true)
	if loadErr != nil {
		warning(loadErr.Error())
		healthy = false
	} else {
		cfgPath, _ := config.ConfigPath()
		if loaded.WelcomeComplete {
			success(config.Text("menu.config", cfgPath))
		} else {
			hint(config.Text("menu.config_incomplete", cfgPath))
			maybeHintLegacyOnevoke()
		}
		healthy = validateConfiguredResources(loaded, agents, paths, tools) && healthy
	}
	return healthy
}

func maybeHintLegacyOnevoke() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	legacy := filepath.Join(home, ".config", "onevoke", "config.json")
	if fileExists(legacy) {
		hint(config.Text(
			"menu.found_a_legacy_onevoke_config_at_copy_it_by", legacy,
		))
	}
}

func validateConfiguredResources(cfg *config.Config, agents map[string]agentState, paths config.InstallPaths, tools TerminalTools) bool {
	healthy := true
	effective, err := config.Effective(cfg)
	if err != nil {
		warning(err.Error())
		return false
	}
	for _, selected := range config.ExecutionAgentsInUse(effective) {
		if !agentUsable(agents[selected]) {
			healthy = false
			warning(config.Text(
				"menu.configured_execution_agent_is_unavailable_install_it_then_run", selected,
			))
		}
	}
	if ok, msg := reviewGatePresent(paths); !ok {
		healthy = false
		warning(msg)
	}
	for _, role := range config.ReviewRoles {
		reviewer := effective.Reviewers[role]
		if !agentUsable(agents[reviewer]) {
			healthy = false
			warning(config.Text(
				"menu.configured_reviewer_is_unavailable_install_it_then_run_kander", role, reviewer,
			))
		}
	}
	launcher := effective.Launcher
	switch launcher {
	case "tmux", "tmux-session":
		if isWindowsOS() {
			healthy = false
			warning(config.Text(
				"menu.the_configured_launcher_is_but_native_windows_does_not", launcher,
			))
		} else if !tools.Tmux.Available() {
			healthy = false
			warning(config.Text(
				"menu.the_configured_launcher_is_but_tmux_is_not_in", launcher,
			))
		} else if !cfg.WelcomeComplete {
			break
		} else if launcher == "tmux-session" {
			hint(config.Text(
				"menu.launcher_tmux_session_creates_or_reuses_a_per_project",
			))
		} else if os.Getenv("TMUX") == "" {
			hint(config.Text(
				"menu.launcher_tmux_requires_entering_the_tmux_session_shown_above",
			))
		}
	case "console":
		if !isWindowsOS() {
			healthy = false
			warning(config.Text(
				"menu.the_configured_launcher_is_console_but_it_is_available",
			))
		} else if cfg.WelcomeComplete {
			hint(config.Text(
				"menu.launcher_console_starts_the_agent_in_a_separate_windows",
			))
		}
	case "herdr":
		if !tools.Herdr.Available() {
			healthy = false
			warning(config.Text(
				"menu.the_configured_launcher_is_herdr_but_herdr_is_not",
			))
		} else if !cfg.WelcomeComplete {
			break
		} else if os.Getenv("HERDR_ENV") != "1" {
			hint(config.Text(
				"menu.launcher_herdr_requires_running_inside_herdr_herdr_env_1",
			))
		} else if strings.TrimSpace(os.Getenv("HERDR_WORKSPACE_ID")) == "" {
			healthy = false
			warning(config.Text(
				"menu.the_configured_launcher_is_herdr_but_herdr_workspace_id",
			))
		} else {
			hint(config.Text(
				"menu.launcher_herdr_creates_a_tab_in_the_current_workspace",
			))
		}
	case "auto":
		if !tools.Tmux.Available() && !tools.Herdr.Available() {
			healthy = false
			// Native Windows never probes tmux and auto can only land on herdr, so the hint must not mention tmux.
			key := "menu.the_configured_launcher_is_auto_but_neither_herdr_nor"
			if isWindowsOS() {
				key = "menu.the_configured_launcher_is_auto_but_herdr_is_not_available"
			}
			warning(config.Text(key))
		} else {
			hint(config.Text(
				"menu.launcher_auto_chooses_at_start_a_herdr_tab_if",
			))
		}
	case "foreground":
		if cfg.WelcomeComplete {
			launcherHint := "tmux"
			if isWindowsOS() {
				launcherHint = config.Text("menu.console")
			}
			hint(config.Text(
				"menu.launcher_foreground_requires_an_interactive_terminal_stdin_stdout_stderr", launcherHint,
			))
		}
	}
	return healthy
}
