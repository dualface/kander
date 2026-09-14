package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/install"
)

func init() {
	install.SetConfirmCopy(runTUICopyConfirm)
}

func runTUICopyConfirm(title, body string) (bool, error) {
	return runConfirmPrompt(title, []string{body})
}

// confirmPrompt is a short-lived ready-only confirmation used before the
// board TUI starts, so CheckStartupCopy can reuse the shared key contract.
type confirmPrompt struct {
	title      string
	paragraphs []string
	dialog     confirmDialog
	choice     bool
	done       bool
	width      int
	height     int
	theme      string
}

func newConfirmPrompt(title string, paragraphs []string) *confirmPrompt {
	return &confirmPrompt{
		title:      title,
		paragraphs: paragraphs,
		dialog:     confirmDialog{phase: confirmReady},
		theme:      "dark",
	}
}

func runConfirmPrompt(title string, paragraphs []string) (bool, error) {
	model := newConfirmPrompt(title, paragraphs)
	program := tea.NewProgram(model, tea.WithMouseCellMotion())
	result, err := program.Run()
	if err != nil {
		return false, err
	}
	prompt, _ := result.(*confirmPrompt)
	if prompt == nil {
		return false, nil
	}
	return prompt.choice, nil
}

func (m *confirmPrompt) Init() tea.Cmd { return nil }

func (m *confirmPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch event := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = event.Width, event.Height
		return m, nil
	case tea.MouseMsg:
		buttons := 0
		switch event.Button {
		case tea.MouseButtonWheelUp:
			buttons = mouseBtn4Pressed
		case tea.MouseButtonWheelDown:
			buttons = mouseBtn5Pressed
		}
		m.dialog.handleWheel(event.X, event.Y, buttons, nil, nil)
		return m, nil
	case tea.KeyMsg:
		switch m.dialog.handleKey(mapKey(event)) {
		case confirmAccept:
			m.choice, m.done = true, true
			return m, tea.Quit
		case confirmCancel, confirmClose:
			m.choice, m.done = false, true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *confirmPrompt) View() string {
	w, h := m.width, m.height
	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 24
	}
	box, popup := renderConfirmDialog(w, h, m.theme, m.paragraphs, confirmHint(confirmReady), m.title, &m.dialog.bodyView)
	p := themePalette(m.theme)
	return overlay(paintScreen("", w, h, p), popup, box.X, box.Y, p)
}
