package tui

import (
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/flow"
	"github.com/dualface/kander/internal/menu"
)

func (p *optionsPanel) openFlow() {
	if p.session == nil {
		return
	}
	if p.flowScale == "" {
		p.flowScale = "large"
	}
	p.refreshFlowReport()
	p.form = nil
}

func (p *optionsPanel) refreshFlowReport() {
	if p.session == nil {
		return
	}
	if p.flowScale != "large" && p.flowScale != "small" {
		p.flowScale = "large"
	}
	chart := flow.BuildChart(p.session.Config, p.flowScale)
	width := p.innerWidth()
	if width < 24 {
		width = 24
	}
	body := renderFlowChart(chart, width, newFlowText())
	lines := make([]menu.ReportLine, 0, len(body)+3)
	for _, row := range flowScaleTabs(p.flowScale) {
		lines = append(lines, menu.ReportLine{Level: menu.LevelNote, Text: row})
	}
	lines = append(lines, menu.ReportLine{})
	for _, row := range body {
		lines = append(lines, menu.ReportLine{Level: menu.LevelInfo, Text: row})
	}
	title := t("flow.title")
	if p.dirty {
		title += t("tui.unsaved")
	}
	p.showReport(title, lines, "")
}

func (p *optionsPanel) cycleFlowScale(delta int) {
	scales := config.TaskScales
	index := 0
	for i, scale := range scales {
		if scale == p.flowScale {
			index = i
			break
		}
	}
	index = (index + delta) % len(scales)
	if index < 0 {
		index += len(scales)
	}
	p.flowScale = scales[index]
	p.refreshFlowReport()
}

func flowScaleTabs(active string) []string {
	labels := []string{
		t("tui.review_group_large"),
		t("tui.review_group_small"),
	}
	scales := config.TaskScales
	parts := make([]string, 0, len(scales))
	for i, scale := range scales {
		label := labels[i]
		if scale == active {
			parts = append(parts, "▸ "+label)
			continue
		}
		parts = append(parts, "  "+label)
	}
	return []string{strings.Join(parts, "   ")}
}
