package menu

import (
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestReviewerSwitchKeepsModelsWithAgent(t *testing.T) {
	for _, target := range []string{config.TargetScope, config.TargetOverlay} {
		for _, scale := range config.TaskScales {
			for _, next := range []string{"grok", "cursor", "claude"} {
				t.Run(target+"/"+scale+"/"+next, func(t *testing.T) {
					s, path := tempOverlaySession(t, config.ModeGlobal)
					s.Config.Models.ReviewRoles["PMQA"] = map[string]string{
						"model": "legacy-model", "effort": "legacy-effort",
						"large_model": "old-large", "small_model": "old-small",
					}
					if _, err := config.Save(s.Config); err != nil {
						t.Fatal(err)
					}
					stored, err := config.LoadScope(false)
					if err != nil {
						t.Fatal(err)
					}
					fresh, err := NewSessionForTest(stored)
					if err != nil {
						t.Fatal(err)
					}
					if err := fresh.AttachOverlay(config.ModeGlobal, s.OverlayLocation, nil); err != nil {
						t.Fatal(err)
					}
					s = fresh
					if err := s.SetTarget(target); err != nil {
						t.Fatal(err)
					}
					s.SetReviewer(scale, "PMQA", next)
					check := func(cfg *config.Config) {
						t.Helper()
						model, effort := config.ReviewModelFor(cfg, next, "PMQA", scale)
						want := cfg.Models.Review[next]
						if model != want["model"] || effort != want["effort"] {
							t.Fatalf("wrong selection: %s/%s", model, effort)
						}
						other := "small"
						if scale == other {
							other = "large"
						}
						model, effort = config.ReviewModelFor(cfg, "codex", "PMQA", other)
						if model != "old-"+other || effort != "legacy-effort" {
							t.Fatalf("other scale changed: %s/%s", model, effort)
						}
					}
					check(s.Config)
					// Rebuilding the form must not seed a legacy value back into the selection.
					fields := s.ReviewModelFieldsFor("PMQA", scale)
					if fields[0].Value() != s.Config.Models.Review[next]["model"] {
						t.Fatal("stale field")
					}
					if next == "cursor" && len(fields) != 1 {
						t.Fatal("cursor effort visible")
					}
					if _, err := s.Save(); err != nil {
						t.Fatal(err)
					}
					raw, err := config.LoadScopeDocument(false)
					if err != nil {
						t.Fatal(err)
					}
					overlay, err := config.ReadOverlayFile(path)
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := config.MergeOverlayOnRaw(raw, overlay)
					if err != nil {
						t.Fatal(err)
					}
					check(loaded)
					if target == config.TargetOverlay {
						if err := s.RestoreInherit("models", "review_roles", "PMQA", scale+"_model"); err != nil {
							t.Fatal(err)
						}
						check(s.Config)
						if err := s.RestoreInherit("reviewers", scale, "PMQA"); err != nil {
							t.Fatal(err)
						}
						model, _ := config.ReviewModelFor(s.Config, "codex", "PMQA", scale)
						if model != "old-"+scale {
							t.Fatalf("restore reviewer kept model %q", model)
						}
					}
				})
			}
		}
	}
}

func TestProjectReviewModelEditPinsReviewer(t *testing.T) {
	s, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := s.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	field := s.ReviewModelFieldsFor("PMQA", "large")[0]
	s.NoteModelOverride(field, "project-codex")
	if !s.FieldOverridden("reviewers", "large", "PMQA") {
		t.Fatal("reviewer not pinned")
	}
	if err := s.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	s.SetReviewer("large", "PMQA", "claude")
	if err := s.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if config.ReviewerFor(s.Config, "large", "PMQA") != "codex" {
		t.Fatal("project reviewer changed")
	}
	model, _ := config.ReviewModelFor(s.Config, "codex", "PMQA", "large")
	if model != "project-codex" {
		t.Fatalf("project model changed: %q", model)
	}
}

func TestRestoreReviewerRetiresLegacySharedProjectModel(t *testing.T) {
	s, _ := tempOverlaySession(t, config.ModeGlobal)
	overlay := map[string]any{}
	config.OverlaySet(overlay, "claude", "reviewers", "large", "PM")
	config.OverlaySet(overlay, "claude", "reviewers", "small", "PM")
	config.OverlaySet(overlay, "project-legacy", "models", "review_roles", "PM", "model")
	if err := s.AttachOverlay(config.ModeGlobal, s.OverlayLocation, overlay); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	s.SetReviewer("large", "PMQA", "grok")
	if err := s.RestoreInherit("reviewers", "large", "PMQA"); err != nil {
		t.Fatal(err)
	}
	model, _ := config.ReviewModelFor(s.Config, "codex", "PMQA", "large")
	if model != s.Config.Models.Review["codex"]["model"] {
		t.Fatalf("restored reviewer inherited legacy model: %s", model)
	}
	model, _ = config.ReviewModelFor(s.Config, "claude", "PMQA", "small")
	if model != "project-legacy" {
		t.Fatalf("other scale lost legacy model: %s", model)
	}
}

func TestReviewFieldsPreserveAnotherOwnersStoredValues(t *testing.T) {
	s, _ := tempOverlaySession(t, config.ModeGlobal)
	entry := s.Config.Models.ReviewRoles["PMQA"]
	entry["large_agent"], entry["large_model"], entry["large_effort"] = "claude", "claude-custom", "low"
	fields := s.ReviewModelFieldsFor("PMQA", "large")
	if fields[0].Value() != s.Config.Models.Review["codex"]["model"] {
		t.Fatal("field did not display selected reviewer's value")
	}
	if _, err := s.Save(); err != nil {
		t.Fatal(err)
	}
	stored, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	entry = stored.Models.ReviewRoles["PMQA"]
	if entry["large_agent"] != "claude" || entry["large_model"] != "claude-custom" || entry["large_effort"] != "low" {
		t.Fatalf("opening fields corrupted stored overrides: %+v", entry)
	}
	fields[0].Set("codex-custom")
	s.NoteModelOverride(fields[0], "codex-custom")
	model, _ := config.ReviewModelFor(s.Config, "codex", "PMQA", "large")
	if model != "codex-custom" || s.Config.Models.ReviewRoles["PMQA"]["large_agent"] != "codex" {
		t.Fatal("explicit edit did not bind the selected reviewer")
	}
}
