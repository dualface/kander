package config

import "strings"

// Rules controls optional workflow advice, not the contracts of the commands themselves.
type Rules map[string]bool

const (
	RuleCollaboration = "collaboration"
	RuleCode          = "code"
	RuleGit           = "git"
	RuleReview        = "review"
	RuleTaskIntake    = "task_intake"
	RuleTaskGroups    = "task_groups"
	RuleReporting     = "reporting"
)

var RuleModules = []string{
	RuleCollaboration, RuleCode, RuleGit, RuleReview, RuleTaskIntake, RuleTaskGroups, RuleReporting,
}

// DefaultRules returns an independent selection for the current modules.
func DefaultRules(enabled bool) Rules {
	rules := make(Rules, len(RuleModules))
	for _, module := range RuleModules {
		rules[module] = enabled
	}
	return rules
}

// legacyRules preserves only the modules shipped before rules became configurable.
// Future modules must not be implicitly enabled for old configurations.
func legacyRules() Rules {
	rules := DefaultRules(false)
	for _, module := range []string{RuleCollaboration, RuleCode, RuleGit, RuleReview, RuleTaskIntake, RuleTaskGroups, RuleReporting} {
		rules[module] = true
	}
	return rules
}

func (r Rules) Clone() Rules {
	copy := make(Rules, len(r))
	for key, enabled := range r {
		copy[key] = enabled
	}
	return copy
}

func ValidateRules(rules Rules) error {
	for _, key := range sortedRulesKeys(rules) {
		if !contains(RuleModules, key) {
			return configErrorf("rules.unknown_module", key)
		}
	}
	if rules[RuleTaskGroups] && !rules[RuleGit] {
		return configErrorf("rules.group_requires_git")
	}
	return nil
}

func sortedRulesKeys(rules Rules) []string {
	keys := make([]string, 0, len(rules))
	for key := range rules {
		keys = append(keys, key)
	}
	return sorted(keys)
}

func validateRules(raw any) (Rules, error) {
	obj, ok := asObject(raw)
	if !ok {
		return nil, configErrorf("rules.object_required")
	}
	rules := DefaultRules(false)
	for key, value := range obj {
		enabled, ok := value.(bool)
		if !ok {
			return nil, configErrorf("rules.boolean_required", key)
		}
		rules[key] = enabled
	}
	return rules, ValidateRules(rules)
}

// CheckTaskGroup is shared by start, resume, takeover and notify before side effects.
func (r Rules) CheckTaskGroup(group string) error {
	if group == "" {
		return nil
	}
	if !r[RuleTaskGroups] || !r[RuleGit] {
		return configErrorf("rules.group_disabled", group)
	}
	return nil
}

func RuleLabel(module string) string {
	labels := map[string]string{
		RuleCollaboration: "rules.collaboration",
		RuleCode:          "rules.code",
		RuleGit:           "rules.git",
		RuleReview:        "rules.review",
		RuleTaskIntake:    "rules.task_intake",
		RuleTaskGroups:    "rules.task_groups",
		RuleReporting:     "rules.reporting",
	}
	if label, ok := labels[module]; ok {
		return Text(label)
	}
	return module
}

func FormatRulesSummary(rules Rules) string {
	var enabled []string
	for _, module := range RuleModules {
		if rules[module] {
			enabled = append(enabled, module)
		}
	}
	if len(enabled) == 0 {
		return Text("rules.all_off_summary")
	}
	return strings.Join(enabled, ", ")
}
