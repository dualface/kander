package tui

import (
	"strings"

	"github.com/dualface/kander/internal/flow"
)

// flowMaxWidth is the diagram width ceiling from the collaboration rules.
const flowMaxWidth = 100

// renderFlowChart draws the chart as a vertical spine of phase boxes with the
// gates hanging off it and every Exit.Target routed as a real edge through the
// side gutters. When the edges do not fit, the compact form names each edge's
// target instead of drawing it.
func renderFlowChart(chart flow.Chart, width int) []string {
	limit := width
	if limit > flowMaxWidth {
		limit = flowMaxWidth
	}
	if limit < 24 {
		limit = 24
	}
	if lines, ok := layoutFlowChart(chart, limit, false); ok {
		return lines
	}
	lines, _ := layoutFlowChart(chart, limit, true)
	return lines
}

// flowEdge is one drawn connection from a gate exit to a phase box.
type flowEdge struct {
	origin int // row of the exit line
	from   int // column just right of the edge's horizontal run
	to     string
	back   bool // true when the target sits above the origin
	lane   int
}

type flowBuilder struct {
	canvas   *flowCanvas
	spine    int
	row      int
	offset   int // x of the content area
	avail    int
	order    map[string]int
	phaseMid map[string]int
	phaseBox map[string][2]int // left, right column of the box
	edges    []flowEdge
	compact  bool
}

func layoutFlowChart(chart flow.Chart, limit int, compact bool) ([]string, bool) {
	if len(chart.Phases) == 0 {
		return nil, true
	}
	back, forward := countFlowEdges(chart)
	left, right := 0, 0
	if !compact {
		if back > 0 {
			left = 2*back + 2
		}
		if forward > 0 {
			right = 2*forward + 2
		}
	}
	// Routed edges are worth their gutters only while the labels stay readable;
	// below that the compact form carries more of the text.
	avail := limit - left - right
	minimum := 56
	if compact {
		minimum = 20
	}
	if avail < minimum {
		return nil, false
	}

	boxes := make([][]string, len(chart.Phases))
	spine := 0
	for i, phase := range chart.Phases {
		boxes[i] = flowPhaseBox(phase, avail)
		spine = max(spine, linesWidth(boxes[i])/2)
	}

	order := map[string]int{}
	for i, phase := range chart.Phases {
		order[phase.Key] = i
	}
	b := &flowBuilder{
		canvas:   &flowCanvas{},
		spine:    spine,
		offset:   left,
		avail:    avail,
		order:    order,
		phaseMid: map[string]int{},
		phaseBox: map[string][2]int{},
		compact:  compact,
	}
	for i, phase := range chart.Phases {
		if i > 0 {
			b.arrow()
		}
		b.placeBox(phase.Key, boxes[i])
		for _, note := range phase.Notes {
			b.note(t("flow." + note))
		}
		next := ""
		if i+1 < len(chart.Phases) {
			next = chart.Phases[i+1].Key
		}
		for _, gate := range phase.Gates {
			b.arrow()
			b.gate(gate, phase.Key, next)
		}
	}
	lines, ok := b.finish(limit, left)
	return lines, ok || compact
}

// countFlowEdges counts the drawn edges: an exit whose target is the next phase
// simply falls through and needs no edge.
func countFlowEdges(chart flow.Chart) (back, forward int) {
	order := map[string]int{}
	for i, phase := range chart.Phases {
		order[phase.Key] = i
	}
	for i, phase := range chart.Phases {
		next := ""
		if i+1 < len(chart.Phases) {
			next = chart.Phases[i+1].Key
		}
		for _, gate := range phase.Gates {
			for _, exit := range gate.Exits {
				if !flowEdgeDrawn(exit, next, order) {
					continue
				}
				if order[exit.Target] <= i {
					back++
					continue
				}
				forward++
			}
		}
	}
	return back, forward
}

func flowEdgeDrawn(exit flow.Exit, next string, order map[string]int) bool {
	if exit.Target == "" || exit.Target == next {
		return false
	}
	_, ok := order[exit.Target]
	return ok
}

func (b *flowBuilder) put(x, y int, text string) {
	b.canvas.put(b.offset+x, y, text)
}

func (b *flowBuilder) arrow() {
	b.put(b.spine, b.row, "│")
	b.put(b.spine, b.row+1, "▼")
	b.row += 2
}

