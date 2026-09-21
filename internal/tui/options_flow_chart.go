package tui

import (
	"strings"

	"github.com/dualface/kander/internal/flow"
)

// flowText carries the localized labels of the flowchart so rendering stays pure.
type flowText struct {
	execute        string
	selfCheck      string
	deliveryCheck  string
	unresolved     string
	done           string
	finish         string
	reviewOff      string
	cliDefault     string
	required       string
	auto           string
	stageNA        string
	naOverride     string
	yes            string
	no             string
	pass           string
	gateReview     string
	gateRole       string
	skipLoop       string
	roleSkip       string
	fix            string
	rereview       string
	decision       string
	disposition    string
	unverifiable   string
	mechanical     string
	other          string
	contract       string
	roundCap       string
	otherTiers     string
	accept         string
	stop           string
	stopDelivery   string
	timeout        string
	wait           string
	pmqaCross      string
	securityReturn string
	notePrecedence string
	noteRebase     string
	noteGroup      string
	stageNames     map[string]string
	gates          map[string]string
}

func newFlowText() flowText {
	return flowText{
		execute:        t("flow.stage_execute"),
		selfCheck:      t("flow.stage_self_check"),
		deliveryCheck:  t("flow.stage_delivery_check"),
		unresolved:     t("flow.stage_unresolved"),
		done:           t("flow.stage_done"),
		finish:         t("flow.stage_finish"),
		reviewOff:      t("flow.review_off"),
		cliDefault:     t("flow.cli_default"),
		required:       t("flow.mode_required"),
		auto:           t("flow.mode_auto"),
		stageNA:        t("flow.stage_na"),
		naOverride:     t("flow.na_override"),
		yes:            t("flow.yes"),
		no:             t("flow.no"),
		pass:           t("flow.pass"),
		gateReview:     t("flow.gate_review"),
		gateRole:       t("flow.gate_role"),
		skipLoop:       t("flow.skip_loop"),
		roleSkip:       t("flow.role_skip"),
		fix:            t("flow.stage_fix"),
		rereview:       t("flow.stage_rereview"),
		decision:       t("flow.stage_user_decision"),
		disposition:    t("flow.branch_disposition"),
		unverifiable:   t("flow.branch_unverifiable"),
		mechanical:     t("flow.branch_mechanical"),
		other:          t("flow.branch_other"),
		contract:       t("flow.branch_contract"),
		roundCap:       t("flow.branch_round_cap"),
		otherTiers:     t("flow.branch_other_tiers"),
		accept:         t("flow.branch_accept"),
		stop:           t("flow.branch_stop"),
		stopDelivery:   t("flow.branch_stop_delivery"),
		timeout:        t("flow.branch_timeout"),
		wait:           t("flow.branch_wait"),
		pmqaCross:      t("flow.branch_pmqa"),
		securityReturn: t("flow.branch_security_return"),
		notePrecedence: t("flow.note_precedence"),
		noteRebase:     t("flow.note_rebase"),
		noteGroup:      t("flow.note_group"),
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

// renderFlowChart draws execution and, when review is on, the whitelist, the
// delivery check, both review stages with their rule exits, unresolved items,
// and finish. Wide widths keep branch text; narrow widths clip it.
func renderFlowChart(chart flow.Chart, width int, text flowText) []string {
	if lines, ok := layoutFlowChart(chart, width, text, false); ok {
		return lines
	}
	lines, _ := layoutFlowChart(chart, width, text, true)
	return lines
}

func layoutFlowChart(chart flow.Chart, width int, text flowText, compact bool) ([]string, bool) {
	doneLabel := text.done
	if !chart.Integrate {
		doneLabel = text.finish
	}
	selfLabel := text.selfCheck
	if !chart.DeliveryFull {
		selfLabel = text.deliveryCheck
	}
	main := [][]string{titledBox(text.execute, nodeLabel(chart.Execution, text.cliDefault), width)}
	var stageBoxes [][]string
	if !chart.ReviewDisabled {
		main = append(main, labelBox(selfLabel, width))
		for _, stage := range chart.Stages {
			box := stageBox(stage, width, text)
			stageBoxes = append(stageBoxes, box)
			main = append(main, box)
		}
		main = append(main, labelBox(text.unresolved, width))
	}
	main = append(main, labelBox(doneLabel, width))
	spine := 0
	for _, box := range main {
		spine = max(spine, linesWidth(box)/2)
	}

	c := &flowCanvas{}
	y := c.center(spine, 0, main[0])
	if chart.ReviewDisabled {
		c.put(spine, y, "│")
		note := text.reviewOff
		if compact {
			note = clipText(note, width-(spine+2))
		}
		c.put(spine+2, y, note)
		c.put(spine, y+1, "▼")
		c.center(spine, y+2, main[len(main)-1])
		return c.finish(width)
	}
	y = c.arrow(spine, y)
	y = drawChoices(c, y, spine, width, compact, text.gateReview, []flowChoice{
		{head: text.skipLoop},
		{head: text.yes},
	}, "")
	y = c.arrow(spine, y)
	y = c.center(spine, y, main[1])
	pmqaOn := stageHasNodes(chart, flow.StagePrimary)
	for i, stage := range chart.Stages {
		y = c.arrow(spine, y)
		y = c.center(spine, y, stageBoxes[i])
		if len(stage.Nodes) == 0 {
			continue
		}
		if stageAuto(stage) {
			y = c.arrow(spine, y)
			y = drawChoices(c, y, spine, width, compact, text.gateRole, []flowChoice{
				{head: text.roleSkip},
				{head: text.yes},
			}, "")
		}
		y = c.arrow(spine, y)
		y = drawChoices(c, y, spine, width, compact, text.gates[stage.Name], stageChoices(stage, text, pmqaOn, chart.Integrate), text.pass)
	}
	y = c.arrow(spine, y)
	y = c.center(spine, y, main[len(main)-2])
	y = c.arrow(spine, y)
	y = c.center(spine, y, main[len(main)-1])
	y = drawFlowNotes(c, y, width, flowNotes(chart, text))
	lines, ok := c.finish(width)
	return lines, ok || compact
}

// flowChoice is one labeled exit under a gate. lines are the steps of that exit.
type flowChoice struct {
	head  string
	lines []string
}

func stageHasNodes(chart flow.Chart, name string) bool {
	for _, stage := range chart.Stages {
		if stage.Name == name && len(stage.Nodes) > 0 {
			return true
		}
	}
	return false
}

func stageAuto(stage flow.Stage) bool {
	if len(stage.Nodes) == 0 {
		return false
	}
	for _, node := range stage.Nodes {
		if node.Mode != "auto" {
			return false
		}
	}
	return true
}

func stageChoices(stage flow.Stage, text flowText, pmqaOn, integrate bool) []flowChoice {
	back := "↺ " + text.stageNames[stage.Name]
	if stage.Name == flow.StageSecurity {
		stop := text.stop
		if !integrate {
			stop = text.stopDelivery
		}
		lines := []string{
			"▶ " + text.decision,
			"1 ▶ " + text.fix,
			"▶ " + text.rereview,
		}
		if pmqaOn {
			lines = append(lines, "▶ "+text.pmqaCross)
		}
		lines = append(lines, back)
		if pmqaOn {
			lines = append(lines, "▶ "+text.securityReturn)
		}
		lines = append(lines,
			"2 ▶ "+text.accept,
			"3 ▶ "+stop,
			"▶ "+text.timeout,
			"▶ "+text.wait,
			"▶ "+text.roundCap,
		)
		return []flowChoice{
			{head: text.no + " · " + text.otherTiers},
			{head: text.yes, lines: lines},
		}
	}
	return []flowChoice{
		{head: text.no + " · " + text.disposition},
		{head: text.unverifiable},
		{head: text.mechanical},
		{head: text.other, lines: []string{
			"▶ " + text.contract,
			"▶ " + text.fix,
			"▶ " + text.rereview,
			back,
			"▶ " + text.roundCap,
		}},
	}
}

// drawChoices lists exits under a gate. pass is the spine label after the
// exits; an empty pass means the last exit falls through with no extra label.
func drawChoices(c *flowCanvas, y, spine, width int, compact bool, gate string, choices []flowChoice, pass string) int {
	x := spine
	if compact {
		x = 0
	}
	put := func(row int, value string) {
		if compact {
			value = clipText(value, width-x)
		}
		c.put(x, row, value)
	}
	put(y, "◆ "+gate)
	y++
	for i, choice := range choices {
		mark := "├─ "
		if i == len(choices)-1 {
			mark = "└─ "
		}
		put(y, mark+choice.head)
		y++
		for _, line := range choice.lines {
			put(y, "   "+line)
			y++
		}
	}
	if pass == "" {
		return y
	}
	label := pass
	if compact {
		label = clipText(label, width-(spine+2))
	}
	c.put(spine, y, "│")
	c.put(spine+2, y, label)
	return y + 1
}

func flowNotes(chart flow.Chart, text flowText) []string {
	notes := []string{text.notePrecedence}
	if chart.Integrate {
		notes = append(notes, text.noteRebase)
	}
	if chart.GroupBatches {
		notes = append(notes, text.noteGroup)
	}
	return notes
}

func drawFlowNotes(c *flowCanvas, y, width int, notes []string) int {
	if len(notes) == 0 {
		return y
	}
	y++
	for _, note := range notes {
		for _, row := range wrapFlowNote(note, width) {
			c.put(0, y, row)
			y++
		}
	}
	return y
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

// stageBox frames one review stage, titled with its name, around its parallel
// role boxes (side by side when they fit, stacked otherwise).
func stageBox(stage flow.Stage, width int, text flowText) []string {
	name := text.stageNames[stage.Name]
	if len(stage.Nodes) == 0 {
		return flowFrame(clipText(name, width-7), []string{
			clipText(text.stageNA, width-4),
			clipText(text.naOverride, width-4),
		})
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
