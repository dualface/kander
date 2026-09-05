package menu

import (
	"encoding/json"
	"reflect"

	"github.com/dualface/kander/internal/config"
)

// ApplyDoctorConfig 同步 doctor 修复结果, 保留面板中尚未保存的编辑.
func (s *Session) ApplyDoctorConfig(before, after *config.Config, dirty bool) error {
	merged := after
	if dirty {
		if before == nil {
			before = config.DefaultConfig()
		}
		objects := make([]map[string]any, 3)
		for i, cfg := range []*config.Config{before, s.Config, after} {
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

// SyncTUI 同步已在会话之外改动的界面偏好.
// persisted 表示该取值已经落盘, 需要一并推进编辑基线, 否则保存时会被误判为其他进程改动.
func (s *Session) SyncTUI(value config.TUI, persisted bool) {
	if s == nil {
		return
	}
	if s.Config != nil {
		s.Config.TUI = value
	}
	if persisted && s.existing != nil {
		s.existing.TUI = value
	}
}
