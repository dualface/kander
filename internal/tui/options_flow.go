package tui

import (
	"github.com/dualface/kander/internal/flow"
	"github.com/dualface/kander/internal/menu"
)

func (p *optionsPanel) openFlow() {
	if p.session == nil {
		return
	}
	var lines []menu.ReportLine
	connected := false
	for _, line := range flow.Build(p.session.Config) {
		text := t(line.Key, line.Args...)
		level := menu.LevelInfo
		switch line.Kind {
		case flow.Heading:
			if len(lines) > 0 {
				lines = append(lines, menu.ReportLine{})
			}
			level = menu.LevelNote
			connected = false
		case flow.Step:
			if connected {
				lines = append(lines, menu.ReportLine{Text: "  │"})
			}
			text = "• " + text
			connected = true
		case flow.Detail:
			text = "  └ " + text
		}
		lines = append(lines, menu.ReportLine{Level: level, Text: text})
	}
	title := t("flow.title")
	if p.dirty {
		title += t("tui.unsaved")
	}
	p.showReport(title, lines, "")
	p.form = nil
}
