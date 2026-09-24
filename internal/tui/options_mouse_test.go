package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/config"
)

func clickOptions(t *testing.T, app *App, x, y int) {
	t.Helper()
	app.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	cmd := app.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if app.Options != nil {
		pumpPanel(app.Options, cmd)
	}
}

// Click a visible string in the rendered frame, as the terminal user sees it.
func clickOptionsText(t *testing.T, app *App, label string) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(app.View()), "\n") {
		if i := strings.Index(line, label); i >= 0 {
			clickOptions(t, app, displayWidth(line[:i]), y)
			return
		}
	}
	t.Fatalf("missing clickable text %q:\n%s", label, ansi.Strip(app.View()))
}

func TestStatusMouseEntrances(t *testing.T) {
	for _, width := range []int{45, 120} {
		app := newPanelApp(t)
		app.Width = width
		app.dismissWelcome()
		app.Context.StatusOptions, app.Context.StatusHelp = "o 选项", "? 帮助"
		view := ansi.Strip(app.View())
		if !strings.Contains(view, "o 选项 | ? 帮助") {
			t.Fatalf("missing status at width %d", width)
		}
		clickOptionsText(t, app, "? 帮助")
		if !app.Help {
			t.Fatal("help click failed")
		}
		app.View()
		clickOptions(t, app, 1, 1)
		if app.Help || app.Options != nil {
			t.Fatal("help click must close without fallthrough")
		}
		clickOptionsText(t, app, "o 选项")
		if app.Options == nil || app.pendingWork == nil {
			t.Fatal("options click did not start load")
		}
	}
}

