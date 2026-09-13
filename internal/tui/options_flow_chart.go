package tui

import (
	"strings"

	"github.com/dualface/kander/internal/flow"
)

// flowText carries the localized labels of the flowchart so rendering stays pure.
type flowText struct {
	execute    string
	selfCheck  string
	done       string
	reviewOff  string
	cliDefault string
	required   string
	auto       string
	stageNA    string
	yes        string
	no         string
	fix        string
	rereview   string
	decision   string
	stageNames map[string]string
	gates      map[string]string
}

func newFlowText() flowText {
	return flowText{
		execute:    t("flow.stage_execute"),
		selfCheck:  t("flow.stage_self_check"),
		done:       t("flow.stage_done"),
		reviewOff:  t("flow.review_off"),
		cliDefault: t("flow.cli_default"),
		required:   t("flow.mode_required"),
		auto:       t("flow.mode_auto"),
		stageNA:    t("flow.stage_na"),
		yes:        t("flow.yes"),
		no:         t("flow.no"),
		fix:        t("flow.stage_fix"),
		rereview:   t("flow.stage_rereview"),
		decision:   t("flow.stage_user_decision"),
		stageNames: map[string]string{
			flow.StagePrimary:  t("flow.stage_primary"),
			flow.StageSecurity: t("flow.stage_security"),
		},
		gates: map[string]string{
			flow.StagePrimary:  t("flow.gate_primary"),
			flow.StageSecurity: t("flow.gate_security"),
		},
	}
}

// renderFlowChart draws execution, self-check, both review stages with their
// fix and incremental re-review loops, and completion. Loops are drawn as a
// rail on the right; when the rail does not fit the width, each loop becomes a
// short indented branch under its gate instead.
func renderFlowChart(chart flow.Chart, width int, text flowText) []string {
	if lines, ok := layoutFlowChart(chart, width, text, false); ok {
		return lines
	}
	lines, _ := layoutFlowChart(chart, width, text, true)
	return lines
}

func layoutFlowChart(chart flow.Chart, width int, text flowText, compact bool) ([]string, bool) {
	main := [][]string{titledBox(text.execute, nodeLabel(chart.Execution, text.cliDefault), width)}
	var stageBoxes [][]string
	if !chart.ReviewDisabled {
		main = append(main, labelBox(text.selfCheck, width))
		for _, stage := range chart.Stages {
			box := stageBox(stage, width, text)
			stageBoxes = append(stageBoxes, box)
			main = append(main, box)
		}
	}
	main = append(main, labelBox(text.done, width))
	spine := 0
	for _, box := range main {
		spine = max(spine, linesWidth(box)/2)
	}

	c := &flowCanvas{}
	y := c.center(spine, 0, main[0])
	if chart.ReviewDisabled {
		c.put(spine, y, "│")
		c.put(spine+2, y, text.reviewOff)
		c.put(spine, y+1, "▼")
		c.center(spine, y+2, main[len(main)-1])
		return c.finish(width)
	}
	y = c.arrow(spine, y)
	y = c.center(spine, y, main[1])
	for i, stage := range chart.Stages {
		y = c.arrow(spine, y)
		top := y
		box := stageBoxes[i]
		y = c.center(spine, y, box)
		if len(stage.Nodes) == 0 {
			continue
		}
		c.put(spine, y, "│")
		y++
		steps := []string{text.fix, text.rereview}
		if stage.Name == flow.StageSecurity {
			steps = append([]string{text.decision}, steps...)
		}
		loop := flowLoop{
			spine:      spine,
			gate:       text.gates[stage.Name],
			yes:        text.yes,
			no:         text.no,
			steps:      steps,
			entryY:     top + len(box)/2,
			entryRight: spine - linesWidth(box)/2 + linesWidth(box),
		}
		if compact {
			y = loop.drawCompact(c, y, text.stageNames[stage.Name], width)
		} else {
			y = loop.drawRail(c, y, width)
		}
	}
	y = c.arrow(spine, y)
	c.center(spine, y, main[len(main)-1])
	lines, ok := c.finish(width)
	return lines, ok || compact
}

