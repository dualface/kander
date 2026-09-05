package tui

import (
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

// newTestSession 直接构造一个不做环境探测的配置会话.
// 始终覆盖进程继承的 KANDER_CONFIG, 禁止测试写入用户配置.
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

// 送一个消息给面板并跑完它返回的命令, 模拟 Bubble Tea 的事件循环.
func drivePanel(panel *optionsPanel, msg teaMsg) {
	pumpPanel(panel, panel.Update(msg))
}

// pumpPanel 代替 Bubble Tea 跑一遍命令链. 命令里可能混有光标闪烁这类
// 定时器命令, 同步执行会真的 sleep, 所以给每一步加个很短的超时.
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
	// ←→ 就地改值, 不需要 Enter 走完整节.
	drivePanel(panel, keyMsg("right"))
	after := panel.session.Config.Reviewers["PM"]
	if after == before {
		t.Fatalf("right arrow did not change the reviewer (%s)", after)
	}
	// Esc 只是返回上一层, 不能把已改的值丢掉.
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
	// 先改相邻字段, 再连续切换主题, 验证刷新不丢设置也不移动焦点.
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
		if panel.current != sectionInterface || panel.form.GetFocusedField() != panel.bind.formFields[2] {
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

func TestInterfaceWriteDoesNotCommitOtherSessionEdits(t *testing.T) {
	stored := config.DefaultConfig()
	stored.WelcomeComplete = true
	app, panel := openPanel(t, stored)
	panel.session.SetReviewer("PM", "claude")
	panel.markDirty()
	app.Theme = "light"
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
	// 干净状态下 q 直接关闭.
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

// TestMeasureFormDropsViewportPadding 保证量到的是内容高度, 不是 Huh 视口补的空白.
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

// 嵌套布局里, 小任务 Agent 前面隔着大任务的模型字段, 它的位置不是固定的 1.
// 重建分区后要按这个位置恢复焦点, 否则焦点会跳回第一个 Agent 的模型输入框.
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
	// 大任务 Agent 下面挂着它的模型与推理档位, 所以小任务至少要往后让两位.
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