func TestStatusMouseIgnoresDragAndNotice(t *testing.T) {
	app := newPanelApp(t)
	app.dismissWelcome()
	app.View()
	hit := app.statusHits[0]
	y := app.Height - 1
	app.Update(tea.MouseMsg{X: hit.x - 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	app.Update(tea.MouseMsg{X: hit.x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if app.Options != nil {
		t.Fatal("drag opened options")
	}
	app.PrefsError = "notice"
	app.View()
	clickOptions(t, app, hit.x, y)
	if app.Options != nil || app.Help {
		t.Fatal("notice retained status targets")
	}
}

func TestHelpWheelAndResize(t *testing.T) {
	app := newPanelApp(t)
	app.Width, app.Height = 48, 16
	app.openHelp()
	first := app.View()
	app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if !app.Help || app.helpView.YOffset == 0 || app.View() == first {
		t.Fatal("help did not scroll")
	}
	app.View()
	for range 100 {
		app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	if !app.helpView.AtBottom() {
		t.Fatal("help bottom unreachable")
	}
	app.Width, app.Height = 120, 70
	app.View()
	if app.helpView.YOffset != 0 {
		t.Fatal("resize did not clamp offset")
	}
	app.Width, app.Height = 48, 16
	app.View()
	app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if app.helpView.YOffset != 0 {
		t.Fatal("help scroll up failed")
	}
	clickOptions(t, app, 3, 3)
	if app.Help {
		t.Fatal("single click failed to close")
	}
	app.HandleKey("?")
	if !app.Help || app.helpView.YOffset != 0 {
		t.Fatal("help reopen offset")
	}
}

func TestOptionsWheelPreservesValuesAndReachesLastField(t *testing.T) {
	app, panel := openPanel(t)
	app.Height = 16
	pumpPanel(panel, panel.openSection(sectionInterface))
	app.View()
	before, _ := json.Marshal(panel.session.Config)
	focused := panel.form.GetFocusedField()
	app.View()
	for range 100 {
		app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	app.View()
	after, _ := json.Marshal(panel.session.Config)
	if string(before) != string(after) || focused != panel.form.GetFocusedField() {
		t.Fatal("wheel edited values or focus")
	}
	if panel.formView.YOffset == 0 || !panel.formView.AtBottom() {
		t.Fatal("options bottom unreachable")
	}
	old := app.Model.ShowArchived
	// Last option is deliberately below the initial viewport.
	label := "Yes"
	if old {
		label = "No"
	}
	clickOptionsText(t, app, label)
	if app.Model.ShowArchived == old || panel.current != sectionInterface {
		t.Fatal("scrolled click did not toggle last field in place")
	}
	app.View()
	for range 100 {
		app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	}
	app.View()
	if panel.formView.YOffset != 0 {
		t.Fatal("options top unreachable")
	}
	drivePanel(panel, keyMsg("up"))
	app.View()
	lo, _, ok := focusRange(panel.currentBodyLines())
	if !ok || lo < panel.formView.YOffset || lo >= panel.formView.YOffset+panel.formView.Height {
		t.Fatal("keyboard did not reveal focus")
	}
	app.Height = 90
	pumpPanel(panel, panel.resizeForm())
	app.View()
	if panel.formView.YOffset != 0 {
		t.Fatal("expanded form kept stale scroll")
	}
}

func TestOptionsMouseSelectAndInput(t *testing.T) {
	for _, section := range []string{sectionInterface, sectionExecution, sectionReview, sectionReviewStages, sectionRules} {
		t.Run(section, func(t *testing.T) {
			useInterfaceLanguage(t, "en")
			app, panel := openPanel(t)
			pumpPanel(panel, panel.openSection(section))
			app.View()
			field := panel.form.GetFocusedField()
			before := field.GetValue()
			// Use the right arrow in the current selector; one click must edit, never submit.
			clickOptionsText(t, app, "→")
			if panel.current != section {
				t.Fatal("selector click submitted section")
			}
			if field.GetValue() == before {
				t.Fatal("selector click did not edit")
			}
			if section == sectionExecution || section == sectionReview {
				var input *huh.Input
				for _, f := range panel.bind.formFields {
					if v, ok := f.(*huh.Input); ok {
						input = v
						break
					}
				}
				if input == nil {
					t.Fatal("missing model input")
				}
				app.Height = 90
				app.View()
				// The input's plain line is unique to its model field and includes its label.
				line := strings.TrimSpace(ansi.Strip(input.View()))
				clickOptionsText(t, app, line)
				if panel.form.GetFocusedField() != input {
					t.Fatal("text click did not focus input")
				}
				before := input.GetValue().(string)
				drivePanel(panel, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
				if input.GetValue() == before || panel.current != section {
					t.Fatal("typing did not stay in input")
				}
			}
		})
	}
}

func TestOptionsMouseSaveBackAndClose(t *testing.T) {
	app, panel := openPanel(t)
	pumpPanel(panel, panel.openSection(sectionReviewStages))
	clickOptionsText(t, app, "→")
	expected, _ := config.ReviewStageFor(panel.session.Config, "large", "PMQA")
	clickOptionsText(t, app, "["+tuiMouseLabel("save")+"]")
	if panel.current != "" {
		t.Fatal("save did not return to root")
	}
	saved, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := config.ReviewStageFor(saved, "large", "PMQA")
	if got != expected {
		t.Fatalf("saved %s, want %s", got, expected)
	}
	pumpPanel(panel, panel.openSection(sectionExecution))
	clickOptionsText(t, app, "["+tuiMouseLabel("back")+"]")
	if panel.current != "" {
		t.Fatal("back did not return")
	}
	clickOptionsText(t, app, "["+tuiMouseLabel("close")+"]")
	if app.Options != nil {
		t.Fatal("close failed")
	}
}

func tuiMouseLabel(name string) string { return t("tui.mouse_" + name) }

func TestOptionsMouseMenuWrappedAndScrolled(t *testing.T) {
	app, panel := openPanel(t)
	app.Width, app.Height = 45, 14
	pumpPanel(panel, panel.openRoot())
	app.View()
	for range 100 {
		app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	clickOptionsText(t, app, config.Text("tui.close_2"))
	if app.Options != nil {
		t.Fatal("scrolled menu close failed")
	}
}

func TestOptionsMouseRestoreAndRuleButtons(t *testing.T) {
	app, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	pumpPanel(panel, panel.switchTab(config.TargetOverlay))
	pumpPanel(panel, panel.openSection(sectionRules))
	app.Width, app.Height = 48, 24
	pumpPanel(panel, panel.resizeForm())
	app.View()
	before := panel.session.Config.Rules["collaboration"]
	// Reach the boolean rows below the preset selector and click the off label.
	clickOptionsText(t, app, config.Text("rules.off"))
	if panel.session.Config.Rules["collaboration"] == before {
		t.Fatalf("narrow rule button failed:\n%s", ansi.Strip(app.View()))
	}
	app.View()
	for range 100 {
		app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	clickOptionsText(t, app, "Yes")
	if panel.confirm == nil {
		t.Fatal("restore click did not request confirmation")
	}
	clickOptionsText(t, app, "["+tuiMouseLabel("no")+"]")
	if panel.confirm != nil || panel.session.Config.Rules["collaboration"] == before {
		t.Fatal("cancel restored config")
	}
	app.View()
	for range 100 {
		app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	clickOptionsText(t, app, "Yes")
	clickOptionsText(t, app, "["+tuiMouseLabel("yes")+"]")
	if panel.session.Config.Rules["collaboration"] != before {
		t.Fatal("confirm did not restore config")
	}
}

func TestOptionsFieldsFitMouseViewport(t *testing.T) {
	useInterfaceLanguage(t, "en")
	app, panel := openPanel(t)
	app.Width = 40
	for _, section := range []string{sectionInterface, sectionExecution, sectionReview, sectionReviewStages, sectionRules} {
		pumpPanel(panel, panel.openSection(section))
		app.View()
		for _, field := range panel.bind.formFields {
			if got := blockWidth(field.View()); got > panel.formView.Width {
				t.Fatalf("%s field width %d exceeds viewport %d: %s", section, got, panel.formView.Width, ansi.Strip(field.View()))
			}
		}
	}
}

func TestOptionsMouseKeepEditingAfterCloseConfirmation(t *testing.T) {
	useInterfaceLanguage(t, "en")
	app, panel := openPanel(t)
	pumpPanel(panel, panel.openSection(sectionReviewStages))
	drivePanel(panel, keyMsg("right"))
	drivePanel(panel, keyMsg("q"))
	if !panel.confirming {
		t.Fatal("missing unsaved close confirmation")
	}
	before, _ := json.Marshal(panel.session.Config)
	clickOptionsText(t, app, "["+config.Text("tui.keep_editing")+"]")
	after, _ := json.Marshal(panel.session.Config)
	if app.Options == nil || panel.confirming || !panel.dirty || string(before) != string(after) {
		t.Fatal("keep editing lost panel or unsaved values")
	}
}

func TestStatusWheelStillMovesBoardSelection(t *testing.T) {
	app := newPanelApp(t)
	app.Model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "one", Title: "one", State: "backlog"},
		{TaskID: "two", Title: "two", State: "backlog"},
	}})
	app.Model.FocusState("backlog")
	app.View()
	app.Update(tea.MouseMsg{X: app.Width - 3, Y: app.Height - 1, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if task := app.Model.SelectedTask(); task == nil || task.TaskID != "two" {
		t.Fatal("status wheel did not select next task")
	}
	app.Update(tea.MouseMsg{X: app.Width - 3, Y: app.Height - 1, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if task := app.Model.SelectedTask(); task == nil || task.TaskID != "one" {
		t.Fatal("status wheel did not select previous task")
	}
	if app.Options != nil || app.Help {
		t.Fatal("wheel activated status action")
	}
}
