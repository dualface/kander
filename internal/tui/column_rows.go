package tui

import "strings"

// columnLine is one painted row inside a column body.
// kind is "header", "card", or "gap". A gap is never a focus stop.
// Rail gaps keep the predecessor card's slot; the blank after a cluster does not.
type columnLine struct {
	kind     string
	group    string
	task     int
	cardLine int
	rail     bool
}

// focusStop is a header or a card. Height is 1 for a header and 3 for a card.
type focusStop struct {
	kind   string
	group  string
	task   int
	start  int
	height int
}

// groupCollapsed reports the drawn state. A non-empty query draws every
// cluster expanded and leaves the stored flag alone.
func (m *BoardModel) groupCollapsed(group string) bool {
	if m == nil || group == "" || strings.TrimSpace(m.Query) != "" {
		return false
	}
	return m.Collapsed[group]
}

// columnRows walks TasksFor, which is already clustered, and reuses
// columnGroupRails for membership and the inter-member gap. It does not
// hash group ids again. A column with fewer than two cards of a group
// stays a plain card even when that group is collapsed elsewhere.
func columnRows(m *BoardModel, state string) []columnLine {
	if m == nil {
		return nil
	}
	tasks := m.TasksFor(state)
	rails := columnGroupRails(tasks, false)
	lines := make([]columnLine, 0, len(tasks)*cardHeight)
	for i := 0; i < len(tasks); {
		if !rails[i].member {
			for line := range 3 {
				lines = append(lines, columnLine{kind: "card", task: i, cardLine: line})
			}
			lines = append(lines, columnLine{kind: "gap", task: i})
			i++
			continue
		}
		group := tasks[i].TaskGroup
		j := i + 1
		for j < len(tasks) && rails[j].member && tasks[j].TaskGroup == group {
			j++
		}
		lines = append(lines, columnLine{kind: "header", group: group, task: i})
		if !m.groupCollapsed(group) {
			for k := i; k < j; k++ {
				for line := range 3 {
					lines = append(lines, columnLine{kind: "card", group: group, task: k, cardLine: line})
				}
				if rails[k].continueAfter {
					lines = append(lines, columnLine{kind: "gap", group: group, task: k, rail: true})
				}
			}
		}
		lines = append(lines, columnLine{kind: "gap", group: group})
		i = j
	}
	return lines
}

// columnContentLines counts the rows up to the last card or header. The
// trailing gap is never a focus stop and panels sized to their content
// leave it out.
func columnContentLines(lines []columnLine) int {
	n := len(lines)
	if n > 0 && lines[n-1].kind == "gap" {
		n--
	}
	return n
}

// columnMaxScroll stops scrolling at the last content row, so a panel
// sized to its content never scrolls its first row out for the trailing gap.
func columnMaxScroll(lines []columnLine, bodyHeight int) int {
	return max(0, columnContentLines(lines)-bodyHeight)
}

func focusStops(lines []columnLine) []focusStop {
	stops := make([]focusStop, 0, len(lines)/cardHeight+1)
	for i, line := range lines {
		switch {
		case line.kind == "header":
			stops = append(stops, focusStop{kind: "header", group: line.group, task: line.task, start: i, height: 1})
		case line.kind == "card" && line.cardLine == 0:
			stops = append(stops, focusStop{kind: "card", group: line.group, task: line.task, start: i, height: 3})
		}
	}
	return stops
}

func columnHasHeader(lines []columnLine, group string) bool {
	for _, line := range lines {
		if line.kind == "header" && line.group == group {
			return true
		}
	}
	return false
}

func clusterInColumn(tasks []Task, group string) bool {
	if group == "" {
		return false
	}
	count := 0
	for _, task := range tasks {
		if task.TaskGroup != group {
			continue
		}
		count++
		if count >= 2 {
			return true
		}
	}
	return false
}

func clusterSize(tasks []Task, group string) int {
	count := 0
	for _, task := range tasks {
		if task.TaskGroup == group {
			count++
		}
	}
	return count
}

// focusedHeaderGroup is the header that currently owns the column focus.
// A stale id whose column no longer has a cluster falls back to the card.
func (m *BoardModel) focusedHeaderGroup(state string) string {
	if m == nil || m.HeaderFocus == nil {
		return ""
	}
	group := m.HeaderFocus[state]
	if group == "" || !clusterInColumn(m.TasksFor(state), group) {
		return ""
	}
	return group
}

