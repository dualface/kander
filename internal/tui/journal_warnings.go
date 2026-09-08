package tui

import "strings"

// Journal advisories use the existing notice renderer, never terminal streams.
func (a *App) showJournalWarnings(warnings []string) {
	if len(warnings) > 0 {
		a.showFocusNotice(strings.Join(warnings, " "))
	}
}
