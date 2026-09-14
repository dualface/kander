package menu

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestSessionLegacyReviewHintGlobalAndProject(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := config.DocumentFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	scope["reviewers"] = map[string]any{
		"large": map[string]any{"CSA": "cursor", "Hacker": "codex", "QA": "claude", "Security": "claude"},
		"small": map[string]any{"QA": "claude", "Security": "claude"},
	}
	scope["review_stages"] = map[string]any{
		"large": map[string]any{"QA": "required", "Security": "auto"},
		"small": map[string]any{"PM": "auto", "QA": "auto", "Security": "skip"},
	}
	session.SeedScopeRawForTest(scope)
	keys := session.LegacyReviewKeys()
	for _, want := range []string{"reviewers.large.CSA", "reviewers.large.Hacker", "review_stages.small.PM"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("global keys=%v missing %s", keys, want)
		}
	}
	hint := session.LegacyReviewHint()
	if !strings.Contains(hint, "reviewers.large.CSA") || !strings.Contains(strings.ToLower(hint), "doctor") {
		t.Fatalf("global hint=%q", hint)
	}

	overlay := map[string]any{
		"reviewers": map[string]any{
			"large": map[string]any{"CSA": "grok"},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, config.OverlayFilename)
	if err := session.AttachOverlay(config.ModeGlobal, config.OverlayLocation{ProjectRoot: dir, Path: path}, overlay); err != nil {
		t.Fatal(err)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	projectKeys := session.LegacyReviewKeys()
	if !slices.Contains(projectKeys, "reviewers.large.CSA") {
		t.Fatalf("project keys=%v", projectKeys)
	}
	if session.Config.Reviewers["large"]["Security"] != "grok" {
		t.Fatalf("mapped reviewer=%v", session.Config.Reviewers["large"])
	}
	projectHint := session.LegacyReviewHint()
	if !strings.Contains(projectHint, "reviewers.large.CSA") {
		t.Fatalf("project hint=%q", projectHint)
	}
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	globalOnly := session.LegacyReviewKeys()
	if !slices.Contains(globalOnly, "review_stages.small.PM") {
		t.Fatalf("scope keys lost after tab switch: %v", globalOnly)
	}
}

func TestSessionLegacyReviewHintEmptyWithoutLegacyKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := config.DocumentFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.SeedScopeRawForTest(raw)
	if keys := session.LegacyReviewKeys(); len(keys) != 0 {
		t.Fatalf("keys=%v", keys)
	}
	if hint := session.LegacyReviewHint(); hint != "" {
		t.Fatalf("hint=%q", hint)
	}
}
