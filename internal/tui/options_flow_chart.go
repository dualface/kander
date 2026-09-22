package tui

import (
	"strings"

	"github.com/dualface/kander/internal/flow"
)

// flowText keeps rendering independent of the active locale.
type flowText struct {
	plan, execute, integrate, done                  string
	cliDefault, required, auto                      string
	fix, securityFix, needsFix, decideFix, rereview string
	reviewOff, note                                 string
}

func newFlowText() flowText {
	return flowText{
		plan:        t("flow.stage_plan"),
		execute:     t("flow.stage_execute"),
		integrate:   t("flow.stage_integrate"),
		done:        t("flow.stage_done"),
		cliDefault:  t("flow.cli_default"),
		required:    t("flow.mode_required"),
		auto:        t("flow.mode_auto"),
		fix:         t("flow.stage_fix"),
		securityFix: t("flow.stage_security_fix"),
		needsFix:    t("flow.needs_fix"),
		decideFix:   t("flow.decide_fix"),
		rereview:    t("flow.stage_rereview"),
		reviewOff:   t("flow.review_off"),
		note:        t("flow.note"),
	}
}

type flowStep struct {
	box                  []string
	role, fix, condition string
}

// renderFlowChart shows the main path and an independent repair loop for each
// enabled review role. Detailed gates remain in the rules, outside this view.
func renderFlowChart(chart flow.Chart, width int, text flowText) []string {
	width = max(width, 24)
	lines, ok := layoutFlowChart(chart, width, text, false)
	if !ok {
		lines, _ = layoutFlowChart(chart, width, text, true)
	}
	lines = append(lines, "")
	if chart.ReviewDisabled {
		lines = append(lines, wrapText(text.reviewOff, width)...)
	}
	return append(lines, wrapText(text.note, width)...)
}

func layoutFlowChart(chart flow.Chart, width int, text flowText, compact bool) ([]string, bool) {
	var steps []flowStep
	if chart.ConfirmPlan {
		steps = append(steps, flowStep{box: labelBox(text.plan, width)})
	}
	steps = append(steps, flowStep{box: titledBox(text.execute, nodeLabel(chart.Execution, text.cliDefault), width)})
	if !chart.ReviewDisabled {
		for _, stage := range chart.Stages {
			for _, node := range stage.Nodes {
				mode := text.auto
				if node.Mode == "required" {
					mode = text.required
				}
				step := flowStep{
					box:       titledBox(node.Role+" · "+mode, nodeLabel(node, text.cliDefault), width),
					role:      node.Role,
					fix:       text.fix,
					condition: text.needsFix,
				}
				if stage.Name == flow.StageSecurity {
					step.fix, step.condition = text.securityFix, text.decideFix
				}
				steps = append(steps, step)
			}
		}
	}
	if chart.Integrate {
		steps = append(steps, flowStep{box: labelBox(text.integrate, width)})
	}
	steps = append(steps, flowStep{box: labelBox(text.done, width)})
	spine := 0
	for _, step := range steps {
		spine = max(spine, linesWidth(step.box)/2)
	}
	canvas := &flowCanvas{}
	y := 0
	for i, step := range steps {
		if i > 0 {
			y = canvas.arrow(spine, y)
		}
		top := y
		y = canvas.center(spine, y, step.box)
		if step.role == "" {
			continue
		}
		loop := flowLoop{
			spine:      spine,
			entryY:     top + len(step.box)/2,
			entryRight: spine - linesWidth(step.box)/2 + linesWidth(step.box),
			condition:  step.condition,
			fix:        step.fix,
			rereview:   text.rereview,
			role:       step.role,
		}
		if compact {
			y = loop.drawCompact(canvas, y, width)
		} else {
			y = loop.drawRail(canvas, y, width)
		}
	}
	return canvas.finish(width)
}

// flowLoop returns only to its own review node. The main path continues below it.
type flowLoop struct {
	spine, entryY, entryRight      int
	condition, fix, rereview, role string
}

