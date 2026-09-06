package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

func TestDisplayHelpers(t *testing.T) {
	if displayWidth("任务") != 4 {
		t.Fatalf("width=%d", displayWidth("任务"))
	}
	if got := clipText("任务标题", 5); got != "任..." {
		t.Fatalf("clip=%q", got)
	}
	if compactTime("2026-08-21 18:00") != "08-21 18:00" {
		t.Fatal(compactTime("2026-08-21 18:00"))
	}
	if compactTime("-") != "-" {
		t.Fatal(compactTime("-"))
	}
	if compactGroup("20260820-terminal-group") != "terminal-group" {
		t.Fatal(compactGroup("20260820-terminal-group"))
	}
	if clipText("A\rB\x1bC", 10) != "A B C" {
		t.Fatalf("%q", clipText("A\rB\x1bC", 10))
	}
	wrapped := wrapText("任务标题", 4)
	if len(wrapped) != 2 || wrapped[0] != "任务" || wrapped[1] != "标题" {
		t.Fatalf("%v", wrapped)
	}
	task := Task{Title: "Alpha", TaskID: "id", TaskGroup: "group-one", Type: "Feature", Assignee: "Codex", State: "todo"}
	for _, kw := range []string{"alpha", "ID", "group-one", "feature", "codex", "todo"} {
		if !taskMatches(task, kw) {
			t.Fatalf("expected match %q", kw)
		}
	}
	if taskMatches(task, "missing") {
		t.Fatal("unexpected match")
	}
}

func TestVisibleColumns(t *testing.T) {
	// 用户设定同屏栏目数; 终端放不下 desired*minColumnWidth 时自动减少栏数.
	for _, tc := range []struct {
		name                  string
		width, total, desired int
		want                  int
	}{
		{"宽度充裕按用户设定", 160, 5, 3, 3},
		{"宽度充裕也不超过栏目总数", 400, 2, 5, 2},
		// 3 栏还要占 2 列分隔线, 所以"刚好放下"是 minColumnWidth*3+2.
		{"刚好放下", minColumnWidth*3 + 2, 5, 3, 3},
		{"差一列就减一栏", minColumnWidth*3 + 1, 5, 3, 2},
		{"放不下两栏最小宽度就只显示一栏", minColumnWidth * 2, 5, 4, 1},
		{"比最小宽度还窄也只显示一栏", minColumnWidth - 5, 5, 4, 1},
		{"够两栏最小宽度加分隔线才排两栏", minColumnWidth*2 + 1, 5, 4, 2},
		{"用户只要一栏", 400, 5, 1, 1},
		{"越界的设定被夹住", 400, 5, 99, 5},
	} {
		if got := visibleColumnCount(tc.width, tc.total, false, tc.desired, minColumnWidth); got != tc.want {
			t.Fatalf("%s: width=%d desired=%d got=%d want=%d", tc.name, tc.width, tc.desired, got, tc.want)
		}
	}
	if visibleColumnCount(400, 4, true, 4, minColumnWidth) != 1 {
		t.Fatal("single")
	}
	if got := visibleColumnCount(81, 5, false, 4, 40); got != 2 {
		t.Fatalf("min width 40 should fit 2 columns in 81, got %d", got)
	}
	// 结算出的栏宽必须铺满终端宽度.
	layout := columnGeometry(160, 3)
	total := 0
	for _, col := range layout {
		total += col.Width
		if col.HasSeparator {
			total++
		}
		if col.Width < minColumnWidth {
			t.Fatalf("column narrower than the minimum: %+v", layout)
		}
	}
	if total != 160 {
		t.Fatalf("columns must fill the width, got %d: %+v", total, layout)
	}
	// 回归: 结算栏数时漏算分隔线, 会让每栏都窄于用户设定的最小宽度.
	for _, minWidth := range []int{minMinColumnWidth, 40, 48, maxMinColumnWidth} {
		for width := 40; width <= 400; width++ {
			count := visibleColumnCount(width, 5, false, maxColumns(), minWidth)
			if count < 2 {
				continue
			}
			for _, col := range columnGeometry(width, count) {
				if col.Width < minWidth {
					t.Fatalf("width=%d min=%d count=%d: column %d narrower than the minimum", width, minWidth, count, col.Width)
				}
			}
		}
	}
}

