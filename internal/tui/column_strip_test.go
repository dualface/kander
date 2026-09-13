package tui

import (
	"context"
	"strings"
	"testing"
	"time"

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

func TestEmptyColumnHintHasBlankLineAbove(t *testing.T) {
	for _, width := range []int{64, 80} {
		app := stripBoardApp(t, width, 20)
		app.Model.FocusState("review")
		blank := viewLine(app, bodyTop)
		hint := viewLine(app, bodyTop+1)
		if strings.Contains(blank, "empty") {
			t.Fatalf("width %d first body has hint: %q", width, blank)
		}
		if !strings.Contains(hint, "empty") {
			t.Fatalf("width %d second body missing hint: %q", width, hint)
		}
		app.Model.FocusState("backlog")
		card := viewLine(app, bodyTop)
		if !strings.Contains(card, "one") {
			t.Fatalf("width %d occupied first body %q", width, card)
		}
		if strings.Contains(card, "empty") {
			t.Fatalf("width %d occupied showed empty hint: %q", width, card)
		}
	}
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
	for _, want := range []string{"todo 1", "working 1"} {
		if !strings.Contains(names, want) {
			t.Fatalf("idle non-empty tab missing count %q in %q", want, names)
		}
	}
	if strings.Contains(names, "review 0") || strings.Contains(names, "done 0") {
		t.Fatalf("idle empty tab showed zero count: %q", names)
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
	if got := len(app.columnTabCells(32)); got != 5 {
		t.Fatalf("shortened tabs=%d, want 5 in %q", got, names)
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

func TestLongestIdleLabelSkipsUnshortenableCJK(t *testing.T) {
	states := []string{"backlog", "todo", "working"}
	labels := []string{"待办池", "待", "进"}
	if got := longestIdleLabel(states, labels, "backlog"); got != -1 {
		t.Fatalf("unshortenable idle picked %d label %q", got, labels[got])
	}
	labels = []string{"待办池", "待处理", "进"}
	if got := longestIdleLabel(states, labels, "backlog"); got != 1 {
		t.Fatalf("shortenable idle got %d, want 1", got)
	}
}

func TestColumnTabCellsCJKIdleShrinkTerminates(t *testing.T) {
	cases := []struct {
		lang    string
		width   int
		current string
		labels  map[string]string
	}{
		{
			lang: "zh-CN", width: 20, current: "backlog",
			labels: map[string]string{
				"backlog": "待办池", "todo": "待处理", "working": "进行中",
				"review": "审核中", "done": "已完成",
			},
		},
		{
			lang: "ja", width: 24, current: "backlog",
			labels: map[string]string{
				"backlog": "バックログ", "todo": "未着手", "working": "作業中",
				"review": "レビュー", "done": "完了",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			app := stripBoardApp(t, tc.width, 20)
			app.Context.StateLabels = tc.labels
			app.Model.SetBoard(BoardPayload{GeneratedAt: "t"})
			app.Model.FocusState(tc.current)
			result := make(chan []columnTabCell, 1)
			go func() {
				result <- app.columnTabCells(tc.width)
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			select {
			case cells := <-result:
				used := 0
				for _, cell := range cells {
					used += cell.width
				}
				if used > tc.width {
					t.Fatalf("%s width %d current %s used %d labels %v cells %+v", tc.lang, tc.width, tc.current, used, tc.labels, cells)
				}
				tabCellByState(t, cells, tc.current)
			case <-ctx.Done():
				t.Fatalf("%s width %d current %s labels %v hung", tc.lang, tc.width, tc.current, tc.labels)
			}
		})
	}
}