func (b *flowBuilder) placeBox(key string, box []string) {
	left := b.spine - linesWidth(box)/2
	for i, line := range box {
		b.put(left, b.row+i, line)
	}
	b.phaseMid[key] = b.row + len(box)/2
	b.phaseBox[key] = [2]int{b.offset + left, b.offset + left + linesWidth(box) - 1}
	b.row += len(box)
}

func (b *flowBuilder) note(text string) {
	for _, row := range wrapFlowNote("· "+text, b.avail) {
		b.put(0, b.row, row)
		b.row++
	}
}

// gate lists one decision and its exits. The drawn layout hangs the tree off
// the spine; the compact layout starts at the left edge, because there the
// exit lines also carry each edge's target name.
func (b *flowBuilder) gate(gate flow.Gate, phase, next string) {
	x := b.spine
	if b.compact {
		x = 0
	}
	room := b.avail - x
	b.put(x, b.row, clipText("◆ "+t("flow."+gate.Key), room))
	b.row++
	for i, exit := range gate.Exits {
		mark := "├─ "
		if i == len(gate.Exits)-1 {
			mark = "└─ "
		}
		label := t("flow." + exit.Key)
		if exit.User {
			label += " " + t("flow.user_exit")
		}
		drawn := flowEdgeDrawn(exit, next, b.order)
		target := ""
		if drawn && b.compact {
			// The named target replaces the drawn edge, so it is never clipped
			// away: the label yields room to it, and takes its own line when
			// even that is not enough.
			target = " → " + t("flow.phase_"+exit.Target)
			if fit := room - displayWidth(mark) - displayWidth(target); fit >= 8 {
				label, target = clipText(label, fit)+target, ""
			}
		}
		b.put(x, b.row, clipText(mark+label, room))
		if drawn && !b.compact {
			b.edges = append(b.edges, flowEdge{
				origin: b.row,
				from:   b.offset + x,
				to:     exit.Target,
				back:   b.order[exit.Target] <= b.order[phase],
			})
		}
		b.row++
		if target != "" {
			b.put(x, b.row, clipText("  "+target, room))
			b.row++
		}
		for _, step := range exit.Steps {
			b.put(x, b.row, clipText("   ▶ "+t("flow."+step), room))
			b.row++
		}
	}
}

// finish routes the recorded edges through the gutters and renders the canvas.
func (b *flowBuilder) finish(limit, left int) ([]string, bool) {
	content := 0
	for _, row := range b.canvas.rows {
		content = max(content, len(row))
	}
	b.assignLanes()
	for _, edge := range b.edges {
		target, ok := b.phaseMid[edge.to]
		if !ok {
			continue
		}
		box := b.phaseBox[edge.to]
		if edge.back {
			b.drawBackEdge(edge, target, box[0], left)
			continue
		}
		b.drawForwardEdge(edge, target, box[1], content)
	}
	used := 0
	for _, row := range b.canvas.rows {
		used = max(used, len(row))
	}
	lines := make([]string, len(b.canvas.rows))
	for i, row := range b.canvas.rows {
		lines[i] = strings.TrimRight(strings.Join(row, ""), " ")
	}
	return lines, used <= limit
}

// assignLanes gives the shortest edges the lanes nearest the content so nested
// edges do not cross.
func (b *flowBuilder) assignLanes() {
	spans := make([]int, len(b.edges))
	for i, edge := range b.edges {
		target := b.phaseMid[edge.to]
		span := edge.origin - target
		if span < 0 {
			span = -span
		}
		spans[i] = span
	}
	backLane, forwardLane := 0, 0
	for _, order := range flowLaneOrder(spans) {
		if b.edges[order].back {
			b.edges[order].lane = backLane
			backLane++
			continue
		}
		b.edges[order].lane = forwardLane
		forwardLane++
	}
}

