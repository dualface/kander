package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuleDefaultsMigrationAndSave(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	missing, err := Load(true)
	if err != nil || !reflect.DeepEqual(missing.Rules, DefaultRules(true)) {
		t.Fatalf("missing config: %+v, %v", missing, err)
	}
	for _, tc := range []struct {
		name string
		raw  map[string]any
		want Rules
	}{
		{"legacy", minimalPayload(nil), DefaultRules(true)},
		{"empty", minimalPayload(map[string]any{"rules": map[string]any{}}), DefaultRules(false)},
		{"partial", minimalPayload(map[string]any{"rules": map[string]any{"review": true}}), func() Rules { r := DefaultRules(false); r[RuleReview] = true; return r }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.raw["welcome_complete"] = false
			data, err := json.Marshal(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(false)
			if err != nil {
				t.Fatal(err)
			}
			effective, err := Effective(loaded)
			if err != nil || !reflect.DeepEqual(effective.Rules, tc.want) {
				t.Fatalf("rules depend on welcome: %+v, %v", effective, err)
			}
			fixed, _, err := Repair(nil)
			if err != nil || !reflect.DeepEqual(fixed.Rules, tc.want) {
				t.Fatalf("doctor changed rules: %+v, %v", fixed, err)
			}
			if _, hasLanguage := tc.raw["language"]; !hasLanguage && fixed.Language != "en" {
				t.Fatal("rule migration changed doctor's missing-language default")
			}
			if _, err := Save(fixed); err != nil {
				t.Fatal(err)
			}
			reloaded, err := Load(false)
			if err != nil || !reflect.DeepEqual(reloaded.Rules, tc.want) {
				t.Fatalf("save changed rules: %+v, %v", reloaded, err)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var explicit map[string]any
			if err := json.Unmarshal(saved, &explicit); err != nil {
				t.Fatal(err)
			}
			if fields, ok := explicit["rules"].(map[string]any); !ok || len(fields) != len(RuleModules) {
				t.Fatal("save must make every rule selection explicit")
			}
		})
	}
}

func TestRulesRejectInvalidSelectionsWithoutOverwriting(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	cfg := DefaultConfig()
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []any{nil, true, map[string]any{"git": "true"}, map[string]any{"unknown": true}, map[string]any{"task_groups": true}} {
		if _, err := Validate(minimalPayload(map[string]any{"rules": raw})); err == nil {
			t.Fatalf("invalid rules accepted: %#v", raw)
		}
	}
	cfg.Rules[RuleGit] = false
	if _, err := Save(cfg); err == nil {
		t.Fatal("invalid dependency saved")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("invalid save changed file: %v", err)
	}
	if err := cfg.Rules.CheckTaskGroup("group-1"); err == nil {
		t.Fatal("invalid group dependency accepted")
	}
	cfg.Rules[RuleGit] = true
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Rules.CheckTaskGroup("group-1"); err != nil {
		t.Fatal(err)
	}
	if err := DefaultRules(false).CheckTaskGroup(""); err != nil {
		t.Fatal("single card requires workflow rules")
	}
}

func TestDoctorRepairsOnlyInvalidRuleFields(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	raw := minimalPayload(map[string]any{"rules": map[string]any{
		"git": false, "task_groups": true, "review": true, "code": "bad", "retired": true,
	}})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Rules[RuleReview] || cfg.Rules[RuleGit] || cfg.Rules[RuleTaskGroups] || cfg.Rules[RuleCode] {
		t.Fatalf("valid off/on choices lost: %+v", cfg.Rules)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || string(backup) != string(data) {
		t.Fatal("original config backup missing")
	}
}
