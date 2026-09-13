package tui

import (
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/config"
)

// optionTitle appends ":" after a field name. Existing trailing colons or spaces are trimmed first.
func optionTitle(name string) string {
	name = strings.TrimRightFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || r == ':' || r == '：'
	})
	return name + ":"
}

func (p *optionsPanel) inheritTitle(title, display string, path ...string) string {
	title = optionTitle(title)
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

const (
	restoreChoiceYes = "yes"
	restoreChoiceNo  = "no"
)

// addPageRestore appends one page-level restore control when the Project tab has
// overrides for the current section. Selecting it opens a confirmation dialog.
func (b *formBinding) addPageRestore(p *optionsPanel) {
	if p == nil || p.session == nil || !p.session.EditingOverlay() || !p.session.SectionHasOverrides(p.current) {
		return
	}
	flag := false
	b.restoreFlag = &flag
	b.addSpacer()
	b.addField(inlineConfirm().
		Title(optionTitle(t("tui.restore_field_inherit"))).
		Value(&flag))
}

// applyRestores opens the restore confirmation when the page-level control is selected.
func (b *formBinding) applyRestores(p *optionsPanel) bool {
	if p == nil || b.restoreFlag == nil || !*b.restoreFlag {
		return false
	}
	*b.restoreFlag = false
	p.wantRestoreConfirm = true
	return true
}

func (p *optionsPanel) openRestoreConfirm() tea.Cmd {
	if p.current == "" {
		return nil
	}
	p.restoreConfirming = true
	p.restoreChoice = restoreChoiceNo
	p.bind = nil
	form := p.newForm(rootKeyMap(), huh.NewGroup(
		huh.NewSelect[string]().
			Description(t("tui.restore_page_confirm")).
			Options(
				huh.NewOption(t("tui.restore_page_confirm_yes"), restoreChoiceYes),
				huh.NewOption(t("tui.restore_page_confirm_no"), restoreChoiceNo),
			).
			Value(&p.restoreChoice),
	))
	return p.startForm(form)
}

func (p *optionsPanel) finishRestoreConfirm() tea.Cmd {
	section := p.current
	p.restoreConfirming = false
	if p.restoreChoice == restoreChoiceYes && p.session != nil {
		if err := p.session.RestoreSection(section); err != nil {
			p.showReport(t("tui.load_failed"), nil, err.Error())
			return nil
		}
		p.syncAppFromSession()
		p.dirty = p.session.HasUnsaved()
	}
	if section == "" {
		return p.openRoot()
	}
	return p.openSection(section)
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
