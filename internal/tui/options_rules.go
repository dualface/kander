package tui

import (
	"reflect"

	"github.com/charmbracelet/huh"
	"github.com/dualface/kander/internal/config"
)

// The workflow presets only manage this list; standalone modules are rendered separately and take part in neither preset application nor matching.
var workflowRuleModules = []string{
	config.RuleCollaboration, config.RuleGit, config.RuleReview,
	config.RuleTaskIntake, config.RuleTaskGroups, config.RuleReporting,
}

var independentRuleModules = []string{config.RuleCode}

const customRulePreset = "custom"

var workflowRulePresets = []struct {
	id      string
	label   string
	enabled config.Rules
}{
	{"basic", "rules.basic_workflow", config.Rules{
		config.RuleCollaboration: true, config.RuleGit: true,
		config.RuleTaskIntake: true, config.RuleReporting: true,
	}},
	{"full", "rules.full_workflow", config.Rules{
		config.RuleCollaboration: true, config.RuleGit: true, config.RuleReview: true,
		config.RuleTaskIntake: true, config.RuleTaskGroups: true, config.RuleReporting: true,
	}},
}

func matchWorkflowPreset(rules config.Rules) string {
	for _, preset := range workflowRulePresets {
		matches := true
		for _, module := range workflowRuleModules {
			if rules[module] != preset.enabled[module] {
				matches = false
				break
			}
		}
		if matches {
			return preset.id
		}
	}
	return customRulePreset
}

func (p *optionsPanel) rulesGroup(bind *formBinding) *huh.Group {
	bind.reset()
	bind.rules = map[string]*bool{}
	bind.rulePreset = matchWorkflowPreset(p.session.Config.Rules)
	bind.prevRulePreset = bind.rulePreset
	bind.fieldIndex["rules:preset"] = bind.focusable
	options := make([]huh.Option[string], 0, len(workflowRulePresets)+1)
	for _, preset := range workflowRulePresets {
		options = append(options, huh.NewOption(t(preset.label), preset.id))
	}
	options = append(options, huh.NewOption(t("rules.custom"), customRulePreset))
	bind.addField(huh.NewSelect[string]().Title(optionTitle(t("rules.selection"))).
		Options(options...).
		Inline(true).Value(&bind.rulePreset))
	bind.addRuleFields(p, p.session.Config.Rules, workflowRuleModules)
	bind.addSpacer()
	bind.formFields = append(bind.formFields, huh.NewNote().Title(t("rules.independent")))
	bind.addRuleFields(p, p.session.Config.Rules, independentRuleModules)
	bind.addGitBranchModeField(p)
	bind.addIssueResultField(p)
	bind.addInstallRulesFields(p)
	bind.formFields = append(bind.formFields, huh.NewNote().Description(t("rules.contract_notice")))
	bind.addPageRestore(p)
	return huh.NewGroup(bind.formFields...)
}

func (bind *formBinding) addRuleFields(p *optionsPanel, rules config.Rules, modules []string) {
	for _, module := range modules {
		enabled := rules[module]
		bind.rules[module] = &enabled
		bind.fieldIndex["rules:"+module] = bind.focusable
		bind.addField(inlineConfirm().Title(p.inheritTitle(config.RuleLabel(module), formatBool(enabled), "rules", module)).
			Affirmative(t("rules.on")).Negative(t("rules.off")).
			Value(&enabled))
	}
}

// addIssueResultField edits issue.result_on_done, the completion handoff the
// issue rules apply once every card of an issue's work set is complete.
func (bind *formBinding) addIssueResultField(p *optionsPanel) {
	mode := p.session.Config.Issue.ResultOnDone
	bind.issueResult = &mode
	options := make([]huh.Option[string], 0, len(config.IssueResultModes))
	for _, value := range config.IssueResultModes {
		options = append(options, huh.NewOption(config.IssueResultLabel(value), value))
	}
	bind.fieldIndex[issueResultFocusKey] = bind.focusable
	bind.addField(huh.NewSelect[string]().
		Title(p.inheritTitle(t("config.issue_result_on_done"), config.IssueResultLabel(mode), "issue", "result_on_done")).
		Options(options...).Inline(true).Value(bind.issueResult))
}

const issueResultFocusKey = "issue:result_on_done"

// addGitBranchModeField edits git.branch_mode, the mainline branch the Git
// rules integrate cards and task group branches into.
func (bind *formBinding) addGitBranchModeField(p *optionsPanel) {
	mode := p.session.Config.Git.BranchMode
	bind.gitBranchMode = &mode
	options := make([]huh.Option[string], 0, len(config.GitBranchModes))
	for _, value := range config.GitBranchModes {
		options = append(options, huh.NewOption(config.GitBranchModeLabel(value), value))
	}
	bind.fieldIndex[gitBranchModeFocusKey] = bind.focusable
	bind.addField(huh.NewSelect[string]().
		Title(p.inheritTitle(t("config.git_branch_mode"), config.GitBranchModeLabel(mode), "git", "branch_mode")).
		Options(options...).Inline(true).Value(bind.gitBranchMode))
}

const gitBranchModeFocusKey = "git:branch_mode"

