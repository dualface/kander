package tui

import (
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
					if field.FieldName() == "large_model" && (section == sectionExecution || field.Agent == "PM") {
						*binding.modelValues[i] = "old-agent-draft"
						break
					}
				}
				if section == sectionReview {
					*binding.reviewers[reviewerFocusKey("PM", "large")] = "grok"
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
					model, effort := config.ReviewModelFor(loaded, "grok", "PM", "large")
					if config.ReviewerFor(loaded, "large", "PM") != "grok" || model != "" || effort != loaded.Models.Review["grok"]["effort"] {
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
