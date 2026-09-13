package menu

import (
	"maps"
	"reflect"

	"github.com/dualface/kander/internal/config"
)

// LanguageState is a snapshot of the interface language in both Options edit buffers:
// the scope value with its raw scope key, and the raw project overlay key.
type LanguageState struct {
	scopeLanguage   string
	scopeRaw        any
	scopeRawPresent bool
	overlayRaw      any
	overlayPresent  bool
}

// Equal reports whether two snapshots hold the same language values and key presence.
func (a LanguageState) Equal(b LanguageState) bool {
	return a.scopeLanguage == b.scopeLanguage &&
		a.scopeRawPresent == b.scopeRawPresent && reflect.DeepEqual(a.scopeRaw, b.scopeRaw) &&
		a.overlayPresent == b.overlayPresent && reflect.DeepEqual(a.overlayRaw, b.overlayRaw)
}

// scopeLanguageBuffer is the scope config that holds the Global-tab language.
func (s *Session) scopeLanguageBuffer() *config.Config {
	if s.EditingOverlay() {
		return s.scopeConfig
	}
	return s.Config
}

// CaptureLanguage snapshots the unsaved interface language of both tabs.
func (s *Session) CaptureLanguage() LanguageState {
	var state LanguageState
	if s == nil {
		return state
	}
	if scope := s.scopeLanguageBuffer(); scope != nil {
		state.scopeLanguage = scope.Language
	}
	state.scopeRaw, state.scopeRawPresent = s.scopeRaw["language"]
	state.overlayRaw, state.overlayPresent = s.overlayRaw["language"]
	return state
}

// RestoreLanguage puts both tabs' interface language back to a snapshot and
// refreshes the Project-tab view. It changes no other field and no dirty flag.
func (s *Session) RestoreLanguage(state LanguageState) error {
	if s == nil || s.Config == nil || s.CaptureLanguage().Equal(state) {
		return nil
	}
	previousScopeRaw := s.scopeRaw
	if s.scopeRaw != nil {
		// Only the top-level key changes, so a shallow copy keeps the old document intact on failure.
		s.scopeRaw = maps.Clone(s.scopeRaw)
		setLanguageKey(s.scopeRaw, state.scopeRaw, state.scopeRawPresent)
	}
	overlay := config.CloneOverlay(s.overlayRaw)
	setLanguageKey(overlay, state.overlayRaw, state.overlayPresent)
	draft := s.overlayDraft
	var merged *config.Config
	if s.EditingOverlay() {
		var err error
		merged, err = config.MergeOverlayOnRaw(s.scopeRaw, overlay)
		if err != nil && s.overlayDraft {
			merged, err = s.previewOverlayDraft(overlay)
		} else if err == nil {
			draft = false
		}
		if err != nil {
			s.scopeRaw = previousScopeRaw
			return err
		}
	}
	s.overlayRaw = overlay
	s.overlayDraft = draft
	if scope := s.scopeLanguageBuffer(); scope != nil {
		scope.Language = state.scopeLanguage
	}
	if merged != nil {
		s.Config = merged
	}
	return nil
}

// AdvanceLanguage moves the tabs that have no unsaved edits left onto their
// current language, keeping the snapshot of any tab that is still dirty.
func (s *Session) AdvanceLanguage(base LanguageState) LanguageState {
	if s == nil {
		return base
	}
	current := s.CaptureLanguage()
	if !s.ScopeDirty {
		base.scopeLanguage = current.scopeLanguage
		base.scopeRaw, base.scopeRawPresent = current.scopeRaw, current.scopeRawPresent
	}
	if !s.OverlayDirty {
		base.overlayRaw, base.overlayPresent = current.overlayRaw, current.overlayPresent
	}
	return base
}

func setLanguageKey(document map[string]any, value any, present bool) {
	if document == nil {
		return
	}
	if present {
		document["language"] = value
		return
	}
	delete(document, "language")
}