func TestModelNavigationAndSearch(t *testing.T) {
	model := newBoardModel(true)
	model.SetBoard(BoardPayload{
		GeneratedAt: "2026-08-20 22:30:00",
		Tasks: []Task{
			{TaskID: "20260820-first-task", Title: "第一项", State: "todo", TaskGroup: "20260820-terminal-group", Type: "Feature", Assignee: "Codex"},
			{TaskID: "20260820-second-task", Title: "Second item", State: "todo", Type: "Bug"},
			{TaskID: "20260820-old-task", Title: "Old item", State: "archived", Type: "Chore", Assignee: "QA"},
		},
	})
	if strings.Join(model.States(), ",") != strings.Join(activeStates, ",") {
		t.Fatalf("%v", model.States())
	}
	model.ColumnIndex = indexOf(model.States(), "todo")
	if model.SelectedTask().TaskID != "20260820-first-task" {
		t.Fatal(model.SelectedTask())
	}
	model.MoveTask(1)
	if model.SelectedTask().TaskID != "20260820-second-task" {
		t.Fatal(model.SelectedTask())
	}
	model.Query = "terminal-group"
	model.Normalize()
	todo := model.TasksFor("todo")
	if len(todo) != 1 || todo[0].TaskID != "20260820-first-task" {
		t.Fatalf("%v", todo)
	}
	model.Query = "qa"
	model.Normalize()
	if len(model.TasksFor("todo")) != 0 {
		t.Fatal("todo should be empty")
	}
	arch := model.TasksFor("archived")
	if len(arch) != 1 || arch[0].TaskID != "20260820-old-task" {
		t.Fatalf("%v", arch)
	}
	model.Query = ""
	model.ToggleArchived()
	if strings.Join(model.States(), ",") != strings.Join(allStates, ",") {
		t.Fatalf("%v", model.States())
	}
	model.ColumnIndex = 0
	model.MoveColumn(-1)
	if model.CurrentState() != "trash" {
		t.Fatal(model.CurrentState())
	}
	model.ToggleArchived()
	if model.CurrentState() != "done" {
		t.Fatal(model.CurrentState())
	}
}

func TestKeepSelectionOnRefresh(t *testing.T) {
	makeTasks := func(ids []string, prefix string) []Task {
		out := make([]Task, len(ids))
		for i, id := range ids {
			out[i] = Task{TaskID: id, Title: prefix + id, State: "todo"}
		}
		return out
	}
	ids := make([]string, 8)
	for i := 0; i < 8; i++ {
		ids[i] = "20260820-keep-" + pad2(i) + "-task"
	}
	model := newBoardModel(false)
	model.SetBoard(BoardPayload{GeneratedAt: "t1", Tasks: makeTasks(ids, "")})
	model.ColumnIndex = indexOf(model.States(), "todo")
	model.SelectedIDs["todo"] = ids[4]
	model.SelectedIndexes["todo"] = 4
	model.Scrolls["todo"] = 2
	model.SetBoard(BoardPayload{GeneratedAt: "t2", Tasks: makeTasks(ids, "updated-")})
	if model.SelectedIDs["todo"] != ids[4] || model.Scrolls["todo"] != 2 {
		t.Fatalf("%s %d", model.SelectedIDs["todo"], model.Scrolls["todo"])
	}
	remaining := append(append([]string{}, ids[:4]...), ids[5:]...)
	model.SetBoard(BoardPayload{GeneratedAt: "t3", Tasks: makeTasks(remaining, "")})
	if model.SelectedIDs["todo"] != ids[5] {
		t.Fatalf("got %s want %s", model.SelectedIDs["todo"], ids[5])
	}
	if model.SetBoard(BoardPayload{GeneratedAt: "t4", Tasks: makeTasks(remaining, "")}) {
		t.Fatal("same content should not change")
	}
}

