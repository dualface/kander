package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeReviewersFlatAndScaled(t *testing.T) {
	flat := map[string]any{"QA": "claude", "Security": "codex"}
	normalized, err := NormalizeReviewers(flat)
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		roles, ok := normalized[scale].(map[string]any)
		if !ok || roles["QA"] != "claude" || roles["Security"] != "codex" {
			t.Fatalf("%s=%v", scale, normalized[scale])
		}
	}

	scaled := map[string]any{
		"large": map[string]any{"QA": "claude", "Security": "codex"},
		"small": map[string]any{"QA": "codex", "Security": "claude"},
	}
	again, err := NormalizeReviewers(scaled)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again["large"], scaled["large"]) || !reflect.DeepEqual(again["small"], scaled["small"]) {
		t.Fatalf("scaled changed: %#v", again)
	}

	if _, err := NormalizeReviewers(map[string]any{"large": map[string]any{}, "QA": "codex"}); err == nil {
		t.Fatal("mixed keys must fail")
	}
}

func TestValidateReviewersRewritesFlatOnLoad(t *testing.T) {
	setupHome(t)
	raw := minimalPayload(map[string]any{
		"reviewers": map[string]any{
			"QA": "claude", "Security": "codex"},
	})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ValidateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if ReviewerFor(cfg, "large", "QA") != "claude" || ReviewerFor(cfg, "small", "QA") != "claude" {
		t.Fatalf("flat reviewers not expanded: %#v", cfg.Reviewers)
	}
	if ReviewerFor(cfg, "large", "Security") != "codex" {
		t.Fatalf("Security=%s", ReviewerFor(cfg, "large", "Security"))
	}
}

func TestReviewModelForPrefersScaleKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models.ReviewRoles["QA"] = map[string]string{
		"model": "shared", "effort": "low",
		"large_model": "large-only", "large_effort": "high",
	}
	model, effort := ReviewModelFor(cfg, "codex", "QA", "large")
	if model != "large-only" || effort != "high" {
		t.Fatalf("large=%s/%s", model, effort)
	}
	model, effort = ReviewModelFor(cfg, "codex", "QA", "small")
	if model != "shared" || effort != "low" {
		t.Fatalf("small fallback=%s/%s", model, effort)
	}
}
