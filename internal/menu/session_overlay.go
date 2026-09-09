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

// RestoreInheritLabel is the restore-inherit control copy for one field.
func (s *Session) RestoreInheritLabel(value string) string {
	return config.Text("tui.restore_inherit", s.InheritPrefix(), value)
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
	if mode == config.ModeProject {
		return s.SetTarget(config.TargetOverlay)
	}
	return nil
}

// SetTarget switches the edit buffer. Unsaved edits on the other tab are kept.
func (s *Session) SetTarget(target string) error {
	if s.Target == config.TargetScope && s.Config != nil {
		s.scopeConfig = s.Config
	}
	s.Target = target
	if target == config.TargetOverlay {
		return s.rebuildOverlayConfig()
	}
	if s.scopeConfig == nil {
		s.scopeConfig = s.Config
	}
	s.Config = s.scopeConfig
	return nil
}

func (s *Session) rebuildOverlayConfig() error {
	base := s.scopeConfig
	if base == nil {
		base = s.existing
	}
	merged, err := config.MergeScopeAndOverlay(base, s.overlayRaw)
	if err != nil {
		return err
	}
	s.Config = merged
	return nil
}

func (s *Session) noteOverride(path []string, value any) {
	if !s.EditingOverlay() {
		s.ScopeDirty = true
		return
	}
	if s.overlayRaw == nil {
		s.overlayRaw = map[string]any{}
	}
	config.OverlaySet(s.overlayRaw, value, path...)
	s.OverlayDirty = true
}

// RestoreInherit deletes an overlay key and refreshes the effective view.
func (s *Session) RestoreInherit(path ...string) error {
	if s.overlayRaw == nil {
		s.overlayRaw = map[string]any{}
	}
	config.OverlayDelete(s.overlayRaw, path...)
	s.OverlayDirty = true
	if s.EditingOverlay() {
		return s.rebuildOverlayConfig()
	}
	return nil
}

// SetTUIField updates one TUI key so a Project-tab edit cannot copy the whole section.
func (s *Session) SetTUIField(field string, value any) {
	if s.Config == nil {
		return
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
	s.noteOverride([]string{"tui", field}, value)
}

// NoteModelOverride records a model or executable field the user actually edited.
func (s *Session) NoteModelOverride(field ModelField, value string) {
	if field.Agent == "" || field.field == "" {
		return
	}
	switch field.field {
	case "path", "process_name":
		if value == "" {
			_ = s.RestoreInherit("agents", field.Agent, field.field)
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
	if s.overlayRaw == nil {
		return
	}
	stages, ok := s.overlayRaw["review_stages"]
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
	s.overlayRaw["review_stages"] = normalized
}

func (s *Session) saveScope() (string, error) {
	path, err := config.SaveIfUnchanged(s.Config, s.existing)
	if err == nil {
		s.existing = config.Clone(s.Config)
		s.scopeExisting = config.Clone(s.Config)
		s.scopeConfig = s.Config
		s.ScopeDirty = false
	}
	return path, err
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
