package tui

// visibleColumnCount 结算本次实际并排的栏目数.
// 用户设定的是想同屏看几栏 (desired), 栏宽由 columnGeometry 平分终端宽度得出.
// 非单栏设置允许因宽度减少栏目, 但不会自行变成单栏.
func visibleColumnCount(width, total int, single bool, desired, minWidth int) int {
	if single || total <= 1 {
		return 1
	}
	count := clampColumns(desired)
	minWidth = clampMinColumnWidth(minWidth)
	if fit := width / minWidth; count > fit {
		count = fit
	}
	if count > total {
		count = total
	}
	if desired > 1 && total > 1 && count < 2 {
		count = 2
	}
	if count < 1 {
		count = 1
	}
	return count
}

type columnGeom struct {
	X, Width     int
	HasSeparator bool
}

func columnGeometry(width, count int) []columnGeom {
	if count < 1 {
		count = 1
	}
	separators := count - 1
	usable := width - separators
	if usable < 0 {
		usable = 0
	}
	layout := make([]columnGeom, 0, count)
	cursor := 0
	for index := 0; index < count; index++ {
		start := index * usable / count
		end := (index + 1) * usable / count
		colWidth := end - start
		hasSep := index < separators
		layout = append(layout, columnGeom{X: cursor, Width: colWidth, HasSeparator: hasSep})
		extra := 0
		if hasSep {
			extra = 1
		}
		cursor += colWidth + extra
	}
	return layout
}

func columnTaskWindow(model *BoardModel, state string, bodyHeight int) (tasks []Task, scroll, capacity int) {
	tasks = model.TasksFor(state)
	capacity = (bodyHeight + 1) / cardHeight
	if capacity < 1 {
		capacity = 1
	}
	if len(tasks) == 0 {
		model.Scrolls[state] = 0
		return tasks, 0, capacity
	}
	taskIDs := make([]string, len(tasks))
	for i, task := range tasks {
		taskIDs[i] = task.TaskID
	}
	selectedID := model.SelectedIDs[state]
	selectedIndex := 0
	found := false
	for i, id := range taskIDs {
		if id == selectedID {
			selectedIndex = i
			found = true
			break
		}
	}
	if !found {
		selectedIndex = 0
		model.SelectedIDs[state] = taskIDs[0]
		model.SelectedIndexes[state] = 0
	}
	scroll = model.Scrolls[state]
	if selectedIndex < scroll {
		scroll = selectedIndex
	} else if selectedIndex >= scroll+capacity {
		scroll = selectedIndex - capacity + 1
	}
	maxScroll := len(tasks) - capacity
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	model.Scrolls[state] = scroll
	return tasks, scroll, capacity
}