func TestThemeCycle(t *testing.T) {
	app := newApp(true, 60, pageContext{}, func() (BoardPayload, error) { return BoardPayload{}, nil }, func(string) (Task, error) { return Task{}, nil }, "auto", 40, nil, func(string) (bool, string) { return true, "" })
	seen := []string{}
	for range themes {
		app.handleBoardKey("t")
		seen = append(seen, app.Theme)
	}
	if strings.Join(seen, ",") != "light,dark,auto" {
		t.Fatalf("%v", seen)
	}
}

func TestThemePaletteFillsBackground(t *testing.T) {
	dark := themePalette("dark")
	light := themePalette("light")
	if dark.Bg == light.Bg {
		t.Fatal("light and dark should paint different canvas backgrounds")
	}
	if dark.Bg != "0" || light.Bg != "15" {
		t.Fatalf("dark bg=%q light bg=%q", dark.Bg, light.Bg)
	}
	filled := paintScreen("", 8, 2, light)
	if got := len(strings.Split(filled, "\n")); got != 2 {
		t.Fatalf("paintScreen height=%d", got)
	}
}

func TestResolveThemeUsesCachedDetection(t *testing.T) {
	originalDetector := detectDarkBackground
	t.Cleanup(func() { detectDarkBackground = originalDetector })
	detections := 0
	detectDarkBackground = func() bool {
		detections++
		return false
	}

	if got := resolveTheme("dark"); got != "dark" || detections != 0 {
		t.Fatalf("explicit dark: theme=%q detections=%d", got, detections)
	}
	if got := resolveTheme("light"); got != "light" || detections != 0 {
		t.Fatalf("explicit light: theme=%q detections=%d", got, detections)
	}

	if got := resolveTheme("auto"); got != "light" || detections != 1 {
		t.Fatalf("cached terminal result: theme=%q detections=%d", got, detections)
	}
	detectDarkBackground = func() bool {
		detections++
		return true
	}
	if got := resolveTheme("auto"); got != "dark" || detections != 2 {
		t.Fatalf("cached dark result: theme=%q detections=%d", got, detections)
	}
}

func TestSelectionHelpers(t *testing.T) {
	if got := extractCharSelection([]string{"alpha", "beta"}, [2]int{0, 1}, [2]int{1, 2}); got != "lpha\nbe" {
		t.Fatalf("%q", got)
	}
	if got := extractLineSelection([]string{"alpha", "beta", "gamma"}, [2]int{1, 0}, [2]int{1, 3}); got != "beta" {
		t.Fatalf("%q", got)
	}
	if displayColumnToCharIndex("a中b", 2) != 1 {
		t.Fatal(displayColumnToCharIndex("a中b", 2))
	}
	if charIndexToDisplayColumn("a中b", 2) != 3 {
		t.Fatal(charIndexToDisplayColumn("a中b", 2))
	}
	if displayColumnToCaretIndex("复制测试", 7) != 4 {
		t.Fatal(displayColumnToCaretIndex("复制测试", 7))
	}
	if extractMouseCharSelection([]string{"hello"}, [2]int{0, 4}, [2]int{0, 0}) != "hello" {
		t.Fatal("mouse sel")
	}
	if extractMouseCharSelection([]string{"hello"}, [2]int{0, 2}, [2]int{0, 2}) != "" {
		t.Fatal("empty sel")
	}
	if !mouseLeftPressed(mouseBtn1Pressed) {
		t.Fatal("pressed")
	}
	if mouseLeftClicked(mouseBtn1Pressed) {
		t.Fatal("pressed is not click")
	}
	if !mouseLeftClicked(mouseBtn1Clicked) {
		t.Fatal("clicked")
	}
}

