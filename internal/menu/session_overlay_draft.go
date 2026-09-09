package menu

import (
	"encoding/json"

	"github.com/dualface/kander/internal/config"
)

// restoreDraftField updates only the removed field's presentation while other
// rejected inputs remain editable. This projection is never used for persistence:
// saveOverlay always validates overlayRaw against the raw scope document.
func (s *Session) restoreDraftField(candidate map[string]any, path []string) (*config.Config, error) {
	section := map[string]any{}
	if value, exists := candidate[path[0]]; exists {
		section[path[0]] = value
	}
	inherited, err := config.MergeOverlayOnRaw(s.scopeRaw, section)
	if err != nil {
		// An incomplete section has no valid merged view yet. Its removed key
		// inherits from the scope; the other rejected fields keep their inputs.
		inherited, err = config.MergeOverlayOnRaw(s.scopeRaw, nil)
		if err != nil {
			return nil, err
		}
	}
	defaults, err := config.DocumentFromConfig(inherited)
	if err != nil {
		return nil, err
	}
	draft, err := config.DocumentFromConfig(s.overlayDraft)
	if err != nil {
		return nil, err
	}
	if value, exists := config.OverlayGet(defaults, path...); exists {
		config.OverlaySet(draft, value, path...)
	} else {
		config.OverlayDelete(draft, path...)
	}
	data, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	var view config.Config
	if err := json.Unmarshal(data, &view); err != nil {
		return nil, err
	}
	return &view, nil
}
