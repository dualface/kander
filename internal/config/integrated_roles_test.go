package config

import (
	"reflect"
	"testing"
)

func TestLegacyFourRoleConfigFoldsWithoutUnknownRoleErrors(t *testing.T) {
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
		if got, err := ReviewStageFor(cfg, scale, "PMQA"); err != nil || got != "required" {
			t.Fatalf("%s.PMQA=%s %v", scale, got, err)
		}
		if got, err := ReviewStageFor(cfg, scale, "Security"); err != nil || got != "required" {
			t.Fatalf("%s.Security=%s %v", scale, got, err)
		}
		if ReviewerFor(cfg, scale, "PMQA") != "claude" || ReviewerFor(cfg, scale, "Security") != "cursor" {
			t.Fatal("folded reviewers", cfg.Reviewers)
		}
	}
	if cfg.Models.ReviewRoles["PMQA"]["model"] != "original-pm" || cfg.Models.ReviewRoles["Security"]["model"] != "original-csa" {
		t.Fatal("folded models", cfg.Models.ReviewRoles)
	}
	before := cloneRawObject(raw)
	overlay := map[string]any{
		"reviewers":     map[string]any{"large": map[string]any{"PMQA": "grok", "Security": "pi"}},
		"review_stages": map[string]any{"large": map[string]any{"PMQA": "auto", "Security": "skip"}},
		"models":        map[string]any{"review_roles": map[string]any{"PMQA": map[string]any{"large_model": "integrated-model", "large_effort": "high"}}},
	}
	merged, err := MergeOverlayOnRaw(raw, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Fatal("overlay merge mutated the scope document")
	}
	if ReviewerFor(merged, "large", "PMQA") != "grok" || ReviewerFor(merged, "large", "Security") != "pi" {
		t.Fatal("overlay reviewers", merged.Reviewers)
	}
	if merged.ReviewStages["large"]["PMQA"] != "auto" || merged.ReviewStages["large"]["Security"] != "skip" {
		t.Fatal("overlay stages", merged.ReviewStages["large"])
	}
	if merged.ReviewStages["small"]["PMQA"] != "required" {
		t.Fatal("small scale lost folded scope", merged.ReviewStages["small"])
	}
	model, effort := ReviewModelFor(merged, "grok", "PMQA", "large")
	if model != "integrated-model" || effort != "high" {
		t.Fatalf("integrated model=%s/%s", model, effort)
	}
}
