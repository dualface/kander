package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
)

type tabHit struct {
	target string
	x0     int
	x1     int
}

func (p *optionsPanel) cycleTab(delta int) tea.Cmd {
	if p.session == nil || p.confirming || p.report != nil {
		return nil
	}
	targets := p.session.AvailableTargets()
	if len(targets) < 2 {
		return nil
	}
	if p.bind != nil {
		p.bind.apply(p)
	}
	current := p.session.Target
	if current == "" {
		current = config.TargetScope
	}
	index := 0
	for i, item := range targets {
		if item == current {
			index = i
			break
		}
	}
	next := targets[(index+delta+len(targets))%len(targets)]
	return p.switchTab(next)
}

func (p *optionsPanel) switchTab(target string) tea.Cmd {
	if p.session == nil || p.session.Target == target {
		return nil
	}
	if p.bind != nil {
		p.bind.apply(p)
	}
	if err := p.session.SetTarget(target); err != nil {
		p.showReport(t("tui.load_failed"), nil, err.Error())
		return nil
	}
	p.syncAppFromSession()
	p.dirty = p.session.HasUnsaved()
	if p.current == "" {
		return p.openRoot()
	}
	return p.openSection(p.current)
}

func (p *optionsPanel) renderScopeChrome(palette palette, width int) (string, int) {
	if p.confirming {
		p.tabHits = nil
		return "", 0
	}
	var lines []string
	if p.session != nil && len(p.session.AvailableTargets()) > 1 {
		lines = append(lines, p.renderTabBar(palette, width))
	}
	loc := overlayDisplayLocation(p)
	if loc.ProjectRoot != "" {
		lines = append(lines, styleFor("popup-dim", palette).Render(clipPath(t("tui.project_path", loc.ProjectRoot), width)))
	}
	if loc.Path != "" {
		lines = append(lines, styleFor("popup-dim", palette).Render(clipPath(t("tui.overlay_file", loc.Path), width)))
	}
	if base := overlayBasePath(p); base != "" {
		lines = append(lines, styleFor("popup-dim", palette).Render(clipPath(t("tui.base_config", base), width)))
	}
	if p.session != nil && p.session.EditingOverlay() && loc.Path != "" && !loc.Exists {
		lines = append(lines, styleFor("popup-dim", palette).Render(clipText(t("tui.overlay_will_create"), width)))
	}
	if len(lines) == 0 {
		return "", 0
	}
	return strings.Join(lines, "\n") + "\n", len(lines)
}

func (p *optionsPanel) renderTabBar(palette palette, width int) string {
	targets := p.session.AvailableTargets()
	active := p.session.Target
	if active == "" {
		active = config.TargetScope
	}
	p.tabHits = nil
	cursor := 0
	var parts []string
	for i, target := range targets {
		label := t("tui.tab_global")
		if target == config.TargetOverlay {
			label = t("tui.tab_project")
		}
		text := " " + label + " "
		style := lipgloss.NewStyle().Foreground(palette.Dim).Background(palette.Bg)
		if target == active {
			style = lipgloss.NewStyle().Foreground(palette.ChromeFg).Background(palette.ChromeBg).Bold(true)
		}
		rendered := style.Render(text)
		p.tabHits = append(p.tabHits, tabHit{target: target, x0: cursor, x1: cursor + displayWidth(text)})
		parts = append(parts, rendered)
		cursor += displayWidth(text)
		if i+1 < len(targets) {
			gap := "  "
			parts = append(parts, styleFor("popup-dim", palette).Render(gap))
			cursor += displayWidth(gap)
		}
	}
	line := strings.Join(parts, "")
	if displayWidth(ansi.Strip(line)) > width {
		return clipText(ansi.Strip(line), width)
	}
	return line
}

func (p *optionsPanel) hitTab(x, y int) string {
	if y != 0 || len(p.tabHits) == 0 {
		return ""
	}
	for _, hit := range p.tabHits {
		if x >= hit.x0 && x < hit.x1 {
			return hit.target
		}
	}
	return ""
}

func (p *optionsPanel) handleTabMouse(x, y, bstate int) tea.Cmd {
	if p.confirming || p.report != nil || !optionsMouseActivate(bstate) || p.session == nil {
		return nil
	}
	localX := x - p.bodyX
	localY := y - p.bodyY
	target := p.hitTab(localX, localY)
	if target == "" {
		return nil
	}
	return p.switchTab(target)
}
