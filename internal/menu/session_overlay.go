package menu

import (
	"github.com/dualface/kander/internal/config"
)

// EditingOverlay reports whether the session is editing the project overlay.
func (s *Session) EditingOverlay() bool {
	return s != nil && s.Target == config.TargetOverlay
}

// AvailableTargets returns the Options tabs for the current install mode.
func (s *Session) AvailableTargets() []string {
	if s == nil {
		return config.OptionsTargets(config.ModeGlobal)
	}
	return config.OptionsTargets(s.InstallMode)
}

// HasUnsaved reports whether either tab still has unpublished edits.
func (s *Session) HasUnsaved() bool {
	return s != nil && (s.ScopeDirty || s.OverlayDirty)
}

// FieldOverridden reports overlay key presence for the current edit buffer.
func (s *Session) FieldOverridden(path ...string) bool {
	if s == nil || !s.EditingOverlay() {
		return false
	}
	if config.OverlayHas(s.overlayRaw, path...) {
		return true
	}
	if len(path) == 3 && path[0] == "review_stages" {
		return config.OverlayHas(s.overlayRaw, "review_stages", path[2])
	}
	if len(path) == 3 && path[0] == "reviewers" {
		return config.OverlayHas(s.overlayRaw, "reviewers", path[2])
	}
	return false
}

// FormatInherited renders an uncovered Project-tab value as "(inherited value)".
// Selects append it to the label; Inputs place it inside the field.
// Parentheses are always ASCII so CJK fullwidth （） cannot widen the chrome.
func (s *Session) FormatInherited(value string) string {
	if value == "" {
		value = config.Text("flow.cli_default")
	}
	return "(" + config.Text("tui.inherited_label") + " " + value + ")"
}

func (s *Session) initOverlayState() {
	s.Target = config.TargetScope
	s.InstallMode = config.ModeGlobal
	s.overlayRaw = map[string]any{}
	s.overlayExisting = map[string]any{}
	s.scopeConfig = s.Config
	if s.existing != nil {
		s.scopeExisting = config.Clone(s.existing)
	}
}

func (s *Session) loadOverlayContext() error {
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		return err
	}
	s.InstallMode = paths.Mode
	base, err := config.ConfigPath()
	if err != nil {
		return err
	}
	s.BasePath = base
	loc, err := config.ResolveOverlayLocation("")
	if err != nil {
		return err
	}
	s.OverlayLocation = loc
	if loc.Exists {
		raw, err := config.ReadOverlayFile(loc.Path)
		if err != nil {
			return err
		}
		s.overlayRaw = raw
		s.overlayExisting = config.CloneOverlay(raw)
	}
	if err := s.ensureScopeRaw(); err != nil {
		return err
	}
	if paths.Mode == config.ModeProject {
		return s.SetTarget(config.TargetOverlay)
	}
	return nil
}

// AttachOverlay installs a test overlay location without probing the real cwd.
func (s *Session) AttachOverlay(mode config.Mode, loc config.OverlayLocation, raw map[string]any) error {
	s.InstallMode = mode
	s.OverlayLocation = loc
	s.overlayRaw = config.CloneOverlay(raw)
	s.overlayExisting = config.CloneOverlay(raw)
	if err := s.ensureScopeRaw(); err != nil {
		return err
	}
	if mode == config.ModeProject {
		return s.SetTarget(config.TargetOverlay)
	}
	return nil
}

// PreferProjectTabIfPresent switches Options to the Project tab when a global
// install already has project overlay settings. Doctor and other callers keep
// the Global buffer until they opt in through the Options panel.
func (s *Session) PreferProjectTabIfPresent() error {
	if s == nil || s.InstallMode != config.ModeGlobal || len(s.AvailableTargets()) < 2 {
		return nil
	}
	if len(s.overlayRaw) == 0 || s.Target == config.TargetOverlay {
		return nil
	}
	return s.SetTarget(config.TargetOverlay)
}

// SetTarget switches the edit buffer. Unsaved edits on the other tab are kept.
func (s *Session) SetTarget(target string) error {
	if target == config.TargetOverlay {
		merged, err := s.previewOverlay()
		if err != nil {
			if !s.overlayDraft {
				return err
			}
			merged, err = s.previewOverlayDraft(s.overlayRaw)
			if err != nil {
				return err
			}
		} else {
			s.overlayDraft = false
		}
		if s.Target == config.TargetScope && s.Config != nil {
			s.scopeConfig = s.Config
		}
		s.Target = target
		s.Config = merged
		return nil
	}
	if s.scopeConfig == nil {
		s.scopeConfig = s.Config
	}
	s.Target = target
	s.Config = s.scopeConfig
	return nil
}

