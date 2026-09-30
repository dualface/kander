package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/dualface/kander/internal/config"
)

func clusterTasks() []Task {
	return []Task{
		{TaskID: "A", Title: "alpha", State: "todo", TaskGroup: "20260930-g1"},
		{TaskID: "B", Title: "beta", State: "todo"},
		{TaskID: "C", Title: "gamma", State: "todo", TaskGroup: "20260930-g1"},
		{TaskID: "solo", Title: "only", State: "working", TaskGroup: "20260930-g1"},
	}
}

func collapseModel(tasks []Task) *BoardModel {
	model := newBoardModel(false)
	model.SetBoard(BoardPayload{Tasks: tasks})
	model.FocusState("todo")
	return model
}

func collapseApp(tasks []Task) *App {
	app := newApp(true, 30, tuiPageContext(),
		func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, nil },
		"light", 5, nil, func(string) (bool, string) { return true, "" })
	app.Width = 80
	app.Height = 30
	app.Model.SetBoard(BoardPayload{Tasks: tasks})
	app.Model.FocusState("todo")
	return app
}

func renderStateColumn(t *testing.T, model *BoardModel, state string) string {
	t.Helper()
	app := newApp(true, 30, tuiPageContext(),
		func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, nil },
		"light", 5, nil, nil)
	app.Model = model
	app.Model.FocusState(state)
	body := len(columnRows(model, state))
	if body < 1 {
		body = 1
	}
	return app.renderColumnPanel(themePalette("light"), boardLayout{
		State: state, Width: 44, Height: body + 2, BodyHeight: body,
		FirstVisual: true, LastVisual: true,
	}, 1)
}

func columnPlain(t *testing.T, model *BoardModel, state string) string {
	t.Helper()
	return ansi.Strip(renderStateColumn(t, model, state))
}

func TestGroupHeaderStartsExpanded(t *testing.T) {
	model := collapseModel(clusterTasks())
	if len(model.Collapsed) != 0 {
		t.Fatalf("flag stored before a collapse: %v", model.Collapsed)
	}
	rows := columnRows(model, "todo")
	if len(rows) == 0 || rows[0].kind != "header" || rows[0].group != "20260930-g1" {
		t.Fatalf("first row %#v", rows)
	}
	want := []string{"header", "card", "card", "card", "gap", "card", "card", "card", "gap", "card", "card", "card", "gap"}
	if len(rows) != len(want) {
		t.Fatalf("rows %d want %d", len(rows), len(want))
	}
	for i, kind := range want {
		if rows[i].kind != kind {
			t.Fatalf("row %d %s want %s", i, rows[i].kind, kind)
		}
	}
	if !rows[4].rail || rows[8].rail {
		t.Fatalf("rail gaps header-to-member %#v", rows)
	}
	plain := columnPlain(t, model, "todo")
	if !strings.Contains(plain, "▾") || strings.Contains(plain, "▸") {
		t.Fatalf("twistie:\n%s", plain)
	}
	for _, id := range []string{"A", "C", "B"} {
		if !strings.Contains(plain, id) {
			t.Fatalf("missing %s\n%s", id, plain)
		}
	}
	working := columnRows(model, "working")
	for _, row := range working {
		if row.kind == "header" {
			t.Fatal("singleton column drew a header")
		}
	}
	if !strings.Contains(columnPlain(t, model, "working"), "solo") {
		t.Fatal("singleton card missing")
	}
	if !strings.Contains(strings.Split(plain, "\n")[0], " 3 ") {
		t.Fatalf("badge %q", strings.Split(plain, "\n")[0])
	}
}

