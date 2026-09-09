package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// bareSpaces matches padding that no style covers — the start of a line, or anything right after a
// reset. Such spaces are drawn in the raw terminal color and punch holes in the theme background.
var bareSpaces = regexp.MustCompile(`(\x1b\[0m|^) +`)

func expectFilled(t *testing.T, name, screen string) {
	t.Helper()
	for i, line := range strings.Split(screen, "\n") {
		if match := bareSpaces.FindStringSubmatch(line); match != nil {
			t.Errorf("%s line %d has unpainted spaces: %q", name, i, line)
			return
		}
	}
}

func fillProbeApp(t *testing.T, theme string, width, height int) *App {
	t.Helper()
	board := BoardPayload{GeneratedAt: "t", Tasks: []Task{
		{TaskID: "20260821-one-task", Title: "一号任务", State: "backlog", Type: "Feature", Kind: "small", Time: "-"},
		{TaskID: "20260821-two-task", Title: "two", State: "working", Type: "Bug", Kind: "large", Time: "-"},
		{TaskID: "20260821-rev-task", Title: "review", State: "review", Type: "Bug", Kind: "large", Time: "-"},
	}}
	ctx := pageContext{
		StateLabels: map[string]string{"backlog": "backlog", "todo": "todo", "working": "working", "review": "review", "done": "done"},
		QuitHelp:    "q quit",
		StatusHelp:  "? help",
		TooSmall:    "too small",
	}
	app := newApp(false, 30, ctx,
		func() (BoardPayload, error) { return board, nil },
		func(id string) (Task, error) {
			return Task{TaskID: id, Title: id, Document: strings.Repeat("some line of text\n", 40)}, nil
		}, theme, 5, nil, func(string) (bool, string) { return true, "" })
	app.Width, app.Height = width, height
	app.Model.SetBoard(board)
	return app
}

// Every screen has to paint its whole rectangle with the theme background. Lip Gloss joins, the
// viewport and Glamour all pad with bare spaces, which show the terminal background instead —
// glaring when the theme and the terminal disagree, such as the light theme in a dark terminal.
func TestScreensFillBackground(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(profile)

	// The narrowest size drops the board to its "too small" notice and the help overlay to one column.
	sizes := [][2]int{{200, 50}, {120, 30}, {60, 20}, {40, 8}}
	for _, theme := range []string{"light", "dark"} {
		for _, size := range sizes {
			width, height := size[0], size[1]
			app := fillProbeApp(t, theme, width, height)
			expectFilled(t, "board", app.View())

			app.Searching = true
			app.Model.Query = "one"
			expectFilled(t, "board with search", app.View())
			app.Searching, app.Model.Query = false, ""

			app.Help = true
			expectFilled(t, "help overlay", app.View())
			app.Help = false

			app.openDetail()
			expectFilled(t, "detail", app.View())

			app.DetailSearching, app.DetailQuery = true, "line"
			expectFilled(t, "detail with search", app.View())
			app.DetailSearching = false

			app.DetailSelectMode, app.DetailAnchor, app.DetailCursor = "line", &[2]int{1, 0}, [2]int{3, 4}
			expectFilled(t, "detail with selection", app.View())
			app.DetailSelectMode, app.DetailAnchor = "", nil

			app.Detail, app.detailCache = &Task{TaskID: "20260821-one-task", Title: "empty"}, detailRender{}
			expectFilled(t, "detail with an empty document", app.View())
			app.Detail = nil

			for _, phase := range []startPhase{startLoading, startReady, startRunning, startFinished} {
				dialog := fillProbeApp(t, theme, width, height)
				dialog.confirmSelectedStart()
				if dialog.StartConfirmation == nil {
					break
				}
				dialog.StartConfirmation.phase = phase
				dialog.StartConfirmation.message = "started\n" + strings.Repeat("result line\n", 6)
				dialog.StartConfirmation.Warnings = []string{"short warning", "a warning long enough to wrap on a narrow screen"}
				expectFilled(t, "start dialog", dialog.View())
			}

			notice := fillProbeApp(t, theme, width, height)
			_, popup := notice.renderStartPopup([]string{"copied a fairly long notice line", "second line"})
			expectFilled(t, "start notice", popup)

			options := fillProbeApp(t, theme, width, height)
			options.openOptions()
			if options.Options != nil {
				expectFilled(t, "options panel", options.View())
			}
		}
	}
}