func (b *formBinding) applyGitBranchMode(p *optionsPanel) {
	if b.gitBranchMode == nil || *b.gitBranchMode == p.session.Config.Git.BranchMode {
		return
	}
	before := p.overridePresence("git", "branch_mode")
	if err := p.session.SetGitBranchMode(*b.gitBranchMode); err != nil {
		*b.gitBranchMode = p.session.Config.Git.BranchMode
		p.showReport(t("config.git_branch_mode"), nil, err.Error())
		p.rebuildAt(gitBranchModeFocusKey)
		return
	}
	p.markDirty()
	p.rebuildIfOverrideChanged(before, gitBranchModeFocusKey, "git", "branch_mode")
}

func (b *formBinding) applyIssueResult(p *optionsPanel) {
	if b.issueResult == nil || *b.issueResult == p.session.Config.Issue.ResultOnDone {
		return
	}
	before := p.overridePresence("issue", "result_on_done")
	if err := p.session.SetIssueResultOnDone(*b.issueResult); err != nil {
		*b.issueResult = p.session.Config.Issue.ResultOnDone
		p.showReport(t("config.issue_result_on_done"), nil, err.Error())
		p.rebuildAt(issueResultFocusKey)
		return
	}
	p.markDirty()
	p.rebuildIfOverrideChanged(before, issueResultFocusKey, "issue", "result_on_done")
}

func (b *formBinding) applyRules(p *optionsPanel) {
	b.applyGitBranchMode(p)
	b.applyIssueResult(p)
	b.applyInstallRules(p)
	rules := p.session.Config.Rules.Clone()
	focus := "rules:preset"
	for _, module := range config.RuleModules {
		if value := b.rules[module]; value != nil {
			if rules[module] != *value {
				focus = "rules:" + module
			}
			rules[module] = *value
		}
	}
	// Apply a preset only when the user switches the selector, so later per-item edits are not overwritten by the old preset.
	presetChanged := b.rulePreset != b.prevRulePreset
	if presetChanged {
		for _, preset := range workflowRulePresets {
			if preset.id == b.rulePreset {
				for _, module := range workflowRuleModules {
					rules[module] = preset.enabled[module]
				}
				break
			}
		}
		focus = "rules:preset"
	}
	if !reflect.DeepEqual(rules, p.session.Config.Rules) {
		if err := p.session.SetRules(rules); err != nil {
			for module, value := range b.rules {
				*value = p.session.Config.Rules[module]
			}
			p.showReport(t("rules.dependency"), nil, err.Error())
			p.rebuildAt(focus)
			return
		}
		// Raw merges may change inherited siblings as well as the edited rule.
		for module, value := range b.rules {
			*value = p.session.Config.Rules[module]
		}
		p.markDirty()
		p.rebuildAt(focus)
	}
	if presetChanged || matchWorkflowPreset(p.session.Config.Rules) != b.rulePreset {
		p.rebuildAt(focus)
	}
}

const installRulesFocusKey = "install_rules:"

// addInstallRulesFields edits the rules-injection switches: the global one, bound under "",
// and one per agent the scope covers. Only the scope config file holds them, so the Project
// tab shows none.
func (bind *formBinding) addInstallRulesFields(p *optionsPanel) {
	bind.installRules = nil
	if p.session.EditingOverlay() {
		return
	}
	switches := p.session.InstallRules()
	bind.installRules = map[string]*bool{}
	bind.addSpacer()
	bind.formFields = append(bind.formFields, huh.NewNote().Title(t("config.install_rules")))
	global := switches.Global
	bind.installRules[""] = &global
	bind.fieldIndex[installRulesFocusKey] = bind.focusable
	bind.addField(inlineConfirm().Title(optionTitle(t("rules.install_rules_global"))).
		Affirmative(t("rules.on")).Negative(t("rules.off")).
		Value(&global))
	for _, agent := range p.session.InstallRulesAgents() {
		enabled := switches.Enabled(agent)
		bind.installRules[agent] = &enabled
		bind.fieldIndex[installRulesFocusKey+agent] = bind.focusable
		bind.addField(inlineConfirm().Title(optionTitle(config.AgentDisplayName(agent))).
			Affirmative(t("rules.on")).Negative(t("rules.off")).
			Value(&enabled))
	}
}

// applyInstallRules stores edited switches; agent fields are rebuilt after a global change,
// since agents without their own value follow it.
func (b *formBinding) applyInstallRules(p *optionsPanel) {
	if b.installRules == nil {
		return
	}
	switches := p.session.InstallRules()
	if value := b.installRules[""]; value != nil && *value != switches.Global {
		if err := p.session.SetInstallRules(*value); err != nil {
			*value = switches.Global
			p.showReport(t("config.install_rules"), nil, err.Error())
			return
		}
		p.markDirty()
		p.rebuildAt(installRulesFocusKey)
		return
	}
	for agent, value := range b.installRules {
		if agent == "" || value == nil || *value == switches.Enabled(agent) {
			continue
		}
		if err := p.session.SetAgentInstallRules(agent, *value); err != nil {
			*value = switches.Enabled(agent)
			p.showReport(t("config.install_rules"), nil, err.Error())
			return
		}
		p.markDirty()
		p.rebuildAt(installRulesFocusKey + agent)
		return
	}
}