func TestGroupHeaderTextWithoutRail(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	text := groupHeaderText(compactGroup("20260930-g1"), 2, 40, false)
	if displayWidth(text) != 40 || !strings.HasPrefix(text, "▾ ") || !strings.HasSuffix(text, "2") || !strings.Contains(text, "g1") {
		t.Fatalf("header text %q", text)
	}
	narrow := groupHeaderText("group-name", 12, 4, true)
	if displayWidth(narrow) != 4 || !strings.HasSuffix(narrow, "12") {
		t.Fatalf("narrow header %q", narrow)
	}

	model := collapseModel(clusterTasks())
	app := newApp(true, 30, tuiPageContext(),
		func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, nil },
		"light", 5, nil, nil)
	app.Model = model
	app.Model.FocusState("todo")
	plain := ansi.Strip(renderStateColumn(t, model, "todo"))
	if !strings.Contains(plain, text) {
		t.Fatalf("rendered header missing %q\n%s", text, plain)
	}
	if strings.Contains(plain, "20260930-g1") {
		t.Fatal("raw group id leaked")
	}
	model.HeaderFocus["todo"] = "20260930-g1"
	p := themePalette("light")
	for _, collapsed := range []bool{false, true} {
		model.Collapsed["20260930-g1"] = collapsed
		header := app.renderGroupHeader(p, "todo", "20260930-g1", 2, 40, true)
		want := cardStyle(p, "todo", 0, true).Render(" " + groupHeaderText("g1", 2, 40, collapsed) + " ")
		if header != want || strings.Contains(ansi.Strip(header), "│") {
			t.Fatalf("collapsed=%v header style or rail: %q", collapsed, header)
		}
	}
}

func TestCollapseHidesOnlyClusteredColumn(t *testing.T) {
	model := collapseModel(clusterTasks())
	model.SelectTaskIndex("todo", 0)
	model.ToggleCollapsed("20260930-g1")
	if !model.Collapsed["20260930-g1"] {
		t.Fatal("collapse flag was not stored")
	}
	if model.HeaderFocus["todo"] != "20260930-g1" {
		t.Fatalf("focus %q", model.HeaderFocus["todo"])
	}
	if model.HeaderFocus["working"] != "" || model.SelectedIDs["working"] != "solo" {
		t.Fatalf("working focus header %q card %q", model.HeaderFocus["working"], model.SelectedIDs["working"])
	}
	todo := columnPlain(t, model, "todo")
	if strings.Contains(todo, "\nA") || strings.Contains(todo, " A") || strings.Contains(todo, "C") {
		t.Fatalf("collapsed members still drawn:\n%s", todo)
	}
	if !strings.Contains(todo, "▸") || strings.Contains(todo, "▾") {
		t.Fatalf("collapsed twistie:\n%s", todo)
	}
	if !strings.Contains(todo, "B") {
		t.Fatal("plain card hidden with the cluster")
	}
	working := columnPlain(t, model, "working")
	if !strings.Contains(working, "solo") || strings.Contains(working, "▸") || strings.Contains(working, "▾") {
		t.Fatalf("singleton changed:\n%s", working)
	}
	model.ToggleCollapsed("20260930-g1")
	model.FocusState("todo")
	if model.Collapsed["20260930-g1"] {
		t.Fatal("expand removed the remembered flag value")
	}
	if _, ok := model.Collapsed["20260930-g1"]; !ok {
		t.Fatal("expand dropped the flag")
	}
	if model.HeaderFocus["todo"] != "20260930-g1" || model.SelectedTask() != nil {
		t.Fatalf("header %q task %#v", model.HeaderFocus["todo"], model.SelectedTask())
	}
	if !strings.Contains(columnPlain(t, model, "todo"), "A") {
		t.Fatal("expand did not draw members")
	}
}

