package install

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/config"
)

func isTTY(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func requireInteractive() error {
	if isTTY(os.Stdin) && isTTY(os.Stdout) {
		return nil
	}
	return fmt.Errorf("%s", config.Text("install.requires_terminal"))
}

func applyLanguage(lang string) {
	config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
}

func supportedLanguage(lang string) bool {
	for _, item := range config.Languages {
		if item == lang {
			return true
		}
	}
	return false
}

func runWizard() (Request, error) {
	req := Request{
		Language: config.ResolveLanguage(),
		Mode:     config.ModeGlobal,
	}
	if !supportedLanguage(req.Language) {
		req.Language = "en"
	}
	cwd, _ := os.Getwd()
	project := cwd
	scope := "global"

	langForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(config.Text("install.choose_language")).
			Options(
				huh.NewOption(config.Text("install.language_cn"), "cn"),
				huh.NewOption(config.Text("install.language_en"), "en"),
				huh.NewOption(config.Text("install.language_ja"), "ja"),
			).
			Value(&req.Language),
	))
	if err := langForm.Run(); err != nil {
		return req, mapWizardErr(err)
	}
	applyLanguage(req.Language)

	destForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(config.Text("install.choose_destination")).
			Options(
				huh.NewOption(config.Text("install.destination_global"), "global"),
				huh.NewOption(config.Text("install.destination_project"), "project"),
			).
			Value(&scope),
	))
	if err := destForm.Run(); err != nil {
		return req, mapWizardErr(err)
	}
	if scope == "project" {
		req.Mode = config.ModeProject
		input := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title(config.Text("install.project_directory")).
				Value(&project),
		))
		if err := input.Run(); err != nil {
			return req, mapWizardErr(err)
		}
		req.Project = project
	}
	choice, err := confirmRulesInjection(req)
	if err != nil {
		return req, err
	}
	req.RulesChoice = choice
	return req, nil
}

// confirmRulesInjection lists the agent files the install would write and lets the user pick
// the agents Kander writes rules for, before anything is written. A group starts selected when
// every agent in it has install_rules on, so a fresh install selects all and a reinstall keeps
// an agent the user turned off. Cancelling writes nothing.
func confirmRulesInjection(req Request) (map[string]bool, error) {
	paths, err := resolvePaths(req)
	if err != nil {
		return nil, err
	}
	switches, err := config.ScopeInstallRules(paths)
	if err != nil {
		return nil, err
	}
	groups := PlanRulesInjection(paths)
	fields := []huh.Field{
		huh.NewNote().Title(config.Text("install.rules_plan_title")).Description(planDescription(groups, paths)),
	}
	var selected []int
	if len(groups) > 0 {
		options := make([]huh.Option[int], 0, len(groups))
		for index, group := range groups {
			options = append(options, huh.NewOption(groupOptionLabel(group, paths), index))
			if groupEnabled(group, switches) {
				selected = append(selected, index)
			}
		}
		fields = append(fields, huh.NewMultiSelect[int]().
			Title(config.Text("install.rules_select_title")).
			Description(config.Text("install.rules_select_description")).
			Options(options...).
			Value(&selected))
	}
	confirmed := true
	fields = append(fields, huh.NewConfirm().
		Title(config.Text("install.rules_confirm")).
		Affirmative(config.Text("install.rules_confirm_yes")).
		Negative(config.Text("install.rules_confirm_no")).
		Value(&confirmed))
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return nil, mapWizardErr(err)
	}
	if !confirmed {
		return nil, fmt.Errorf("%s", config.Text("install.wizard_cancelled"))
	}
	return rulesChoice(groups, selected, switches), nil
}

// planDescription renders one line per planned file: the change, the path, and the agents
// whose switches control it.
func planDescription(groups []PlanGroup, paths config.InstallPaths) string {
	if len(groups) == 0 {
		return config.Text("install.rules_plan_empty")
	}
	var lines []string
	for _, group := range groups {
		agents := AgentLabelList(group.Agents)
		for _, file := range group.Files {
			if file.Err != nil {
				lines = append(lines, config.Text("install.rules_plan_error", agents, file.Err.Error()))
				continue
			}
			lines = append(lines, config.Text("install.rules_plan_line",
				rulesChangeLabel(file.Change), displayTarget(file.Target, paths), agents))
		}
	}
	return strings.Join(lines, "\n")
}