func (s *Session) previewOverlay() (*config.Config, error) {
	if err := s.ensureScopeRaw(); err != nil {
		return nil, err
	}
	return config.MergeOverlayOnRaw(s.scopeRaw, s.overlayRaw)
}

func (s *Session) ensureScopeRaw() error {
	if s.scopeRaw != nil {
		return nil
	}
	raw, err := config.LoadScopeDocument(true)
	if err == nil {
		s.scopeRaw = raw
		return nil
	}
	base := s.scopeBuffer()
	if base == nil {
		base = s.existing
	}
	encoded, encErr := config.DocumentFromConfig(base)
	if encErr != nil {
		return err
	}
	s.scopeRaw = encoded
	return nil
}

func (s *Session) rebuildOverlayConfig() error {
	merged, err := s.previewOverlay()
	if err != nil {
		return err
	}
	s.overlayDraft = false
	s.Config = merged
	return nil
}

func (s *Session) applyOverlayEdit(mutate func(map[string]any)) error {
	if err := s.ensureScopeRaw(); err != nil {
		return err
	}
	candidate := config.CloneOverlay(s.overlayRaw)
	mutate(candidate)
	merged, err := config.MergeOverlayOnRaw(s.scopeRaw, candidate)
	if err != nil {
		return err
	}
	s.overlayRaw = candidate
	s.OverlayDirty = true
	s.overlayDraft = false
	s.Config = merged
	return nil
}

func (s *Session) applyOverlaySet(path []string, value any) error {
	return s.applyOverlayEdit(func(candidate map[string]any) {
		if len(path) == 3 && path[0] == "review_stages" {
			expandReviewStagesOverlay(candidate)
		}
		if len(path) == 3 && path[0] == "reviewers" {
			expandReviewersOverlay(candidate)
		}
		config.OverlaySet(candidate, value, path...)
	})
}

func (s *Session) syncScopeRaw(path []string, value any) {
	if s.scopeRaw == nil || len(path) == 0 {
		return
	}
	if _, ok := s.scopeRaw[path[0]]; !ok {
		filled, err := config.DocumentFromConfig(s.Config)
		if err == nil {
			if section, exists := filled[path[0]]; exists {
				s.scopeRaw[path[0]] = section
				return
			}
		}
	}
	config.OverlaySet(s.scopeRaw, value, path...)
}

func (s *Session) noteOverride(path []string, value any) error {
	if !s.EditingOverlay() {
		s.ScopeDirty = true
		s.syncScopeRaw(path, value)
		return nil
	}
	err := s.applyOverlaySet(path, value)
	if err != nil {
		// Keep invalid edits visible and dirty. Save validates this same candidate
		// and reports the error instead of silently publishing the old overlay.
		config.OverlaySet(s.overlayRaw, value, path...)
		s.OverlayDirty = true
		s.overlayDraft = true
		// Model inputs may still reference a map replaced by a previous merge.
		// Rebuild the visible draft from raw edits rather than that old map.
		if draft, draftErr := s.previewOverlayDraft(s.overlayRaw); draftErr == nil {
			s.Config = draft
		}
	}
	return err
}

// OptionsSectionOverlayRoots lists the overlay paths owned by one Options page.
func OptionsSectionOverlayRoots(section string) [][]string {
	switch section {
	case "interface":
		return [][]string{{"language"}, {"agent_language"}, {"tui"}}
	case "execution":
		return [][]string{{"kanban_agents"}, {"launcher"}, {"models", "kanban"}}
	case "review":
		return [][]string{{"reviewers"}, {"models", "review_roles"}}
	case "review_stages":
		return [][]string{{"review_stages"}}
	case "rules":
		return [][]string{{"rules"}}
	default:
		return nil
	}
}

// SectionHasOverrides reports whether the Project tab has any override for the page.
func (s *Session) SectionHasOverrides(section string) bool {
	if s == nil || !s.EditingOverlay() {
		return false
	}
	for _, path := range OptionsSectionOverlayRoots(section) {
		if config.OverlayHas(s.overlayRaw, path...) {
			return true
		}
	}
	return false
}

// RestoreSection clears every Project-tab override owned by one Options page.
func (s *Session) RestoreSection(section string) error {
	roots := OptionsSectionOverlayRoots(section)
	if len(roots) == 0 {
		return nil
	}
	if err := s.ensureScopeRaw(); err != nil {
		return err
	}
	candidate := config.CloneOverlay(s.overlayRaw)
	for _, path := range roots {
		if len(path) > 0 && path[0] == "review_stages" {
			expandReviewStagesOverlay(candidate)
		}
		if len(path) > 0 && path[0] == "reviewers" {
			expandReviewersOverlay(candidate)
		}
		config.OverlayDelete(candidate, path...)
	}
	merged, err := config.MergeOverlayOnRaw(s.scopeRaw, candidate)
	if err != nil {
		if !s.overlayDraft {
			return err
		}
		merged, err = s.previewOverlayDraft(candidate)
		if err != nil {
			return err
		}
		s.overlayDraft = true
	} else {
		s.overlayDraft = false
	}
	s.overlayRaw = candidate
	s.OverlayDirty = true
	if s.EditingOverlay() {
		s.Config = merged
	}
	return nil
}

