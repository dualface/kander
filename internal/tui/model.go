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
	GeneratedAt     string
	ContentKey      string
	RefreshError    string
	DetailError     string
}

func newBoardModel(single bool) *BoardModel {
	m := &BoardModel{
		Single:          single,
		SelectedIDs:     map[string]string{},
		SelectedIndexes: map[string]int{},
		Scrolls:         map[string]int{},
	}
	for _, state := range allStates {
		m.SelectedIDs[state] = ""
		m.SelectedIndexes[state] = 0
		m.Scrolls[state] = 0
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

func (m *BoardModel) TasksFor(state string) []Task {
	var out []Task
	for _, task := range m.Tasks {
		if task.State == state && taskMatches(task, m.Query) {
			out = append(out, task)
		}
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
		tasks := m.TasksFor(state)
		taskIDs := make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}
		selected := m.SelectedIDs[state]
		found := -1
		for i, id := range taskIDs {
			if id == selected {
				found = i
				break
			}
		}
		if found >= 0 {
			m.SelectedIndexes[state] = found
		} else if len(taskIDs) > 0 {
			index := m.SelectedIndexes[state]
			if index > len(taskIDs)-1 {
				index = len(taskIDs) - 1
			}
			if index < 0 {
				index = 0
			}
			m.SelectedIDs[state] = taskIDs[index]
			m.SelectedIndexes[state] = index
		} else {
			m.SelectedIDs[state] = ""
		}
		maxScroll := len(tasks) - 1
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
	tasks := m.TasksFor(state)
	if len(tasks) == 0 {
		m.SelectedIDs[state] = ""
		return
	}
	index := 0
	found := false
	for i, task := range tasks {
		if task.TaskID == m.SelectedIDs[state] {
			index = i
			found = true
			break
		}
	}
	if !found {
		index = 0
	}
	index += delta
	if index < 0 {
		index = 0
	}
	if index > len(tasks)-1 {
		index = len(tasks) - 1
	}
	m.SelectedIDs[state] = tasks[index].TaskID
	m.SelectedIndexes[state] = index
}

func (m *BoardModel) SelectedTask() *Task {
	selectedID := m.SelectedIDs[m.CurrentState()]
	for i := range m.TasksFor(m.CurrentState()) {
		tasks := m.TasksFor(m.CurrentState())
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
