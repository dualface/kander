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

func TestEvenCellsFillWidth(t *testing.T) {
	for _, width := range []int{20, 64, 65} {
		for _, count := range []int{1, 5, 7} {
			cells := evenCells(width, count)
			if len(cells) != count {
				t.Fatalf("width=%d count=%d len=%d", width, count, len(cells))
			}
			total := 0
			for i, cell := range cells {
				if cell.X != total {
					t.Fatalf("width=%d count=%d cell %d x=%d want %d", width, count, i, cell.X, total)
				}
				if cell.Width < 0 {
					t.Fatalf("negative width %+v", cell)
				}
				total += cell.Width
			}
			if total != width {
				t.Fatalf("width=%d count=%d total=%d", width, count, total)
			}
		}
	}
}

func TestColumnStripShowsOnNarrowBoard(t *testing.T) {
	app := stripBoardApp(t, 64, 20)
	if !app.columnStripVisible() {
		t.Fatal("width 64 should show tabs")
	}
	names := viewLine(app, panelTopRow)
	if !strings.Contains(names, borderVertical) {
		t.Fatalf("tab row missing side border: %q", names)
	}
	for _, name := range []string{"backlog", "todo", "working", "review", "done"} {
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
	layout := app.visibleColumnLayout()
	if len(layout) != 1 {
		t.Fatalf("visible columns %d", len(layout))
	}
	cells := evenCells(layout[0].Width, len(app.Model.States()))
	todo := cells[1]
	x := layout[0].X + todo.X + todo.Width/2
	app.HandleMouse(x, panelTopRow, mouseBtn1Clicked)
	if app.Model.CurrentState() != "todo" {
		t.Fatalf("tab click %s", app.Model.CurrentState())
	}
	working := cells[2]
	app.HandleMouse(layout[0].X+working.X+working.Width/2, panelTopRow, mouseBtn1Clicked)
	if app.Model.CurrentState() != "working" {
		t.Fatalf("tab click %s", app.Model.CurrentState())
	}
	if app.visibleColumnLayout()[0].State != "working" {
		t.Fatalf("visible %s", app.visibleColumnLayout()[0].State)
	}
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
	style := columnStripStyle(p, "todo", false)
	if style.GetBackground() != p.Headings["todo"] {
		t.Fatalf("todo background %v want %v", style.GetBackground(), p.Headings["todo"])
	}
	focused := columnStripStyle(p, "todo", true)
	if focused.GetForeground() == style.GetForeground() && focused.GetBackground() == style.GetBackground() {
		t.Fatal("focused strip matched the idle cell")
	}
}

func TestColumnStripClipsLongNames(t *testing.T) {
	app := stripBoardApp(t, 20, 20)
	nameLine := viewLine(app, panelTopRow)
	if strings.Contains(nameLine, "working") {
		t.Fatalf("unclipped name in %q", nameLine)
	}
	layout := app.visibleColumnLayout()
	cells := evenCells(layout[0].Width, len(app.Model.States()))
	inner := cells[2].Width - 1
	if inner < 1 {
		inner = cells[2].Width
	}
	if inner >= displayWidth("working") {
		t.Skip("cell wide enough for working")
	}
	got := clipText("working", inner)
	if !strings.Contains(nameLine, strings.TrimSpace(got)) && got != "" {
		t.Fatalf("clipped %q not in %q", got, nameLine)
	}
}
