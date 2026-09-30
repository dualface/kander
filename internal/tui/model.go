package tui

// BoardModel holds column focus, search, and per-state selection/scroll.
type BoardModel struct {
	Single          bool
	Tasks           []Task
	Query           string
	ShowArchived    bool
	ColumnIndex     int
	ColumnOffset    int
	SelectedIDs     map[string]string
	SelectedIndexes map[string]int
	Scrolls         map[string]int
	// Collapsed is true after the user folds a group. A missing key stays expanded.
	// SetBoard, column normalize, and query edits do not clear it.
	Collapsed map[string]bool
	// HeaderFocus names the focused header in a column. Empty means the selected card.
	HeaderFocus  map[string]string
	GeneratedAt  string
	ContentKey   string
	RefreshError string
	DetailError  string
}

func newBoardModel(single bool) *BoardModel {
	m := &BoardModel{
		Single:          single,
		SelectedIDs:     map[string]string{},
		SelectedIndexes: map[string]int{},
		Scrolls:         map[string]int{},
		Collapsed:       map[string]bool{},
		HeaderFocus:     map[string]string{},
	}
	for _, state := range allStates {
		m.SelectedIDs[state] = ""
		m.SelectedIndexes[state] = 0
		m.Scrolls[state] = 0
		m.HeaderFocus[state] = ""
	}
	return m
}

func (m *BoardModel) Error() string {
	if m.RefreshError != "" {
		return m.RefreshError
	}
	return m.DetailError
}

func (m *BoardModel) States() []string {
	if m.ShowArchived {
		return append([]string{}, allStates...)
	}
	return append([]string{}, activeStates...)
}

func (m *BoardModel) CurrentState() string {
	states := m.States()
	if m.ColumnIndex < 0 || m.ColumnIndex >= len(states) {
		return states[0]
	}
	return states[m.ColumnIndex]
}

func (m *BoardModel) SetBoard(payload BoardPayload) bool {
	parsed := make([]Task, 0, len(payload.Tasks))
	for _, task := range payload.Tasks {
		if knownState(task.State) {
			parsed = append(parsed, task)
		}
	}
	nextKey := boardContentKey(parsed)
	errorCleared := m.RefreshError != ""
	m.GeneratedAt = payload.GeneratedAt
	m.RefreshError = ""
	if nextKey == m.ContentKey {
		return errorCleared
	}
	m.Tasks = parsed
	m.ContentKey = nextKey
	m.Normalize()
	return true
}

// TasksFor returns one column's visible cards. Filtering keeps the previous
// field match, then same-column groups of two or more cluster at the first
// member. Render, selection, scroll, and hit testing all read this list.
func (m *BoardModel) TasksFor(state string) []Task {
	var out []Task
	for _, task := range m.Tasks {
		if task.State == state && taskMatches(task, m.Query) {
			out = append(out, task)
		}
	}
	return clusterColumnTasks(out)
}

// clusterColumnTasks pulls each non-empty group of two or more cards forward
// to that group's first visible member. Members keep their relative order.
// Ungrouped cards and singleton groups stay where that pull leaves them.
func clusterColumnTasks(tasks []Task) []Task {
	if len(tasks) < 2 {
		return tasks
	}
	counts := map[string]int{}
	for _, task := range tasks {
		if task.TaskGroup == "" {
			continue
		}
		counts[task.TaskGroup]++
	}
	emitted := make([]bool, len(tasks))
	out := make([]Task, 0, len(tasks))
	for i, task := range tasks {
		if emitted[i] {
			continue
		}
		group := task.TaskGroup
		if group != "" && counts[group] >= 2 {
			for j := i; j < len(tasks); j++ {
				if tasks[j].TaskGroup != group {
					continue
				}
				out = append(out, tasks[j])
				emitted[j] = true
			}
			continue
		}
		out = append(out, task)
		emitted[i] = true
	}
	return out
}

