package config

import (
	"encoding/json"
	"testing"
)

func TestReviewModelBindingResolution(t *testing.T) {
	for _, tc := range []struct {
		name, owner, selected, model, effort string
	}{
		{"legacy configured reviewer", "", "codex", "legacy-model", "legacy-effort"},
		{"legacy explicit different reviewer", "", "grok", "", "high"},
		{"bound empty model", "grok", "grok", "", "high"},
		{"bound different reviewer", "codex", "grok", "", "high"},
		{"bound without effort", "cursor", "cursor", "cursor-default", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Models.Review["cursor"]["model"] = "cursor-default"
			cfg.Models.ReviewRoles["PM"] = map[string]string{
				"model": "legacy-model", "effort": "legacy-effort", "large_agent": tc.owner,
			}
			model, effort := ReviewModelFor(cfg, tc.selected, "PM", "large")
			if model != tc.model || effort != tc.effort {
				t.Fatalf("got %q/%q, want %q/%q", model, effort, tc.model, tc.effort)
			}
		})
	}
}

func TestReviewModelOverlayOwnership(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		fields                map[string]any
		wantModel, wantEffort string
	}{
		{"reviewer only", nil, "", "high"},
		{"bound empty", map[string]any{"large_agent": "grok", "large_model": "", "large_effort": ""}, "", "high"},
		{"restored model", map[string]any{"large_agent": "grok", "large_effort": "low"}, "", "low"},
		{"legacy project model", map[string]any{"large_model": "project-model"}, "project-model", "high"},
		{"legacy shared project model", map[string]any{"model": "project-shared"}, "project-shared", "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Models.ReviewRoles["PM"] = map[string]string{
				"model": "old-shared", "effort": "old-effort",
				"large_model": "old-large", "large_effort": "old-large-effort",
			}
			scope, err := DocumentFromConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			overlay := map[string]any{}
			OverlaySet(overlay, "grok", "reviewers", "large", "PM")
			if tc.fields != nil {
				OverlaySet(overlay, tc.fields, "models", "review_roles", "PM")
			}
			merged, err := MergeOverlayOnRaw(scope, overlay)
			if err != nil {
				t.Fatal(err)
			}
			model, effort := ReviewModelFor(merged, "grok", "PM", "large")
			if model != tc.wantModel || effort != tc.wantEffort {
				t.Fatalf("got %q/%q, want %q/%q", model, effort, tc.wantModel, tc.wantEffort)
			}
			if tc.name != "legacy shared project model" {
				model, effort = ReviewModelFor(merged, "codex", "PM", "small")
				if model != "old-shared" || effort != "old-effort" {
					t.Fatalf("other scale changed: %s/%s", model, effort)
				}
			}
		})
	}
}

func TestReviewModelBindingValidationAndRoundTrip(t *testing.T) {
	for _, owner := range []string{"grok", "custom-reviewer", "bad name", "bad\nname"} {
		t.Run(owner, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Models.ReviewRoles["PM"]["large_agent"] = owner
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ValidateJSON(data)
			if !ValidAgentName(owner) {
				if err == nil {
					t.Fatal("invalid binding accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Models.ReviewRoles["PM"]["large_agent"] != owner {
				t.Fatal("binding lost")
			}
		})
	}
}

func TestBoundProjectFieldInheritsCompatibleLegacyGlobal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models.ReviewRoles["PM"]["model"] = "global-custom"
	overlay := map[string]any{}
	OverlaySet(overlay, "codex", "models", "review_roles", "PM", "large_agent")
	OverlaySet(overlay, "low", "models", "review_roles", "PM", "large_effort")
	merged, err := ApplyOverlay(cfg, overlay)
	if err != nil {
		t.Fatal(err)
	}
	model, effort := ReviewModelFor(merged, "codex", "PM", "large")
	if model != "global-custom" || effort != "low" {
		t.Fatalf("compatible inheritance lost: %s/%s", model, effort)
	}
}
