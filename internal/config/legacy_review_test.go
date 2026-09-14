package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func legacyRolePayload() map[string]any {
	return minimalPayload(map[string]any{
		"reviewers":     map[string]any{"large": map[string]any{"CSA": "cursor", "Hacker": "codex", "PM": "claude"}},
		"review_stages": map[string]any{"large": map[string]any{"CSA": "required", "Hacker": "skip"}, "small": map[string]any{"PM": "auto"}},
		"models":        map[string]any{"review_roles": map[string]any{"CSA": map[string]any{"model": "security-model"}, "Hacker": map[string]any{"model": "unused-model"}, "PM": map[string]any{"model": "ignored-model"}}},
	})
}

func TestLegacyReviewConfigurationAndDiagnostics(t *testing.T) {
	setupHome(t)
	raw := legacyRolePayload()
	before, _ := json.Marshal(raw)
	cfg, err := Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Reviewers["large"]["Security"] != "cursor" || cfg.ReviewStages["large"]["Security"] != "required" || cfg.Models.ReviewRoles["Security"]["model"] != "security-model" {
		t.Fatalf("%+v", cfg)
	}
	keys := cfg.LegacyReviewKeys()
	if len(keys) != 9 || !slices.Contains(keys, "review_stages.small.PM") || !slices.Contains(keys, "models.review_roles.Hacker") {
		t.Fatal(keys)
	}
	keys[0] = "mutated"
	if cfg.LegacyReviewKeys()[0] == "mutated" {
		t.Fatal("diagnostics alias configuration")
	}
	after, _ := json.Marshal(raw)
	if string(before) != string(after) {
		t.Fatal("Validate mutated input")
	}
	encoded, _ := json.Marshal(cfg)
	for _, key := range []string{`"PM"`, `"CSA"`, `"Hacker"`, `legacyReviewKeys`} {
		if strings.Contains(string(encoded), key) {
			t.Fatalf("legacy key in normalized JSON: %s", key)
		}
	}
	normalized, err := ValidateJSON(encoded)
	if err != nil || len(normalized.LegacyReviewKeys()) != 0 {
		t.Fatalf("%v %v", normalized, err)
	}
	if !reflect.DeepEqual(ReviewRoles, []string{"QA", "Security"}) {
		t.Fatal(ReviewRoles)
	}
}

func TestLegacyReviewStagePolicyAndExplicitSecurityPrecedence(t *testing.T) {
	setupHome(t)
	for _, tc := range []struct{ csa, hacker, want string }{
		{"required", "skip", "required"}, {"skip", "required", "required"}, {"skip", "auto", "auto"}, {"auto", "skip", "auto"}, {"skip", "skip", "skip"},
	} {
		cfg, err := Validate(minimalPayload(map[string]any{"review_stages": map[string]any{"CSA": tc.csa, "Hacker": tc.hacker}}))
		if err != nil || cfg.ReviewStages["large"]["Security"] != tc.want {
			t.Fatalf("%+v %v %v", tc, cfg, err)
		}
	}
	raw := legacyRolePayload()
	raw["reviewers"].(map[string]any)["large"].(map[string]any)["Security"] = "claude"
	cfg, err := Validate(raw)
	if err != nil || cfg.Reviewers["large"]["Security"] != "claude" {
		t.Fatalf("%v %v", cfg, err)
	}
	cfg, err = Validate(minimalPayload(map[string]any{"reviewers": map[string]any{"Hacker": "claude"}, "models": map[string]any{"review_roles": map[string]any{"Hacker": map[string]any{"model": "fallback"}}}}))
	if err != nil || cfg.Reviewers["small"]["Security"] != "claude" || cfg.Models.ReviewRoles["Security"]["model"] != "fallback" {
		t.Fatalf("%v %v", cfg, err)
	}
}

func TestLegacyReviewOverlayPrecedenceAndRepair(t *testing.T) {
	root := setupHome(t)
	t.Chdir(root)
	path := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, path)
	raw := legacyRolePayload()
	data, _ := json.Marshal(raw)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	overlay := map[string]any{"reviewers": map[string]any{"large": map[string]any{"Hacker": "claude"}}, "review_stages": map[string]any{"large": map[string]any{"CSA": "skip", "Hacker": "skip"}}, "models": map[string]any{"review_roles": map[string]any{"Hacker": map[string]any{"model": "overlay-model"}}}}
	data, _ = json.Marshal(overlay)
	if err := os.WriteFile(filepath.Join(root, OverlayFilename), data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Reviewers["large"]["Security"] != "claude" || cfg.ReviewStages["large"]["Security"] != "skip" || cfg.Models.ReviewRoles["Security"]["model"] != "overlay-model" {
		t.Fatalf("%+v", cfg)
	}
	if !slices.Contains(cfg.LegacyReviewKeys(), "reviewers.large.Hacker") || !slices.Contains(cfg.LegacyReviewKeys(), "reviewers.large.PM") {
		t.Fatal(cfg.LegacyReviewKeys())
	}
	repaired, result, err := Repair(nil)
	if err != nil || !result.Changed || repaired.Reviewers["large"]["Security"] != "cursor" {
		t.Fatalf("%+v %+v %v", repaired, result, err)
	}
	scope, err := LoadScope(false)
	if err != nil || len(scope.LegacyReviewKeys()) != 0 {
		t.Fatalf("%v %v", scope, err)
	}
	unchanged, err := os.ReadFile(filepath.Join(root, OverlayFilename))
	if err != nil || string(unchanged) != string(data) {
		t.Fatal("scope repair changed project overlay", err)
	}
}

func TestLegacyReviewStageMergeDoesNotHideInvalidPolicies(t *testing.T) {
	setupHome(t)
	for _, roles := range []map[string]any{
		{"CSA": "invalid", "Hacker": "required"},
		{"CSA": "required", "Hacker": 42},
		{"Hacker": nil},
	} {
		if _, err := Validate(minimalPayload(map[string]any{"review_stages": roles})); err == nil {
			t.Fatal("invalid legacy policy hidden by merge", roles)
		}
	}
}