func TestGroupHeaderKeys(t *testing.T) {
	app := collapseApp(clusterTasks())
	app.Model.MoveFocusEdge(false)
	if app.Model.focusedHeaderGroup("todo") != "20260930-g1" {
		t.Fatal("home did not land on the header")
	}
	app.handleBoardKey("enter")
	if !app.Model.Collapsed["20260930-g1"] || app.Detail != nil {
		t.Fatal("enter on a header should collapse without opening detail")
	}
	app.handleBoardKey(" ")
	if app.Model.Collapsed["20260930-g1"] || app.Detail != nil {
		t.Fatal("space on a header should expand without opening detail")
	}
	app.handleBoardKey("j")
	if app.Model.SelectedTask() == nil || app.Model.SelectedTask().TaskID != "A" {
		t.Fatalf("j from header = %#v", app.Model.SelectedTask())
	}
	app.handleBoardKey(" ")
	if app.Model.Collapsed["20260930-g1"] || app.Detail != nil {
		t.Fatal("space on a card changed the group or opened detail")
	}
	app.handleBoardKey("z")
	if !app.Model.Collapsed["20260930-g1"] || app.Model.focusedHeaderGroup("todo") != "20260930-g1" {
		t.Fatal("z on a member did not collapse to the header")
	}
	app.handleBoardKey("j")
	if app.Model.SelectedTask() == nil || app.Model.SelectedTask().TaskID != "B" {
		t.Fatalf("j skipped to %#v", app.Model.SelectedTask())
	}
	app.handleBoardKey("k")
	if app.Model.focusedHeaderGroup("todo") != "20260930-g1" {
		t.Fatal("k did not return to the collapsed header")
	}
	app.HandleMouse(8, 8, mouseBtn5Pressed)
	if app.Model.SelectedTask() == nil || app.Model.SelectedTask().TaskID != "B" {
		t.Fatalf("wheel skipped to %#v", app.Model.SelectedTask())
	}
	app.handleBoardKey("k")
	app.Model.SelectTaskIndex("todo", 2)
	before := app.Model.Collapsed["20260930-g1"]
	app.handleBoardKey("z")
	if app.Model.Collapsed["20260930-g1"] != before || app.Model.SelectedIDs["todo"] != "B" {
		t.Fatal("z on a plain card toggled the group")
	}
	app.Model.MoveFocusEdge(false)
	copied := ""
	app.CopyFn = func(text string) (bool, string) {
		copied = text
		return true, ""
	}
	app.handleBoardKey("y")
	app.handleBoardKey("m")
	if copied != "" || app.CopyNotice != "" || app.TaskActions != nil {
		t.Fatalf("header y/m copied %q notice %q menu %v", copied, app.CopyNotice, app.TaskActions != nil)
	}
	app.handleBoardKey("enter")
	if app.Detail != nil {
		t.Fatal("enter on a header opened detail")
	}
}

func TestGroupHeaderClickAndDoubleClick(t *testing.T) {
	app := collapseApp(clusterTasks())
	panel, body, ok := focusedBody(t, app, "todo")
	if !ok {
		t.Fatal("todo panel missing")
	}
	x := panel.X + 4
	headerY := body
	when := time.Unix(1_700_000_100, 0)
	when = releaseClick(app, x, headerY, when)
	if !app.Model.Collapsed["20260930-g1"] || app.Detail != nil || app.Model.focusedHeaderGroup("todo") != "20260930-g1" {
		t.Fatal("single click should focus and collapse once")
	}
	when = releaseClick(app, x, headerY, when)
	if !app.Model.Collapsed["20260930-g1"] || app.Detail != nil {
		t.Fatal("the double-click release toggled again or opened detail")
	}
	when = when.Add(600 * time.Millisecond)
	when = releaseClick(app, x, headerY, when)
	when = releaseClick(app, x, headerY, when)
	if app.Model.Collapsed["20260930-g1"] || app.Detail != nil {
		t.Fatalf("second double-click collapsed=%v detail=%v", app.Model.Collapsed["20260930-g1"], app.Detail != nil)
	}

	rows := columnRows(app.Model, "todo")
	cardY := -1
	for i, row := range rows {
		if row.kind == "card" && row.cardLine == 0 && row.task == 0 {
			cardY = body + i
		}
	}
	if cardY < 0 {
		t.Fatal("member line missing")
	}
	releaseClick(app, x, cardY, when.Add(time.Second))
	if app.Model.Collapsed["20260930-g1"] || app.Detail != nil || app.Model.SelectedIDs["todo"] != "A" {
		t.Fatalf("member click collapsed=%v detail=%v id=%s", app.Model.Collapsed["20260930-g1"], app.Detail != nil, app.Model.SelectedIDs["todo"])
	}
	gapY := -1
	for i, row := range rows {
		if row.kind == "gap" && row.rail {
			gapY = body + i
			break
		}
	}
	hit := app.hitBoard(x, gapY)
	if hit == nil || hit.Kind != "column" {
		t.Fatalf("rail gap hit %#v", hit)
	}
}

