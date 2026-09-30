package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestColumnClustersOrderAndRail(t *testing.T) {
	model := newBoardModel(true)
	model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "A", Title: "alpha", State: "todo", TaskGroup: "20260930-g1"},
		{TaskID: "B", Title: "beta", State: "todo"},
		{TaskID: "C", Title: "gamma", State: "todo", TaskGroup: "20260930-g1"},
		{TaskID: "D", Title: "delta", State: "todo", TaskGroup: "20260930-g2"},
		{TaskID: "E", Title: "epsilon", State: "todo", TaskGroup: "20260930-g2"},
	}})
	got := taskIDs(model.TasksFor("todo"))
	if strings.Join(got, ",") != "A,C,B,D,E" {
		t.Fatalf("order %v", got)
	}
	pads := columnLeftPads(t, model, "todo")
	want := []rune{
		'│',
		'│', '│', '│', '│',
		'│', '│', '│', ' ',
		' ', ' ', ' ', ' ',
		'│',
		'│', '│', '│', '│',
		'│', '│', '│', ' ',
	}
	if string(pads) != string(want) {
		t.Fatalf("pads %q want %q", string(pads), string(want))
	}
	plain := ansi.Strip(renderTodoColumn(t, model))
	if strings.Contains(plain, "20260930-g1") || strings.Contains(plain, "20260930-g2") {
		t.Fatalf("raw group id leaked:\n%s", plain)
	}
	if !strings.Contains(plain, "g1") || !strings.Contains(plain, "g2") {
		t.Fatalf("compact group missing:\n%s", plain)
	}
	if !strings.Contains(strings.Split(plain, "\n")[0], " 5 ") {
		t.Fatalf("badge lost the card count:\n%s", strings.Split(plain, "\n")[0])
	}
}

func TestPlainCardsKeepFilteredOrderWithoutRail(t *testing.T) {
	model := newBoardModel(true)
	model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "U1", Title: "one", State: "todo"},
		{TaskID: "S", Title: "single", State: "todo", TaskGroup: "only-one"},
		{TaskID: "U2", Title: "two", State: "todo"},
	}})
	if strings.Join(taskIDs(model.TasksFor("todo")), ",") != "U1,S,U2" {
		t.Fatalf("order %v", taskIDs(model.TasksFor("todo")))
	}
	for i, pad := range columnLeftPads(t, model, "todo") {
		if pad == '│' {
			t.Fatalf("pad %d is a group rail", i)
		}
	}
}

func TestFilteredSingletonStaysPutWithoutRail(t *testing.T) {
	model := newBoardModel(true)
	model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "S", Title: "keep-s", State: "todo", TaskGroup: "g1"},
		{TaskID: "U", Title: "keep-u", State: "todo"},
		{TaskID: "H", Title: "drop-h", State: "todo", TaskGroup: "g1"},
		{TaskID: "M1", Title: "keep-m1", State: "todo", TaskGroup: "g2"},
		{TaskID: "M2", Title: "keep-m2", State: "todo", TaskGroup: "g2"},
	}})
	model.Query = "keep"
	model.Normalize()
	got := taskIDs(model.TasksFor("todo"))
	if strings.Join(got, ",") != "S,U,M1,M2" {
		t.Fatalf("order %v", got)
	}
	pads := columnLeftPads(t, model, "todo")
	// S and U are plain. M1 and M2 are a cluster, with a header and the gap under M1.
	want := []rune{
		' ', ' ', ' ', ' ',
		' ', ' ', ' ', ' ',
		'│',
		'│', '│', '│', '│',
		'│', '│', '│', ' ',
	}
	if string(pads) != string(want) {
		t.Fatalf("pads %q want %q", string(pads), string(want))
	}
}

func TestGroupRailSlotsBumpOnlyAgainstPreviousCluster(t *testing.T) {
	zeros := groupsWithSlot(t, 0, 3)
	if groupRailSlot(zeros[0]) != groupRailSlot(zeros[0]) {
		t.Fatal("base slot changed")
	}
	tasks := make([]Task, 0, 6)
	for _, group := range zeros {
		tasks = append(tasks,
			Task{TaskID: group + "-a", State: "todo", TaskGroup: group},
			Task{TaskID: group + "-b", State: "todo", TaskGroup: group},
		)
	}
	if got := displayedRailSlots(tasks, false); strings.Join(ints(got), ",") != "0,1,0" {
		t.Fatalf("slots %v", got)
	}

	// A singleton between two base-0 clusters does not take a slot.
	between := []Task{
		{TaskID: "a1", State: "todo", TaskGroup: zeros[0]},
		{TaskID: "a2", State: "todo", TaskGroup: zeros[0]},
		{TaskID: "s", State: "todo", TaskGroup: "solo"},
		{TaskID: "b1", State: "todo", TaskGroup: zeros[1]},
		{TaskID: "b2", State: "todo", TaskGroup: zeros[1]},
	}
	if got := displayedRailSlots(between, false); strings.Join(ints(got), ",") != "0,1" {
		t.Fatalf("slots around a singleton %v", got)
	}

	todo := []Task{
		{TaskID: "t1", State: "todo", TaskGroup: zeros[0]},
		{TaskID: "t2", State: "todo", TaskGroup: zeros[0]},
		{TaskID: "t3", State: "todo", TaskGroup: zeros[1]},
		{TaskID: "t4", State: "todo", TaskGroup: zeros[1]},
	}
	working := []Task{
		{TaskID: "w1", State: "working", TaskGroup: zeros[1]},
		{TaskID: "w2", State: "working", TaskGroup: zeros[1]},
	}
	todoSlots := displayedRailSlots(todo, false)
	workingSlots := displayedRailSlots(working, false)
	if todoSlots[1] == workingSlots[0] {
		t.Fatalf("same group should diverge across columns: todo %v working %v", todoSlots, workingSlots)
	}
	if todoSlots[1] != 1 || workingSlots[0] != 0 {
		t.Fatalf("todo %v working %v", todoSlots, workingSlots)
	}
}

