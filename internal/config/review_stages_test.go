package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateReviewStagesScaledAndFlat(t *testing.T) {
	setupHome(t)
	scaled := map[string]any{
		"large": map[string]any{"PM": "required", "QA": "auto"},
		"small": map[string]any{"PM": "skip", "CSA": "skip", "Hacker": "skip", "QA": "auto"},
	}
	validated, err := Validate(minimalPayload(map[string]any{"review_stages": scaled}))
	if err != nil {
		t.Fatal(err)
	}
	if validated.ReviewStages["large"]["PM"] != "required" || validated.ReviewStages["large"]["CSA"] != "auto" {
		t.Fatalf("large=%v", validated.ReviewStages["large"])
	}
	if validated.ReviewStages["small"]["PM"] != "skip" || validated.ReviewStages["small"]["QA"] != "auto" {
		t.Fatalf("small=%v", validated.ReviewStages["small"])
	}

	flat := map[string]any{"PM": "required", "CSA": "skip"}
	validated, err = Validate(minimalPayload(map[string]any{"review_stages": flat}))
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		if validated.ReviewStages[scale]["PM"] != "required" || validated.ReviewStages[scale]["CSA"] != "skip" {
			t.Fatalf("%s=%v", scale, validated.ReviewStages[scale])
		}
		if validated.ReviewStages[scale]["QA"] != "auto" || validated.ReviewStages[scale]["Hacker"] != "auto" {
			t.Fatalf("missing roles on %s: %v", scale, validated.ReviewStages[scale])
		}
	}
}

func TestValidateReviewStagesRejectsUnknownAndMixed(t *testing.T) {
	setupHome(t)
	cases := []struct {
		name     string
		stages   any
		fragment string
	}{
		{"unknown scale", map[string]any{"medium": map[string]any{"PM": "auto"}}, "未知档名"},
		{"unknown role flat", map[string]any{"Owner": "auto"}, "未知角色"},
		{"unknown role scaled", map[string]any{"large": map[string]any{"Owner": "auto"}}, "未知角色"},
		{"invalid mode", map[string]any{"PM": "always"}, "review_stages.large.PM"},
		{"mixed", map[string]any{"large": map[string]any{"PM": "auto"}, "PM": "auto"}, "PM, large"},
		{"not object", "auto", "必须是 JSON object"},
		{"scale not object", map[string]any{"large": "auto"}, "review_stages.large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Validate(minimalPayload(map[string]any{"review_stages": tc.stages}))
			if err == nil || !strings.Contains(err.Error(), tc.fragment) {
				t.Fatalf("err=%v want %s", err, tc.fragment)
			}
		})
	}
}