func (m *BoardModel) cardHighlighted(state, id string) bool {
	if m == nil || id == "" || m.focusedHeaderGroup(state) != "" {
		return false
	}
	return id == m.SelectedIDs[state]
}

// ensureColumnFocus keeps a header focus that still has a header.
// A selected card hidden by collapse moves to that column's header.
func (m *BoardModel) ensureColumnFocus(state string, lines []columnLine) {
	if m.HeaderFocus == nil {
		m.HeaderFocus = map[string]string{}
	}
	if group := m.HeaderFocus[state]; group != "" {
		if columnHasHeader(lines, group) {
			return
		}
		m.HeaderFocus[state] = ""
	}
	tasks := m.TasksFor(state)
	if len(tasks) == 0 {
		m.SelectedIDs[state] = ""
		return
	}
	index := -1
	for i, task := range tasks {
		if task.TaskID == m.SelectedIDs[state] {
			index = i
			break
		}
	}
	if index < 0 {
		index = m.SelectedIndexes[state]
		if index < 0 || index >= len(tasks) {
			index = 0
		}
		m.SelectedIDs[state] = tasks[index].TaskID
	}
	m.SelectedIndexes[state] = index
	group := tasks[index].TaskGroup
	if group != "" && m.groupCollapsed(group) && columnHasHeader(lines, group) {
		m.HeaderFocus[state] = group
	}
}

func (m *BoardModel) focusStopIndex(state string, stops []focusStop) int {
	if group := m.focusedHeaderGroup(state); group != "" {
		for i, stop := range stops {
			if stop.kind == "header" && stop.group == group {
				return i
			}
		}
	}
	tasks := m.TasksFor(state)
	id := m.SelectedIDs[state]
	for i, stop := range stops {
		if stop.kind == "card" && stop.task >= 0 && stop.task < len(tasks) && tasks[stop.task].TaskID == id {
			return i
		}
	}
	return 0
}

func (m *BoardModel) applyFocusStop(state string, stop focusStop) {
	if m.HeaderFocus == nil {
		m.HeaderFocus = map[string]string{}
	}
	if stop.kind == "header" {
		m.HeaderFocus[state] = stop.group
		return
	}
	m.HeaderFocus[state] = ""
	tasks := m.TasksFor(state)
	if stop.task < 0 || stop.task >= len(tasks) {
		return
	}
	m.SelectedIDs[state] = tasks[stop.task].TaskID
	m.SelectedIndexes[state] = stop.task
}

// ToggleCollapsed flips the stored flag. The first collapse is what stores it.
// Expanding leaves the key set to false. Focus moves to a header only in a
// column that already has one and whose selected card is being hidden.
func (m *BoardModel) ToggleCollapsed(group string) {
	if m == nil || group == "" {
		return
	}
	if m.Collapsed == nil {
		m.Collapsed = map[string]bool{}
	}
	m.Collapsed[group] = !m.Collapsed[group]
	if !m.Collapsed[group] {
		return
	}
	if m.HeaderFocus == nil {
		m.HeaderFocus = map[string]string{}
	}
	for _, state := range allStates {
		if m.focusedHeaderGroup(state) != "" {
			continue
		}
		tasks := m.TasksFor(state)
		if !clusterInColumn(tasks, group) {
			continue
		}
		id := m.SelectedIDs[state]
		for _, task := range tasks {
			if task.TaskID == id && task.TaskGroup == group {
				m.HeaderFocus[state] = group
				break
			}
		}
	}
}

// FocusGroupHeader selects the column and the header without toggling.
func (m *BoardModel) FocusGroupHeader(state, group string) {
	if m == nil || group == "" || !m.FocusState(state) {
		return
	}
	if m.HeaderFocus == nil {
		m.HeaderFocus = map[string]string{}
	}
	m.HeaderFocus[state] = group
}

// MoveFocusEdge jumps to the first or last visible focus stop.
func (m *BoardModel) MoveFocusEdge(last bool) {
	state := m.CurrentState()
	stops := focusStops(columnRows(m, state))
	if len(stops) == 0 {
		m.SelectedIDs[state] = ""
		if m.HeaderFocus != nil {
			m.HeaderFocus[state] = ""
		}
		return
	}
	index := 0
	if last {
		index = len(stops) - 1
	}
	m.applyFocusStop(state, stops[index])
}