// RestoreInherit deletes an overlay key and refreshes the effective view.
// Restoring a Project-tab execution agent also clears that scale's model
// overrides for the agent that was stored in the overlay.
func (s *Session) RestoreInherit(path ...string) error {
	if len(path) == 0 {
		return nil
	}
	if err := s.ensureScopeRaw(); err != nil {
		return err
	}
	candidate := config.CloneOverlay(s.overlayRaw)
	if len(path) == 3 && path[0] == "review_stages" {
		expandReviewStagesOverlay(candidate)
	}
	if len(path) == 3 && path[0] == "reviewers" {
		expandReviewersOverlay(candidate)
	}
	if len(path) == 2 && path[0] == "kanban_agents" {
		agent, _ := overlayStringAt(candidate, path...)
		config.OverlayDelete(candidate, path...)
		if agent != "" {
			deleteKanbanScaleModelKeys(candidate, agent, path[1])
		}
	} else {
		config.OverlayDelete(candidate, path...)
	}
	merged, err := config.MergeOverlayOnRaw(s.scopeRaw, candidate)
	if err != nil {
		if !s.overlayDraft {
			return err
		}
		merged, err = s.previewOverlayDraft(candidate)
		if err != nil {
			return err
		}
		s.overlayDraft = true
	} else {
		s.overlayDraft = false
	}
	s.overlayRaw = candidate
	s.OverlayDirty = true
	if s.EditingOverlay() {
		s.Config = merged
	}
	return nil
}

func deleteKanbanScaleModelKeys(overlay map[string]any, agent, scale string) {
	if overlay == nil || agent == "" || scale == "" {
		return
	}
	config.OverlayDelete(overlay, "models", "kanban", agent, scale+"_model")
	config.OverlayDelete(overlay, "models", "kanban", agent, scale+"_effort")
}

func overlayStringAt(overlay map[string]any, path ...string) (string, bool) {
	if !config.OverlayHas(overlay, path...) {
		return "", false
	}
	current := any(overlay)
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current = obj[key]
	}
	text, ok := current.(string)
	return text, ok
}

// SetTUIField updates one TUI key so a Project-tab edit cannot copy the whole section.
// An invalid overlay edit remains visible and dirty; the returned error also blocks Save.
func (s *Session) SetTUIField(field string, value any) error {
	if s.Config == nil {
		return nil
	}
	switch field {
	case "theme":
		if text, ok := value.(string); ok {
			s.Config.TUI.Theme = text
		}
	case "columns":
		if n, ok := intValue(value); ok {
			s.Config.TUI.Columns = n
		}
	case "min_column_width":
		if n, ok := intValue(value); ok {
			s.Config.TUI.MinColumnWidth = n
		}
	case "refresh":
		if n, ok := intValue(value); ok {
			s.Config.TUI.Refresh = n
		}
	case "single":
		if flag, ok := value.(bool); ok {
			s.Config.TUI.Single = flag
		}
	}
	return s.noteOverride([]string{"tui", field}, value)
}

// NoteModelOverride records a model field the user actually edited.
// On the Project tab, editing a kanban model or effort also pins that scale's
// Agent into the overlay so the choice is no longer inherited alone.
func (s *Session) NoteModelOverride(field ModelField, value string) {
	if field.Agent == "" || field.field == "" {
		return
	}
	switch field.field {
	case "path", "process_name":
		// Agents definitions are hand-edited only; Options must not write them.
		return
	case "model", "effort":
		s.noteOverride([]string{"models", "review_roles", field.Agent, field.field}, value)
	default:
		if isReviewRoleName(field.Agent) {
			s.noteOverride([]string{"models", "review_roles", field.Agent, field.field}, value)
			return
		}
		s.noteOverride([]string{"models", "kanban", field.Agent, field.field}, value)
		s.pinKanbanAgentForField(field)
	}
}

func isReviewRoleName(name string) bool {
	for _, role := range config.ReviewRoles {
		if role == name {
			return true
		}
	}
	return false
}