// flowLoop is one gate with its "yes" branch that returns to the stage entry.
type flowLoop struct {
	spine      int
	gate       string
	yes        string
	no         string
	steps      []string
	entryY     int // stage box row the loop arrow re-enters
	entryRight int // first column right of the stage box
}

func (l flowLoop) drawRail(c *flowCanvas, y, width int) int {
	boxes := make([][]string, len(l.steps))
	branchWidth := 0
	for i, step := range l.steps {
		boxes[i] = labelBox(step, width)
		branchWidth = max(branchWidth, linesWidth(boxes[i]))
	}
	yes := "─ " + l.yes + " ─"
	branch := max(l.spine+2+displayWidth(l.gate)+1+displayWidth(yes)+1, l.spine+displayWidth(l.no)+4+branchWidth/2)
	rail := max(l.entryRight, branch-branchWidth/2+branchWidth) + 2

	c.put(l.spine, y, "◆ "+l.gate+" ")
	x := l.spine + 2 + displayWidth(l.gate) + 1
	c.put(x, y, yes)
	c.put(x+displayWidth(yes), y, strings.Repeat("─", branch-x-displayWidth(yes))+"┐")
	start := y
	y++
	c.put(branch, y, "▼")
	y++
	for i, box := range boxes {
		if i > 0 {
			c.put(branch, y, "│")
			c.put(branch, y+1, "▼")
			y += 2
		}
		y = c.center(branch, y, box)
	}
	c.put(branch, y, "└"+strings.Repeat("─", rail-branch-1)+"┘")
	for row := l.entryY + 1; row < y; row++ {
		c.put(rail, row, "│")
	}
	c.put(l.entryRight, l.entryY, "◀"+strings.Repeat("─", rail-l.entryRight-1)+"┐")
	for row := start + 1; row <= y; row++ {
		c.put(l.spine, row, "│")
	}
	c.put(l.spine+2, start+1, l.no)
	return y
}

// drawCompact lists the loop under the gate so it fits narrow widths:
// each step is one line, followed by the stage the loop returns to.
func (l flowLoop) drawCompact(c *flowCanvas, y int, back string, width int) int {
	c.put(l.spine, y, "◆ "+clipText(l.gate, width-l.spine-2))
	c.put(l.spine, y+1, "├─ "+l.yes)
	rows := make([]string, 0, len(l.steps)+1)
	for _, step := range l.steps {
		rows = append(rows, "▶ "+step)
	}
	rows = append(rows, "↺ "+back)
	for i, row := range rows {
		c.put(l.spine, y+2+i, "│")
		c.put(l.spine+3, y+2+i, clipText(row, width-l.spine-3))
	}
	y += 2 + len(rows)
	c.put(l.spine, y, "│")
	c.put(l.spine+2, y, l.no)
	return y
}

// stageBox frames one review stage, titled with its name, around its parallel
// role boxes (side by side when they fit, stacked otherwise).
func stageBox(stage flow.Stage, width int, text flowText) []string {
	name := text.stageNames[stage.Name]
	if len(stage.Nodes) == 0 {
		return titledBox(name, text.stageNA, width)
	}
	roles := make([][]string, len(stage.Nodes))
	for i, node := range stage.Nodes {
		mode := text.auto
		if node.Mode == "required" {
			mode = text.required
		}
		roles[i] = titledBox(node.Role+" · "+mode, nodeLabel(node, text.cliDefault), width-4)
	}
	const gap = 3
	rowWidth := gap * (len(roles) - 1)
	for _, box := range roles {
		rowWidth += linesWidth(box)
	}
	var inner []string
	if rowWidth+4 <= width {
		for row := range roles[0] {
			parts := make([]string, len(roles))
			for i, box := range roles {
				parts[i] = box[row]
			}
			inner = append(inner, strings.Join(parts, strings.Repeat(" ", gap)))
		}
	} else {
		for _, box := range roles {
			inner = append(inner, box...)
		}
	}
	return flowFrame(clipText(name, width-7), inner)
}

func titledBox(title, body string, width int) []string {
	return flowFrame(clipText(title, width-7), []string{clipText(body, width-4)})
}

func labelBox(label string, width int) []string {
	return flowFrame("", []string{clipText(label, width-4)})
}

// frame draws a border around lines, centering each one, with an optional title
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
