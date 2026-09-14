package menu

import (
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestRuleSessionCopiesSelectionsAndRejectsInvalidChanges(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.Rules = config.DefaultRules(false)
	cfg.Rules[config.RuleReview] = true
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	selection := config.DefaultRules(false)
	selection[config.RuleTaskGroups] = true
	if err := session.SetRules(selection); err == nil {
		t.Fatal("invalid dependency accepted")
	}
	if !session.Config.Rules[config.RuleReview] {
		t.Fatal("rejected edit changed rules")
	}
	selection[config.RuleGit] = true
	if err := session.SetRules(selection); err != nil {
		t.Fatal(err)
	}
	selection[config.RuleGit] = false
	if !session.Config.Rules[config.RuleGit] || cfg.Rules[config.RuleGit] {
		t.Fatal("rule selections share mutable state")
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(false)
	if err != nil || !loaded.Rules[config.RuleTaskGroups] || loaded.Rules[config.RuleReview] {
		t.Fatalf("selection not persisted: %+v, %v", loaded, err)
	}
}

func TestSessionPreservesReviewRoleModels(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Models.ReviewRoles["PM"] = map[string]string{"model": "role-model", "effort": "xhigh"}
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.Config.Models.ReviewRoles["PM"]; got["model"] != "role-model" || got["effort"] != "xhigh" {
		t.Fatalf("role model lost while opening session: %+v", got)
	}
	session.Config.Models.ReviewRoles["PM"]["model"] = "changed"
	if cfg.Models.ReviewRoles["PM"]["model"] != "role-model" {
		t.Fatal("session shares review role model map with source config")
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Models.ReviewRoles["PM"]["model"] != "changed" || loaded.Models.ReviewRoles["PM"]["effort"] != "xhigh" {
		t.Fatalf("role model lost while saving session: %+v", loaded.Models.ReviewRoles["PM"])
	}
}
