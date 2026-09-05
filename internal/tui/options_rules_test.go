package tui

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestRulePanelSelectionDependencyAndPersistence(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	_, panel := openPanel(t)
	panel.session.Config.Rules = config.DefaultRules(false)
	pumpPanel(panel, panel.dispatch(sectionRules))
	// An all-off configuration is custom; moving left selects the full workflow.
	if panel.bind.rulePreset != customRulePreset || panel.dirty {
		t.Fatal("opening custom rules must not change the configuration")
	}
	drivePanel(panel, keyMsg("left"))
	for _, module := range workflowRuleModules {
		if !panel.session.Config.Rules[module] {
			t.Fatalf("bulk enable missed %s", module)
		}
	}
	if panel.session.Config.Rules[config.RuleCode] || panel.bind.rulePreset != "full" {
		t.Fatal("full workflow must preserve the independent code rule")
	}
	// Attempting to disable Git while task groups remain enabled must keep a valid selection.
	*panel.bind.rules[config.RuleGit] = false
	panel.bind.apply(panel)
	if !panel.session.Config.Rules[config.RuleGit] || panel.report == nil {
		t.Fatal("dependency should reject the edit and explain why")
	}
	drivePanel(panel, keyMsg("esc"))
	pumpPanel(panel, panel.openSection(sectionRules))
	*panel.bind.rules[config.RuleReview] = false
	*panel.bind.rules[config.RuleCode] = true
	panel.bind.apply(panel)
	pumpPanel(panel, panel.finishSection())
	saved, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Rules[config.RuleReview] || !saved.Rules[config.RuleTaskGroups] || !saved.Rules[config.RuleGit] || !saved.Rules[config.RuleCode] {
		t.Fatalf("independent review selection not saved: %+v", saved.Rules)
	}
	pumpPanel(panel, panel.dispatch(sectionRules))
	drivePanel(panel, keyMsg("left"))
	drivePanel(panel, keyMsg("left"))
	pumpPanel(panel, panel.openSection(sectionRules))
	pumpPanel(panel, panel.finishSection())
	saved, err = config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if matchWorkflowPreset(saved.Rules) != "basic" || !saved.Rules[config.RuleCode] {
		t.Fatalf("basic workflow was not saved: %+v", saved.Rules)
	}
}

func TestWorkflowPresetsPreserveIndependentRules(t *testing.T) {
	for _, code := range []bool{false, true} {
		_, panel := openPanel(t)
		panel.session.Config.Rules = config.DefaultRules(false)
		panel.session.Config.Rules[config.RuleCode] = code
		pumpPanel(panel, panel.dispatch(sectionRules))
		for _, step := range []struct {
			key, preset string
			advanced    bool
		}{
			{"left", "full", true},
			{"left", "basic", false},
			{"right", "full", true},
		} {
			drivePanel(panel, keyMsg(step.key))
			want := config.Rules{
				config.RuleCollaboration: true, config.RuleGit: true,
				config.RuleTaskIntake: true, config.RuleReporting: true,
				config.RuleReview: step.advanced, config.RuleTaskGroups: step.advanced,
				config.RuleCode: code,
			}
			if !reflect.DeepEqual(panel.session.Config.Rules, want) || panel.bind.rulePreset != step.preset {
				t.Fatalf("%s with code=%v: rules=%v preset=%s", step.preset, code, panel.session.Config.Rules, panel.bind.rulePreset)
			}
			for module, value := range panel.bind.rules {
				if *value != want[module] {
					t.Fatalf("%s field did not synchronize", module)
				}
			}
		}
		// Reopening derives the preset from persisted module values, not UI history.
		pumpPanel(panel, panel.openSection(sectionRules))
		if panel.bind.rulePreset != "full" {
			t.Fatal("reopening must recognize the full workflow")
		}
		for i := 0; i < panel.bind.fieldIndex["rules:"+config.RuleCode]; i++ {
			drivePanel(panel, keyMsg("down"))
		}
		drivePanel(panel, keyMsg("right"))
		if panel.session.Config.Rules[config.RuleCode] == code || panel.bind.rulePreset != "full" {
			t.Fatal("independent toggle must not change the preset")
		}
	}
}

func TestWorkflowManualEditsMatchPresetsAndKeepFocus(t *testing.T) {
	_, panel := openPanel(t)
	panel.session.Config.Rules = config.DefaultRules(false)
	pumpPanel(panel, panel.dispatch(sectionRules))
	drivePanel(panel, keyMsg("left"))
	drivePanel(panel, keyMsg("left"))
	for i := 0; i < panel.bind.fieldIndex["rules:"+config.RuleReview]; i++ {
		drivePanel(panel, keyMsg("down"))
	}
	drivePanel(panel, keyMsg("right"))
	if !panel.session.Config.Rules[config.RuleReview] || panel.bind.rulePreset != customRulePreset {
		t.Fatal("manual review toggle must change basic workflow to custom")
	}
	// The selector rebuild must leave focus on Review, so the next toggle restores Basic.
	drivePanel(panel, keyMsg("right"))
	if panel.session.Config.Rules[config.RuleReview] || panel.bind.rulePreset != "basic" {
		t.Fatal("manual restoration must recognize basic and retain field focus")
	}
	drivePanel(panel, keyMsg("down"))
	drivePanel(panel, keyMsg("right"))
	if panel.session.Config.Rules[config.RuleTaskIntake] || panel.bind.rulePreset != customRulePreset {
		t.Fatal("moving after rebuild must reach the next workflow field")
	}
}

func TestRuleModuleCategoriesCoverConfig(t *testing.T) {
	seen := map[string]bool{}
	for _, modules := range [][]string{workflowRuleModules, independentRuleModules} {
		for _, module := range modules {
			if seen[module] {
				t.Fatalf("rule %s appears in multiple categories", module)
			}
			seen[module] = true
		}
	}
	for _, module := range config.RuleModules {
		if !seen[module] {
			t.Fatalf("rule %s needs a UI category", module)
		}
		delete(seen, module)
	}
	if len(seen) != 0 {
		t.Fatalf("unknown rules in UI categories: %v", seen)
	}
}
