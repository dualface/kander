package tui

import "github.com/dualface/kander/internal/config"

func (p *optionsPanel) scopeTUI() config.TUI {
	if p.session != nil && p.session.Config != nil {
		return p.session.Config.TUI
	}
	return config.TUI{
		Compact:        p.app.Compact,
		Theme:          p.app.Theme,
		Columns:        p.app.Columns,
		MinColumnWidth: p.app.MinColumnWidth,
		Refresh:        p.app.RefreshSecs,
		Single:         p.app.Model.Single,
	}
}

func (b *formBinding) addCompactField(p *optionsPanel) {
	b.addSpacer()
	b.fieldIndex[interfaceFocusKey("compact")] = b.focusable
	b.addField(inlineConfirm().
		Title(p.inheritTitle(t("tui.compact_columns"), formatBool(b.compact), "tui", "compact")).
		Value(&b.compact))
	b.addSpacer()
}

func (b *formBinding) applyCompact(p *optionsPanel, previous config.TUI, scopeTUI *config.TUI, overlay bool) bool {
	if previous.Compact == b.compact {
		return false
	}
	p.app.Compact = b.compact
	scopeTUI.Compact = b.compact
	if overlay {
		p.setOverlayTUIField("compact", b.compact)
	}
	return true
}
