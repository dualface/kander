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
			cfg.Models.ReviewRoles["QA"] = map[string]string{
				"model": "legacy-model", "effort": "legacy-effort", "large_agent": tc.owner,
			}
			model, effort := ReviewModelFor(cfg, tc.selected, "QA", "large")
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
			cfg.Models.ReviewRoles["QA"] = map[string]string{
				"model": "old-shared", "effort": "old-effort",
				"large_model": "old-large", "large_effort": "old-large-effort",
			}
			scope, err := DocumentFromConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			overlay := map[string]any{}
			OverlaySet(overlay, "grok", "reviewers", "large", "QA")
			if tc.fields != nil {
				OverlaySet(overlay, tc.fields, "models", "review_roles", "QA")
			}
			merged, err := MergeOverlayOnRaw(scope, overlay)
			if err != nil {
				t.Fatal(err)
			}
			model, effort := ReviewModelFor(merged, "grok", "QA", "large")
			if model != tc.wantModel || effort != tc.wantEffort {
				t.Fatalf("got %q/%q, want %q/%q", model, effort, tc.wantModel, tc.wantEffort)
			}
			if tc.name != "legacy shared project model" {
				model, effort = ReviewModelFor(merged, "codex", "QA", "small")
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
			cfg.Models.ReviewRoles["QA"]["large_agent"] = owner
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
			if got.Models.ReviewRoles["QA"]["large_agent"] != owner {
				t.Fatal("binding lost")
			}
		})
	}
}

func TestBoundProjectFieldInheritsCompatibleLegacyGlobal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models.ReviewRoles["QA"]["model"] = "global-custom"
	overlay := map[string]any{}
	OverlaySet(overlay, "codex", "models", "review_roles", "QA", "large_agent")
	OverlaySet(overlay, "low", "models", "review_roles", "QA", "large_effort")
	merged, err := ApplyOverlay(cfg, overlay)
	if err != nil {
		t.Fatal(err)
	}
	model, effort := ReviewModelFor(merged, "codex", "QA", "large")
	if model != "global-custom" || effort != "low" {
		t.Fatalf("compatible inheritance lost: %s/%s", model, effort)
	}
}

func TestLegacyProjectSharedValuesSurviveGlobalBinding(t *testing.T) {
	for _, field := range []string{"model", "effort"} {
		for _, global := range []string{"", "global-specific"} {
			t.Run(field+"/"+global, func(t *testing.T) {
				cfg := DefaultConfig()
				cfg.Reviewers["large"]["QA"] = "grok"
				cfg.Models.ReviewRoles["QA"]["large_"+field] = global
				overlay := map[string]any{}
				OverlaySet(overlay, "project-shared", "models", "review_roles", "QA", field)
				before, err := ApplyOverlay(cfg, overlay)
				if err != nil {
					t.Fatal(err)
				}
				wantModel, wantEffort := ReviewModelFor(before, "grok", "QA", "large")
				cfg.Models.ReviewRoles["QA"]["large_agent"] = "grok"
				after, err := ApplyOverlay(cfg, overlay)
				if err != nil {
					t.Fatal(err)
				}
				model, effort := ReviewModelFor(after, "grok", "QA", "large")
				if model != wantModel || effort != wantEffort {
					t.Fatalf("binding changed %s/%s to %s/%s", wantModel, wantEffort, model, effort)
				}
			})
		}
	}
}
