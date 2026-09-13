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
	// bound is the binding to restore and boundSeq the edit that produced it (0 for the load binding).
	bound    string
	boundSeq int
	// edits holds, per Options tab, the last unsaved language edit that set the binding.
	// A save moves bound only to an edit whose tab it published, never to another tab's preview.
	edits map[string]languageEdit
	seq   int
}

type languageEdit struct {
	language string
	seq      int
}

// applyLanguage switches the interface language and redraws every surface that
// caches translated text: the page context, agent labels and the current form.
func (p *optionsPanel) applyLanguage(language string) {
	base := p.languageBase
	if base == nil {
		base = &languageBaseline{
			session: p.session.CaptureLanguage(),
			bound:   config.BoundConfigLanguage(),
			edits:   map[string]languageEdit{},
		}
		p.languageBase = base
	}
	base.seq++
	base.edits[p.languageTab()] = languageEdit{language: language, seq: base.seq}
	p.session.SetLanguage(language)
	p.refreshLanguageCopy()
	p.markDirty()
	p.rebuildAt(interfaceFocusKey("language"))
}

func (p *optionsPanel) languageTab() string {
	if p.session.EditingOverlay() {
		return config.TargetOverlay
	}
	return config.TargetScope
}

func (p *optionsPanel) refreshLanguageCopy() {
	p.app.Context = tuiPageContext()
	p.session.RefreshCopy()
}

// advanceLanguageBaseline accepts the language of every tab a save published.
// A tab that is still dirty keeps its baseline, so a later cancel still restores it.
func (p *optionsPanel) advanceLanguageBaseline() {
	base := p.languageBase
	if base == nil || p.session == nil {
		return
	}
	base.session = p.session.AdvanceLanguage(base.session)
	for tab, edit := range base.edits {
		dirty := p.session.ScopeDirty
		if tab == config.TargetOverlay {
			dirty = p.session.OverlayDirty
		}
		if dirty {
			continue
		}
		if edit.seq > base.boundSeq {
			base.bound, base.boundSeq = edit.language, edit.seq
		}
		delete(base.edits, tab)
	}
	if len(base.edits) == 0 && base.session.Equal(p.session.CaptureLanguage()) {
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
