package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReviewRolesArePMQAAndSecurityWithAutoDefaults(t *testing.T) {
	setupHome(t)
	if !reflect.DeepEqual(ReviewRoles, []string{"PMQA", "Security"}) {
		t.Fatalf("ReviewRoles=%v", ReviewRoles)
	}
	cfg, err := Validate(minimalPayload(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		if len(cfg.ReviewStages[scale]) != 2 {
			t.Fatalf("%s stages=%v", scale, cfg.ReviewStages[scale])
		}
		for _, role := range ReviewRoles {
			got, err := ReviewStageFor(cfg, scale, role)
			if err != nil || got != "auto" {
				t.Fatalf("%s.%s=%s %v", scale, role, got, err)
			}
			if _, ok := cfg.Reviewers[scale][role]; !ok {
				t.Fatalf("missing reviewer %s.%s", scale, role)
			}
			if _, ok := cfg.Models.ReviewRoles[role]; !ok {
				t.Fatalf("missing models.review_roles.%s", role)
			}
		}
		for _, legacy := range []string{"PM", "QA", "CSA", "Hacker"} {
			if _, ok := cfg.ReviewStages[scale][legacy]; ok {
				t.Fatalf("legacy role materialized: %s.%s", scale, legacy)
			}
		}
	}
}

func TestSavedSixKeyConfigFoldsIntegratedSkipToAuto(t *testing.T) {
	setupHome(t)
	six := map[string]any{}
	for _, role := range []string{"PM", "QA", "CSA", "Hacker"} {
		six[role] = "auto"
	}
	six["PMQA"] = "skip"
	six["Security"] = "skip"
	cfg, err := Validate(minimalPayload(map[string]any{
		"review_stages": map[string]any{"large": cloneRawObject(six), "small": cloneRawObject(six)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		if cfg.ReviewStages[scale]["PMQA"] != "auto" || cfg.ReviewStages[scale]["Security"] != "auto" {
			t.Fatalf("%s folded to skip: %v", scale, cfg.ReviewStages[scale])
		}
	}
}

func TestLegacyRoleKeysFoldByFixedRulesAndSaveWritesTwoRoles(t *testing.T) {
	root := setupHome(t)
	t.Setenv(EnvConfig, filepath.Join(root, "config.json"))
	cfg, err := Validate(minimalPayload(map[string]any{
		"review_stages": map[string]any{
			"large": map[string]any{"PM": "required", "PMQA": "skip"},
			"small": map[string]any{"CSA": "auto", "Hacker": "skip", "Security": "skip"},
		},
		"reviewers": map[string]any{
			"large": map[string]any{"QA": "claude", "PMQA": defaultAgentName()},
			"small": map[string]any{"CSA": "cursor"},
		},
		"models": map[string]any{"review_roles": map[string]any{
			"CSA": map[string]any{
				"model": "csa-model", "effort": "high",
				"large_model": "csa-large", "small_model": "csa-small",
				"large_effort": "xhigh", "small_effort": "low",
				"large_agent": "cursor", "small_agent": "grok",
			},
			"Security": map[string]any{"model": ""},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReviewStages["large"]["PMQA"] != "required" {
		t.Fatalf("stages large=%v", cfg.ReviewStages["large"])
	}
	if cfg.ReviewStages["small"]["Security"] != "auto" {
		t.Fatalf("stages small=%v", cfg.ReviewStages["small"])
	}
	if ReviewerFor(cfg, "large", "PMQA") != "claude" {
		t.Fatalf("reviewers large=%v", cfg.Reviewers["large"])
	}
	if ReviewerFor(cfg, "small", "Security") != "cursor" {
		t.Fatalf("reviewers small=%v", cfg.Reviewers["small"])
	}
	entry := cfg.Models.ReviewRoles["Security"]
	want := map[string]string{
		"model": "csa-model", "effort": "high",
		"large_model": "csa-large", "small_model": "csa-small",
		"large_effort": "xhigh", "small_effort": "low",
		"large_agent": "cursor", "small_agent": "grok",
	}
	for field, value := range want {
		if entry[field] != value {
			t.Fatalf("models.%s=%q want %q in %+v", field, entry[field], value, entry)
		}
	}
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	assertSavedTwoRoles(t, raw)
}

func TestConflictingConstituentReviewersAndModelsUseSourceOrder(t *testing.T) {
	setupHome(t)
	cfg, err := Validate(minimalPayload(map[string]any{
		"reviewers": map[string]any{
			"PM": "codex", "QA": "claude",
			"CSA": "cursor", "Hacker": "grok",
		},
		"models": map[string]any{"review_roles": map[string]any{
			"PM": map[string]any{"model": "pm-model", "large_agent": "codex"},
			"QA": map[string]any{"model": "qa-model", "large_agent": "claude"},
			"CSA": map[string]any{"effort": "high"},
			"Hacker": map[string]any{"effort": "low"},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	// First non-default wins. defaultAgentName is codex, so PM:codex is skipped and QA:claude wins.
	if ReviewerFor(cfg, "large", "PMQA") != "claude" {
		t.Fatalf("PMQA reviewer=%s", ReviewerFor(cfg, "large", "PMQA"))
	}
	if ReviewerFor(cfg, "large", "Security") != "cursor" {
		t.Fatalf("Security reviewer=%s", ReviewerFor(cfg, "large", "Security"))
	}
	if cfg.Models.ReviewRoles["PMQA"]["model"] != "pm-model" || cfg.Models.ReviewRoles["PMQA"]["large_agent"] != "codex" {
		t.Fatalf("PMQA models=%v", cfg.Models.ReviewRoles["PMQA"])
	}
	if cfg.Models.ReviewRoles["Security"]["effort"] != "high" {
		t.Fatalf("Security models=%v", cfg.Models.ReviewRoles["Security"])
	}
	cfg, err = Validate(minimalPayload(map[string]any{
		"reviewers": map[string]any{"PM": "grok", "QA": "cursor"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if ReviewerFor(cfg, "small", "PMQA") != "grok" {
		t.Fatalf("first constituent should win: %s", ReviewerFor(cfg, "small", "PMQA"))
	}
}

func TestFlatLegacyReviewKeysFoldTheSameWay(t *testing.T) {
	setupHome(t)
	cfg, err := Validate(minimalPayload(map[string]any{
		"review_stages": map[string]any{"PM": "required", "QA": "skip", "CSA": "auto", "Hacker": "required"},
		"reviewers":     map[string]any{"PM": "claude", "QA": "codex", "CSA": "cursor", "Hacker": "codex"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		if cfg.ReviewStages[scale]["PMQA"] != "required" || cfg.ReviewStages[scale]["Security"] != "required" {
			t.Fatalf("%s stages=%v", scale, cfg.ReviewStages[scale])
		}
		if ReviewerFor(cfg, scale, "PMQA") != "claude" || ReviewerFor(cfg, scale, "Security") != "cursor" {
			t.Fatalf("%s reviewers=%v", scale, cfg.Reviewers[scale])
		}
	}
}

func TestOverlayLegacyRolesFoldHasOverrideAndSaveTwoKeys(t *testing.T) {
	root := setupHome(t)
	t.Setenv(EnvConfig, filepath.Join(root, "config.json"))
	scope, err := Validate(minimalPayload(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(scope); err != nil {
		t.Fatal(err)
	}
	overlay := map[string]any{
		"review_stages": map[string]any{"large": map[string]any{"PM": "required", "QA": "auto"}},
		"reviewers":     map[string]any{"large": map[string]any{"QA": "claude"}},
		"models":        map[string]any{"review_roles": map[string]any{"CSA": map[string]any{"model": "from-csa"}}},
	}
	if OverlayHasReviewPath(overlay, "review_stages", "large", "PMQA") != true {
		t.Fatal("PM overlay must count as PMQA override")
	}
	if OverlayHasReviewPath(overlay, "reviewers", "large", "PMQA") != true {
		t.Fatal("QA overlay must count as PMQA reviewer override")
	}
	if OverlayHasReviewPath(overlay, "models", "review_roles", "Security", "model") != true {
		t.Fatal("CSA model overlay must count as Security override")
	}
	merged, err := MergeOverlayOnRaw(mustDocument(t, scope), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ReviewStages["large"]["PMQA"] != "required" {
		t.Fatalf("merged stages=%v", merged.ReviewStages["large"])
	}
	if ReviewerFor(merged, "large", "PMQA") != "claude" {
		t.Fatalf("merged reviewers=%v", merged.Reviewers["large"])
	}
	if merged.Models.ReviewRoles["Security"]["model"] != "from-csa" {
		t.Fatalf("merged models=%v", merged.Models.ReviewRoles["Security"])
	}
	path := filepath.Join(root, OverlayFilename)
	if _, err := SaveOverlayIfUnchanged(path, overlay, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	assertSavedTwoRoles(t, saved)
}

func TestRepositoryStyleSixKeyOverlayLoads(t *testing.T) {
	setupHome(t)
	overlay := map[string]any{
		"review_stages": map[string]any{
			"large": map[string]any{
				"CSA": "skip", "Hacker": "skip", "PM": "skip",
				"PMQA": "required", "QA": "skip", "Security": "skip",
			},
			"small": map[string]any{
				"CSA": "skip", "Hacker": "skip", "PM": "skip",
				"PMQA": "required", "QA": "skip", "Security": "skip",
			},
		},
	}
	cfg, err := MergeOverlayOnRaw(minimalPayload(nil), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReviewStages["large"]["PMQA"] != "required" || cfg.ReviewStages["large"]["Security"] != "skip" {
		t.Fatalf("repo overlay fold=%v", cfg.ReviewStages["large"])
	}
}

func TestFormatConfigLinesListOnlyTwoRoles(t *testing.T) {
	setupHome(t)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	lines, err := FormatConfigLines(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, legacy := range []string{"CSA=", "Hacker="} {
		if strings.Contains(joined, legacy) {
			t.Fatalf("legacy role in summary: %s\n%s", legacy, joined)
		}
	}
	if !strings.Contains(joined, "PMQA=") || !strings.Contains(joined, "Security=") {
		t.Fatalf("missing current roles:\n%s", joined)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatal(err)
	}
	assertSavedTwoRoles(t, encoded)
}

func mustDocument(t *testing.T, cfg *Config) map[string]any {
	t.Helper()
	raw, err := DocumentFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertSavedTwoRoles(t *testing.T, raw map[string]any) {
	t.Helper()
	legacy := []string{"PM", "QA", "CSA", "Hacker"}
	if stages, ok := raw["review_stages"].(map[string]any); ok {
		assertNoLegacyRoleKeys(t, "review_stages", stages, legacy)
	}
	if reviewers, ok := raw["reviewers"].(map[string]any); ok {
		assertNoLegacyRoleKeys(t, "reviewers", reviewers, legacy)
	}
	if models, ok := raw["models"].(map[string]any); ok {
		if roles, ok := models["review_roles"].(map[string]any); ok {
			for _, name := range legacy {
				if _, ok := roles[name]; ok {
					t.Fatalf("models.review_roles still has %s: %v", name, roles)
				}
			}
		}
	}
}

func assertNoLegacyRoleKeys(t *testing.T, section string, obj map[string]any, legacy []string) {
	t.Helper()
	for key, value := range obj {
		if nested, ok := value.(map[string]any); ok {
			assertNoLegacyRoleKeys(t, section+"."+key, nested, legacy)
			continue
		}
		for _, name := range legacy {
			if key == name {
				t.Fatalf("%s still has %s: %v", section, name, obj)
			}
		}
	}
}