func TestCopyAndDetailKeys(t *testing.T) {
	copied := []string{}
	board := BoardPayload{
		GeneratedAt: "2026-08-22 12:00:00",
		Tasks: []Task{{
			TaskID: "20260822-copy-task", Title: "复制测试", State: "todo", Type: "Feature", Kind: "small", Time: "-",
		}},
	}
	app := newApp(true, 60, pageContext{Copied: "Copied", Unassigned: "未指派"}, func() (BoardPayload, error) { return board, nil }, func(id string) (Task, error) {
		return Task{TaskID: id, Title: "复制测试", State: "todo", Document: "line one\nline two\nline three\n"}, nil
	}, "auto", 40, nil, func(text string) (bool, string) {
		copied = append(copied, text)
		return true, ""
	})
	app.Width, app.Height = 100, 24
	app.Model.SetBoard(board)
	app.Model.ColumnIndex = indexOf(app.Model.States(), "todo")
	app.copySelectedTaskID()
	if len(copied) != 1 || copied[0] != "20260822-copy-task" {
		t.Fatalf("%v", copied)
	}
	task, _ := app.GetTask("20260822-copy-task")
	app.Detail = &task
	app.handleDetailKey("j")
	if app.DetailCursor != [2]int{1, 0} {
		t.Fatalf("%v", app.DetailCursor)
	}
}

func TestColumnCountPrefs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv(config.EnvConfig, configPath)
	if loadPrefs().Columns != defaultColumns {
		t.Fatalf("default %d", loadPrefs().Columns)
	}
	if _, err := saveColumns(2); err != nil {
		t.Fatal(err)
	}
	if loadPrefs().Columns != 2 {
		t.Fatalf("saved %d", loadPrefs().Columns)
	}
	cfg, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TUI.Columns != 2 {
		t.Fatalf("config columns %d", cfg.TUI.Columns)
	}
	if loadPrefs().MinColumnWidth != minColumnWidth {
		t.Fatalf("min width default %d", loadPrefs().MinColumnWidth)
	}
	ui := loadPrefs()
	ui.MinColumnWidth = 32
	if _, err := savePrefs(ui); err != nil {
		t.Fatal(err)
	}
	if loadPrefs().MinColumnWidth != 32 {
		t.Fatalf("saved min width %d", loadPrefs().MinColumnWidth)
	}
	if loadPrefs().Columns != 2 {
		t.Fatal("saving min width must keep columns")
	}
	saved := []int{}
	app := newApp(false, 30, pageContext{}, func() (BoardPayload, error) { return BoardPayload{}, nil }, func(string) (Task, error) { return Task{}, nil }, "auto", 3, func(n int) (config.TUI, error) {
		saved = append(saved, n)
		return config.TUI{Columns: n}, nil
	}, nil)
	app.handleBoardKey("-")
	if app.Columns != 2 || len(saved) != 1 || saved[0] != 2 {
		t.Fatalf("%d %v", app.Columns, saved)
	}
	app.handleBoardKey("=")
	if app.Columns != 3 {
		t.Fatalf("%d", app.Columns)
	}
	// 栏目数有上下界: 最少 1 栏, 最多把活跃栏目全放下.
	for i := 0; i < 20; i++ {
		app.handleBoardKey("-")
	}
	if app.Columns != minColumns {
		t.Fatalf("min %d", app.Columns)
	}
	for i := 0; i < 20; i++ {
		app.handleBoardKey("=")
	}
	if app.Columns != maxColumns() {
		t.Fatalf("max %d", app.Columns)
	}
}

