package menu

import "github.com/dualface/kander/internal/config"

// setReviewRoleSelection writes both values with their owner. Project edits also
// pin the reviewer so later Global changes cannot reinterpret these overrides.
func (s *Session) setReviewRoleSelection(role, scale, reviewer, model, effort string) {
	values := map[string]any{
		scale + "_agent":  reviewer,
		scale + "_model":  model,
		scale + "_effort": effort,
	}
	if s.EditingOverlay() {
		// noteOverride retains invalid drafts and their errors for Save.
		candidate := config.CloneOverlay(s.overlayRaw)
		expandReviewersOverlay(candidate)
		config.OverlaySet(candidate, reviewer, "reviewers", scale, role)
		for key, value := range values {
			config.OverlaySet(candidate, value, "models", "review_roles", role, key)
		}
		// Publish the coupled edit through the session's draft-aware path.
		s.noteOverride([]string{"reviewers"}, candidate["reviewers"])
		s.noteOverride([]string{"models"}, candidate["models"])
		return
	}
	entry := s.Config.Models.ReviewRoles[role]
	if entry == nil {
		entry = map[string]string{}
		s.Config.Models.ReviewRoles[role] = entry
	}
	for key, value := range values {
		entry[key] = value.(string)
		s.noteOverride([]string{"models", "review_roles", role, key}, value)
	}
}

func (s *Session) noteReviewModelOverride(field ModelField, value string) {
	scale := kanbanScaleForField(field.field)
	reviewer := config.ReviewerFor(s.Config, scale, field.Agent)
	model, effort := config.ReviewModelFor(s.Config, reviewer, field.Agent, scale)
	if field.field == scale+"_model" {
		model = value
	} else {
		effort = value
	}
	s.setReviewRoleSelection(field.Agent, scale, reviewer, model, effort)
}

// Restoring one reviewer also retires legacy shared project overrides for that
// scale. Materialize their other scale first so its effective selection survives.
func (s *Session) preserveOtherReviewScale(candidate map[string]any, role, restoredScale string) {
	if !config.OverlayHasReviewPath(candidate, "models", "review_roles", role, "model") &&
		!config.OverlayHasReviewPath(candidate, "models", "review_roles", role, "effort") {
		return
	}
	for _, scale := range config.TaskScales {
		if scale == restoredScale {
			continue
		}
		reviewer := config.ReviewerFor(s.Config, scale, role)
		model, effort := config.ReviewModelFor(s.Config, reviewer, role, scale)
		config.OverlaySet(candidate, reviewer, "reviewers", scale, role)
		config.OverlaySet(candidate, reviewer, "models", "review_roles", role, scale+"_agent")
		config.OverlaySet(candidate, model, "models", "review_roles", role, scale+"_model")
		config.OverlaySet(candidate, effort, "models", "review_roles", role, scale+"_effort")
	}
	config.OverlayDeleteReviewRoleModel(candidate, role, "model")
	config.OverlayDeleteReviewRoleModel(candidate, role, "effort")
}
