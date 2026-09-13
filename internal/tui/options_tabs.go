package tui

import (
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/version"
)

type tabHit struct {
	target string
	x0     int
	x1     int
}

func (p *optionsPanel) canCycleTabs() bool {
	return p.session != nil && !p.confirming && !p.restoreConfirming && p.report == nil && len(p.session.AvailableTargets()) > 1
}

func tabLabel(target string) string {
	if target == config.TargetOverlay {
		return t("tui.tab_project")
	}
	return t("tui.tab_global")
}

func (p *optionsPanel) scopeTabHint() string {
	if !p.canCycleTabs() {
		return ""
	}
	return t("tui.switch_scope_tabs")
}

func (p *optionsPanel) cycleTab(delta int) tea.Cmd {
	if !p.canCycleTabs() {
		return nil
	}
	if p.bind != nil {
		p.bind.apply(p)
	}
	targets := p.session.AvailableTargets()
	current := p.session.Target
	if current == "" {
		current = config.TargetScope
	}
	index := slices.Index(targets, current)
	if index < 0 {
		index = 0
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
	focusKey, focusIndex := p.currentFocus()
	if err := p.session.SetTarget(target); err != nil {
		p.showReport(t("tui.load_failed"), nil, err.Error())
		return nil
	}
	p.syncAppFromSession()
	p.dirty = p.session.HasUnsaved()
	if p.current == "" {
		return p.openRoot()
	}
	cmd := p.openSection(p.current)
	return tea.Batch(cmd, p.restoreFocus(focusKey, focusIndex))
}

// currentFocus returns the focused field's stable key (when registered) and its
// focusable index, so a Global/Project rebuild can put the cursor back.
func (p *optionsPanel) currentFocus() (key string, index int) {
	if p.bind == nil || p.form == nil {
		return "", 0
	}
	focused := p.form.GetFocusedField()
	i := 0
	for _, field := range p.bind.formFields {
		if field.Skip() {
			continue
		}
		if field == focused {
			for name, pos := range p.bind.fieldIndex {
				if pos == i {
					return name, i
				}
			}
			return "", i
		}
		i++
	}
	return "", 0
}

// restoreFocus moves to the field named by key after a rebuild, falling back to
// the previous focusable index and clamping when the new page is shorter.
func (p *optionsPanel) restoreFocus(key string, fallback int) tea.Cmd {
	index := fallback
	if p.bind != nil {
		if key != "" {
			if pos, ok := p.bind.fieldIndex[key]; ok {
				index = pos
			}
		}
		if p.bind.focusable > 0 && index >= p.bind.focusable {
			index = p.bind.focusable - 1
		}
	}
	if index < 0 {
		index = 0
	}
	return repeatCmd(index, huh.NextField)
}

const scopeChromeIndent = 2

// tabFrameHeaderRows is the dialog chrome above the body when Global/Project tabs are shown:
// top border, label row, and the joiner into the content pane.
const tabFrameHeaderRows = 3

func (p *optionsPanel) showScopeTabs() bool {
	return p.canCycleTabs()
}

func (p *optionsPanel) renderScopeChrome(palette palette, width int) (string, int) {
	if p.confirming || p.restoreConfirming {
		return "", 0
	}
	indent := scopeChromeIndent
	innerWidth := width - indent
	if innerWidth < 1 {
		indent = 0
		innerWidth = width
	}
	pad := strings.Repeat(" ", indent)
	dim := styleFor("popup-dim", palette)
	var lines []string
	loc := overlayDisplayLocation(p)
	if loc.ProjectRoot != "" {
		lines = append(lines, pad+dim.Render(clipPath(t("tui.project_path", homePath(loc.ProjectRoot)), innerWidth)))
	}
	editingOverlay := p.session != nil && p.session.EditingOverlay()
	if editingOverlay {
		if value := overlayChromeValue(loc); value != "" {
			lines = append(lines, pad+dim.Render(clipPath(t("tui.overlay_file", value), innerWidth)))
		}
	} else if base := overlayBasePath(p); base != "" {
		lines = append(lines, pad+dim.Render(clipPath(t("tui.base_config", homePath(base)), innerWidth)))
	}
	if len(lines) == 0 {
		return "", 0
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n") + "\n", len(lines)
}

// overlayChromeValue is the path shown on the Project tab: a project-relative
// path when the overlay exists, otherwise a short missing placeholder.
func overlayChromeValue(loc config.OverlayLocation) string {
	if loc.Path == "" {
		return ""
	}
	if !loc.Exists {
		return t("tui.overlay_missing")
	}
	if loc.ProjectRoot != "" {
		if rel, err := filepath.Rel(loc.ProjectRoot, loc.Path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(loc.Path)
}

func padTabLabel(label string, width int) string {
	text := " " + label + " "
	for displayWidth(text) < width {
		text += " "
	}
	if displayWidth(text) > width {
		return clipText(text, width)
	}
	return text
}

type tabHeaderMetrics struct {
	targets []string
	labels  []string
	widths  []int
	active  string
	restW   int
	inner   int
}

func (p *optionsPanel) tabHeaderMetrics(inner int, pageName string) tabHeaderMetrics {
	targets := p.session.AvailableTargets()
	active := p.session.Target
	if active == "" {
		active = config.TargetScope
	}
	labels := make([]string, len(targets))
	widths := make([]int, len(targets))
	for i, target := range targets {
		label := tabLabel(target)
		if target == active && pageName != "" {
			label = label + " - " + pageName
		}
		labels[i] = label
		widths[i] = displayWidth(label) + 2
		if widths[i] < 3 {
			widths[i] = 3
		}
	}
	n := len(labels)
	ver := version.String()
	if ver == "" {
		ver = "dev"
	}
	minRest := displayWidth(ver) + 2
	if minRest < 5 {
		minRest = 5
	}
	used := n // one join after each tab cell before the version cell
	for _, w := range widths {
		used += w
	}
	restW := inner - used
	if restW < minRest {
		// Shrink the active tab first so the version cell still fits.
		need := minRest - restW
		for i, target := range targets {
			if target != active || need <= 0 {
				continue
			}
			minW := displayWidth(tabLabel(target)) + 2
			if minW < 3 {
				minW = 3
			}
			shrink := min(need, max(0, widths[i]-minW))
			widths[i] -= shrink
			need -= shrink
			labels[i] = clipText(labels[i], max(1, widths[i]-2))
		}
		used = n
		for _, w := range widths {
			used += w
		}
		restW = inner - used
		if restW < 1 {
			restW = 1
		}
	}
	return tabHeaderMetrics{
		targets: targets,
		labels:  labels,
		widths:  widths,
		active:  active,
		restW:   restW,
		inner:   inner,
	}
}

// renderTabFrameHeader draws the dialog's own top chrome (not an inset box):
//
//	╭──────────────────┬─────────┬─────╮
//	│ Global - Interface│ Project │ ver │
//	├──────────────────┴─────────┴─────┤
//
// The page name is shown only on the active Global/Project tab; the trailing
// cell is the build version alone.
func (p *optionsPanel) renderTabFrameHeader(palette palette, m tabHeaderMetrics) (top, mid, join string) {
	edge := styleFor("popup-edge", palette)
	n := len(m.labels)
	hline := func(first, midJoin, last string) string {
		var b strings.Builder
		b.WriteString(first)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteString(midJoin)
			}
			b.WriteString(strings.Repeat("─", m.widths[i]))
		}
		b.WriteString(midJoin)
		b.WriteString(strings.Repeat("─", m.restW))
		b.WriteString(last)
		return edge.Render(b.String())
	}
	top = hline(borderTopLeft, "┬", borderTopRight)
	join = hline("├", "┴", "┤")

	p.tabHits = nil
	p.tabLabelRow = 1 // relative to the dialog top content row (under the top border)
	var midB strings.Builder
	x := 0
	for i, label := range m.labels {
		midB.WriteString(edge.Render("│"))
		x++
		cell := padTabLabel(label, m.widths[i])
		style := lipgloss.NewStyle().Foreground(palette.Dim).Background(palette.Bg)
		if m.targets[i] == m.active {
			style = lipgloss.NewStyle().Foreground(palette.ChromeFg).Background(palette.ChromeBg).Bold(true)
		}
		midB.WriteString(style.Render(cell))
		p.tabHits = append(p.tabHits, tabHit{target: m.targets[i], x0: x, x1: x + m.widths[i]})
		x += m.widths[i]
	}
	midB.WriteString(edge.Render("│"))
	ver := clipText(version.String(), m.restW)
	rest := styleFor("popup-dim", palette).Render(padTabLabel(ver, m.restW))
	midB.WriteString(padLineFill(rest, m.restW, palette))
	midB.WriteString(edge.Render("│"))
	mid = midB.String()
	return top, mid, join
}

func (p *optionsPanel) hitTab(x, y int) string {
	if y != p.tabLabelRow || len(p.tabHits) == 0 {
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
	if p.confirming || p.restoreConfirming || p.report != nil || !optionsMouseActivate(bstate) || p.session == nil {
		return nil
	}
	// Tabs live in the dialog header, not the body.
	localX := x - p.headerX
	localY := y - p.headerY
	target := p.hitTab(localX, localY)
	if target == "" {
		return nil
	}
	return p.switchTab(target)
}