func TestPageKeys(t *testing.T) {
	tasks := make([]Task, 10)
	for i := 0; i < 10; i++ {
		tasks[i] = Task{TaskID: "20260821-page-0" + itoa(i) + "-task", Title: itoa(i), State: "todo"}
	}
	app := newApp(true, 30, pageContext{}, func() (BoardPayload, error) { return BoardPayload{Tasks: tasks}, nil }, func(string) (Task, error) { return Task{}, nil }, "auto", 40, nil, nil)
	app.Width, app.Height = 80, 24
	app.Model.SetBoard(BoardPayload{Tasks: tasks})
	app.Model.ColumnIndex = indexOf(app.Model.States(), "todo")
	app.handleBoardKey("pgdn")
	if app.Model.SelectedIndexes["todo"] == 0 {
		t.Fatal("page did not move")
	}
}

func TestRenderKeepsFocusColumn(t *testing.T) {
	tasks := []Task{}
	for _, state := range []string{"backlog", "todo", "working", "done"} {
		tasks = append(tasks, Task{TaskID: "20260821-" + state + "-task", Title: state, State: state})
	}
	// 同屏只要 2 栏, 便于观察左右切换时选中栏是否始终可见.
	app := newApp(false, 30, pageContext{
		StateLabels: map[string]string{"backlog": "backlog", "todo": "todo", "working": "working", "review": "review", "done": "done"},
		Empty:       "No tasks",
		TooSmall:    "Terminal is too small.",
	}, func() (BoardPayload, error) { return BoardPayload{Tasks: tasks}, nil }, func(string) (Task, error) { return Task{}, nil }, "auto", 2, nil, nil)
	app.Width, app.Height = 122, 24
	app.Model.SetBoard(BoardPayload{GeneratedAt: "2026-08-21 00:00:00", Tasks: tasks})
	headings := viewLine(app, panelTopRow)
	if !strings.Contains(headings, "backlog") || strings.Contains(headings, "done") {
		t.Fatalf("headings %q", headings)
	}
	app.Model.MoveColumn(3)
	headings = viewLine(app, panelTopRow)
	if !strings.Contains(headings, "review") || strings.Contains(headings, "backlog") {
		t.Fatalf("focused %q", headings)
	}
	// 终端窄到放不下 2 栏最小宽度时收敛成一栏, 而不是硬排两栏.
	app.Width = minColumnWidth + 4
	if strings.Contains(strings.ToLower(viewLine(app, panelTopRow)+viewLine(app, headerRow)), "too small") {
		t.Fatal("narrow should still render")
	}
	if got := len(app.visibleColumnLayout()); got != 1 {
		t.Fatalf("narrow terminal should fall back to one column, got %d", got)
	}
}

func TestDefaultRunRejectsNonTTY(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	config.ApplyLanguageArgument(nil)
	oldOut, oldErr := os.Stdout, os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	os.Stdout, os.Stderr = devNull, devNull
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	if Run(nil) == 0 {
		t.Fatal("non tty")
	}
}

func TestJSONContentKeyIgnoresGeneratedAt(t *testing.T) {
	a := []Task{{TaskID: "t", Title: "x", State: "todo"}}
	b := []Task{{TaskID: "t", Title: "x", State: "todo"}}
	if boardContentKey(a) != boardContentKey(b) {
		t.Fatal(boardContentKey(a))
	}
	raw, _ := json.Marshal(a)
	if !strings.Contains(string(raw), "task_id") {
		t.Fatal(string(raw))
	}
}

func applyMouse(app *App, x, y int, buttons mouseButtons, when time.Time) {
	app.HandleMouse(x, y, app.mouse.mapButtons(x, y, buttons, when))
}

