package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
		t.Fatal("width 64 should show the strip")
	}
	lines := strings.Split(ansi.Strip(app.View()), "\n")
	if len(lines) < 3 {
		t.Fatalf("lines=%d", len(lines))
	}
	names, bot := lines[len(lines)-3], lines[len(lines)-2]
	if !strings.Contains(names, borderVertical) {
		t.Fatalf("name row missing side border: %q", names)
	}
	if !strings.Contains(bot, borderBottomLeft) || !strings.Contains(bot, borderHorizontal) {
		t.Fatalf("bottom row missing border: %q", bot)
	}
	for _, name := range []string{"backlog", "todo", "working", "review", "done"} {
		if !strings.Contains(names, name) {
			t.Fatalf("name row %q missing %s", names, name)
		}
	}
	wide := stripBoardApp(t, 65, 20)
	if wide.columnStripVisible() {
		t.Fatal("width 65 should hide the strip")
	}
	wideLines := strings.Split(ansi.Strip(wide.View()), "\n")
	if strings.Contains(wideLines[len(wideLines)-3], "backlog") && strings.Contains(wideLines[len(wideLines)-3], "done") {
		t.Fatalf("wide board kept a strip: %q", wideLines[len(wideLines)-3])
	}
}

func TestColumnStripClickSwitchesColumn(t *testing.T) {
	app := stripBoardApp(t, 64, 20)
	if app.Model.CurrentState() != "backlog" {
		t.Fatalf("start %s", app.Model.CurrentState())
	}
	cells := evenCells(64, len(app.Model.States()))
	todo := cells[1]
	x := todo.X + todo.Width/2
	top := 20 - 1 - columnStripHeight
	app.HandleMouse(x, top, mouseBtn1Clicked)
	if app.Model.CurrentState() != "todo" {
		t.Fatalf("name row click %s", app.Model.CurrentState())
	}
	working := cells[2]
	app.HandleMouse(working.X+working.Width/2, top+1, mouseBtn1Clicked)
	if app.Model.CurrentState() != "working" {
		t.Fatalf("bottom border click %s", app.Model.CurrentState())
	}
	if got := len(app.visibleColumnLayout()); got != 1 {
		t.Fatalf("visible columns %d", got)
	}
	if app.visibleColumnLayout()[0].State != "working" {
		t.Fatalf("visible %s", app.visibleColumnLayout()[0].State)
	}
}

func TestColumnStripShrinksBoardBody(t *testing.T) {
	narrow := stripBoardApp(t, 64, 20)
	wide := stripBoardApp(t, 80, 20)
	if narrow.boardBodyHeight() != wide.boardBodyHeight()-columnStripHeight {
		t.Fatalf("narrow body %d wide body %d", narrow.boardBodyHeight(), wide.boardBodyHeight())
	}
	if narrow.hitColumnStrip(1, 20-2) == "" {
		t.Fatal("strip hit missed")
	}
	if wide.hitColumnStrip(1, 20-2) != "" {
		t.Fatal("wide board hit a strip")
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
	mid := strings.Split(ansi.Strip(app.View()), "\n")
	nameLine := mid[len(mid)-3]
	if strings.Contains(nameLine, "working") {
		t.Fatalf("unclipped name in %q", nameLine)
	}
	cells := evenCells(20, len(app.Model.States()))
	inner := cells[2].Width - 2
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
