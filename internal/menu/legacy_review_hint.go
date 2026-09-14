package menu

import (
	"sort"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// LegacyReviewKeys returns sorted legacy review key paths for the active tab.
// Project uses Config diagnostics from MergeOverlayOnRaw when present; otherwise
// both tabs recompute via NormalizeLegacyReviewKeys on the raw layers only.
func (s *Session) LegacyReviewKeys() []string {
	if s == nil {
		return nil
	}
	if s.EditingOverlay() {
		if keys := s.Config.LegacyReviewKeys(); len(keys) > 0 {
			return keys
		}
		return legacyKeysFromLayers(s.scopeRaw, s.overlayRaw)
	}
	if keys := legacyKeysFromLayers(s.scopeRaw); len(keys) > 0 {
		return keys
	}
	if s.existing != nil {
		return s.existing.LegacyReviewKeys()
	}
	return nil
}

// LegacyReviewHint returns the non-blocking migration notice for options chrome,
// or empty when the active tab has no legacy keys.
func (s *Session) LegacyReviewHint() string {
	keys := s.LegacyReviewKeys()
	if len(keys) == 0 {
		return ""
	}
	return config.Text("config.legacy_review_keys", strings.Join(keys, ", "))
}

func legacyKeysFromLayers(layers ...map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		_, keys := config.NormalizeLegacyReviewKeys(layer)
		for _, key := range keys {
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// SeedScopeRawForTest installs a scope document so options tests can exercise
// Global-tab legacy key diagnostics without probing a real install.
func (s *Session) SeedScopeRawForTest(raw map[string]any) {
	if s == nil {
		return
	}
	s.scopeRaw = config.CloneOverlay(raw)
}
