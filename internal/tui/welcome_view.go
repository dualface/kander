package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// shouldShowWelcome reports whether the empty-board welcome overlay is on
// top. Higher overlays hide it; a session dismiss keeps it closed until the
// next process even if the board stays empty.
func (a *App) shouldShowWelcome() bool {
	if a == nil || a.welcomeDismissed || len(a.Model.Tasks) > 0 {
		return false
	}
	return a.TaskActions == nil && a.Chat == nil && a.Options == nil &&
		a.StartConfirmation == nil && a.BoardInit == nil && a.Takeover == nil && a.Issues == nil &&
		a.Detail == nil && !a.Help && !a.Searching
}

func (a *App) dismissWelcome() {
	a.welcomeDismissed = true
}

func (a *App) renderWelcome() (popupBox, string) {
	h, w := a.size()
	p := themePalette(a.Theme)
	frame := popup{Title: t("tui.welcome_title"), Hint: t("tui.welcome_hint")}
	inner := frame.inner(w, h, 68)
	bodyStyle := styleFor("popup", p)
	tipStyle := styleFor("popup-group", p)
	rows := styledWrap(t("tui.welcome_intro"), inner, bodyStyle, p)
	rows = append(rows, p.fillLine(inner))
	for _, id := range []string{
		"tui.welcome_item_chat",
		"tui.welcome_item_issues",
		"tui.welcome_item_help",
		"tui.welcome_item_options",
	} {
		rows = append(rows, styledWrap(t(id), inner, bodyStyle, p)...)
	}
	rows = append(rows, p.fillLine(inner))
	rows = append(rows, styledWrap(t("tui.welcome_tip_title"), inner, tipStyle, p)...)
	rows = append(rows, styledWrap(t("tui.welcome_tip"), inner, bodyStyle, p)...)
	box, _, out := frame.render(p, w, h, inner, strings.Join(rows, "\n"))
	return box, out
}

func styledWrap(text string, width int, style lipgloss.Style, p palette) []string {
	lines := wrapText(text, width)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			out = append(out, p.fillLine(width))
			continue
		}
		out = append(out, style.Render(line))
	}
	return out
}
