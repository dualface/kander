package menu

import (
	"encoding/json"
	"reflect"

	"github.com/dualface/kander/internal/config"
)

// ApplyDoctorConfig adopts the doctor repair result while keeping edits not yet saved in the panel.
func (s *Session) ApplyDoctorConfig(before, after *config.Config, dirty bool) error {
	merged := after
	edited := s.scopeConfig
	if edited == nil {
		edited = s.Config
	}
	preserve := s.ScopeDirty || (dirty && !s.EditingOverlay())
	if preserve {
		if before == nil {
			before = config.DefaultConfig()
		}
		objects := make([]map[string]any, 3)
		for i, cfg := range []*config.Config{before, edited, after} {
			data, err := json.Marshal(cfg)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &objects[i]); err != nil {
				return err
			}
		}
		preserveDoctorEdits(objects[0], objects[1], objects[2])
		objects[2]["welcome_complete"] = after.WelcomeComplete
		data, err := json.Marshal(objects[2])
		if err != nil {
			return err
		}
		merged, err = config.ValidateJSON(data)
		if err != nil {
			return err
		}
	}
	s.existing = after
	s.scopeExisting = config.Clone(after)
	s.scopeConfig = merged
	if raw, err := config.DocumentFromConfig(merged); err == nil {
		s.scopeRaw = raw
	}
	if s.EditingOverlay() {
		return s.rebuildOverlayConfig()
	}
	s.Config = merged
	return nil
}

func preserveDoctorEdits(before, edited, after map[string]any) {
	for key, value := range edited {
		oldObject, oldOK := before[key].(map[string]any)
		editObject, editOK := value.(map[string]any)
		newObject, newOK := after[key].(map[string]any)
		if oldOK && editOK && newOK {
			preserveDoctorEdits(oldObject, editObject, newObject)
		} else if !reflect.DeepEqual(before[key], value) {
			after[key] = value
		}
	}
}

// SyncTUI adopts UI preferences that changed outside this session.
// persisted means the value already reached disk and the editing baseline must advance with it, otherwise saving would mistake it for another process's change.
func (s *Session) SyncTUI(value config.TUI, persisted bool) {
	if s == nil {
		return
	}
	if s.scopeConfig != nil {
		s.scopeConfig.TUI = value
	}
	if !s.EditingOverlay() && s.Config != nil {
		s.Config.TUI = value
	}
	if s.scopeRaw != nil {
		// Sync the whole section, including legacy scopes without a tui object.
		for key, field := range map[string]any{
			"theme": value.Theme, "columns": value.Columns,
			"min_column_width": value.MinColumnWidth,
			"refresh":          value.Refresh, "single": value.Single,
		} {
			config.OverlaySet(s.scopeRaw, field, "tui", key)
		}
	}
	if persisted && s.existing != nil {
		s.existing.TUI = value
	}
	if persisted && s.scopeExisting != nil {
		s.scopeExisting.TUI = value
	}
}
