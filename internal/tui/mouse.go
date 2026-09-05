package tui

func (a *App) boardCardHit(x, y int) *mouseSel {
	hit := a.hitBoard(x, y)
	if hit == nil || hit.Kind != "task" {
		return nil
	}
	h, _ := a.size()
	bodyHeight := h - bodyTop - 2
	tasks, scroll, _ := columnTaskWindow(a.Model, hit.State, bodyHeight)
	if hit.Index < scroll || hit.Index >= len(tasks) {
		return nil
	}
	task := tasks[hit.Index]
	layout := a.visibleColumnLayout()
	colX, colWidth := 0, 1
	for _, col := range layout {
		if col.State == hit.State {
			colX, colWidth = col.X, col.Width
			break
		}
	}
	if x < colX || x >= colX+colWidth {
		return nil
	}
	row := (y - bodyTop) / cardHeight
	lineInCard := y - bodyTop - row*cardHeight
	if lineInCard < 0 {
		lineInCard = 0
	}
	if lineInCard > 2 {
		lineInCard = 2
	}
	contentWidth := colWidth - panelChrome
	if contentWidth < 1 {
		contentWidth = 1
	}
	displayCol := x - colX - 2
	if displayCol < 0 {
		displayCol = 0
	}
	lines := a.boardCardLines(task, contentWidth)
	lineText := lines[lineInCard]
	charCol := displayColumnToCharIndex(lineText, displayCol)
	return &mouseSel{Kind: "board", TaskID: task.TaskID, Line: lineInCard, Col: charCol, ContentWidth: contentWidth}
}

func (a *App) detailHit(x, y int) *mouseSel {
	if y < detailBodyTop {
		return nil
	}
	_, w := a.size()
	bodyHeight := a.detailBodyHeight()
	if y >= detailBodyTop+bodyHeight {
		return nil
	}
	lineIndex := a.DetailScroll + (y - detailBodyTop)
	lines := a.detailLines()
	if lineIndex < 0 || lineIndex >= len(lines) {
		return nil
	}
	// 正文在面板内: 左边框 1 列, 内边距 1 列.
	displayCol := x - 2
	if displayCol < 0 {
		displayCol = 0
	}
	if displayCol > w-1 {
		displayCol = w - 1
	}
	lineText := lines[lineIndex]
	charCol := displayColumnToCharIndex(lineText, displayCol)
	return &mouseSel{Kind: "detail", Line: lineIndex, Col: charCol}
}

func (a *App) extractBoardMouseSelection() string {
	if a.MouseAnchor == nil || a.MouseCursor == nil {
		return ""
	}
	if a.MouseAnchor.Kind != "board" || a.MouseCursor.Kind != "board" {
		return ""
	}
	if a.MouseAnchor.TaskID != a.MouseCursor.TaskID {
		return ""
	}
	var task *Task
	for i := range a.Model.Tasks {
		if a.Model.Tasks[i].TaskID == a.MouseAnchor.TaskID {
			task = &a.Model.Tasks[i]
			break
		}
	}
	if task == nil {
		return ""
	}
	lines := a.boardCardLines(*task, a.MouseAnchor.ContentWidth)
	return extractMouseCharSelection(lines, [2]int{a.MouseAnchor.Line, a.MouseAnchor.Col}, [2]int{a.MouseCursor.Line, a.MouseCursor.Col})
}

func (a *App) extractDetailMouseSelection() string {
	if a.MouseAnchor == nil || a.MouseCursor == nil {
		return ""
	}
	if a.MouseAnchor.Kind != "detail" || a.MouseCursor.Kind != "detail" {
		return ""
	}
	return extractMouseCharSelection(a.detailLines(), [2]int{a.MouseAnchor.Line, a.MouseAnchor.Col}, [2]int{a.MouseCursor.Line, a.MouseCursor.Col})
}

func (a *App) finishMouseSelection() {
	if !a.MouseSelecting {
		return
	}
	if a.MouseAnchor == nil || a.MouseCursor == nil {
		a.resetMouseSelection()
		return
	}
	var text string
	if a.MouseAnchor.Kind == "board" {
		text = a.extractBoardMouseSelection()
	} else {
		text = a.extractDetailMouseSelection()
	}
	a.resetMouseSelection()
	if text != "" {
		a.copyText(text)
	}
}

func (a *App) handleSearchMouse(x, y, bstate int) {
	if mouseButton1Released(bstate) && bstate&mouseBtn1Clicked == 0 {
		if y != headerRow {
			a.Searching = false
			a.ShowCursor = false
		}
		return
	}
	if !mouseLeftClicked(bstate) {
		return
	}
	if y != headerRow {
		a.Searching = false
		a.ShowCursor = false
	}
}

