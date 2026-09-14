//go:build unix

package menu

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestDoctorRepairsOverlayEnabledIntegratedRoles(t *testing.T) {
	for _, role := range []string{"PMQA", "Security"} {
		for _, scale := range config.TaskScales {
			for _, stage := range []string{"auto", "required"} {
				t.Run(role+"/"+scale+"/"+stage, func(t *testing.T) {
					checkDoctorIntegratedOverlay(t, role, scale, stage, "valid", "auto", "")
				})
			}
		}
	}
}

func TestDoctorIntegratedOverlayRepairBoundaries(t *testing.T) {
	for _, scope := range []string{"missing", "broken"} {
		t.Run(scope, func(t *testing.T) {
			checkDoctorIntegratedOverlay(t, "PMQA", "large", "required", scope, "auto", "")
		})
	}
	t.Run("overlay-cannot-disable-scope-repair", func(t *testing.T) {
		checkDoctorIntegratedOverlay(t, "Security", "small", "skip", "valid", "auto", "")
	})
	t.Run("overlay-reviewer-remains-project-owned", func(t *testing.T) {
		checkDoctorIntegratedOverlay(t, "PMQA", "large", "auto", "valid", "auto", "grok")
	})
}

func TestDoctorIntegratedOverlayErrorsPreserveScopeRepair(t *testing.T) {
	for _, original := range []string{"broken JSON", `{"review_stages":{"large":{"PMQA":"invalid"}}}`} {
		t.Run(original, func(t *testing.T) {
			h := newHarness(t)
			h.fakeCommand("claude", "")
			project := filepath.Join(h.root, "project")
			if err := os.Mkdir(project, 0o755); err != nil {
				t.Fatal(err)
			}
			overlay := filepath.Join(project, config.OverlayFilename)
			if err := os.WriteFile(overlay, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, output := h.runIn(project, "doctor")
			if code != 1 {
				t.Fatalf("invalid overlay must remain unhealthy: %d %s", code, output)
			}
			cfg := readDoctorConfig(t, h)
			if cfg.KanbanAgent != "claude" || cfg.Reviewers["large"]["PMQA"] != "claude" {
				t.Fatalf("invalid overlay prevented scope repair: %+v", cfg)
			}
			after, err := os.ReadFile(overlay)
			if err != nil || string(after) != original {
				t.Fatalf("invalid overlay was rewritten: %s %v", after, err)
			}
		})
	}
}

func checkDoctorIntegratedOverlay(t *testing.T, role, scale, stage, scopeState, scopeStage, overlayReviewer string) {
	t.Helper()
	h := newHarness(t)
	h.fakeCommand("claude", "")
	if scopeState == "valid" {
		h.writeConfig(defaultPayload(map[string]any{
			"kanban_agent": "claude", "launcher": "foreground", "language": "en",
			"reviewers": map[string]any{"PM": "claude", "QA": "claude", "CSA": "claude", "Hacker": "claude"},
			"review_stages": map[string]any{
				"large": map[string]any{}, "small": map[string]any{},
			},
		}))
		if scopeStage != "skip" {
			cfg := readDoctorConfig(t, h)
			cfg.ReviewStages[scale][role] = scopeStage
			t.Setenv(config.EnvConfig, h.configPath)
			if _, err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
		}
	} else if scopeState == "broken" {
		if err := os.MkdirAll(filepath.Dir(h.configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(h.configPath, []byte("broken JSON"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	project := filepath.Join(h.root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"language":      "ja",
		"review_stages": map[string]any{scale: map[string]any{role: stage}},
		"models":        map[string]any{"review_roles": map[string]any{role: map[string]any{scale + "_model": "project-only"}}},
	}
	if overlayReviewer != "" {
		payload["reviewers"] = map[string]any{scale: map[string]any{role: overlayReviewer}}
	}
	overlay, original := writeOverlayFile(t, project, payload)
	code, _, output := h.runIn(project, "doctor")
	wantCode := 0
	if overlayReviewer != "" {
		wantCode = 1
	}
	if code != wantCode {
		t.Errorf("doctor=%d want %d: %s", code, wantCode, output)
	}
	after, err := os.ReadFile(overlay)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("overlay changed: %s, %v", after, err)
	}
	cfg := readDoctorConfig(t, h)
	if cfg.Reviewers[scale][role] != "claude" || cfg.ReviewStages[scale][role] != scopeStage || cfg.Language != "en" || cfg.Models.ReviewRoles[role][scale+"_model"] == "project-only" {
		t.Fatalf("scope repair ignored active role or absorbed overlay: %+v", cfg)
	}
	for _, otherScale := range config.TaskScales {
		if otherScale != scale && (cfg.Reviewers[otherScale][role] != "claude" || cfg.ReviewStages[otherScale][role] != "auto") {
			t.Fatalf("unrelated scale changed: %+v", cfg)
		}
	}
	if overlayReviewer != "" {
		code, text, output := h.runIn(project, "config", "--json")
		merged, err := config.ValidateJSON([]byte(text))
		if code != 0 || err != nil || merged.Reviewers[scale][role] != overlayReviewer {
			t.Fatalf("effective overlay reviewer changed: %s %v", output, err)
		}
	}
}