func TestMouseSelectsTask(t *testing.T) {
	board := BoardPayload{GeneratedAt: "t", Tasks: []Task{
		{TaskID: "20260821-one-task", Title: "一号", State: "backlog", Type: "Feature", Kind: "small", Time: "-"},
		{TaskID: "20260821-two-task", Title: "二号", State: "backlog", Type: "Feature", Kind: "small", Time: "-"},
		{TaskID: "20260821-todo-task", Title: "待办", State: "todo", Type: "Feature", Kind: "small", Time: "-"},
	}}
	app := newApp(false, 30, pageContext{StateLabels: map[string]string{"backlog": "backlog", "todo": "todo"}}, func() (BoardPayload, error) { return board, nil }, func(id string) (Task, error) {
		return Task{TaskID: id, Title: id, Document: strings.Repeat("line\n", 40)}, nil
	}, "auto", 40, nil, nil)
	app.Width, app.Height = 120, 24
	app.Model.SetBoard(board)
	app.Model.FocusState("todo")
	app.Model.SelectTaskIndex("backlog", 1)
	now := time.Unix(1_700_000_000, 0)
	if got := app.mouse.mapButtons(2, bodyTop, buttonLeft, now); got != mouseBtn1Pressed {
		t.Fatalf("press %d", got)
	}
	app.HandleMouse(2, bodyTop, mouseBtn1Pressed)
	rel := app.mouse.mapButtons(2, bodyTop, buttonNone, now.Add(20*time.Millisecond))
	if rel&mouseBtn1Released == 0 || rel&mouseBtn1Clicked != 0 {
		t.Fatalf("release %d", rel)
	}
	app.HandleMouse(2, bodyTop, rel)
	if app.Model.CurrentState() != "backlog" {
		t.Fatal(app.Model.CurrentState())
	}
	if app.Model.SelectedIDs["backlog"] != "20260821-one-task" {
		t.Fatal(app.Model.SelectedIDs["backlog"])
	}

	hoverApp := newApp(false, 30, pageContext{StateLabels: map[string]string{"backlog": "backlog", "todo": "todo"}}, func() (BoardPayload, error) { return board, nil }, func(id string) (Task, error) {
		return Task{TaskID: id, Title: id, Document: "body"}, nil
	}, "auto", 40, nil, nil)
	hoverApp.Width, hoverApp.Height = 120, 24
	hoverApp.Model.SetBoard(board)
	hoverApp.Model.FocusState("todo")
	before := hoverApp.Model.CurrentState()
	applyMouse(hoverApp, 2, bodyTop, buttonNone, time.Time{})
	if hoverApp.Model.CurrentState() != before {
		t.Fatal("hover must not click")
	}
	ctrl := hoverApp.mouse.mapButtons(2, bodyTop, buttonLeft, now.Add(time.Second))
	if ctrl&mouseBtn1Double != 0 {
		t.Fatal("ctrl/button1 is not double-click")
	}

	dbl := newApp(false, 30, pageContext{StateLabels: map[string]string{"backlog": "backlog", "todo": "todo"}}, func() (BoardPayload, error) { return board, nil }, func(id string) (Task, error) {
		return Task{TaskID: id, Title: id, Document: "body"}, nil
	}, "auto", 40, nil, nil)
	dbl.Width, dbl.Height = 120, 24
	dbl.Model.SetBoard(board)
	t1 := now.Add(2 * time.Second)
	for _, step := range []struct {
		btn  mouseButtons
		when time.Time
	}{
		{buttonLeft, t1},
		{buttonNone, t1.Add(10 * time.Millisecond)},
		{buttonLeft, t1.Add(20 * time.Millisecond)},
		{buttonNone, t1.Add(30 * time.Millisecond)},
	} {
		st := dbl.mouse.mapButtons(2, bodyTop, step.btn, step.when)
		dbl.HandleMouse(2, bodyTop, st)
	}
	if dbl.Detail == nil || dbl.Detail.TaskID != "20260821-one-task" {
		t.Fatal("double-click should open detail")
	}
}

