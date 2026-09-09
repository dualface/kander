package menu

import "github.com/dualface/kander/internal/config"

// SetRules keeps both configuration UIs on the same validation and copy boundary.
func (s *Session) SetRules(rules config.Rules) error {
	if err := config.ValidateRules(rules); err != nil {
		return err
	}
	previous := s.Config.Rules.Clone()
	if s.EditingOverlay() {
		return s.applyOverlayEdit(func(candidate map[string]any) {
			for _, module := range config.RuleModules {
				if previous[module] != rules[module] {
					config.OverlaySet(candidate, rules[module], "rules", module)
				}
			}
		})
	}
	s.Config.Rules = rules.Clone()
	s.ScopeDirty = true
	for _, module := range config.RuleModules {
		if previous[module] != rules[module] {
			s.syncScopeRaw([]string{"rules", module}, rules[module])
		}
	}
	return nil
}

func RulesSessionNotice() string {
	return config.Text("rules.session_notice")
}

func editRules(session *Session) error {
	for {
		var choices []Choice
		for _, module := range config.RuleModules {
			state := config.Text("rules.off")
			if session.Config.Rules[module] {
				state = config.Text("rules.on")
			}
			choices = append(choices, Choice{Value: module, Label: config.RuleLabel(module) + ": " + state})
		}
		choices = append(choices,
			Choice{Value: "all", Label: config.Text("rules.all")},
			Choice{Value: "none", Label: config.Text("rules.none")},
			Choice{Value: "back", Label: config.Text("rules.back")},
		)
		selected, err := askChoice(config.Text("rules.toggle"), choices, "back")
		if err != nil {
			return err
		}
		if selected == "back" {
			return nil
		}
		rules := session.Config.Rules.Clone()
		if selected == "all" || selected == "none" {
			rules = config.DefaultRules(selected == "all")
		} else {
			rules[selected] = !rules[selected]
		}
		if err := session.SetRules(rules); err != nil {
			warning(err.Error())
		}
	}
}
