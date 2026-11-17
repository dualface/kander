package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	tinyMaxHeight = 10
	tinyMaxWidth  = 30
)

// renderSurvives renders one frame and reports the panic value, if any.
func renderSurvives(render func() string) (panicked any) {
	defer func() { panicked = recover() }()
	render()
	return nil
}

// resizeThroughProgram delivers the size the way Bubble Tea does, so the overlays that keep sized
// forms (the options panel) resize too.
func resizeThroughProgram(app *App, width, height int) {
	program{app: app}.Update(tea.WindowSizeMsg{Width: width, Height: height})
}

// forEachTinySize resizes the app to every height 1..tinyMaxHeight and width 1..tinyMaxWidth and fails
// for each size whose frame panics. A fresh frame is rendered after every resize.
func forEachTinySize(t *testing.T, app *App, prepare func(*App), render func() string) {
	t.Helper()
	for height := 1; height <= tinyMaxHeight; height++ {
		for width := 1; width <= tinyMaxWidth; width++ {
			resizeThroughProgram(app, width, height)
			if prepare != nil {
				prepare(app)
			}
			if p := renderSurvives(render); p != nil {
				t.Errorf("%dx%d (width x height): render panicked: %v", width, height, p)
			}
		}
	}
}

func TestBoardViewSurvivesTinyTerminals(t *testing.T) {
	app := fillProbeApp(t, "dark", 80, 24)
	forEachTinySize(t, app, nil, app.View)
}

func TestDetailViewSurvivesTinyTerminals(t *testing.T) {
	app := fillProbeApp(t, "dark", 80, 24)
	app.openDetail()
	finishQueuedWork(t, app)
	if app.Detail == nil {
		t.Fatal("the detail view did not open")
	}
	forEachTinySize(t, app, nil, app.View)
}

func TestHelpOverlaySurvivesTinyTerminals(t *testing.T) {
	app := fillProbeApp(t, "dark", 80, 24)
	app.Help = true
	forEachTinySize(t, app, nil, app.View)
}

func TestOptionsPanelSurvivesTinyTerminals(t *testing.T) {
	app, panel := openPanel(t)
	if app.Options != panel {
		t.Fatal("the options panel did not open")
	}
	forEachTinySize(t, app, nil, app.View)
}

func TestPadBlockToleratesNonPositiveSizes(t *testing.T) {
	p := themePalette("dark")
	for _, height := range []int{-1, 0} {
		if got := padBlock("a\nb", 4, height, p); got != "" {
			t.Errorf("padBlock height %d = %q, want empty", height, got)
		}
	}
	for _, width := range []int{-1, 0, 1} {
		if r := renderSurvives(func() string { return padBlock("a\nb", width, 1, p) }); r != nil {
			t.Errorf("padBlock width %d panicked: %v", width, r)
		}
	}
	if got := padBlock("a\nb", 4, 1, p); strings.Count(got, "\n") != 0 {
		t.Errorf("padBlock height 1 = %q, want one line", got)
	}
}