func groupOptionLabel(group PlanGroup, paths config.InstallPaths) string {
	for _, file := range group.Files {
		if file.Target != "" {
			return AgentLabelList(group.Agents) + " — " + displayTarget(file.Target, paths)
		}
	}
	return AgentLabelList(group.Agents)
}

func rulesChangeLabel(change RulesChange) string {
	switch change {
	case RulesChangeCreate:
		return config.Text("install.rules_change_create")
	case RulesChangeAppend:
		return config.Text("install.rules_change_append")
	case RulesChangeRewrite:
		return config.Text("install.rules_change_rewrite")
	case RulesChangeReplace:
		return config.Text("install.rules_change_replace")
	case RulesChangeUpdate:
		return config.Text("install.rules_change_update")
	default:
		return config.Text("install.rules_change_none")
	}
}

func mapWizardErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return fmt.Errorf("%s", config.Text("install.wizard_cancelled"))
	}
	return err
}

func printResult(result Result) {
	fmt.Println(config.Text("install.installed"))
	if result.ServerBinary != "" {
		fmt.Println(result.ServerBinary)
	}
	if result.DestBinary == "" {
		fmt.Println(config.Text("install.existing_entry", result.RunBinary))
	}
	if result.Paths.Mode == config.ModeProject {
		fmt.Println(result.DestBinary)
		fmt.Fprintln(os.Stderr, config.Text("install.project_finished"))
	}
	if result.LegacyRemoved {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_removed"))
	} else if len(result.Legacy) > 0 {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_kept"))
	}
	for _, path := range result.LegacyLinksRemoved {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_link_replaced", path))
	}
	for _, item := range result.Integrations {
		target := item.Target
		if target == "" {
			target = item.Agent
		}
		if item.Err != nil {
			fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_failed", target, item.Err.Error()))
			continue
		}
		printIntegrationLine(item.IntegrationOutcome)
		if item.Override != nil {
			printIntegrationLine(*item.Override)
		}
	}
	for _, item := range result.Extensions {
		target := item.Target
		if target == "" {
			target = item.Agent
		}
		label := config.AgentDisplayName(item.Agent)
		if item.Err != nil {
			fmt.Fprintln(os.Stderr, config.Text("install.agent_extension_failed", label, target, item.Err.Error()))
			continue
		}
		switch item.Status {
		case ExtensionDisabled:
			fmt.Fprintln(os.Stderr, config.Text("install.rules_injection_disabled", label, target))
		case ExtensionWritten:
			fmt.Fprintln(os.Stderr, config.Text("install.agent_extension_written", label, target))
		case ExtensionUpdated:
			fmt.Fprintln(os.Stderr, config.Text("install.agent_extension_updated", label, target))
		case ExtensionModified:
			fmt.Fprintln(os.Stderr, config.Text("install.agent_extension_modified", label, target))
		}
	}
}

// printIntegrationLine prints the per-file result of one ensured rules file, so the
// AGENTS.override.md sibling reports the same way the configured target does.
func printIntegrationLine(item IntegrationOutcome) {
	target := item.Target
	if target == "" {
		target = item.Agent
	}
	switch item.Status {
	case IntegrationDisabled:
		fmt.Fprintln(os.Stderr, config.Text("install.rules_injection_disabled", AgentLabelList(item.DisabledBy), target))
		return
	case IntegrationRewritten:
		fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_updated", target))
	case IntegrationCleaned:
		fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_cleaned", target, item.Removed))
	case IntegrationCreated, IntegrationUpdated:
		fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_added", target))
	case IntegrationFrontmatterAdded:
		fmt.Fprintln(os.Stderr, config.Text("install.always_apply_frontmatter_added", target))
	}
	if item.Inactive {
		fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_inactive", target))
	}
}

// RunInteractive initializes the chosen scope and hands off to its selected entry.
func RunInteractive() int {
	if err := requireInteractive(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req, err := runWizard()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if req.Mode != config.ModeProject {
		applyGlobalInstallDefaults(&req)
	}
	result, err := Perform(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return finishSuccessfulInstall(result, req.Language)
}

// finishSuccessfulInstall continues with the existing or explicitly installed binary.
func finishSuccessfulInstall(result Result, lang string) int {
	printResult(result)
	if result.Paths.Mode != config.ModeProject && result.DestBinary != "" {
		warnPath(result.RunBinary)
	}
	if err := launchInstalled(result.RunBinary, lang); err != nil {
		fmt.Fprintln(os.Stderr, config.Text("install.failed_handoff", err.Error()))
		return 1
	}
	return 0
}