func TestDetailRendersMarkdown(t *testing.T) {
	board := BoardPayload{Tasks: []Task{{TaskID: "20260822-copy-task", Title: "复制测试", State: "todo"}}}
	document := "# 标题\n\n- 第一项\n- 第二项\n"
	app := newApp(true, 60, pageContext{Unassigned: "未指派"}, func() (BoardPayload, error) { return board, nil }, func(id string) (Task, error) {
		return Task{TaskID: id, Title: "复制测试", State: "todo", Document: document}, nil
	}, "auto", 40, nil, nil)
	app.Width, app.Height = 80, 24
	task, _ := app.GetTask("20260822-copy-task")
	app.Detail = &task

	lines := app.detailLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "标题") || !strings.Contains(joined, "第一项") {
		t.Fatalf("markdown not rendered: %q", joined)
	}
	if strings.Contains(joined, "# 标题") {
		t.Fatalf("markdown markers should be consumed: %q", joined)
	}

	app.handleDetailKey("j")
	if app.DetailCursor[0] < 0 || app.DetailCursor[0] > len(lines)-1 {
		t.Fatalf("cursor out of range: %v", app.DetailCursor)
	}

	view := stripANSI(app.View())
	if !strings.Contains(view, "复制测试") || !strings.Contains(view, "第二项") {
		t.Fatalf("detail view missing content: %q", view)
	}
}

func TestRefreshOpenDetailUpdatesState(t *testing.T) {
	state := "todo"
	app := newApp(true, 30, pageContext{}, func() (BoardPayload, error) { return BoardPayload{}, nil }, func(id string) (Task, error) {
		return Task{TaskID: id, Title: "same", State: state, Time: "-", Document: "body"}, nil
	}, "auto", 40, nil, nil)
	first, _ := app.GetTask("t1")
	app.Detail = &first
	state = "working"
	if !app.refreshOpenDetail() {
		t.Fatal("expected metadata change")
	}
	if app.Detail.State != "working" {
		t.Fatal(app.Detail.State)
	}
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func indexOf(list []string, value string) int {
	for i, item := range list {
		if item == value {
			return i
		}
	}
	return 0
}

func TestAutoRefreshInterval(t *testing.T) {
	calls := 0
	payload := BoardPayload{GeneratedAt: "0", Tasks: []Task{{TaskID: "a", Title: "a", State: "todo"}}}
	app := newApp(true, 1, pageContext{}, func() (BoardPayload, error) {
		calls++
		payload.GeneratedAt = itoa(calls)
		payload.Tasks[0].Title = "n" + itoa(calls)
		return payload, nil
	}, func(string) (Task, error) { return Task{}, nil }, "auto", 40, nil, nil)
	now := time.Now()
	app.Now = func() time.Time { return now }
	app.Model.SetBoard(payload)
	app.LastRefresh = now.Add(-2 * time.Second)
	if !app.refreshBoard() {
		t.Fatal("expected change")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

// stripANSI 去掉样式转义, 便于断言渲染后的可见文本.
func stripANSI(text string) string {
	return ansi.Strip(text)
}

// viewLine 返回渲染结果里第 index 行的可见文本.
func viewLine(app *App, index int) string {
	lines := strings.Split(stripANSI(app.View()), "\n")
	if index < 0 || index >= len(lines) {
		return ""
	}
	return lines[index]
}

// 看板界面调整栏目数会直接写 config.json; 缓存的选项会话必须同步基线,
// 否则随后的保存会误报「配置已被其他进程修改」.
func TestAdjustColumnsKeepsOptionsSessionInSync(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(dir, "config.json"))
	if _, err := config.Update(func(cfg *config.Config) error {
		cfg.WelcomeComplete = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	existing, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := menu.NewSessionForTest(existing)
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(false, 30, pageContext{}, func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, nil }, "auto", 3, saveColumns, nil)
	app.Session = session
	app.handleBoardKey("-")
	if app.PrefsError != "" {
		t.Fatalf("persist columns: %s", app.PrefsError)
	}
	if app.Columns != 2 {
		t.Fatalf("columns %d", app.Columns)
	}
	if _, err := session.Save(); err != nil {
		t.Fatalf("save after column change: %v", err)
	}
	saved, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.TUI.Columns != 2 {
		t.Fatalf("config columns %d", saved.TUI.Columns)
	}
}