func (l flowLoop) drawRail(c *flowCanvas, y, width int) int {
	box := labelBox(l.fix, width)
	boxWidth := linesWidth(box)
	exit := "├─ " + l.condition + " ─"
	branch := max(l.spine+displayWidth(exit)+1, l.spine+3+boxWidth/2)
	back := "◀ " + l.rereview + " "
	rail := max(branch-boxWidth/2+boxWidth+2, l.entryRight+displayWidth(back)+1)
	c.put(l.spine, y, exit+strings.Repeat("─", branch-l.spine-displayWidth(exit))+"┐")
	start := y
	c.put(branch, y+1, "▼")
	y = c.center(branch, y+2, box)
	c.put(branch, y, "└"+strings.Repeat("─", rail-branch-1)+"┘")
	for row := l.entryY + 1; row < y; row++ {
		c.put(rail, row, "│")
	}
	c.put(l.entryRight, l.entryY, back+strings.Repeat("─", rail-l.entryRight-displayWidth(back))+"┐")
	for row := start + 1; row <= y; row++ {
		c.put(l.spine, row, "│")
	}
	return y + 1
}

// Compact branches wrap their labels and keep the return role explicit, so a
// narrow viewport never loses the destination or clips a wide character.
func (l flowLoop) drawCompact(c *flowCanvas, y, width int) int {
	first := true
	for _, label := range []string{l.condition, l.fix, l.rereview, "↺ " + l.role} {
		for _, line := range wrapText(label, width-l.spine-3) {
			if first {
				c.put(l.spine, y, "├─")
				first = false
			} else {
				c.put(l.spine, y, "│")
			}
			c.put(l.spine+3, y, line)
			y++
		}
	}
	return y
}

func titledBox(title, body string, width int) []string {
	return flowFrame(clipText(title, width-7), []string{clipText(body, width-4)})
}

func labelBox(label string, width int) []string {
	return flowFrame("", []string{clipText(label, width-4)})
}

// flowFrame draws a border around lines, centering each one, with an optional title
// in the top border.
func flowFrame(title string, lines []string) []string {
	inner := linesWidth(lines)
	if title != "" {
		inner = max(inner, displayWidth(title)+2)
	}
	inner = max(inner, 1)
	top := "┌" + strings.Repeat("─", inner+2) + "┐"
	if title != "" {
		top = "┌─ " + title + " " + strings.Repeat("─", inner-1-displayWidth(title)) + "┐"
	}
	out := []string{top}
	for _, line := range lines {
		pad := inner - displayWidth(line)
		out = append(out, "│ "+strings.Repeat(" ", pad/2)+line+strings.Repeat(" ", pad-pad/2)+" │")
	}
	return append(out, "└"+strings.Repeat("─", inner+2)+"┘")
}

func linesWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, displayWidth(line))
	}
	return width
}

func nodeLabel(node flow.Node, cliDefault string) string {
	model := node.Model
	if model == "" {
		model = cliDefault
	}
	if node.Effort == "" {
		return model
	}
	return model + " (" + node.Effort + ")"
}

// flowCanvas is a sparse character grid; a wide rune occupies its cell and an
// empty continuation cell.
type flowCanvas struct {
	rows [][]string
}

func (c *flowCanvas) put(x, y int, text string) {
	for len(c.rows) <= y {
		c.rows = append(c.rows, nil)
	}
	row := c.rows[y]
	for _, r := range text {
		w := runeDisplayWidth(r)
		if w == 0 {
			continue
		}
		for len(row) < x+w {
			row = append(row, " ")
		}
		if row[x] == "" && x > 0 {
			row[x-1] = " "
		}
		row[x] = string(r)
		for i := 1; i < w; i++ {
			row[x+i] = ""
		}
		if next := x + w; next < len(row) && row[next] == "" {
			row[next] = " "
		}
		x += w
	}
	c.rows[y] = row
}

// center draws a block centered on column x starting at row y and returns the next free row.
func (c *flowCanvas) center(x, y int, block []string) int {
	left := x - linesWidth(block)/2
	for i, line := range block {
		c.put(left, y+i, line)
	}
	return y + len(block)
}

func (c *flowCanvas) arrow(x, y int) int {
	c.put(x, y, "│")
	c.put(x, y+1, "▼")
	return y + 2
}

// finish centers the drawing in width and reports whether it fits.
func (c *flowCanvas) finish(width int) ([]string, bool) {
	used := 0
	for _, row := range c.rows {
		used = max(used, len(row))
	}
	pad := strings.Repeat(" ", max(0, (width-used)/2))
	lines := make([]string, len(c.rows))
	for i, row := range c.rows {
		lines[i] = strings.TrimRight(pad+strings.Join(row, ""), " ")
	}
	return lines, used <= width
}