func TestSearchDrawsCollapsedGroupExpanded(t *testing.T) {
	model := collapseModel(clusterTasks())
	model.SelectTaskIndex("todo", 0)
	model.ToggleCollapsed("20260930-g1")
	model.Query = "a"
	model.Normalize()
	if !model.Collapsed["20260930-g1"] {
		t.Fatal("search cleared the flag")
	}
	plain := columnPlain(t, model, "todo")
	if !strings.Contains(plain, "A") || !strings.Contains(plain, "C") || !strings.Contains(plain, "▾") {
		t.Fatalf("search did not draw the cluster:\n%s", plain)
	}
	model.SelectTaskIndex("todo", 0)
	model.Query = ""
	model.Normalize()
	if model.focusedHeaderGroup("todo") != "20260930-g1" {
		t.Fatal("clearing search left focus on a hidden member")
	}
	hidden := columnPlain(t, model, "todo")
	if strings.Contains(hidden, "A") || strings.Contains(hidden, "C") || !strings.Contains(hidden, "▸") {
		t.Fatalf("cleared search still draws members:\n%s", hidden)
	}
	if !strings.Contains(strings.Split(hidden, "\n")[0], " 3 ") {
		t.Fatalf("badge lost a hidden card: %q", strings.Split(hidden, "\n")[0])
	}
}

func TestCollapseFlagSurvivesRefresh(t *testing.T) {
	app := collapseApp(clusterTasks())
	writes := 0
	app.PersistColumns = func(int) (config.TUI, error) {
		writes++
		return config.TUI{}, nil
	}
	app.Model.SelectTaskIndex("todo", 0)
	app.handleBoardKey("z")
	if writes != 0 {
		t.Fatalf("toggle wrote config %d times", writes)
	}
	if !app.Model.Collapsed["20260930-g1"] {
		t.Fatal("flag missing")
	}
	app.Model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "N1", Title: "next", State: "todo", TaskGroup: "20260930-g1"},
		{TaskID: "N2", Title: "next-2", State: "todo", TaskGroup: "20260930-g1"},
	}})
	if !app.Model.Collapsed["20260930-g1"] {
		t.Fatal("SetBoard cleared the flag")
	}
	plain := columnPlain(t, app.Model, "todo")
	if strings.Contains(plain, "N1") || strings.Contains(plain, "N2") {
		t.Fatalf("new cards ignored the flag:\n%s", plain)
	}
	app.Model.ToggleArchived()
	if !app.Model.Collapsed["20260930-g1"] {
		t.Fatal("archived toggle cleared the flag")
	}
}

func TestColumnWindowKeepsFocusedRowVisible(t *testing.T) {
	tasks := make([]Task, 0, 8)
	for i := range 6 {
		tasks = append(tasks, Task{TaskID: "P" + itoa(i), Title: "plain", State: "todo"})
	}
	tasks = append(tasks,
		Task{TaskID: "M1", Title: "member", State: "todo", TaskGroup: "20260930-g1"},
		Task{TaskID: "M2", Title: "member-2", State: "todo", TaskGroup: "20260930-g1"},
	)
	model := collapseModel(tasks)
	model.SelectTaskIndex("todo", len(model.TasksFor("todo"))-1)
	rows := columnRows(model, "todo")
	stops := focusStops(rows)
	var card focusStop
	for _, stop := range stops {
		if stop.kind == "card" && stop.task == len(model.TasksFor("todo"))-1 {
			card = stop
		}
	}
	_, scroll := columnWindow(model, "todo", 8)
	if card.start < scroll || card.start+card.height > scroll+8 {
		t.Fatalf("card start %d height %d scroll %d", card.start, card.height, scroll)
	}
	model.FocusGroupHeader("todo", "20260930-g1")
	_, scroll = columnWindow(model, "todo", 5)
	header := -1
	for i, row := range columnRows(model, "todo") {
		if row.kind == "header" {
			header = i
		}
	}
	if header < scroll || header >= scroll+5 {
		t.Fatalf("header %d scroll %d", header, scroll)
	}
	model.SelectTaskIndex("todo", len(model.TasksFor("todo"))-1)
	_, pinned := columnWindow(model, "todo", 2)
	if pinned != card.start {
		t.Fatalf("short body pinned %d want %d", pinned, card.start)
	}
}

