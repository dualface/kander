package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func stripBoardApp(t *testing.T, width, height int) *App {
	t.Helper()
	board := BoardPayload{GeneratedAt: "t", Tasks: []Task{
		{TaskID: "20260821-one-task", Title: "one", State: "backlog", Type: "Feature", Kind: "small", Time: "-"},
		{TaskID: "20260821-todo-task", Title: "todo", State: "todo", Type: "Feature", Kind: "small", Time: "-"},
		{TaskID: "20260821-work-task", Title: "work", State: "working", Type: "Bug", Kind: "small", Time: "-"},
	}}
	ctx := pageContext{
		StateLabels: map[string]string{
			"backlog": "backlog", "todo": "todo", "working": "working",
			"review": "review", "done": "done", "archived": "archived", "trash": "trash",
		},
		QuitHelp:   "q quit",
		StatusHelp: "? help",
		TooSmall:   "too small",
		Empty:      "empty",
	}
	app := newApp(false, 30, ctx,
		func() (BoardPayload, error) { return board, nil },
		func(id string) (Task, error) { return Task{TaskID: id, Title: id}, nil },
		"dark", 5, nil, func(string) (bool, string) { return true, "" })
	app.Width, app.Height = width, height
	app.Model.SetBoard(board)
	return app
}

func TestColumnStripShowsOnNarrowBoard(t *testing.T) {
	app := stripBoardApp(t, 64, 20)
	if !app.columnStripVisible() {
		t.Fatal("width 64 should show tabs")
	}
	names := viewLine(app, panelTopRow)
	if !strings.Contains(names, "backlog 1") {
		t.Fatalf("selected tab missing count: %q", names)
	}
	if strings.Contains(names, " todo ") || strings.Contains(names, " working ") {
		t.Fatalf("idle tab has padding: %q", names)
	}
	for _, name := range []string{"todo", "working", "review", "done"} {
		if !strings.Contains(names, name) {
			t.Fatalf("tab row %q missing %s", names, name)
		}
	}
	bottom := viewLine(app, 20-2)
	if strings.Contains(bottom, "backlog") && strings.Contains(bottom, "done") {
		t.Fatalf("bottom still a tab strip: %q", bottom)
	}
	wide := stripBoardApp(t, 65, 20)
	if wide.columnStripVisible() {
		t.Fatal("width 65 should hide tabs")
	}
	wideNames := viewLine(wide, panelTopRow)
	if strings.Contains(wideNames, "backlog") && strings.Contains(wideNames, "done") {
		t.Fatalf("wide board kept tabs: %q", wideNames)
	}
}

func TestColumnStripClickSwitchesColumn(t *testing.T) {
	app := stripBoardApp(t, 64, 20)
	if app.Model.CurrentState() != "backlog" {
		t.Fatalf("start %s", app.Model.CurrentState())
	}
	cells := app.columnTabCells(64)
	todo := tabCellByState(t, cells, "todo")
	app.HandleMouse(todo.x+todo.width/2, panelTopRow, mouseBtn1Clicked)
	if app.Model.CurrentState() != "todo" {
		t.Fatalf("tab click %s", app.Model.CurrentState())
	}
	working := tabCellByState(t, app.columnTabCells(64), "working")
	app.HandleMouse(working.x+working.width/2, panelTopRow, mouseBtn1Clicked)
	if app.Model.CurrentState() != "working" {
		t.Fatalf("tab click %s", app.Model.CurrentState())
	}
	if app.visibleColumnLayout()[0].State != "working" {
		t.Fatalf("visible %s", app.visibleColumnLayout()[0].State)
	}
}

func TestColumnStripClickSecondPanelTab(t *testing.T) {
	app := stripBoardApp(t, 64, 20)
	app.MinColumnWidth = 28
	app.Model.Single = false
	layout := app.visibleColumnLayout()
	if len(layout) < 2 {
		t.Fatalf("want two columns, got %d", len(layout))
	}
	done := tabCellByState(t, app.columnTabCells(64), "done")
	app.HandleMouse(done.x+done.width/2, panelTopRow, mouseBtn1Clicked)
	if app.Model.CurrentState() != "done" {
		t.Fatalf("packed tab click %s", app.Model.CurrentState())
	}
	visible := false
	for _, col := range app.visibleColumnLayout() {
		if col.State == "done" {
			visible = true
		}
	}
	if !visible {
		t.Fatal("done not visible after tab click")
	}
}

func tabCellByState(t *testing.T, cells []columnTabCell, state string) columnTabCell {
	t.Helper()
	for _, cell := range cells {
		if cell.state == state {
			return cell
		}
	}
	t.Fatalf("missing tab %s", state)
	return columnTabCell{}
}

func TestColumnStripKeepsBoardBodyHeight(t *testing.T) {
	narrow := stripBoardApp(t, 64, 20)
	wide := stripBoardApp(t, 80, 20)
	if narrow.boardBodyHeight() != wide.boardBodyHeight() {
		t.Fatalf("narrow body %d wide body %d", narrow.boardBodyHeight(), wide.boardBodyHeight())
	}
	if narrow.hitColumnStrip(1, panelTopRow) == "" {
		t.Fatal("tab hit missed")
	}
	if wide.hitColumnStrip(1, panelTopRow) != "" {
		t.Fatal("wide board hit tabs")
	}
	if narrow.hitColumnStrip(1, 20-2) != "" {
		t.Fatal("bottom still a strip")
	}
}

func TestColumnStripStyleUsesColumnColor(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(profile)
	p := themePalette("dark")
	idle := columnStripStyle(p, "todo", false)
	if idle.GetBackground() == p.Headings["todo"] {
		t.Fatal("idle tab used the column fill")
	}
	focused := columnStripStyle(p, "todo", true)
	if focused.GetForeground() == idle.GetForeground() && focused.GetBackground() == idle.GetBackground() {
		t.Fatal("focused tab matched the idle cell")
	}
}

func TestColumnStripClipsLongNames(t *testing.T) {
	app := stripBoardApp(t, 12, 20)
	nameLine := viewLine(app, panelTopRow)
	if strings.Contains(nameLine, "working") {
		t.Fatalf("unclipped name in %q", nameLine)
	}
	cells := app.columnTabCells(12)
	if len(cells) == 0 {
		t.Fatal("no tab cells")
	}
}

func TestColumnStripKeepsSelectedCountWhenNarrow(t *testing.T) {
	app := stripBoardApp(t, 32, 20)
	app.Model.FocusState("done")
	names := viewLine(app, panelTopRow)
	if !strings.Contains(names, "done 0") {
		t.Fatalf("selected count clipped: %q", names)
	}
	app.Width = 26
	names = viewLine(app, panelTopRow)
	if !strings.Contains(names, "done 0") {
		t.Fatalf("selected count dropped: %q", names)
	}
	done := tabCellByState(t, app.columnTabCells(26), "done")
	app.HandleMouse(done.x+done.width/2, panelTopRow, mouseBtn1Clicked)
	if app.Model.CurrentState() != "done" {
		t.Fatalf("selected tab click %s", app.Model.CurrentState())
	}
}