func (m *BoardModel) Normalize() {
	states := m.States()
	if m.ColumnIndex > len(states)-1 {
		m.ColumnIndex = len(states) - 1
	}
	if m.ColumnIndex < 0 {
		m.ColumnIndex = 0
	}
	if m.ColumnOffset < 0 {
		m.ColumnOffset = 0
	}
	if m.ColumnOffset > m.ColumnIndex {
		m.ColumnOffset = m.ColumnIndex
	}
	for _, state := range allStates {
		lines := columnRows(m, state)
		m.ensureColumnFocus(state, lines)
		maxScroll := len(lines) - 1
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.Scrolls[state] > maxScroll {
			m.Scrolls[state] = maxScroll
		}
		if m.Scrolls[state] < 0 {
			m.Scrolls[state] = 0
		}
	}
}

func (m *BoardModel) MoveColumn(delta int) {
	states := m.States()
	n := len(states)
	if n == 0 {
		return
	}
	m.ColumnIndex = (m.ColumnIndex + delta) % n
	if m.ColumnIndex < 0 {
		m.ColumnIndex += n
	}
}

func (m *BoardModel) FocusState(state string) bool {
	states := m.States()
	for i, item := range states {
		if item == state {
			m.ColumnIndex = i
			return true
		}
	}
	return false
}

func (m *BoardModel) SelectTaskIndex(state string, index int) bool {
	if !m.FocusState(state) {
		return false
	}
	tasks := m.TasksFor(state)
	if len(tasks) == 0 {
		return false
	}
	if index < 0 {
		index = 0
	}
	if index > len(tasks)-1 {
		index = len(tasks) - 1
	}
	if m.HeaderFocus == nil {
		m.HeaderFocus = map[string]string{}
	}
	m.HeaderFocus[state] = ""
	m.SelectedIDs[state] = tasks[index].TaskID
	m.SelectedIndexes[state] = index
	return true
}

func (m *BoardModel) EnsureColumnVisible(visibleCount int) {
	states := m.States()
	if visibleCount < 1 {
		visibleCount = 1
	}
	if visibleCount > len(states) {
		visibleCount = len(states)
	}
	if m.ColumnIndex < m.ColumnOffset {
		m.ColumnOffset = m.ColumnIndex
	} else if m.ColumnIndex >= m.ColumnOffset+visibleCount {
		m.ColumnOffset = m.ColumnIndex - visibleCount + 1
	}
	maxOffset := len(states) - visibleCount
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.ColumnOffset < 0 {
		m.ColumnOffset = 0
	}
	if m.ColumnOffset > maxOffset {
		m.ColumnOffset = maxOffset
	}
}

func (m *BoardModel) VisibleStates(visibleCount int) []string {
	m.EnsureColumnVisible(visibleCount)
	states := m.States()
	end := m.ColumnOffset + visibleCount
	if end > len(states) {
		end = len(states)
	}
	if m.ColumnOffset < 0 || m.ColumnOffset > len(states) {
		return nil
	}
	return append([]string{}, states[m.ColumnOffset:end]...)
}

func (m *BoardModel) MoveTask(delta int) {
	state := m.CurrentState()
	lines := columnRows(m, state)
	stops := focusStops(lines)
	if len(stops) == 0 {
		m.SelectedIDs[state] = ""
		if m.HeaderFocus != nil {
			m.HeaderFocus[state] = ""
		}
		return
	}
	m.ensureColumnFocus(state, lines)
	next := m.focusStopIndex(state, stops) + delta
	if next < 0 {
		next = 0
	}
	if next > len(stops)-1 {
		next = len(stops) - 1
	}
	m.applyFocusStop(state, stops[next])
}

func (m *BoardModel) SelectedTask() *Task {
	state := m.CurrentState()
	if m.focusedHeaderGroup(state) != "" {
		return nil
	}
	selectedID := m.SelectedIDs[state]
	tasks := m.TasksFor(state)
	for i := range tasks {
		if tasks[i].TaskID == selectedID {
			task := tasks[i]
			return &task
		}
	}
	return nil
}

func (m *BoardModel) ToggleArchived() {
	current := m.CurrentState()
	m.ShowArchived = !m.ShowArchived
	states := m.States()
	found := false
	for i, state := range states {
		if state == current {
			m.ColumnIndex = i
			found = true
			break
		}
	}
	if !found {
		m.ColumnIndex = len(states) - 1
	}
	m.Normalize()
}