func TestArchivedClusterCollapses(t *testing.T) {
	model := newBoardModel(false)
	model.ShowArchived = true
	model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "AR1", Title: "arch one", State: "archived", TaskGroup: "20260930-arch"},
		{TaskID: "AR2", Title: "arch two", State: "archived", TaskGroup: "20260930-arch"},
	}})
	model.FocusState("archived")
	rows := columnRows(model, "archived")
	if len(rows) == 0 || rows[0].kind != "header" {
		t.Fatal("archived cluster has no header")
	}
	plain := columnPlain(t, model, "archived")
	if !strings.Contains(plain, "▾") || !strings.Contains(plain, "AR1") {
		t.Fatalf("archived expanded:\n%s", plain)
	}
	model.SelectTaskIndex("archived", 0)
	model.ToggleCollapsed("20260930-arch")
	hidden := columnPlain(t, model, "archived")
	if strings.Contains(hidden, "AR1") || strings.Contains(hidden, "AR2") || !strings.Contains(hidden, "▸") {
		t.Fatalf("archived collapsed:\n%s", hidden)
	}
	if model.HeaderFocus["archived"] != "20260930-arch" {
		t.Fatal("archived focus did not move to the header")
	}
}

func TestCompactClusterUsesSameRows(t *testing.T) {
	tasks := []Task{
		{TaskID: "T1", Title: "one", State: "todo", TaskGroup: "20260930-g1"},
		{TaskID: "T2", Title: "two", State: "todo", TaskGroup: "20260930-g1"},
	}
	app := compactLayoutApp(minColumnWidth*2+1, 80, tasks)
	app.Model.FocusState("todo")
	rows := columnRows(app.Model, "todo")
	panel := layoutPanelForState(app.visibleColumnLayout(), "todo")
	if panel == nil || panel.BodyHeight < len(rows) {
		t.Fatalf("panel %#v rows %d", panel, len(rows))
	}
	body := panel.Y
	if !panel.SkipTop {
		body++
	}
	for i, row := range rows {
		hit := app.hitBoard(panel.X+4, body+i)
		switch row.kind {
		case "header":
			if hit == nil || hit.Kind != "header" || hit.Group != row.group {
				t.Fatalf("row %d header hit %#v", i, hit)
			}
		case "card":
			if hit == nil || hit.Kind != "task" || hit.Index != row.task || hit.CardLine != row.cardLine {
				t.Fatalf("row %d card hit %#v", i, hit)
			}
		default:
			if hit == nil || hit.Kind != "column" {
				t.Fatalf("row %d gap hit %#v", i, hit)
			}
		}
	}
}

func TestToggleTaskGroupHelp(t *testing.T) {
	found := false
	for _, entry := range (&App{}).boardHelpGroups()[0].Entries {
		if entry.Keys == "z" {
			found = true
			if entry.Desc == "" || entry.Desc == "tui.toggle_task_group" {
				t.Fatalf("help desc %q", entry.Desc)
			}
		}
	}
	if !found {
		t.Fatal("help missing z")
	}
	wants := map[string]string{
		"en.json":    "collapse or expand task group",
		"zh-CN.json": "折叠或展开任务组",
		"ja.json":    "タスクグループを折りたたむ／展開",
	}
	for name, want := range wants {
		data, err := os.ReadFile(filepath.Join("..", "i18n", "locales", name))
		if err != nil {
			t.Fatal(err)
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			t.Fatal(err)
		}
		if msgs["tui.toggle_task_group"] != want {
			t.Fatalf("%s %q", name, msgs["tui.toggle_task_group"])
		}
	}
}

func focusedBody(t *testing.T, app *App, state string) (boardLayout, int, bool) {
	t.Helper()
	panel := layoutPanelForState(app.visibleColumnLayout(), state)
	if panel == nil {
		return boardLayout{}, 0, false
	}
	body := panel.Y
	if !panel.SkipTop {
		body++
	}
	return *panel, body, true
}

func releaseClick(app *App, x, y int, when time.Time) time.Time {
	applyMouse(app, x, y, buttonLeft, when)
	when = when.Add(20 * time.Millisecond)
	applyMouse(app, x, y, buttonNone, when)
	return when.Add(20 * time.Millisecond)
}
