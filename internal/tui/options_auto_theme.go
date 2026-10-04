package tui

import (
	"slices"

	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/config"
)

// addAutoThemeFields adds the two selectors for the themes "auto" uses on a
// light and a dark terminal background, right below the theme selector.
func (b *formBinding) addAutoThemeFields(p *optionsPanel) {
	b.addSpacer()
	b.addAutoThemeField(p, "theme_light", t("theme.auto_light"), &b.themeLight, false)
	b.addSpacer()
	b.addAutoThemeField(p, "theme_dark", t("theme.auto_dark"), &b.themeDark, true)
	b.addSpacer()
}

func (b *formBinding) addAutoThemeField(p *optionsPanel, key, title string, value *string, dark bool) {
	b.fieldIndex[interfaceFocusKey(key)] = b.focusable
	b.addField(huh.NewSelect[string]().
		Title(p.inheritTitle(title, p.app.Context.themeLabel(*value), "tui", key)).
		Options(autoThemeOptions(p, *value, dark)...).
		Value(value).
		Inline(true))
}

// autoThemeOptions lists the named themes of one background family. A current
// value from the other family (only possible by editing the config by hand)
// stays selectable at the end so opening the page never rewrites it.
func autoThemeOptions(p *optionsPanel, current string, dark bool) []huh.Option[string] {
	names := familyThemeNames(dark)
	if _, ok := themeDefByName(current); ok && !slices.Contains(names, current) {
		names = append(names, current)
	}
	options := make([]huh.Option[string], 0, len(names))
	for _, name := range names {
		options = append(options, huh.NewOption(p.app.Context.themeLabel(name), name))
	}
	return options
}

// applyAutoThemes writes edited auto variants to the App, so the board previews
// them whenever "auto" currently resolves to the edited family, and to the
// config session. Unedited values are never written.
func (b *formBinding) applyAutoThemes(p *optionsPanel, previous config.TUI, scopeTUI *config.TUI, overlay bool) bool {
	changed := false
	for _, field := range []struct {
		key      string
		value    string
		previous string
		scope    *string
		app      *string
	}{
		{"theme_light", b.themeLight, previous.ThemeLight, &scopeTUI.ThemeLight, &p.app.AutoThemes.Light},
		{"theme_dark", b.themeDark, previous.ThemeDark, &scopeTUI.ThemeDark, &p.app.AutoThemes.Dark},
	} {
		if _, ok := themeDefByName(field.value); !ok || field.value == field.previous {
			continue
		}
		*field.app = field.value
		*field.scope = field.value
		changed = true
		if overlay {
			p.setOverlayTUIField(field.key, field.value)
		}
		// Huh caches the body during Update; rebuild so the frame repaints with
		// the palette the new variant may have selected.
		p.rebuildAt(interfaceFocusKey(field.key))
	}
	return changed
}
