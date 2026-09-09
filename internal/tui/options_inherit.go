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
		p.dirty = p.session.HasUnsaved()
		p.rebuildAt("restored")
	}
	return changed
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
