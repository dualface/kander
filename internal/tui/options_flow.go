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
	for _, line := range flow.Build(p.session.Config) {
		if line.Kind == flow.Assignment && line.Args[len(line.Args)-1] == "" {
			line.Args[len(line.Args)-1] = t("flow.cli_default")
		}
		text := t(line.Key, line.Args...)
		level := menu.LevelInfo
		if line.Kind == flow.Heading {
			if len(lines) > 0 {
				lines = append(lines, menu.ReportLine{})
			}
			level = menu.LevelNote
		} else {
			text = "  " + text
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
