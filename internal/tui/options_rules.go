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
	bind.addField(huh.NewSelect[string]().Title(t("rules.selection")).
		Options(options...).
		Inline(true).Value(&bind.rulePreset))
	bind.addRuleFields(p.session.Config.Rules, workflowRuleModules)
	bind.addSpacer()
	bind.formFields = append(bind.formFields, huh.NewNote().Title(t("rules.independent")))
	bind.addRuleFields(p.session.Config.Rules, independentRuleModules)
	bind.formFields = append(bind.formFields, huh.NewNote().Description(t("rules.contract_notice")))
	return huh.NewGroup(bind.formFields...)
}

func (bind *formBinding) addRuleFields(rules config.Rules, modules []string) {
	for _, module := range modules {
		enabled := rules[module]
		bind.rules[module] = &enabled
		bind.fieldIndex["rules:"+module] = bind.focusable
		bind.addField(huh.NewConfirm().Title(config.RuleLabel(module)).
			Affirmative(t("rules.on")).Negative(t("rules.off")).
			Inline(true).Value(&enabled))
	}
}

func (b *formBinding) applyRules(p *optionsPanel) {
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
		p.markDirty()
	}
	if presetChanged || matchWorkflowPreset(rules) != b.rulePreset {
		p.rebuildAt(focus)
	}
}
