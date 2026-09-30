package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestGroupIndentAndColumnWidth(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	title := strings.Repeat("任务abc", 12)
	tasks := []Task{
		{TaskID: "A", Title: title, State: "todo", TaskGroup: "20260930-g1"},
		{TaskID: "B", Title: title, State: "todo", TaskGroup: "20260930-g1"},
	}
	for _, contentWidth := range []int{4, 8, 18, 40} {
		for _, theme := range []string{"light", "dark", "slate-light", "slate-dark"} {
			t.Run(fmt.Sprintf("%s/%d", theme, contentWidth), func(t *testing.T) {
				app := railApp(tasks)
				app.Theme = theme
				app.Model.FocusState("todo")
				app.Model.SelectedIDs["todo"] = "A"
				rows := columnRows(app.Model, "todo")
				width := contentWidth + 4
				frame := app.renderColumnPanel(themePalette(theme), boardLayout{
					State: "todo", Width: width, Height: len(rows) + 2, BodyHeight: len(rows),
					FirstVisual: true, LastVisual: true,
				}, 1)
				expectFilled(t, "grouped column", frame)
				lines := strings.Split(ansi.Strip(frame), "\n")
				for i, line := range lines[1 : len(lines)-1] {
					if displayWidth(line) != width {
						t.Fatalf("row %d width %d want %d: %q", i, displayWidth(line), width, line)
					}
				}
				if !strings.HasPrefix(lines[1], "│ ▾ ") || !strings.HasSuffix(lines[1], "2 │") {
					t.Fatalf("header indent or count: %q", lines[1])
				}
				wantTitle := "│ │ " + clipText(title, contentWidth-2)
				if !strings.HasPrefix(lines[2], wantTitle) {
					t.Fatalf("member title: %q want prefix %q", lines[2], wantTitle)
				}
				for i, row := range rows {
					if row.kind == "card" || row.rail {
						if !strings.HasPrefix(lines[i+1], "│ │ ") {
							t.Fatalf("member row %d indent: %q", i, lines[i+1])
						}
					}
				}
			})
		}
	}
}

func TestGroupIndentMouseSelection(t *testing.T) {
	for _, mode := range []string{"grouped", "ungrouped", "filtered-singleton"} {
		t.Run(mode, func(t *testing.T) {
			task := Task{TaskID: "A", Title: "甲乙abcd" + strings.Repeat("长标题", 30), State: "todo"}
			if mode != "ungrouped" {
				task.TaskGroup = "g1"
			}
			app := collapseApp([]Task{task, {TaskID: "B", Title: "other", State: "todo", TaskGroup: task.TaskGroup}})
			if mode == "filtered-singleton" {
				app.Model.Query = "甲"
				app.Model.Normalize()
			}
			panel, body, ok := focusedBody(t, app, "todo")
			if !ok {
				t.Fatal("todo panel missing")
			}
			startX, width := panel.X+2, panel.Width-4
			if mode == "grouped" {
				startX += 2
				width -= 2
				body++
			}
			for _, point := range []struct{ display, char int }{{0, 0}, {1, 0}, {2, 1}, {4, 2}, {6, 4}} {
				hit := app.boardCardHit(startX+point.display, body)
				if hit == nil || hit.TaskID != "A" || hit.Line != 0 || hit.Col != point.char || hit.ContentWidth != width {
					t.Fatalf("display %d hit %#v; want char %d width %d", point.display, hit, point.char, width)
				}
			}
			app.MouseAnchor = app.boardCardHit(startX, body)
			app.MouseCursor = app.boardCardHit(startX+6, body)
			if got := app.extractBoardMouseSelection(); got != "甲乙abc" {
				t.Fatalf("copied text %q", got)
			}
			app.MouseCursor = app.boardCardHit(panel.X+panel.Width-2, body)
			if got, want := app.extractBoardMouseSelection(), clipText(task.Title, width); got != want {
				t.Fatalf("copied clipped title %q want %q", got, want)
			}
		})
	}
}
