package tui

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

// languageBaseline is the interface language as of the last load or save,
// restored when the edit is cancelled. It is captured lazily before the first
// unsaved language edit, so a nil baseline means there is nothing to restore.
type languageBaseline struct {
	session menu.LanguageState
	bound   string
}

// applyLanguage switches the interface language and redraws every surface that
// caches translated text: the page context, agent labels and the current form.
func (p *optionsPanel) applyLanguage(language string) {
	if p.languageBase == nil {
		p.languageBase = &languageBaseline{session: p.session.CaptureLanguage(), bound: config.BoundConfigLanguage()}
	}
	p.session.SetLanguage(language)
	p.refreshLanguageCopy()
	p.markDirty()
	p.rebuildAt(interfaceFocusKey("language"))
}

func (p *optionsPanel) refreshLanguageCopy() {
	p.app.Context = tuiPageContext()
	p.session.RefreshCopy()
}

// advanceLanguageBaseline accepts the language of every tab a save published.
// A tab that is still dirty keeps its baseline, so a later cancel still restores it.
func (p *optionsPanel) advanceLanguageBaseline() {
	if p.languageBase == nil || p.session == nil {
		return
	}
	p.languageBase.session = p.session.AdvanceLanguage(p.languageBase.session)
	if !p.session.ScopeDirty || !p.session.OverlayDirty {
		p.languageBase.bound = config.BoundConfigLanguage()
	}
	if p.languageBase.session.Equal(p.session.CaptureLanguage()) && p.languageBase.bound == config.BoundConfigLanguage() {
		p.languageBase = nil
	}
}

// restoreLanguage cancels unsaved interface language edits: the session values,
// the overlay key presence and the bound language return to the baseline, and
// the translated copy is rebuilt. Other unsaved fields are left as they are.
func (p *optionsPanel) restoreLanguage() error {
	base := p.languageBase
	if base == nil || p.session == nil {
		return nil
	}
	if err := p.session.RestoreLanguage(base.session); err != nil {
		return err
	}
	p.languageBase = nil
	config.BindConfigLanguage(&config.Config{Language: base.bound})
	p.refreshLanguageCopy()
	return nil
}