// pinKanbanAgentForField writes kanban_agents.<scale> when a Project-tab model
// edit lands and that Agent is still inherited.
func (s *Session) pinKanbanAgentForField(field ModelField) {
	if s == nil || !s.EditingOverlay() || s.overlayRaw == nil {
		return
	}
	scale := kanbanScaleForField(field.field)
	if scale == "" || config.OverlayHas(s.overlayRaw, "kanban_agents", scale) {
		return
	}
	config.OverlaySet(s.overlayRaw, field.Agent, "kanban_agents", scale)
	s.OverlayDirty = true
	if err := s.ensureScopeRaw(); err != nil {
		return
	}
	if merged, err := config.MergeOverlayOnRaw(s.scopeRaw, s.overlayRaw); err == nil {
		s.Config = merged
		s.overlayDraft = false
		return
	}
	if draft, err := s.previewOverlayDraft(s.overlayRaw); err == nil {
		s.Config = draft
		s.overlayDraft = true
	}
}

func kanbanScaleForField(field string) string {
	for _, scale := range config.TaskScales {
		if field == scale+"_model" || field == scale+"_effort" {
			return scale
		}
	}
	return ""
}

func (s *Session) expandOverlayReviewStages() {
	expandReviewStagesOverlay(s.overlayRaw)
}

func (s *Session) expandOverlayReviewers() {
	expandReviewersOverlay(s.overlayRaw)
}

func expandReviewStagesOverlay(overlay map[string]any) {
	if overlay == nil {
		return
	}
	stages, ok := overlay["review_stages"]
	if !ok {
		return
	}
	if obj, ok := stages.(map[string]any); ok {
		if _, hasLarge := obj["large"]; hasLarge {
			return
		}
		if _, hasSmall := obj["small"]; hasSmall {
			return
		}
	}
	normalized, err := config.NormalizeReviewStages(stages)
	if err != nil {
		return
	}
	overlay["review_stages"] = normalized
}

func expandReviewersOverlay(overlay map[string]any) {
	if overlay == nil {
		return
	}
	reviewers, ok := overlay["reviewers"]
	if !ok {
		return
	}
	if obj, ok := reviewers.(map[string]any); ok {
		if _, hasLarge := obj["large"]; hasLarge {
			return
		}
		if _, hasSmall := obj["small"]; hasSmall {
			return
		}
	}
	normalized, err := config.NormalizeReviewers(reviewers)
	if err != nil {
		return
	}
	overlay["reviewers"] = normalized
}

func (s *Session) scopeBuffer() *config.Config {
	if s != nil && s.scopeConfig != nil {
		return s.scopeConfig
	}
	return s.Config
}

func (s *Session) saveScope() (string, error) {
	cfg := s.scopeBuffer()
	if cfg == nil {
		return "", nil
	}
	cfg.WelcomeComplete = true
	baseline := s.scopeExisting
	if baseline == nil {
		baseline = s.existing
	}
	// Options no longer edits agents.*; keep the loaded agents section so a
	// TUI save cannot clobber hand-edited path/process_name/templates.
	if baseline != nil {
		cfg.Agents = config.CloneAgents(baseline.Agents)
	}
	path, err := config.SaveIfUnchanged(cfg, baseline)
	if err == nil {
		s.existing = config.Clone(cfg)
		s.scopeExisting = config.Clone(cfg)
		s.scopeConfig = cfg
		if raw, rawErr := config.DocumentFromConfig(cfg); rawErr == nil {
			s.scopeRaw = raw
		}
		if s.EditingOverlay() {
			_ = s.rebuildOverlayConfig()
		} else {
			s.Config = cfg
		}
		s.ScopeDirty = false
	}
	return path, err
}

// SaveAllDirty writes every unpublished tab. Save still writes only the active tab
// so a section submit on Project cannot copy merged values into the scope file.
func (s *Session) SaveAllDirty() (string, error) {
	if s == nil {
		return "", nil
	}
	var last string
	if s.ScopeDirty {
		path, err := s.saveScope()
		if err != nil {
			return last, err
		}
		last = path
	}
	if s.OverlayDirty {
		path, err := s.saveOverlay()
		if err != nil {
			return last, err
		}
		last = path
	}
	if last != "" {
		return last, nil
	}
	return s.Save()
}

func (s *Session) saveOverlay() (string, error) {
	if s.OverlayLocation.Path == "" {
		return "", config.ErrMissingOverlayPath()
	}
	path, err := config.SaveOverlayIfUnchanged(s.OverlayLocation.Path, s.overlayRaw, s.overlayExisting)
	if err == nil {
		s.overlayExisting = config.CloneOverlay(s.overlayRaw)
		s.OverlayLocation.Exists = len(s.overlayRaw) > 0
		s.OverlayDirty = false
		s.overlayDraft = false
	}
	return path, err
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	default:
		return 0, false
	}
}
