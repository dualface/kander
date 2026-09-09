package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

type teaMsg = tea.Msg
type teaCmd = tea.Cmd
type teaBatchMsg = tea.BatchMsg

func keyMsg(name string) tea.KeyMsg {
	switch name {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func newPanelSpinner(app *App) spinner.Model {
	model := spinner.New()
	model.Spinner = spinner.Dot
	return model
}

func newPanelApp(t *testing.T) *App {
	t.Helper()
	app := newApp(true, 30, tuiPageContext(),
		func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, nil },
		"dark", 40, func(int) (config.TUI, error) { return config.TUI{}, nil }, func(string) (bool, string) { return true, "" })
	app.Width, app.Height = 120, 32
	return app
}

// newTestSession builds a config session directly, without any environment probing.
// It always overrides the inherited KANDER_CONFIG, so tests can never write the user's config.
func newTestSession(t *testing.T, initial ...*config.Config) *menu.Session {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if len(initial) > 0 {
		cfg = config.Clone(initial[0])
	}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session, err := menu.NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestNewTestSessionForcesTemporaryConfig(t *testing.T) {
	external := filepath.Join(t.TempDir(), "user-config.json")
	const sentinel = "do not overwrite\n"
	if err := os.WriteFile(external, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, external)
	_ = newTestSession(t)
	if os.Getenv(config.EnvConfig) == external {
		t.Fatal("test session retained inherited KANDER_CONFIG")
	}
	data, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sentinel {
		t.Fatalf("inherited config changed: %q", data)
	}
}

func TestOpenOptionsRequiresCompleteConfig(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"missing", ""},
		{"invalid-json", "{broken"},
		{"invalid-schema", `{"language":"xx"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newPanelApp(t)
			path := filepath.Join(t.TempDir(), "config.json")
			t.Setenv(config.EnvConfig, path)
			if tc.body != "" {
				if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			app.openOptions()
			if app.Options == nil || app.Options.loadErr == "" || app.Options.form != nil || app.Options.session != nil {
				t.Fatal("Load failure must stay on the error frame without opening the form")
			}
			plain := strings.ReplaceAll(ansi.Strip(app.View()), "\n", "")
			switch tc.name {
			case "missing":
				if !strings.Contains(plain, "配置不存在") {
					t.Fatalf("missing config frame:\n%s", ansi.Strip(app.View()))
				}
			case "invalid-json":
				if !strings.Contains(plain, "读取配置失败") {
					t.Fatalf("invalid JSON frame:\n%s", ansi.Strip(app.View()))
				}
			default:
				if !strings.Contains(plain, "schema") && !strings.Contains(plain, "language") {
					t.Fatalf("invalid schema frame:\n%s", ansi.Strip(app.View()))
				}
			}
		})
	}
}

func TestReopenOptionsRequiresCompleteConfig(t *testing.T) {
	app := newPanelApp(t)
	app.Session = newTestSession(t)
	app.openOptions()
	if app.Options == nil || app.Options.form == nil || app.Options.loadErr != "" {
		t.Fatal("valid config should open the form")
	}
	app.Options.close()
	if app.Options != nil || app.Session == nil {
		t.Fatal("close should keep the cached session")
	}
	path := os.Getenv(config.EnvConfig)
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.openOptions()
	if app.Options == nil || app.Options.loadErr == "" || app.Options.form != nil {
		t.Fatal("reopen after a broken config must stay on the error frame")
	}
	if !strings.Contains(strings.ReplaceAll(ansi.Strip(app.View()), "\n", ""), "读取配置失败") {
		t.Fatalf("reopen error frame:\n%s", ansi.Strip(app.View()))
	}
}

func TestOpenOptionsFromBoardKey(t *testing.T) {
	app := newPanelApp(t)
	app.Session = newTestSession(t)
	app.HandleKey("o")
	if app.Options == nil {
		t.Fatal("o should open the options panel")
	}
	if app.Options.form == nil {
		t.Fatal("panel should start on the root form")
	}
}

func TestOpenOptionsAtInterface(t *testing.T) {
	app := newPanelApp(t)
	app.Session = newTestSession(t)
	app.openOptionsAt(sectionInterface)
	if app.Options == nil || app.Options.current != sectionInterface {
		t.Fatal("panel should start in the interface section")
	}
	if app.Options.form == nil || app.Options.form.GetFocusedField() != app.Options.bind.formFields[0] {
		t.Fatal("default language should have initial focus")
	}
}

// Send one message to the panel and run the commands it returns, simulating the Bubble Tea event loop.
func drivePanel(panel *optionsPanel, msg teaMsg) {
	pumpPanel(panel, panel.Update(msg))
}

// pumpPanel runs one command chain in place of Bubble Tea. The chain may contain timer commands such as
// cursor blinking, which would really sleep when run synchronously, so each step gets a very short timeout.
func pumpPanel(panel *optionsPanel, cmd teaCmd) {
	for i := 0; i < 24 && cmd != nil; i++ {
		msg, ok := runCmd(cmd)
		if !ok || msg == nil {
			return
		}
		if batch, ok := msg.(teaBatchMsg); ok {
			for _, item := range batch {
				pumpPanel(panel, item)
			}
			return
		}
		cmd = panel.Update(msg)
	}
}

func runCmd(cmd teaCmd) (teaMsg, bool) {
	done := make(chan teaMsg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg, true
	case <-time.After(250 * time.Millisecond):
		return nil, false
	}
}

func openPanel(t *testing.T, initial ...*config.Config) (*App, *optionsPanel) {
	t.Helper()
	app := newPanelApp(t)
	panel := &optionsPanel{app: app, spinner: newPanelSpinner(app)}
	app.Options = panel
	panel.session = newTestSession(t, initial...)
	pumpPanel(panel, panel.openRoot())
	return app, panel
}

func TestEscapeKeepsSectionEdits(t *testing.T) {
	app, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionReview))
	if panel.current != sectionReview {
		t.Fatalf("section=%q", panel.current)
	}
	before := panel.session.Config.Reviewers["PM"]
	// ←→ edits in place, with no need to Enter through the whole section.
	drivePanel(panel, keyMsg("right"))
	after := panel.session.Config.Reviewers["PM"]
	if after == before {
		t.Fatalf("right arrow did not change the reviewer (%s)", after)
	}
	// Esc only returns one level up and must not drop the values already changed.
	drivePanel(panel, keyMsg("esc"))
	if panel.current != "" {
		t.Fatalf("esc should return to the root menu, got %q", panel.current)
	}
	if panel.session.Config.Reviewers["PM"] != after {
		t.Fatalf("esc discarded the edit: %s", panel.session.Config.Reviewers["PM"])
	}
	if !panel.dirty {
		t.Fatal("changed config must be marked unsaved")
	}
	if app.Options == nil {
		t.Fatal("panel must stay open")
	}
}

func TestEnterSavesReviewSection(t *testing.T) {
	_, panel := openPanel(t)
	configPath := os.Getenv(config.EnvConfig)
	pumpPanel(panel, panel.dispatch(sectionReview))
	before := panel.session.Config.Reviewers["PM"]
	drivePanel(panel, keyMsg("right"))
	after := panel.session.Config.Reviewers["PM"]
	if after == before {
		t.Fatal("right arrow did not change the reviewer")
	}
	drivePanel(panel, keyMsg("enter"))
	if panel.current != "" {
		t.Fatalf("enter should return to the root menu, got %q", panel.current)
	}
	if panel.dirty {
		t.Fatal("enter should save and clear unsaved state")
	}
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Reviewers["PM"] != after {
		t.Fatalf("disk PM=%s want %s", loaded.Reviewers["PM"], after)
	}
	_, err = os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
}

func TestThemeChangeKeepsInterfaceState(t *testing.T) {
	app, panel := openPanel(t)
	app.Theme = "light"
	pumpPanel(panel, panel.dispatch(sectionInterface))
	if panel.bind == nil || panel.bind.theme != "light" {
		t.Fatalf("theme binding=%q", panel.bind.theme)
	}
	if panel.form.GetFocusedField() != panel.bind.formFields[0] || panel.bind.fieldIndex[interfaceFocusKey("language")] != 0 {
		t.Fatal("default language should be the first interface option")
	}
	// Change a neighboring field first, then switch themes repeatedly, verifying that the refresh loses no setting and does not move focus.
	// The interface section is ordered language, agent language, theme, refresh.
	drivePanel(panel, keyMsg("down"))
	drivePanel(panel, keyMsg("down"))
	drivePanel(panel, keyMsg("down"))
	drivePanel(panel, keyMsg("right"))
	refresh := app.RefreshSecs
	drivePanel(panel, keyMsg("up"))
	for _, step := range []struct{ key, theme string }{
		{"right", "dark"}, {"left", "light"}, {"left", "auto"}, {"right", "light"},
	} {
		drivePanel(panel, keyMsg(step.key))
		if app.Theme != step.theme || panel.bind.theme != step.theme {
			t.Fatalf("theme=%q binding=%q want %q", app.Theme, panel.bind.theme, step.theme)
		}
		if panel.current != sectionInterface || panel.form.GetFocusedField() != panel.bind.formFields[4] {
			t.Fatal("theme change should keep the interface section and theme focus")
		}
		if app.RefreshSecs != refresh || panel.bind.refresh != refresh {
			t.Fatal("theme change lost the refresh setting")
		}
		if prefs := loadPrefs(); prefs.Theme != step.theme || prefs.Refresh != refresh {
			t.Fatalf("preferences=%+v", prefs)
		}
	}
	drivePanel(panel, keyMsg("esc"))
	pumpPanel(panel, panel.dispatch(sectionInterface))
	if panel.bind.theme != "light" || panel.bind.refresh != refresh {
		t.Fatal("reopening the interface section lost the settings")
	}
}

func TestAgentLanguageSelectUpdatesSession(t *testing.T) {
	stored := config.DefaultConfig()
	stored.WelcomeComplete = true
	stored.Language = "en"
	stored.AgentLanguage = "en"
	_, panel := openPanel(t, stored)
	pumpPanel(panel, panel.dispatch(sectionInterface))
	if panel.bind.fieldIndex[interfaceFocusKey("language")] != 0 {
		t.Fatal("language should be first")
	}
	if panel.bind.fieldIndex[interfaceFocusKey("agent_language")] != 1 {
		t.Fatal("agent language should follow language")
	}
	if panel.bind.fieldIndex[interfaceFocusKey("theme")] != 2 {
		t.Fatal("theme should follow agent language")
	}
	// language (0) -> agent language (1)
	drivePanel(panel, keyMsg("down"))
	if panel.form.GetFocusedField() != panel.bind.formFields[2] {
		t.Fatal("focus should be on agent language (formFields[2] after language spacer)")
	}
	before := panel.session.Config.AgentLanguage
	drivePanel(panel, keyMsg("right"))
	after := panel.session.Config.AgentLanguage
	if after == before || after == "" {
		t.Fatalf("right arrow should change agent language: before=%q after=%q", before, after)
	}
	if !panel.dirty {
		t.Fatal("agent language change must mark the panel dirty")
	}
	drivePanel(panel, keyMsg("enter"))
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AgentLanguage != after {
		t.Fatalf("disk agent_language=%q want %q", loaded.AgentLanguage, after)
	}
	pumpPanel(panel, panel.dispatch(sectionInterface))
	if panel.bind.agentLanguage != after || panel.session.Config.AgentLanguage != after {
		t.Fatalf("reopen lost agent language: bind=%q session=%q", panel.bind.agentLanguage, panel.session.Config.AgentLanguage)
	}
}

func TestInterfaceWriteDoesNotCommitOtherSessionEdits(t *testing.T) {
	stored := config.DefaultConfig()
	stored.WelcomeComplete = true
	app, panel := openPanel(t, stored)
	panel.session.SetReviewer("PM", "claude")
	panel.markDirty()
	app.Theme = "light"
	panel.session.Config.TUI.Theme = "light"
	panel.persistUI()
	if app.PrefsError != "" {
		t.Fatal(app.PrefsError)
	}
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Reviewers["PM"] != stored.Reviewers["PM"] {
		t.Fatalf("interface write committed reviewer=%s", loaded.Reviewers["PM"])
	}
	if loaded.TUI.Theme != "light" {
		t.Fatalf("stored theme=%s", loaded.TUI.Theme)
	}
	if panel.session.Config.Reviewers["PM"] != "claude" || panel.session.Config.TUI.Theme != "light" {
		t.Fatalf("session lost edits: %+v", panel.session.Config)
	}
}

func TestArrowsMoveBetweenFields(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionReview))
	first, _, ok := focusRange(panel.currentBodyLines())
	if !ok {
		t.Fatal("no focused field")
	}
	drivePanel(panel, keyMsg("down"))
	second, _, ok := focusRange(panel.currentBodyLines())
	if !ok || second == first {
		t.Fatalf("down should move to the next field: %d -> %d", first, second)
	}
	drivePanel(panel, keyMsg("up"))
	back, _, _ := focusRange(panel.currentBodyLines())
	if back != first {
		t.Fatalf("up should return to the previous field: %d != %d", back, first)
	}
}

func TestUnsavedChangesGuardOnClose(t *testing.T) {
	app, panel := openPanel(t)
	// In a clean state q closes right away.
	drivePanel(panel, keyMsg("q"))
	if app.Options != nil {
		t.Fatal("q should close a clean panel")
	}

	app, panel = openPanel(t)
	panel.markDirty()
	drivePanel(panel, keyMsg("o"))
	if app.Options == nil {
		t.Fatal("unsaved changes must not close silently")
	}
	if !panel.confirming {
		t.Fatal("expected the close confirmation")
	}
	panel.closeChoice = closeDiscard
	pumpPanel(panel, panel.finishCloseConfirm())
	if app.Options != nil {
		t.Fatal("discard should close the panel")
	}
}

func TestMouseClickFocusesRow(t *testing.T) {
	_, panel := openPanel(t)
	panel.view()
	lo, _, ok := focusRange(panel.currentBodyLines())
	if !ok {
		t.Fatal("no focused row")
	}
	target := lo + 2
	if target >= len(panel.bodyLines) {
		t.Skip("popup too short for this assertion")
	}
	pumpPanel(panel, panel.HandleMouse(panel.bodyX+1, panel.bodyY+target, mouseBtn1Clicked))
	moved, _, ok := focusRange(panel.currentBodyLines())
	if !ok || moved != target {
		t.Fatalf("click should focus row %d, focus is %d", target, moved)
	}
}

// TestMeasureFormDropsViewportPadding makes sure the measured value is the content height, not the padding Huh's viewport adds.
func TestMeasureFormDropsViewportPadding(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionReview))
	raw := strings.Split(panel.form.View(), "\n")
	trimmed := trimTrailingBlank(raw)
	if panel.formNatural != len(trimmed) {
		t.Fatalf("formNatural=%d trimmed=%d raw=%d", panel.formNatural, len(trimmed), len(raw))
	}
	if len(raw) <= len(trimmed) {
		t.Fatalf("expected Huh viewport padding, raw=%d trimmed=%d", len(raw), len(trimmed))
	}
}

// In the nested layout the small task agent sits behind the model fields of the large task, so its position is not a fixed 1.
// Focus has to be restored to that position after a section rebuild, otherwise it jumps back to the model input of the first agent.
func TestSelectorFieldIndexAccountsForNestedModels(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionExecution))
	bind := panel.bind
	if bind == nil {
		t.Fatal("no binding")
	}
	large := bind.fieldIndex[scaleFocusKey("large")]
	small := bind.fieldIndex[scaleFocusKey("small")]
	if large != 0 {
		t.Fatalf("大任务 Agent 应排在最前, got %d", large)
	}
	// The large task agent carries its model and reasoning effort below it, so the small task must be at least two positions further down.
	if small < large+3 {
		t.Fatalf("小任务 Agent 位置 %d 没有跳过大任务的模型字段", small)
	}
	if got := len(bind.modelFields); got == 0 {
		t.Fatal("模型字段没有建出来")
	}

	pumpPanel(panel, panel.dispatch(sectionReview))
	bind = panel.bind
	previous := -1
	for _, role := range config.ReviewRoles {
		index := bind.fieldIndex[roleFocusKey(role)]
		if index <= previous {
			t.Fatalf("%s Reviewer 位置 %d 未按顺序递增", role, index)
		}
		previous = index
	}
}

func moveRootTo(t *testing.T, panel *optionsPanel, section string) {
	t.Helper()
	for i := 0; i < 8 && panel.section != section; i++ {
		drivePanel(panel, keyMsg("down"))
	}
	if panel.section != section {
		t.Fatalf("could not move root cursor to %q, got %q", section, panel.section)
	}
}

func assertRootFocus(t *testing.T, panel *optionsPanel, want string) {
	t.Helper()
	if panel.current != "" {
		t.Fatalf("expected root menu, current=%q", panel.current)
	}
	if panel.confirming {
		t.Fatal("still on close confirmation")
	}
	if panel.section != want {
		t.Fatalf("section binding=%q, want %q", panel.section, want)
	}
	lines := panel.currentBodyLines()
	lo, _, ok := focusRange(lines)
	if !ok {
		t.Fatal("no focused root item")
	}
	text := ansi.Strip(lines[lo])
	if !strings.Contains(text, focusMarker) {
		t.Fatalf("row %d missing focus marker: %q", lo, text)
	}
	labels := map[string][2]string{
		sectionInterface: {"界面", "Interface"},
		sectionExecution: {"任务执行与模型", "Execution and models"},
		sectionReview:    {"审核与模型", "Review and models"},
		sectionDoctor:    {"环境检查", "Environment check"},
		sectionSave:      {"保存并应用", "Save and apply"},
		sectionClose:     {"关闭", "Close"},
	}[want]
	if !strings.Contains(text, labels[0]) && !strings.Contains(text, labels[1]) {
		t.Fatalf("focus on %q, want section %s", text, want)
	}
}

func TestRootCursorRestoredAfterEscapeFromSection(t *testing.T) {
	_, panel := openPanel(t)
	moveRootTo(t, panel, sectionReview)
	drivePanel(panel, keyMsg("enter"))
	if panel.current != sectionReview {
		t.Fatalf("enter should open the section, got %q", panel.current)
	}
	drivePanel(panel, keyMsg("esc"))
	assertRootFocus(t, panel, sectionReview)
}

func TestRootCursorRestoredAfterSubmitFromSection(t *testing.T) {
	_, panel := openPanel(t)
	moveRootTo(t, panel, sectionReview)
	drivePanel(panel, keyMsg("enter"))
	if panel.current != sectionReview {
		t.Fatalf("enter should open the section, got %q", panel.current)
	}
	drivePanel(panel, keyMsg("enter"))
	assertRootFocus(t, panel, sectionReview)
}

func TestRootCursorRestoredAfterCloseConfirmKeepEditing(t *testing.T) {
	_, panel := openPanel(t)
	moveRootTo(t, panel, sectionExecution)
	panel.markDirty()
	drivePanel(panel, keyMsg("q"))
	if !panel.confirming {
		t.Fatal("expected the close confirmation")
	}
	panel.closeChoice = closeBack
	pumpPanel(panel, panel.finishCloseConfirm())
	assertRootFocus(t, panel, sectionExecution)
}

// typeRune sends one character into the current form as a keyboard event, through the real input path.
func typeRune(panel *optionsPanel, ch rune) {
	drivePanel(panel, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
}

func assertTypedIntoView(t *testing.T, panel *optionsPanel, want string) {
	t.Helper()
	view := ansi.Strip(panel.form.View())
	if !strings.Contains(view, want) {
		t.Fatalf("form view missing typed value %q:\n%s", want, view)
	}
}

func TestExecutionModelInputSavesWhenAgentsMatch(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionExecution))
	large := panel.session.Config.KanbanAgents["large"]
	small := panel.session.Config.KanbanAgents["small"]
	if large == "" || large != small {
		t.Fatalf("need the same agent for large and small, got large=%q small=%q", large, small)
	}
	if len(panel.bind.modelFields) < 2 {
		t.Fatal("need multiple model fields so later appends can grow the binding slice")
	}
	before := panel.session.Config.Models.Kanban[large]["large_model"]
	drivePanel(panel, keyMsg("down"))
	if panel.form.GetFocusedField() != panel.bind.formFields[1] {
		t.Fatal("down should focus the large-task model input")
	}
	typeRune(panel, 'X')
	want := before + "X"
	assertTypedIntoView(t, panel, want)
	if got := panel.session.Config.Models.Kanban[large]["large_model"]; got != want {
		t.Fatalf("session large_model=%q want %q", got, want)
	}
	if !panel.dirty {
		t.Fatal("typing into the model input must mark unsaved changes")
	}
	drivePanel(panel, keyMsg("enter"))
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Models.Kanban[large]["large_model"]; got != want {
		t.Fatalf("disk large_model=%q want %q", got, want)
	}
}

func TestReviewModelInputSavesPMRole(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionReview))
	if len(panel.bind.formFields) < 4 {
		t.Fatal("review section should have reviewer, two scale stages, and model fields")
	}
	before := panel.session.Config.Models.ReviewRoles["PM"]["model"]
	drivePanel(panel, keyMsg("down"))
	drivePanel(panel, keyMsg("down"))
	drivePanel(panel, keyMsg("down"))
	if panel.form.GetFocusedField() != panel.bind.formFields[3] {
		t.Fatal("three downs should focus the PM model input after the two scale stages")
	}
	typeRune(panel, 'X')
	want := before + "X"
	assertTypedIntoView(t, panel, want)
	if got := panel.session.Config.Models.ReviewRoles["PM"]["model"]; got != want {
		t.Fatalf("session PM model=%q want %q", got, want)
	}
	if !panel.dirty {
		t.Fatal("typing into the PM model input must mark unsaved changes")
	}
	drivePanel(panel, keyMsg("enter"))
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Models.ReviewRoles["PM"]["model"]; got != want {
		t.Fatalf("disk PM model=%q want %q", got, want)
	}
}

func TestReviewStagePerScaleSaves(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionReview))
	before, err := config.ReviewStageFor(panel.session.Config, "large", "PM")
	if err != nil {
		t.Fatal(err)
	}
	drivePanel(panel, keyMsg("down"))
	drivePanel(panel, keyMsg("right"))
	after, err := config.ReviewStageFor(panel.session.Config, "large", "PM")
	if err != nil || after == before {
		t.Fatalf("large PM stage did not change: %s -> %s (%v)", before, after, err)
	}
	small, err := config.ReviewStageFor(panel.session.Config, "small", "PM")
	if err != nil || small != before {
		t.Fatalf("small PM stage changed unexpectedly: %s (%v)", small, err)
	}
	drivePanel(panel, keyMsg("enter"))
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := config.ReviewStageFor(loaded, "large", "PM")
	if err != nil || got != after {
		t.Fatalf("disk large PM=%s want %s (%v)", got, after, err)
	}
}

func TestExecutableInputsDeduplicateAndPersist(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionExecution))
	count := 0
	for i, field := range panel.bind.modelFields {
		if strings.HasSuffix(field.Key(), ".process_name") {
			count++
			*panel.bind.modelValues[i] = "node"
		}
	}
	if count != 1 {
		t.Fatalf("duplicate process inputs: %d", count)
	}
	panel.bind.apply(panel)
	drivePanel(panel, keyMsg("enter"))
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	agent := loaded.KanbanAgents["large"]
	if loaded.Agents[agent].ProcessName != "node" {
		t.Fatal(loaded.Agents)
	}
}

func writeTempOverlay(t *testing.T, dir string, payload map[string]any) (string, []byte) {
	t.Helper()
	path := filepath.Join(dir, config.OverlayFilename)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestOptionsPanelShowsOverlayNotice(t *testing.T) {
	dir := t.TempDir()
	writeTempOverlay(t, dir, map[string]any{"kanban_agent": "claude"})
	t.Chdir(dir)
	_, panel := openPanel(t)
	panel.detectOverlayNotice()
	_, view := panel.view()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, ".kander-config") {
		t.Fatalf("missing overlay notice:\n%s", plain)
	}
}

func TestOptionsSaveLeavesOverlayIsolated(t *testing.T) {
	dir := t.TempDir()
	_, original := writeTempOverlay(t, dir, map[string]any{
		"kanban_agent": "claude",
		"tui":          map[string]any{"theme": "dark", "columns": 2},
	})
	t.Chdir(dir)
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionReview))
	before := panel.session.Config.Reviewers["PM"]
	drivePanel(panel, keyMsg("right"))
	after := panel.session.Config.Reviewers["PM"]
	if after == before {
		t.Fatal("right arrow did not change the reviewer")
	}
	drivePanel(panel, keyMsg("enter"))
	data, err := os.ReadFile(filepath.Join(dir, config.OverlayFilename))
	if err != nil || string(data) != string(original) {
		t.Fatalf("overlay bytes changed: %s", data)
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.KanbanAgent == "claude" {
		t.Fatal("TUI save wrote overlay-only kanban_agent into the scope file")
	}
	if scopeCfg.TUI.Theme == "dark" || scopeCfg.TUI.Columns == 2 {
		t.Fatalf("TUI save wrote overlay-only tui values into the scope file: %+v", scopeCfg.TUI)
	}
	if scopeCfg.Reviewers["PM"] != after {
		t.Fatalf("scope PM=%s want %s", scopeCfg.Reviewers["PM"], after)
	}
	merged, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if merged.KanbanAgent != "claude" {
		t.Fatalf("runtime merge lost overlay: %s", merged.KanbanAgent)
	}
	if merged.TUI.Theme != "dark" || merged.TUI.Columns != 2 {
		t.Fatalf("runtime merge lost overlay tui: %+v", merged.TUI)
	}
}

func TestSaveColumnsLeavesOverlayTUIIsolated(t *testing.T) {
	dir := t.TempDir()
	_, original := writeTempOverlay(t, dir, map[string]any{
		"tui": map[string]any{"theme": "dark", "columns": 6},
	})
	t.Chdir(dir)
	_ = newTestSession(t)
	if _, err := saveColumns(2); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.OverlayFilename))
	if err != nil || string(data) != string(original) {
		t.Fatalf("overlay bytes changed: %s", data)
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.TUI.Theme == "dark" {
		t.Fatal("saveColumns wrote overlay-only theme into the scope file")
	}
	if scopeCfg.TUI.Columns != 2 {
		t.Fatalf("scope columns=%d", scopeCfg.TUI.Columns)
	}
	merged, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if merged.TUI.Theme != "dark" {
		t.Fatalf("runtime merge lost overlay theme: %s", merged.TUI.Theme)
	}
	if merged.TUI.Columns != 6 {
		t.Fatalf("runtime merge lost overlay columns: %d", merged.TUI.Columns)
	}
}