func TestGroupRailContrast(t *testing.T) {
	assertRailFamily(t, groupRailLight, false)
	assertRailFamily(t, groupRailDark, true)
}

func TestSelectedClusterKeepsSlotColorOnLeftPad(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	task := Task{TaskID: "A", Title: "alpha", State: "todo", TaskGroup: "20260930-g1"}
	mate := Task{TaskID: "C", Title: "gamma", State: "todo", TaskGroup: "20260930-g1"}
	app := railApp([]Task{task, mate})
	app.Model.SelectedIDs["todo"] = task.TaskID
	rail := columnGroupRails(app.Model.TasksFor("todo"), false)[0]
	if !rail.member {
		t.Fatal("expected a cluster member")
	}
	const width = 40
	for _, theme := range []string{"light", "slate-light"} {
		p := themePalette(theme)
		lines := app.renderCard(p, "todo", task, width, true, rail)
		for offset, line := range lines {
			style := cardStyle(p, "todo", offset, true)
			body := style.Render(padLine(clipText(app.boardCardLines(task, width)[offset], width), width) + " ")
			left := p.ink(rail.color).Render(borderVertical)
			if line != left+body {
				t.Fatalf("%s line %d left pad or title style diverged", theme, offset)
			}
		}
		if !strings.Contains(lines[0], trueColorSeq(string(rail.color), true)) {
			t.Fatal("left pad lost the slot foreground")
		}
	}
}

func TestUngroupedSelectedCardStaysOneBlock(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	task := Task{TaskID: "U", Title: "plain", State: "todo"}
	app := railApp([]Task{task})
	app.Model.SelectedIDs["todo"] = task.TaskID
	p := themePalette("light")
	const width = 40
	lines := app.renderCard(p, "todo", task, width, true, groupRail{})
	card := app.boardCardLines(task, width)
	for offset, line := range lines {
		style := cardStyle(p, "todo", offset, true)
		want := style.Render(" " + padLine(clipText(card[offset], width), width) + " ")
		if line != want {
			t.Fatalf("line %d split the selected block", offset)
		}
	}
}

func assertRailFamily(t *testing.T, colors [groupRailSlots]lipgloss.Color, dark bool) {
	t.Helper()
	seen := map[string]bool{}
	for _, color := range colors {
		value := string(color)
		assertHexColor(t, "rail", color)
		if seen[value] {
			t.Fatalf("duplicate rail color %s", value)
		}
		seen[value] = true
	}
	if len(seen) != groupRailSlots {
		t.Fatalf("palette size %d", len(seen))
	}
	for _, name := range namedThemeNames() {
		if themeIsDark(name) != dark {
			continue
		}
		bg := string(themePalette(name).Bg)
		for _, color := range colors {
			if ratio := contrastRatio(string(color), bg); ratio < 3 {
				t.Fatalf("%s on %s = %.2f", color, name, ratio)
			}
		}
	}
}

func groupsWithSlot(t *testing.T, slot, count int) []string {
	t.Helper()
	out := make([]string, 0, count)
	for i := range 10000 {
		id := fmt.Sprintf("g-%d", i)
		if groupRailSlot(id) != slot {
			continue
		}
		out = append(out, id)
		if len(out) == count {
			return out
		}
	}
	t.Fatalf("found %d groups for slot %d", len(out), slot)
	return nil
}

func displayedRailSlots(tasks []Task, dark bool) []int {
	ordered := clusterColumnTasks(tasks)
	rails := columnGroupRails(ordered, dark)
	palette := groupRailPalette(dark)
	var slots []int
	seen := map[string]bool{}
	for i, task := range ordered {
		if !rails[i].member || seen[task.TaskGroup] {
			continue
		}
		seen[task.TaskGroup] = true
		found := -1
		for slot, color := range palette {
			if color == rails[i].color {
				found = slot
			}
		}
		slots = append(slots, found)
	}
	return slots
}

func railApp(tasks []Task) *App {
	app := newApp(true, 30, tuiPageContext(),
		func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, nil },
		"light", 5, nil, nil)
	app.Model.SetBoard(BoardPayload{Tasks: tasks})
	return app
}

func renderTodoColumn(t *testing.T, model *BoardModel) string {
	t.Helper()
	app := newApp(true, 30, tuiPageContext(),
		func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, nil },
		"light", 5, nil, nil)
	app.Model = model
	body := len(columnRows(model, "todo"))
	if body < 1 {
		body = 1
	}
	return app.renderColumnPanel(themePalette("light"), boardLayout{
		State: "todo", Width: 44, Height: body + 2, BodyHeight: body,
		FirstVisual: true, LastVisual: true,
	}, 1)
}

func columnLeftPads(t *testing.T, model *BoardModel, state string) []rune {
	t.Helper()
	if state != "todo" {
		t.Fatalf("helper renders %s", state)
	}
	var pads []rune
	for _, line := range strings.Split(renderTodoColumn(t, model), "\n") {
		plain := []rune(ansi.Strip(line))
		if len(plain) < 2 || plain[0] != '│' || plain[len(plain)-1] != '│' {
			continue
		}
		pads = append(pads, plain[1])
	}
	return pads
}

func taskIDs(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, task := range tasks {
		out[i] = task.TaskID
	}
	return out
}

func ints(values []int) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = fmt.Sprintf("%d", value)
	}
	return out
}