// columnWindow returns the painted lines and the line-index scroll.
// When the body can hold the focused stop, that stop is fully inside the
// viewport. A shorter body pins the stop's first line.
func columnWindow(m *BoardModel, state string, bodyHeight int) (lines []columnLine, scroll int) {
	lines = columnRows(m, state)
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	if len(lines) == 0 {
		m.Scrolls[state] = 0
		return lines, 0
	}
	m.ensureColumnFocus(state, lines)
	stops := focusStops(lines)
	if len(stops) == 0 {
		m.Scrolls[state] = 0
		return lines, 0
	}
	stop := stops[m.focusStopIndex(state, stops)]
	scroll = m.Scrolls[state]
	if bodyHeight >= stop.height {
		if stop.start < scroll {
			scroll = stop.start
		} else if stop.start+stop.height > scroll+bodyHeight {
			scroll = stop.start + stop.height - bodyHeight
		}
		maxScroll := columnMaxScroll(lines, bodyHeight)
		if scroll < 0 {
			scroll = 0
		}
		if scroll > maxScroll {
			scroll = maxScroll
		}
	} else {
		scroll = stop.start
		if scroll < 0 {
			scroll = 0
		}
	}
	m.Scrolls[state] = scroll
	return lines, scroll
}

func stopStride(lines []columnLine, stop focusStop) int {
	stride := stop.height
	after := stop.start + stop.height
	if after < len(lines) && lines[after].kind == "gap" {
		stride++
	}
	return stride
}

// focusPageCount is how many focus stops fit in bodyHeight, walking from
// the current stop. A stop's stride is its own height plus a following gap.
func focusPageCount(lines []columnLine, stops []focusStop, from, direction, body int) int {
	if body < 1 {
		body = 1
	}
	if direction == 0 {
		direction = 1
	}
	count := 0
	used := 0
	for i := from; i >= 0 && i < len(stops); i += direction {
		stride := stopStride(lines, stops[i])
		if count > 0 && used+stride > body {
			break
		}
		used += stride
		count++
	}
	if count < 1 {
		return 1
	}
	return count
}

func (a *App) pageFocus(direction int) {
	if direction == 0 || a == nil || a.Model == nil {
		return
	}
	state := a.Model.CurrentState()
	body := a.boardBodyHeight()
	for _, panel := range a.visibleColumnLayout() {
		if panel.State == state {
			body = panel.BodyHeight
			break
		}
	}
	lines := columnRows(a.Model, state)
	stops := focusStops(lines)
	if len(stops) == 0 {
		return
	}
	a.Model.ensureColumnFocus(state, lines)
	current := a.Model.focusStopIndex(state, stops)
	count := focusPageCount(lines, stops, current, direction, body)
	next := current + direction*count
	if next < 0 {
		next = 0
	}
	if next > len(stops)-1 {
		next = len(stops) - 1
	}
	delta := stops[next].start - stops[current].start
	a.Model.applyFocusStop(state, stops[next])
	scroll := a.Model.Scrolls[state] + delta
	maxScroll := columnMaxScroll(lines, body)
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	a.Model.Scrolls[state] = scroll
}

// groupHeaderText is "▾ name" or "▸ name" with the member count flush
// to the right edge of the content width. The twistie and the count win
// when the width cannot hold the name.
func groupHeaderText(name string, count, width int, collapsed bool) string {
	if width <= 0 {
		return ""
	}
	twistie := "▾"
	if collapsed {
		twistie = "▸"
	}
	countText := itoa(count)
	countW := displayWidth(countText)
	if width <= countW {
		clipped := clipText(countText, width)
		return strings.Repeat(" ", width-displayWidth(clipped)) + clipped
	}
	prefix := twistie + " "
	prefixW := displayWidth(prefix)
	room := width - countW
	if room < prefixW {
		clipped := clipText(prefix, room)
		return clipped + strings.Repeat(" ", room-displayWidth(clipped)) + countText
	}
	nameRoom := room - prefixW
	clippedName := ""
	if nameRoom > 0 {
		clippedName = clipText(name, nameRoom)
	}
	gap := width - prefixW - displayWidth(clippedName) - countW
	if gap < 0 {
		gap = 0
	}
	return prefix + clippedName + strings.Repeat(" ", gap) + countText
}
