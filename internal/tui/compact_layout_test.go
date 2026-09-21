package tui

import "testing"

func compactLayoutApp(width, height int, tasks []Task) *App {
	app := newApp(false, 30, pageContext{
		StateLabels: map[string]string{
			"backlog": "backlog", "todo": "todo", "working": "working",
			"review": "review", "done": "done",
		},
		Empty: "empty",
	}, func() (BoardPayload, error) {
		return BoardPayload{GeneratedAt: "now", Tasks: tasks}, nil
	}, func(string) (Task, error) {
		return Task{}, nil
	}, "dark", 5, nil, nil)
	app.Compact = true
	app.Width = width
	app.Height = height
	app.Model.SetBoard(BoardPayload{GeneratedAt: "now", Tasks: tasks})
	return app
}

func tasksPerActiveState(count int) []Task {
	var tasks []Task
	for _, state := range activeStates {
		for index := range count {
			tasks = append(tasks, Task{
				TaskID: state + "-" + itoa(index), Title: state, State: state,
			})
		}
	}
	return tasks
}

func TestBalancedPanelHeights(t *testing.T) {
	for _, test := range []struct {
		minimums []int
		total    int
		want     []int
	}{
		{minimums: []int{3, 3, 3}, total: 17, want: []int{6, 6, 5}},
		{minimums: []int{4, 12}, total: 20, want: []int{8, 12}},
		{minimums: []int{12, 4, 4}, total: 25, want: []int{12, 7, 6}},
	} {
		got := balancedPanelHeights(test.minimums, test.total)
		if len(got) != len(test.want) {
			t.Fatalf("got %v want %v", got, test.want)
		}
		for index := range got {
			if got[index] != test.want[index] {
				t.Fatalf("minimums=%v total=%d got=%v want=%v", test.minimums, test.total, got, test.want)
			}
		}
	}
}

func TestCompactLayoutKeepsWideBoardUnstacked(t *testing.T) {
	app := compactLayoutApp(minColumnWidth*5+4, 20, nil)
	layout := app.visibleColumnLayout()
	if len(layout) != len(activeStates) {
		t.Fatalf("visible panels=%d", len(layout))
	}
	for index, panel := range layout {
		if panel.VisualColumn != index || panel.Y != panelTopRow || panel.Height != app.boardBodyHeight()+2 {
			t.Fatalf("panel %d stacked on a wide board: %+v", index, panel)
		}
	}
}

func TestCompactLayoutStacksOnlyAfterWidthReduction(t *testing.T) {
	app := compactLayoutApp(minColumnWidth*3+2, 20, nil)
	layout := app.visibleColumnLayout()
	if len(layout) != len(activeStates) {
		t.Fatalf("visible panels=%d", len(layout))
	}
	wantColumns := []int{0, 0, 0, 1, 2}
	wantHeights := []int{6, 6, 5}
	for index, want := range wantColumns {
		if layout[index].State != activeStates[index] || layout[index].VisualColumn != want {
			t.Fatalf("panel %d=%+v", index, layout[index])
		}
	}
	for index, want := range wantHeights {
		if layout[index].Height != want {
			t.Fatalf("stack height %d=%d want %d", index, layout[index].Height, want)
		}
	}
	if layout[0].Y != panelTopRow || layout[1].Y != panelTopRow+6 || layout[2].Y != panelTopRow+12 {
		t.Fatalf("stack y positions=%d,%d,%d", layout[0].Y, layout[1].Y, layout[2].Y)
	}
}

func TestCompactLayoutLeavesOverflowingPanelAlone(t *testing.T) {
	app := compactLayoutApp(minColumnWidth*3+2, 20, tasksPerActiveState(4))
	layout := app.visibleColumnLayout()
	if len(layout) != 3 {
		t.Fatalf("overflowing panels should not stack: %+v", layout)
	}
	for index, panel := range layout {
		if panel.VisualColumn != index || panel.State != activeStates[index] {
			t.Fatalf("panel %d=%+v", index, panel)
		}
	}
}

func TestCompactLayoutKeepsFocusedStateVisible(t *testing.T) {
	app := compactLayoutApp(minColumnWidth*3+2, 20, tasksPerActiveState(4))
	app.Model.FocusState("done")
	layout := app.visibleColumnLayout()
	if app.Model.ColumnOffset != 2 || len(layout) != 3 {
		t.Fatalf("offset=%d layout=%+v", app.Model.ColumnOffset, layout)
	}
	for index, want := range []string{"working", "review", "done"} {
		if layout[index].State != want {
			t.Fatalf("panel %d=%s want %s", index, layout[index].State, want)
		}
	}
}

func TestCompactLayoutDoesNotShiftForAlreadyStackedFocus(t *testing.T) {
	app := compactLayoutApp(minColumnWidth*2+1, 20, nil)
	initial := app.visibleColumnLayout()
	if !layoutContainsState(initial, "working") {
		t.Fatalf("working not initially stacked: %+v", initial)
	}
	app.Model.FocusState("working")
	layout := app.visibleColumnLayout()
	if app.Model.ColumnOffset != 0 || !layoutContainsState(layout, "working") {
		t.Fatalf("offset=%d layout=%+v", app.Model.ColumnOffset, layout)
	}
}

func TestCompactLayoutMouseTargetsStackedPanel(t *testing.T) {
	tasks := []Task{
		{TaskID: "backlog-0", Title: "backlog", State: "backlog"},
		{TaskID: "todo-0", Title: "todo", State: "todo"},
		{TaskID: "working-0", Title: "working", State: "working"},
	}
	app := compactLayoutApp(minColumnWidth*3+2, 20, tasks)
	panel := layoutPanelForState(app.visibleColumnLayout(), "todo")
	if panel == nil || panel.VisualColumn != 0 || panel.Y == panelTopRow {
		t.Fatalf("todo panel not stacked: %+v", panel)
	}
	bodyStart := panel.Y + 1
	hit := app.hitBoard(panel.X+2, bodyStart)
	if hit == nil || hit.Kind != "task" || hit.State != "todo" || hit.Index != 0 {
		t.Fatalf("stacked hit=%+v panel=%+v", hit, panel)
	}
	if state := app.hitColumnAt(panel.X+2, bodyStart); state != "todo" {
		t.Fatalf("wheel target=%s", state)
	}
	selection := app.boardCardHit(panel.X+2, bodyStart)
	if selection == nil || selection.TaskID != "todo-0" {
		t.Fatalf("selection=%+v", selection)
	}
}

func TestCompactLayoutStacksBelowNarrowColumnStrip(t *testing.T) {
	app := compactLayoutApp(columnStripMaxWidth, 20, nil)
	layout := app.visibleColumnLayout()
	if len(layout) != len(activeStates) {
		t.Fatalf("visible panels=%d", len(layout))
	}
	if !layout[0].SkipTop || layout[0].Y != bodyTop {
		t.Fatalf("first panel must reuse the tab-strip top: %+v", layout[0])
	}
	for _, panel := range layout[1:] {
		if panel.SkipTop || panel.VisualColumn != 0 {
			t.Fatalf("stacked panel geometry=%+v", panel)
		}
	}
	last := layout[len(layout)-1]
	if last.Y+last.Height != app.Height-1 {
		t.Fatalf("stack ends at %d want status row %d", last.Y+last.Height, app.Height-1)
	}
	todo := tabCellByState(t, app.columnTabCells(app.Width), "todo")
	if state := app.hitColumnAt(todo.x, panelTopRow); state != "todo" {
		t.Fatalf("tab strip target=%q", state)
	}
}