func TestValidateReviewStagesFillsMissingWithAuto(t *testing.T) {
	setupHome(t)
	missingSection, err := Validate(minimalPayload(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		for _, role := range ReviewRoles {
			if missingSection.ReviewStages[scale][role] != "auto" {
				t.Fatalf("missing section %s.%s=%s", scale, role, missingSection.ReviewStages[scale][role])
			}
		}
	}

	oneScale, err := Validate(minimalPayload(map[string]any{
		"review_stages": map[string]any{"large": map[string]any{"PM": "required"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if oneScale.ReviewStages["large"]["PM"] != "required" {
		t.Fatal(oneScale.ReviewStages)
	}
	if oneScale.ReviewStages["small"]["PM"] != "auto" {
		t.Fatalf("missing scale must default to auto, got %v", oneScale.ReviewStages["small"])
	}

	oneRole, err := Validate(minimalPayload(map[string]any{
		"review_stages": map[string]any{
			"large": map[string]any{"PM": "skip"},
			"small": map[string]any{"QA": "required"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if oneRole.ReviewStages["large"]["QA"] != "auto" || oneRole.ReviewStages["small"]["PM"] != "auto" {
		t.Fatal(oneRole.ReviewStages)
	}
}

func TestNormalizeReviewStages(t *testing.T) {
	setupHome(t)
	flat, err := NormalizeReviewStages(map[string]any{"PM": "required", "QA": "skip"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		roles, ok := flat[scale].(map[string]any)
		if !ok || roles["PM"] != "required" || roles["QA"] != "skip" {
			t.Fatalf("%s=%v", scale, flat[scale])
		}
	}

	scaledIn := map[string]any{"large": map[string]any{"PM": "required"}, "small": map[string]any{"QA": "auto"}}
	scaled, err := NormalizeReviewStages(scaledIn)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scaled["large"], scaledIn["large"]) || !reflect.DeepEqual(scaled["small"], scaledIn["small"]) {
		t.Fatalf("%v", scaled)
	}
	scaledIn["large"].(map[string]any)["PM"] = "skip"
	if scaled["large"].(map[string]any)["PM"] != "required" {
		t.Fatal("NormalizeReviewStages must copy the raw object")
	}

	_, err = NormalizeReviewStages(map[string]any{"large": map[string]any{"PM": "auto"}, "PM": "skip"})
	if err == nil || !strings.Contains(err.Error(), "PM, large") {
		t.Fatalf("mixed: %v", err)
	}
	_, err = NormalizeReviewStages("auto")
	if err == nil || !strings.Contains(err.Error(), "必须是 JSON object") {
		t.Fatalf("not object: %v", err)
	}
}

func TestReviewStageFor(t *testing.T) {
	setupHome(t)
	cfg := DefaultConfig()
	cfg.ReviewStages["large"]["PM"] = "required"
	cfg.ReviewStages["small"]["QA"] = "skip"
	got, err := ReviewStageFor(cfg, "large", "PM")
	if err != nil || got != "required" {
		t.Fatal(got, err)
	}
	got, err = ReviewStageFor(cfg, "small", "QA")
	if err != nil || got != "skip" {
		t.Fatal(got, err)
	}
	got, err = ReviewStageFor(cfg, "small", "PM")
	if err != nil || got != "auto" {
		t.Fatal(got, err)
	}
	if _, err := ReviewStageFor(cfg, "medium", "PM"); !IsError(err) {
		t.Fatalf("unknown scale: %v", err)
	}
	if _, err := ReviewStageFor(cfg, "large", "Owner"); !IsError(err) {
		t.Fatalf("unknown role: %v", err)
	}
	if _, err := ReviewStageFor(nil, "large", "PM"); !IsError(err) {
		t.Fatalf("nil config: %v", err)
	}
}

func TestSaveRewritesFlatReviewStages(t *testing.T) {
	root := setupHome(t)
	path := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, path)
	validated, err := Validate(minimalPayload(map[string]any{
		"review_stages": map[string]any{"PM": "required", "CSA": "skip", "Hacker": "skip", "QA": "auto"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(validated); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	stages, ok := raw["review_stages"].(map[string]any)
	if !ok {
		t.Fatalf("review_stages=%T", raw["review_stages"])
	}
	if _, flat := stages["PM"]; flat {
		t.Fatalf("saved flat review_stages: %v", stages)
	}
	large, _ := stages["large"].(map[string]any)
	small, _ := stages["small"].(map[string]any)
	if !reflect.DeepEqual(large, small) {
		t.Fatalf("large=%v small=%v", large, small)
	}
	if large["PM"] != "required" || large["CSA"] != "skip" {
		t.Fatalf("saved roles=%v", large)
	}
}

func TestFormatReviewStagesSummaryFoldsIdenticalScales(t *testing.T) {
	setupHome(t)
	same := DefaultReviewStages()
	same["large"]["PM"] = "required"
	same["small"]["PM"] = "required"
	folded := FormatReviewStagesSummary(same)
	if len(folded) != 1 || !strings.Contains(folded[0], "PM=required") {
		t.Fatalf("%v", folded)
	}
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.ReviewStages = same
	lines, err := FormatConfigLines(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if strings.Count(joined, Text("config.review_stages")) != 1 {
		t.Fatalf("identical scales must fold to one line:\n%s", joined)
	}

	different := DefaultReviewStages()
	different["large"]["PM"] = "required"
	different["small"]["PM"] = "skip"
	split := FormatReviewStagesSummary(different)
	if len(split) != 2 {
		t.Fatalf("%v", split)
	}
	cfg.ReviewStages = different
	lines, err = FormatConfigLines(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(lines, "\n")
	if strings.Count(joined, Text("config.review_stages")) != 2 {
		t.Fatalf("different scales must use two lines:\n%s", joined)
	}
}
