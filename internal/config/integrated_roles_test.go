package config

import (
	"reflect"
	"testing"
)

func TestIntegratedRolesAreOptInAndPreserveOriginalConfiguration(t *testing.T) {
	setupHome(t)
	raw := minimalPayload(map[string]any{
		"reviewers":     map[string]any{"PM": "claude", "QA": "codex", "CSA": "cursor", "Hacker": "codex"},
		"review_stages": map[string]any{"PM": "required", "QA": "skip", "CSA": "auto", "Hacker": "required"},
		"models": map[string]any{"review_roles": map[string]any{
			"PM": map[string]any{"model": "original-pm"}, "CSA": map[string]any{"model": "original-csa"},
		}},
	})
	cfg, err := Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		for role, want := range map[string]string{"PM": "required", "QA": "skip", "CSA": "auto", "Hacker": "required", "PMQA": "skip", "Security": "skip"} {
			if got, err := ReviewStageFor(cfg, scale, role); err != nil || got != want {
				t.Fatalf("%s.%s=%s, want %s: %v", scale, role, got, want, err)
			}
		}
		if ReviewerFor(cfg, scale, "PM") != "claude" || ReviewerFor(cfg, scale, "CSA") != "cursor" {
			t.Fatal("original reviewers changed", cfg.Reviewers)
		}
	}
	if cfg.Models.ReviewRoles["PM"]["model"] != "original-pm" || cfg.Models.ReviewRoles["CSA"]["model"] != "original-csa" {
		t.Fatal("original models changed", cfg.Models.ReviewRoles)
	}
	before := cloneRawObject(raw)
	overlay := map[string]any{
		"reviewers":     map[string]any{"large": map[string]any{"PMQA": "claude", "Security": "cursor"}},
		"review_stages": map[string]any{"large": map[string]any{"PMQA": "required", "Security": "auto"}},
		"models":        map[string]any{"review_roles": map[string]any{"PMQA": map[string]any{"large_model": "integrated-model", "large_effort": "high"}}},
	}
	merged, err := MergeOverlayOnRaw(raw, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, before) || merged.ReviewStages["large"]["PM"] != "required" || merged.ReviewStages["large"]["Hacker"] != "required" {
		t.Fatal("integrated selection rewrote original policies")
	}
	if ReviewerFor(merged, "large", "PMQA") != "claude" || ReviewerFor(merged, "large", "Security") != "cursor" || merged.ReviewStages["large"]["PMQA"] != "required" || merged.ReviewStages["small"]["PMQA"] != "skip" {
		t.Fatal("integrated overlay lost role or scale", merged)
	}
	model, effort := ReviewModelFor(merged, "claude", "PMQA", "large")
	if model != "integrated-model" || effort != "high" {
		t.Fatalf("integrated model=%s/%s", model, effort)
	}
	// Runtime lookup also handles callers with sparse, already-decoded maps.
	merged.ReviewStages = map[string]map[string]string{}
	for role, want := range map[string]string{"PM": "auto", "PMQA": "skip", "Security": "skip"} {
		if got, err := ReviewStageFor(merged, "small", role); err != nil || got != want {
			t.Fatalf("sparse %s=%s: %v", role, got, err)
		}
	}
}
