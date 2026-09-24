package tui

import "slices"

func (a *App) boardCardHit(x, y int) *mouseSel {
	hit := a.hitBoard(x, y)
	if hit == nil || hit.Kind != "task" {
		return nil
	}
	layout := a.visibleColumnLayout()
	panel := layoutPanelForState(layout, hit.State)
	if panel == nil {
		return nil
	}
	tasks, scroll, _ := columnTaskWindow(a.Model, hit.State, panel.BodyHeight)
	if hit.Index < scroll || hit.Index >= len(tasks) {
		return nil
	}
	task := tasks[hit.Index]
	colX, colWidth := panel.X, panel.Width
	if x < colX || x >= colX+colWidth {
		return nil
	}
	bodyStart := panel.Y
	if !panel.SkipTop {
		bodyStart++
	}
	row := (y - bodyStart) / cardHeight
	lineInCard := y - bodyStart - row*cardHeight
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
	// The body sits inside the panel: 1 column for the left border, 1 for the padding.
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
	if state := a.hitColumnStrip(x, y); state != "" {
		a.Model.FocusState(state)
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
		if a.hitColumnStrip(x, y) != "" {
			return
		}
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
	if state := a.hitColumnStrip(x, y); state != "" {
		return state
	}
	for _, panel := range a.visibleColumnLayout() {
		if x >= panel.X && x < panel.X+panel.Width && y >= panel.Y && y < panel.Y+panel.Height {
			return panel.State
		}
	}
	return ""
}

func (a *App) hitColumnStrip(x, y int) string {
	if !a.columnStripVisible() || y != panelTopRow {
		return ""
	}
	_, w := a.size()
	for _, cell := range a.columnTabCells(w) {
		if cell.width > 0 && x >= cell.x && x < cell.x+cell.width {
			return cell.state
		}
	}
	return ""
}

func (a *App) hitBoard(x, y int) *boardHit {
	layout := a.visibleColumnLayout()
	if len(layout) == 0 {
		return nil
	}
	visualColumns := layout[len(layout)-1].VisualColumn + 1
	for _, panel := range layout {
		if x < panel.X || x >= panel.X+panel.Width || y < panel.Y || y >= panel.Y+panel.Height {
			continue
		}
		localX := x - panel.X
		singleNav := a.Model.Single || visualColumns == 1
		if y == panel.Y && !panel.SkipTop && singleNav && len(a.Model.States()) > 1 {
			if localX <= 2 {
				return &boardHit{Kind: "nav", Delta: -1}
			}
			if localX >= max(0, panel.Width-3) {
				return &boardHit{Kind: "nav", Delta: 1}
			}
		}
		bodyStart := panel.Y
		if !panel.SkipTop {
			bodyStart++
		}
		if y < bodyStart || y >= bodyStart+panel.BodyHeight || panel.BodyHeight <= 0 {
			return &boardHit{Kind: "column", State: panel.State}
		}
		tasks, scroll, capacity := columnTaskWindow(a.Model, panel.State, panel.BodyHeight)
		if len(tasks) == 0 {
			return &boardHit{Kind: "column", State: panel.State}
		}
		row := (y - bodyStart) / cardHeight
		if row < 0 || row >= capacity {
			return &boardHit{Kind: "column", State: panel.State}
		}
		taskIndex := scroll + row
		if taskIndex >= len(tasks) {
			return &boardHit{Kind: "column", State: panel.State}
		}
		return &boardHit{Kind: "task", State: panel.State, Index: taskIndex}
	}
	return nil
}

func layoutPanelForState(layout []boardLayout, state string) *boardLayout {
	index := slices.IndexFunc(layout, func(panel boardLayout) bool {
		return panel.State == state
	})
	if index < 0 {
		return nil
	}
	return &layout[index]
}

func (a *App) HandleMouse(x, y, bstate int) {
	if a.UpdateDialog != nil {
		a.UpdateDialog.scroll(mouseWheelDelta(bstate))
		return
	}
	if a.StartConfirmation != nil {
		a.handleStartMouse(x, y, bstate)
		return
	}
	if a.BoardInit != nil {
		a.handleBoardInitMouse(x, y, bstate)
		return
	}
	if a.Help {
		if delta := mouseWheelDelta(bstate); delta != 0 {
			a.helpView.SetYOffset(a.helpView.YOffset + delta*mouseScrollStep)
		} else if a.popupClick(x, y, bstate) && !a.helpBox.contains(x, y) {
			a.Help = false
		}
		return
	}
	if a.shouldShowWelcome() {
		if mouseLeftClicked(bstate) {
			a.dismissWelcome()
		}
		return
	}
	if a.Options != nil {
		a.Options.HandleMouse(x, y, bstate)
		return
	}
	if a.Takeover != nil {
		a.handleTakeoverMouse(x, y, bstate)
		return
	}
	if a.Issues != nil {
		a.handleIssuesMouse(x, y, bstate)
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
	if a.handleStatusMouse(x, y, bstate) {
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