// flowLaneOrder returns indexes sorted by span, shortest first, stable.
func flowLaneOrder(spans []int) []int {
	order := make([]int, len(spans))
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && spans[order[j]] < spans[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	return order
}

func (b *flowBuilder) drawBackEdge(edge flowEdge, target, boxLeft, left int) {
	lane := left - 2 - 2*edge.lane
	if lane < 0 {
		return
	}
	b.canvas.draw(lane, edge.origin, "└")
	for x := lane + 1; x < edge.from; x++ {
		b.canvas.draw(x, edge.origin, "─")
	}
	for y := target + 1; y < edge.origin; y++ {
		b.canvas.draw(lane, y, "│")
	}
	b.canvas.draw(lane, target, "┌")
	for x := lane + 1; x < boxLeft-1; x++ {
		b.canvas.draw(x, target, "─")
	}
	b.canvas.draw(boxLeft-1, target, "▶")
}

func (b *flowBuilder) drawForwardEdge(edge flowEdge, target, boxRight, content int) {
	lane := content + 1 + 2*edge.lane
	end := b.canvas.rowEnd(edge.origin) + 1
	for x := end + 1; x < lane; x++ {
		b.canvas.draw(x, edge.origin, "─")
	}
	b.canvas.draw(lane, edge.origin, "┐")
	for y := edge.origin + 1; y < target; y++ {
		b.canvas.draw(lane, y, "│")
	}
	b.canvas.draw(lane, target, "┘")
	for x := boxRight + 2; x < lane; x++ {
		b.canvas.draw(x, target, "─")
	}
	b.canvas.draw(boxRight+1, target, "◀")
}

// flowPhaseBox frames one phase: its name as the title, the agent and model
// when it has one, and the stage policy when it is a review role.
func flowPhaseBox(phase flow.Phase, width int) []string {
	title := t("flow.phase_" + phase.Key)
	var body []string
	if !phase.Node.IsZero() {
		body = append(body, clipText(flowNodeLabel(phase.Node), width-4))
	}
	if phase.Node.Mode != "" {
		body = append(body, clipText(t("flow.mode_"+phase.Node.Mode), width-4))
	}
	if len(body) == 0 {
		body = []string{""}
	}
	return flowFrame(clipText(title, width-7), body)
}

func flowNodeLabel(node flow.Node) string {
	model := node.Model
	if model == "" {
		model = t("flow.cli_default")
	}
	label := model
	if node.Agent != "" {
		label = node.Agent + " · " + model
	}
	if node.Effort == "" {
		return label
	}
	return label + " (" + node.Effort + ")"
}

// wrapFlowNote breaks a footnote on spaces. A note with no spaces, or a single
// word wider than width, falls back to a rune wrap.
func wrapFlowNote(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	if displayWidth(text) <= width {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) <= 1 {
		return wrapText(text, width)
	}
	var rows []string
	var current []string
	used := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		rows = append(rows, strings.Join(current, " "))
		current = nil
		used = 0
	}
	for _, word := range words {
		wordWidth := displayWidth(word)
		if wordWidth > width {
			flush()
			rows = append(rows, wrapText(word, width)...)
			continue
		}
		gap := 0
		if len(current) > 0 {
			gap = 1
		}
		if len(current) > 0 && used+gap+wordWidth > width {
			flush()
		}
		current = append(current, word)
		used += gap + wordWidth
	}
	flush()
	return rows
}

// flowFrame draws a border around lines, centering each one, with an optional
// title in the top border.
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

// draw writes one edge glyph without overwriting content; a horizontal run that
// meets a lane becomes a crossing.
func (c *flowCanvas) draw(x, y int, glyph string) {
	if x < 0 || y < 0 {
		return
	}
	for len(c.rows) <= y {
		c.rows = append(c.rows, nil)
	}
	row := c.rows[y]
	for len(row) <= x {
		row = append(row, " ")
	}
	switch {
	case row[x] == " " || row[x] == "":
		row[x] = glyph
	case glyph == "─" && row[x] == "│":
		row[x] = "┼"
	case glyph == "│" && row[x] == "─":
		row[x] = "┼"
	case glyph == "─" && (row[x] == "┌" || row[x] == "┘"),
		glyph == "┌" && row[x] == "─",
		glyph == "┘" && row[x] == "─":
		// Several edges reaching the same box share one horizontal run.
		row[x] = "┬"
	}
	c.rows[y] = row
}

// rowEnd returns the last occupied column of a row, or -1 when it is empty.
func (c *flowCanvas) rowEnd(y int) int {
	if y < 0 || y >= len(c.rows) {
		return -1
	}
	for x := len(c.rows[y]) - 1; x >= 0; x-- {
		if cell := c.rows[y][x]; cell != " " && cell != "" {
			return x
		}
	}
	return -1
}