func (a *App) handleDetailMouse(x, y, bstate int) {
	if a.DetailSearching {
		if mouseButton1Released(bstate) && bstate&mouseBtn1Clicked == 0 {
			if y != detailRuleRow {
				a.applyDetailSearch()
			}
			return
		}
		if mouseLeftClicked(bstate) && y != detailRuleRow {
			a.applyDetailSearch()
		}
		return
	}
	if mouseButton1Released(bstate) {
		if a.MouseSelecting {
			if a.mouseSelectionMoved() {
				a.finishMouseSelection()
				if bstate&mouseBtn1Clicked == 0 {
					a.SuppressClick = true
				}
			} else {
				a.resetMouseSelection()
			}
		}
		return
	}
	if a.MouseSelecting && (mouseButton1Dragging(bstate) || mouseLeftPressed(bstate)) {
		if hit := a.detailHit(x, y); hit != nil {
			a.MouseCursor = hit
		}
		return
	}
	if mouseLeftPressed(bstate) {
		a.resetDetailSelection()
		if hit := a.detailHit(x, y); hit != nil {
			a.MouseSelecting = true
			a.MouseAnchor = hit
			a.MouseCursor = hit
		}
		return
	}
	if delta := mouseWheelDelta(bstate); delta != 0 {
		a.scrollDetailBy(delta * mouseScrollStep)
	}
}

func (a *App) handleBoardClick(x, y, bstate int) {
	h, w := a.size()
	if h < minBoardHeight || w < 1 {
		return
	}
	if y == headerRow {
		a.Searching = true
		a.ShowCursor = true
		return
	}
	if y >= h-1 {
		return
	}
	hit := a.hitBoard(x, y)
	if hit == nil {
		return
	}
	switch hit.Kind {
	case "nav":
		a.Model.MoveColumn(hit.Delta)
	case "column":
		a.Model.FocusState(hit.State)
	case "task":
		a.Model.SelectTaskIndex(hit.State, hit.Index)
		if mouseLeftDoubleClicked(bstate) {
			a.openDetail()
		}
	}
}

func (a *App) handleBoardMouse(x, y, bstate int) {
	if mouseButton1Released(bstate) {
		if a.MouseSelecting {
			if a.mouseSelectionMoved() {
				a.finishMouseSelection()
				if bstate&mouseBtn1Clicked == 0 {
					a.SuppressClick = true
				}
			} else {
				a.resetMouseSelection()
				if bstate&mouseBtn1Clicked == 0 {
					a.handleBoardClick(x, y, bstate)
				}
			}
		} else if bstate&mouseBtn1Clicked == 0 {
			a.handleBoardClick(x, y, bstate)
		}
		return
	}
	if a.MouseSelecting && (mouseButton1Dragging(bstate) || mouseLeftPressed(bstate)) {
		hit := a.boardCardHit(x, y)
		if hit != nil && a.MouseAnchor != nil && hit.TaskID == a.MouseAnchor.TaskID {
			a.MouseCursor = hit
		}
		return
	}
	if mouseLeftPressed(bstate) {
		if hit := a.boardCardHit(x, y); hit != nil {
			a.MouseSelecting = true
			a.MouseAnchor = hit
			a.MouseCursor = hit
		}
		return
	}
	if delta := mouseWheelDelta(bstate); delta != 0 {
		if target := a.hitColumnAt(x, y); target != "" {
			a.Model.FocusState(target)
		}
		a.Model.MoveTask(delta)
		return
	}
	if !mouseLeftClicked(bstate) {
		return
	}
	if a.SuppressClick {
		a.SuppressClick = false
		return
	}
	a.handleBoardClick(x, y, bstate)
}

func (a *App) hitColumnAt(x, y int) string {
	if y < panelTopRow {
		return ""
	}
	for _, col := range a.visibleColumnLayout() {
		if x >= col.X && x < col.X+col.Width {
			return col.State
		}
	}
	return ""
}

func (a *App) hitBoard(x, y int) *boardHit {
	h, _ := a.size()
	layout := a.visibleColumnLayout()
	if len(layout) == 0 {
		return nil
	}
	bodyHeight := h - bodyTop - 2
	for index, col := range layout {
		if x < col.X || x >= col.X+col.Width {
			continue
		}
		localX := x - col.X
		first := index == 0
		last := index == len(layout)-1
		singleNav := a.Model.Single || (first && last && len(a.Model.States()) > 1)
		if y == 2 && singleNav && len(a.Model.States()) > 1 {
			if localX <= 2 {
				return &boardHit{Kind: "nav", Delta: -1}
			}
			if localX >= max(0, col.Width-3) {
				return &boardHit{Kind: "nav", Delta: 1}
			}
		}
		if y < bodyTop || y >= bodyTop+bodyHeight || bodyHeight <= 0 {
			return &boardHit{Kind: "column", State: col.State}
		}
		tasks, scroll, capacity := columnTaskWindow(a.Model, col.State, bodyHeight)
		if len(tasks) == 0 {
			return &boardHit{Kind: "column", State: col.State}
		}
		row := (y - bodyTop) / cardHeight
		if row < 0 || row >= capacity {
			return &boardHit{Kind: "column", State: col.State}
		}
		taskIndex := scroll + row
		if taskIndex >= len(tasks) {
			return &boardHit{Kind: "column", State: col.State}
		}
		return &boardHit{Kind: "task", State: col.State, Index: taskIndex}
	}
	return nil
}

func (a *App) HandleMouse(x, y, bstate int) {
	if a.Help {
		if mouseLeftClicked(bstate) {
			a.Help = false
		}
		return
	}
	if a.Options != nil {
		a.Options.HandleMouse(x, y, bstate)
		return
	}
	if a.Detail != nil {
		a.handleDetailMouse(x, y, bstate)
		return
	}
	if a.Searching {
		a.handleSearchMouse(x, y, bstate)
		return
	}
	a.handleBoardMouse(x, y, bstate)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
