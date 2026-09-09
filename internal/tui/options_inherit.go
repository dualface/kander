package tui

import (
	"strconv"

	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/config"
)

type restoreField struct {
	path []string
	flag *bool
}

func (p *optionsPanel) inheritTitle(title, display string, path ...string) string {
	if p.session == nil || !p.session.EditingOverlay() || p.session.FieldOverridden(path...) {
		return title
	}
	return title + "  " + p.session.FormatInherited(display)
}

func (p *optionsPanel) overridePresence(path ...string) bool {
	return p.session != nil && p.session.FieldOverridden(path...)
}

func (p *optionsPanel) rebuildIfOverrideChanged(before bool, key string, path ...string) {
	if p.session == nil || !p.session.EditingOverlay() {
		return
	}
	if p.session.FieldOverridden(path...) != before {
		p.rebuildAt(key)
	}
}

func (b *formBinding) addRestore(p *optionsPanel, display string, path ...string) {
	if p.session == nil || !p.session.EditingOverlay() || !p.session.FieldOverridden(path...) {
		return
	}
	flag := false
	b.restores = append(b.restores, restoreField{
		path: append([]string{}, path...),
		flag: &flag,
	})
	b.addField(huh.NewConfirm().
		Title(modelIndent + t("tui.restore_field_inherit") + "  " + display).
		Value(&flag).
		Inline(true))
}

func (b *formBinding) applyRestores(p *optionsPanel) bool {
	if p.session == nil {
		return false
	}
	changed := false
	for _, item := range b.restores {
		if item.flag == nil || !*item.flag {
			continue
		}
		if err := p.session.RestoreInherit(item.path...); err != nil {
			p.showReport(t("tui.load_failed"), nil, err.Error())
			continue
		}
		changed = true
	}
	if changed {
		p.syncAppFromSession()
		p.dirty = p.session.HasUnsaved()
		p.rebuildAt("restored")
	}
	return changed
}

func (p *optionsPanel) syncAppFromSession() {
	if p == nil || p.app == nil || p.session == nil || p.session.Config == nil {
		return
	}
	tui := p.session.Config.TUI
	p.loadedTUI = tui
	p.appliedTUI = nil
	p.app.Theme = tui.Theme
	p.app.Columns = tui.Columns
	p.app.MinColumnWidth = tui.MinColumnWidth
	p.app.RefreshSecs = tui.Refresh
	p.app.Model.Single = tui.Single
}

func formatBool(flag bool) string {
	if flag {
		return t("rules.on")
	}
	return t("rules.off")
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}

func overlayDisplayLocation(p *optionsPanel) config.OverlayLocation {
	if p.session != nil && p.session.OverlayLocation.Path != "" {
		return p.session.OverlayLocation
	}
	loc, err := config.ResolveOverlayLocation("")
	if err != nil {
		return config.OverlayLocation{}
	}
	return loc
}

func overlayBasePath(p *optionsPanel) string {
	if p.session != nil && p.session.BasePath != "" {
		return p.session.BasePath
	}
	path, err := config.ConfigPath()
	if err != nil {
		return ""
	}
	return path
}

func (p *optionsPanel) setOverlayTUIField(field string, value any) {
	if err := p.session.SetTUIField(field, value); err != nil {
		p.showReport(t("tui.save_failed"), nil, err.Error())
	}
}
