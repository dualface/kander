package tui

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestOptionsAgentSwitchDoesNotReplayOldModelDraft(t *testing.T) {
	for _, project := range []bool{false, true} {
		for _, section := range []string{sectionExecution, sectionReview} {
			name := "global/" + section
			if project {
				name = "project/" + section
			}
			t.Run(name, func(t *testing.T) {
				_, panel := openPanel(t)
				_, path := attachTempOverlay(t, panel.session, config.ModeGlobal)
				if project {
					if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
						t.Fatal(err)
					}
				}
				pumpPanel(panel, panel.openSection(section))
				binding := panel.bind
				// An edit and selector change can be applied by the same update.
				for i, field := range binding.modelFields {
					if field.FieldName() == "large_model" && (section == sectionExecution || field.Agent == "PMQA") {
						*binding.modelValues[i] = "old-agent-draft"
						break
					}
				}
				if section == sectionReview {
					*binding.reviewers[reviewerFocusKey("PMQA", "large")] = "grok"
				} else {
					binding.large = "grok"
				}
				binding.apply(panel)
				if err := panel.persistNow(); err != nil {
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
				if section == sectionReview {
					model, effort := config.ReviewModelFor(loaded, "grok", "PMQA", "large")
					if config.ReviewerFor(loaded, "large", "PMQA") != "grok" || model != "" || effort != loaded.Models.Review["grok"]["effort"] {
						t.Fatalf("wrong reviewer selection: %q/%q", model, effort)
					}
				} else {
					if loaded.KanbanAgents["large"] != "grok" || loaded.Models.Kanban["grok"]["large_model"] != "" {
						t.Fatalf("wrong execution selection: %+v", loaded.Models.Kanban["grok"])
					}
				}
			})
		}
	}
}

func TestReviewModelEditRefreshesCoupledInheritance(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		name := "inherited-reviewer"
		if pinned {
			name = "pinned-reviewer"
		}
		t.Run(name, func(t *testing.T) {
			_, panel := openPanel(t)
			dir, path := attachTempOverlay(t, panel.session, config.ModeGlobal)
			overlay := map[string]any{}
			config.OverlaySet(overlay, "existing-model", "models", "review_roles", "PMQA", "large_model")
			if pinned {
				config.OverlaySet(overlay, "codex", "reviewers", "large", "PMQA")
			}
			if err := panel.session.AttachOverlay(config.ModeGlobal, config.OverlayLocation{ProjectRoot: dir, Path: path}, overlay); err != nil {
				t.Fatal(err)
			}
			if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
				t.Fatal(err)
			}
			pumpPanel(panel, panel.openSection(sectionReview))
			key := ""
			for i, field := range panel.bind.modelFields {
				if field.Agent == "PMQA" && field.FieldName() == "large_model" {
					*panel.bind.modelValues[i] = "edited-model"
					key = modelFocusKey(field)
					break
				}
			}
			panel.bind.apply(panel)
			if key == "" || panel.rebuildFocus != key {
				t.Fatal("coupled override did not request rebuild at the edited model")
			}
			pumpPanel(panel, panel.rebuildSection())
			if !panel.session.FieldOverridden("reviewers", "large", "PMQA") {
				t.Fatal("reviewer remains inherited")
			}
			for i, field := range panel.bind.modelFields {
				if field.Agent == "PMQA" && field.FieldName() == "large_effort" && strings.HasPrefix(*panel.bind.modelValues[i], "(") {
					t.Fatal("paired effort still shows an inherited marker")
				}
			}
		})
	}
}
