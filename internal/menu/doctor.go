package menu

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/terminal/builtin"
	"github.com/dualface/kander/v2/internal/terminal/direct"
)

func printDoctor() bool {
	return printDoctorWithTools(CheckTerminalTools(), false, false)
}

// interactive gates the prompts inside the PATH-binary cleanup: the TUI doctor
// report passes false so the captured output stays passive.
func printDoctorWithTools(tools TerminalTools, repair bool, interactive bool) bool {
	// The same PATH inventory serves the version guard and reportPathKanders, so
	// `kander version` runs once per binary per doctor run.
	return printDoctorWithBinaries(tools, repair, interactive, pathKanderBinaries())
}

// printDoctorWithBinaries is printDoctorWithTools with the PATH inventory already probed. The
// version guard runs before any check, prompt, or repair.
func printDoctorWithBinaries(tools TerminalTools, repair bool, interactive bool, binaries []install.PathKanderBinary) bool {
	if !reportOutdatedRunningKander(binaries) {
		return false
	}
	if interactive {
		tools = offerHerdrInstall(tools)
	}
	healthy := true
	hint(config.Text("menu.kander_environment_check"))
	paths, err := currentPaths()
	if err != nil {
		warning(err.Error())
		return false
	}
	success(config.Text("menu.install_mode_global"))
	install.CleanupStaleBinary(paths)
	if repair {
		rulesRepair, repairErr := install.RepairRules(paths)
		if repairErr != nil {
			warning(repairErr.Error())
			healthy = false
		}
		for _, name := range rulesRepair.Rewritten {
			success(config.Text("install.rule_rewritten", name))
		}
		for _, path := range rulesRepair.Legacy.Removed {
			success(config.Text("install.legacy_rule_removed", path))
		}
		for _, path := range rulesRepair.Legacy.Kept {
			hint(config.Text("install.legacy_rule_kept", path))
		}
	}
	if report, inspectErr := install.InspectRules(paths); inspectErr != nil {
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
		// Only an interactive session may turn the modified list into the
		// backup-and-replace decision; the TUI report stays read-only.
		if interactive && len(report.Modified) > 0 {
			healthy = promptModifiedRules(paths, report.Modified) && healthy
		}
	}
	for name, path := range findCommands() {
		if path != "" {
			success(name + ": " + path)
		} else {
			healthy = false
			warning(commandMissingMessage(name))
		}
	}
	if !reportLegacyProjectInstall(repair) {
		healthy = false
	}
	if !reportPathKanders(interactive, binaries) {
		healthy = false
	}
	reportTerminalTools(tools)
	if tools.Herdr.Error != "" || tools.Tmux.Error != "" || tools.Luvus.Error != "" {
		healthy = false
	}
	if !reportTerminalDefinitions() {
		healthy = false
	}
	reportGitHubCLI()
	if !reportBackgroundWorker() {
		healthy = false
	}
	agentConfig, agentConfigErr := config.Load(false)
	if agentConfigErr != nil && !repair {
		warning(agentConfigErr.Error())
		healthy = false
	}
	agents := findAgents(agentConfig)
	if repair {
		if _, ok := repairDoctorConfig(agents, tools, interactive); !ok {
			healthy = false
		}
		if loaded, loadErr := config.Load(false); loadErr != nil {
			warning(loadErr.Error())
			healthy = false
		} else {
			agentConfig = loaded
			agents = findAgents(agentConfig)
		}
	}
	// The cleanup resolves the agents' config directories through the scope config, so it
	// runs once the repair above had its chance to fix that file.
	if !reportGlobalInjectionCleanup(paths) {
		healthy = false
	}
	var configuredLauncher string
	if loaded, loadErr := config.Load(false); loadErr == nil {
		if effective, effErr := config.Effective(loaded); effErr == nil {
			configuredLauncher = effective.Launcher
		}
	}
	if isWindowsOS() {
		success(config.Text("menu.windows_console_launcher_available"))
	} else if tools.Tmux.Available() {
		if os.Getenv("TMUX") == "" && configuredLauncher != builtin.TmuxSession && configuredLauncher != "auto" && configuredLauncher != builtin.Herdr && configuredLauncher != builtin.Luvus && configuredLauncher != builtin.HerdrCore {
			hint(config.Text(
				"menu.tmux_installed_but_not_in_a_session_start_one", builtin.TmuxSessionHint,
			))
		}
	} else {
		hint(config.Text(
			"menu.tmux_unavailable_welcome_can_select_foreground_mode",
		))
	}
	// herdr works on Windows too, so these hints are not platform-specific.
	if tools.Herdr.Available() {
		if os.Getenv("HERDR_ENV") != "1" && configuredLauncher == builtin.Herdr {
			hint(config.Text(
				"menu.herdr_installed_but_not_currently_in_herdr_the_herdr",
			))
		}
	} else if configuredLauncher == builtin.Herdr {
		hint(config.Text(
			"menu.herdr_unavailable_welcome_can_select_another_launcher",
		))
	}

	labels := agentLabels()
	hint(config.Text("menu.agent_capabilities"))
	anyExec := false
	anyReview := false
	for _, name := range config.AgentNames(agentConfig) {
		state := agents[name]
		if labels[name] == "" {
			labels[name] = name
		}
		if reviewerUsable(state) {
			anyReview = true
		}
		if agentUsable(state) {
			anyExec = true
			if reviewerUsable(state) {
				anyReview = true
			}
			caps := []string{config.Text("menu.execution")}
			if reviewerUsable(state) {
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

	reportOverlayConfigDirs()
	reportOverlayInstallRules()
	loaded, loadErr := config.Load(false)
	if loadErr != nil {
		warning(loadErr.Error())
		healthy = false
	} else {
		reportIgnoredConfig(loaded)
		reportPolicyOverrides(loaded)
		cfgPath, _ := config.ConfigPath()
		if loaded.WelcomeComplete {
			success(config.Text("menu.config", cfgPath))
		} else {
			hint(config.Text("menu.config_incomplete", cfgPath))
			maybeHintLegacyOnevoke()
		}
		healthy = validateConfiguredResources(loaded, agents, tools) && healthy
		reportAgentSessionFileInheritance(loaded)
		if loaded.WelcomeComplete {
			switches, switchErr := config.ScopeInstallRules(paths)
			if switchErr != nil {
				warning(switchErr.Error())
				healthy = false
			} else {
				healthy = reportProjectReferences(paths, switches) && healthy
				healthy = reportAgentExtensions(paths, switches, repair) && healthy
				healthy = reportLeaderSkills(paths, switches, repair) && healthy
			}
		}
	}
	return healthy
}

// reportIgnoredConfig lists what this version ignores in the configuration: keys it
// does not define, agents entries it cannot use, and roles that name an agent it does
// not know. Such content usually comes from a newer version and is kept on save, so it
// is only reported and never counts as a problem to repair.
func reportIgnoredConfig(cfg *config.Config) {
	var ignored []string
	var unusable []string
	for _, item := range cfg.Ignored {
		if item.Kind == config.IgnoredUnusableAgent {
			unusable = append(unusable, config.IgnoredNotice(item))
			continue
		}
		ignored = append(ignored, config.IgnoredNotice(item))
	}
	if len(ignored) > 0 {
		hint(config.Text("menu.doctor_ignored_heading"))
		for _, notice := range ignored {
			hint(notice)
		}
	}
	for _, notice := range unusable {
		warning(notice)
	}
	for _, notice := range config.UnknownAgentRefNotices(cfg) {
		warning(notice)
	}
}

// reportPolicyOverrides lists the configuration file values the cached project policy replaces,
// one hint line each. The files stay as they are and the line does not make doctor unhealthy:
// the policy is the value in force.
func reportPolicyOverrides(cfg *config.Config) {
	if len(cfg.Policy.Overrides) == 0 {
		return
	}
	hint(config.Text("policy.doctor_heading", cfg.Policy.Revision))
	for _, override := range cfg.Policy.Overrides {
		hint(config.PolicyOverrideText(override))
		// With --json the same items are also project_policy_override notices, as in
		// kander config, so a reader of notices sees them too.
		if command.JSON() {
			command.Notify(config.NoticePolicyOverride, config.PolicyOverrideText(override))
		}
	}
}

// reportOverlayConfigDirs warns about agents.<name>.config_dir in the project
// overlay: install and doctor read that key only from the scope config file, so
// a checked-out repository cannot redirect writes to global agent files.
func reportOverlayConfigDirs() {
	file, names, err := config.OverlayAgentConfigDirs()
	if err != nil {
		// config.Load reads the same overlay next and reports this error.
		return
	}
	for _, name := range names {
		warning(config.Text("menu.overlay_config_dir_ignored", name, file))
	}
}

// reportOverlayInstallRules warns about install_rules keys in the project overlay: only the
// scope config file may turn the rules injection on or off.
func reportOverlayInstallRules() {
	file, keys, err := config.OverlayInstallRules()
	if err != nil {
		// config.Load reads the same overlay next and reports this error.
		return
	}
	for _, key := range keys {
		warning(config.Text("menu.overlay_install_rules_ignored", key, file))
	}
}

// reportAgentExtensions reports the state of every covered agent rules extension,
// reusing install.ExtensionAgents so doctor cannot drift from the install-time
// coverage; in repair mode it writes missing and outdated copies instead of only
// warning, while a locally modified file is reported and left untouched.
func reportAgentExtensions(paths config.InstallPaths, switches config.InstallRules, repair bool) bool {
	healthy := true
	for _, agent := range install.ExtensionAgents(paths) {
		label := config.AgentDisplayName(agent)
		if repair {
			outcome, ensureErr := install.EnsureExtensionWith(agent, paths, switches)
			if ensureErr != nil {
				healthy = false
				warning(config.Text("menu.agent_extension_error", label, ensureErr.Error()))
				continue
			}
			switch outcome.Status {
			case install.ExtensionDisabled:
				hint(config.Text("menu.rules_injection_disabled", label, outcome.Target))
			case install.ExtensionInstalled:
				success(config.Text("menu.agent_extension_installed", label, outcome.Target))
			case install.ExtensionWritten:
				success(config.Text("menu.agent_extension_written", label, outcome.Target))
			case install.ExtensionUpdated:
				success(config.Text("menu.agent_extension_updated", label, outcome.Target))
			case install.ExtensionModified:
				hint(config.Text("menu.agent_extension_modified", label, outcome.Target))
			default:
				healthy = false
				warning(config.Text("menu.agent_extension_missing", label, outcome.Target))
			}
			continue
		}
		if !switches.Enabled(agent) {
			target, _ := install.ExtensionTarget(agent, paths)
			hint(config.Text("menu.rules_injection_disabled", label, target))
			continue
		}
		outcome, inspectErr := install.InspectExtension(agent, paths)
		if inspectErr != nil {
			healthy = false
			warning(config.Text("menu.agent_extension_error", label, inspectErr.Error()))
			continue
		}
		switch outcome.Status {
		case install.ExtensionInstalled:
			success(config.Text("menu.agent_extension_installed", label, outcome.Target))
		case install.ExtensionMissing:
			healthy = false
			warning(config.Text("menu.agent_extension_missing", label, outcome.Target))
		case install.ExtensionOutdated:
			healthy = false
			warning(config.Text("menu.agent_extension_outdated", label, outcome.Target))
		default:
			hint(config.Text("menu.agent_extension_modified", label, outcome.Target))
		}
	}
	return healthy
}

// reportLeaderSkills reports the bundled kander-leader skill at every destination the
// install scope covers; in repair mode it installs a missing skill and updates a managed
// one, so a new binary refreshes the skill together with the rules. Local edits,
// user-owned directories and links, and unsafe destinations are reported and kept.
func reportLeaderSkills(paths config.InstallPaths, switches config.InstallRules, repair bool) bool {
	outcomes := install.InspectLeaderSkills(paths, switches)
	if repair {
		outcomes = install.EnsureLeaderSkills(paths, switches)
	}
	healthy := true
	for _, outcome := range outcomes {
		label := config.AgentDisplayName(outcome.Agent)
		if outcome.Err != nil {
			healthy = false
			warning(config.Text("menu.leader_skill_error", label, outcome.Err.Error()))
			continue
		}
		switch outcome.Status {
		case install.LeaderSkillDisabled:
			hint(config.Text("menu.rules_injection_disabled", label, outcome.Target))
		case install.LeaderSkillInstalled:
			success(config.Text("menu.leader_skill_installed", label, outcome.Target))
		case install.LeaderSkillWritten:
			success(config.Text("menu.leader_skill_written", label, outcome.Target))
		case install.LeaderSkillUpdated:
			success(config.Text("menu.leader_skill_updated", label, outcome.Target))
		case install.LeaderSkillMissing:
			healthy = false
			warning(config.Text("menu.leader_skill_missing", label, outcome.Target))
		case install.LeaderSkillOutdated:
			healthy = false
			warning(config.Text("menu.leader_skill_outdated", label, outcome.Target))
		case install.LeaderSkillUnmanaged:
			hint(config.Text("menu.leader_skill_unmanaged", label, outcome.Target))
		case install.LeaderSkillSkipped:
			hint(config.Text("menu.leader_skill_skipped", label, outcome.Reason.Error()))
		}
		if outcome.Modified {
			hint(config.Text("menu.leader_skill_modified", label, outcome.Target))
		}
	}
	return healthy
}

// sessionFileInheritSpec reports whether the named agent overlay declares a
// session without file and therefore inherits the file declaration of its
// dialect's embedded definition, returning the resolved dialect and a detached
// copy of that declaration. Inheritance applies only while the overlay keeps
// the embedded session mode; AgentFor(nil, dialect) resolves the embedded
// definition because dialect accepts embedded names only.
func sessionFileInheritSpec(cfg *config.Config, name string) (dialect string, file *config.SessionFile, ok bool) {
	if cfg == nil {
		return "", nil, false
	}
	overlay, exists := cfg.Agents[name]
	if !exists || overlay.Session == nil || overlay.Session.File != nil {
		return "", nil, false
	}
	dialect = overlay.Dialect
	if dialect == "" {
		if !slices.Contains(config.ExecutionAgents, name) {
			return "", nil, false
		}
		dialect = name
	}
	embedded := config.AgentFor(nil, dialect)
	if embedded.Session == nil || embedded.Session.File == nil || embedded.Session.Mode != overlay.Session.Mode {
		return "", nil, false
	}
	return dialect, embedded.Session.File, true
}

// reportAgentSessionFileInheritance surfaces every agent overlay whose session
// omits the file declaration it now inherits from the same-mode embedded
// definition. The hint is informational only: it never counts as an error and
// never changes the doctor exit status. Interactive repair may offer to store
// the declaration into the scope config.json.
func reportAgentSessionFileInheritance(cfg *config.Config) {
	for _, name := range config.AgentNames(cfg) {
		dialect, _, ok := sessionFileInheritSpec(cfg, name)
		if !ok {
			continue
		}
		hint(config.Text("menu.agent_session_file_inherited", name, dialect))
	}
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

func validateConfiguredResources(cfg *config.Config, agents map[string]agentState, tools TerminalTools) bool {
	healthy := true
	effective, err := config.Effective(cfg)
	if err != nil {
		warning(err.Error())
		return false
	}
	for _, selected := range config.ExecutionAgentsInUse(effective) {
		// An agent this version does not know is listed by reportIgnoredConfig, not as a missing install.
		if config.HasAgent(effective, selected) && !agentUsable(agents[selected]) {
			healthy = false
			warning(config.Text(
				"menu.configured_execution_agent_is_unavailable_install_it_then_run", selected,
			))
		}
	}
	if ok, msg := reviewGatePresent(); !ok {
		healthy = false
		warning(msg)
	}
	seenReviewers := map[string]struct{}{}
	for _, scale := range config.TaskScales {
		for _, role := range config.ReviewRoles {
			reviewer := config.ReviewerFor(effective, scale, role)
			key := role + "\x00" + reviewer
			if _, ok := seenReviewers[key]; ok {
				continue
			}
			seenReviewers[key] = struct{}{}
			if config.HasAgent(effective, reviewer) && !reviewerUsable(agents[reviewer]) {
				healthy = false
				warning(config.Text(
					"menu.configured_reviewer_is_unavailable_install_it_then_run_kander", role, reviewer,
				))
			}
		}
	}
	launcher := effective.Launcher
	switch launcher {
	case builtin.Tmux, builtin.TmuxSession:
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
		} else if launcher == builtin.TmuxSession {
			hint(config.Text(
				"menu.launcher_tmux_session_creates_or_reuses_a_per_project",
			))
		} else if os.Getenv("TMUX") == "" {
			hint(config.Text(
				"menu.launcher_tmux_requires_entering_the_tmux_session_shown_above",
			))
		}
	case direct.Console:
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
	case builtin.Herdr:
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
	case builtin.Luvus:
		if isWindowsOS() {
			healthy = false
			warning(config.Text(
				"menu.the_configured_launcher_is_but_native_windows_does_not", launcher,
			))
		} else if !tools.Luvus.Available() {
			healthy = false
			warning(config.Text("luvus.doctor_not_in_path"))
		} else if !cfg.WelcomeComplete {
			break
		} else if os.Getenv("LUVUS_ENV") != "1" {
			hint(config.Text("luvus.doctor_not_inside"))
		} else if strings.TrimSpace(os.Getenv("LUVUS_PANE_ID")) == "" {
			healthy = false
			warning(config.Text("luvus.doctor_pane_id_missing"))
		} else {
			hint(config.Text("luvus.doctor_ready"))
		}
	case builtin.HerdrCore:
		healthy = validateHerdrCoreLauncher(effective, tools) && healthy
	case "auto":
		if !tools.Tmux.Available() && !tools.Herdr.Available() && !tools.Luvus.Available() && !tools.HerdrCore.Available() {
			healthy = false
			if tools.Herdr.OffPath != "" {
				// herdr is installed but the current PATH cannot see it; pointing at
				// the path beats telling the user to install what they already have.
				warning(config.Text("menu.herdr_installed_at_reopen_terminal", tools.Herdr.OffPath))
			} else {
				// Native Windows never probes tmux or luvus and auto lands only on herdr-core or herdr, so the hint must not mention them.
				key := "menu.the_configured_launcher_is_auto_but_neither_herdr_nor"
				if isWindowsOS() {
					key = "menu.the_configured_launcher_is_auto_but_herdr_is_not_available"
					if herdrCoreUnsupported() {
						key = "menu.the_configured_launcher_is_auto_but_only_herdr_can_run"
					}
				}
				warning(config.Text(key))
			}
		} else {
			key := "menu.launcher_auto_chooses_at_start_a_herdr_tab_if"
			if isWindowsOS() {
				key = "menu.launcher_auto_chooses_at_start_on_windows"
				if herdrCoreUnsupported() {
					key = "menu.launcher_auto_chooses_at_start_on_windows_only_herdr"
				}
			}
			hint(config.Text(key))
		}
	default:
		if !checkDefinitionLauncher(launcher) {
			healthy = false
		}
	case direct.Foreground:
		if cfg.WelcomeComplete {
			launcherHint := builtin.Tmux
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
