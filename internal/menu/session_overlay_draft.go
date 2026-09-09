package menu

import (
	"encoding/json"
	"sort"

	"github.com/dualface/kander/internal/config"
)

// previewOverlayDraft rebuilds presentation from the current scope and explicit
// edits. It never supplies persistence: saveOverlay validates the original raw
// documents, including every rejected input retained in overlayRaw.
func (s *Session) previewOverlayDraft(overlay map[string]any) (*config.Config, error) {
	view, err := config.MergeOverlayOnRaw(s.scopeRaw, nil)
	if err != nil {
		return nil, err
	}
	accepted := map[string]any{}
	pending := config.CloneOverlay(overlay)
	// Keep schema defaults and coupled fields for every valid section. Retry
	// remaining sections when another section supplies a needed definition.
	for len(pending) > 0 {
		keys := make([]string, 0, len(pending))
		for key := range pending {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		progress := false
		for _, key := range keys {
			candidate := config.CloneOverlay(accepted)
			candidate[key] = pending[key]
			merged, mergeErr := config.MergeOverlayOnRaw(s.scopeRaw, candidate)
			if mergeErr != nil {
				continue
			}
			accepted = candidate
			view = merged
			delete(pending, key)
			progress = true
		}
		if !progress {
			break
		}
	}
	document, err := config.DocumentFromConfig(view)
	if err != nil {
		return nil, err
	}
	// The remaining sections contain typed but rejected user inputs. Overlay
	// only those explicit leaves onto the current inherited presentation.
	applyDraftValues(document, pending, nil)
	data, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	var draft config.Config
	if err := json.Unmarshal(data, &draft); err != nil {
		return nil, err
	}
	return &draft, nil
}

func applyDraftValues(document, values map[string]any, path []string) {
	for key, value := range values {
		childPath := append(append([]string{}, path...), key)
		if object, ok := value.(map[string]any); ok {
			applyDraftValues(document, object, childPath)
		} else {
			config.OverlaySet(document, value, childPath...)
		}
	}
}
