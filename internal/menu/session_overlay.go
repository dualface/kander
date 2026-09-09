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
	return false
}

// InheritPrefix is "全局" for a global install and "默认" for a project install.
func (s *Session) InheritPrefix() string {
	if s != nil && s.InstallMode == config.ModeProject {
		return config.Text("tui.inherit_default")
	}
	return config.Text("tui.inherit_global")
}

// FormatInherited renders an uncovered field as "全局：value" / "默认：value".
func (s *Session) FormatInherited(value string) string {
	if value == "" {
		value = config.Text("config.cli_default")
	}
	return config.Text("tui.inherited_value", s.InheritPrefix(), value)
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

// RestoreInherit deletes an overlay key and refreshes the effective view.
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
	config.OverlayDelete(candidate, path...)
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

// NoteModelOverride records a model or executable field the user actually edited.
func (s *Session) NoteModelOverride(field ModelField, value string) {
	if field.Agent == "" || field.field == "" {
		return
	}
	switch field.field {
	case "path", "process_name":
		if value == "" {
			if s.EditingOverlay() {
				_ = s.RestoreInherit("agents", field.Agent, field.field)
			} else {
				s.noteOverride([]string{"agents", field.Agent, field.field}, value)
			}
			return
		}
		s.noteOverride([]string{"agents", field.Agent, field.field}, value)
	case "model", "effort":
		s.noteOverride([]string{"models", "review_roles", field.Agent, field.field}, value)
	default:
		s.noteOverride([]string{"models", "kanban", field.Agent, field.field}, value)
	}
}

func (s *Session) expandOverlayReviewStages() {
	expandReviewStagesOverlay(s.overlayRaw)
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
