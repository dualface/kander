package tui

import (
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

// mouseButtons 是与终端库无关的按键集合, 便于测试直接构造事件.
type mouseButtons int

const (
	buttonNone      mouseButtons = 0
	buttonLeft      mouseButtons = 1 << 0
	buttonWheelUp   mouseButtons = 1 << 1
	buttonWheelDown mouseButtons = 1 << 2
	buttonOther     mouseButtons = 1 << 3
)

type mouseTracker struct {
	down                   bool
	downX, downY           int
	lastClickX, lastClickY int
	lastClickAt            time.Time
}

const doubleClickWindow = 500 * time.Millisecond

// mapButtons 把一次原始按键状态折算成 selection.go 的 bstate 位掩码,
// 并在此识别按下, 拖动, 释放, 单击与双击.
func (m *mouseTracker) mapButtons(x, y int, btns mouseButtons, when time.Time) int {
	if when.IsZero() {
		when = time.Now()
	}
	if btns&buttonWheelUp != 0 {
		return mouseBtn4Pressed
	}
	if btns&buttonWheelDown != 0 {
		return mouseBtn5Pressed
	}
	if btns&buttonLeft != 0 {
		if !m.down {
			m.down = true
			m.downX, m.downY = x, y
		}
		return mouseBtn1Pressed
	}
	if btns != 0 {
		return 0
	}
	if !m.down {
		return mouseReportPos
	}
	m.down = false
	state := mouseBtn1Released
	if x == m.downX && y == m.downY {
		if !m.lastClickAt.IsZero() && when.Sub(m.lastClickAt) <= doubleClickWindow && x == m.lastClickX && y == m.lastClickY {
			state |= mouseBtn1Double
		}
		m.lastClickAt = when
		m.lastClickX, m.lastClickY = x, y
	}
	return state
}

func isTTY(file *os.File) bool {
	return term.IsTerminal(int(file.Fd()))
}

type tickMsg time.Time

const uiTickInterval = 200 * time.Millisecond

func tickCmd() tea.Cmd {
	return tea.Tick(uiTickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// shellOut 让 App 请求把终端暂时交还给一个会占用当前终端的操作
// (安装 tmux). Bubble Tea 在执行期间退出 alt-screen.
type shellOut struct {
	run  func()
	done func()
}

func (s *shellOut) SetStdin(io.Reader)  {}
func (s *shellOut) SetStdout(io.Writer) {}
func (s *shellOut) SetStderr(io.Writer) {}

func (s *shellOut) Run() error {
	s.run()
	return nil
}

type shellDoneMsg struct{}

// workMsg 携带后台任务的结果 (环境探测, 环境检查).
type workMsg struct{ payload any }

// program 是 App 的 Bubble Tea 外壳: 只做事件翻译, 不含界面逻辑.
type program struct {
	app *App
}

func (p program) Init() tea.Cmd {
	return tea.Batch(tickCmd(), p.app.takePending())
}

func (p program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch event := msg.(type) {
	case tea.WindowSizeMsg:
		oldWidth, oldHeight := p.app.Width, p.app.Height
		p.app.Width, p.app.Height = event.Width, event.Height
		p.app.clampDetailCursor()
		if p.app.Options != nil && (oldWidth != event.Width || oldHeight != event.Height) {
			return p, p.app.Options.resizeForm()
		}
		return p, nil
	case tickMsg:
		if p.app.Now().Sub(p.app.LastRefresh) >= time.Duration(p.app.RefreshSecs)*time.Second {
			p.app.refreshBoard()
		}
		return p, tickCmd()
	case shellDoneMsg:
		return p, p.app.takePending()
	case workMsg:
		cmd := p.app.applyWork(event.payload)
		return p, tea.Batch(cmd, p.app.takePending())
	}
	cmd := p.app.Update(msg)
	if !p.app.Running {
		return p, tea.Quit
	}
	return p, tea.Batch(cmd, p.app.takePending())
}

func (p program) View() string {
	return p.app.View()
}

// takePending 取出 App 排队的终端占用请求与后台任务, 没有时返回 nil.
func (a *App) takePending() tea.Cmd {
	var cmds []tea.Cmd
	if a.pendingShell != nil {
		run := a.pendingShell
		a.pendingShell = nil
		cmds = append(cmds, tea.Exec(&shellOut{run: run}, func(error) tea.Msg { return shellDoneMsg{} }))
	}
	if a.pendingWork != nil {
		work := a.pendingWork
		a.pendingWork = nil
		cmds = append(cmds, func() tea.Msg { return workMsg{payload: work()} })
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func neutralButtons(event tea.MouseMsg) mouseButtons {
	if event.Action == tea.MouseActionRelease {
		return buttonNone
	}
	switch event.Button {
	case tea.MouseButtonNone:
		return buttonNone
	case tea.MouseButtonLeft:
		return buttonLeft
	case tea.MouseButtonWheelUp:
		return buttonWheelUp
	case tea.MouseButtonWheelDown:
		return buttonWheelDown
	}
	return buttonOther
}

func mapKey(event tea.KeyMsg) string {
	switch event.Type {
	case tea.KeyRunes:
		if len(event.Runes) == 1 {
			return string(event.Runes)
		}
		return ""
	case tea.KeySpace:
		return " "
	case tea.KeyEsc:
		return "esc"
	case tea.KeyEnter:
		return "enter"
	case tea.KeyBackspace:
		return "backspace"
	case tea.KeyTab:
		return "tab"
	case tea.KeyShiftTab:
		return "shift-tab"
	case tea.KeyLeft:
		return "left"
	case tea.KeyRight:
		return "right"
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	case tea.KeyPgUp:
		return "pgup"
	case tea.KeyPgDown:
		return "pgdn"
	case tea.KeyHome:
		return "home"
	case tea.KeyEnd:
		return "end"
	case tea.KeyCtrlB:
		return "ctrl-b"
	case tea.KeyCtrlF:
		return "ctrl-f"
	case tea.KeyCtrlU:
		return "ctrl-u"
	case tea.KeyCtrlD:
		return "ctrl-d"
	case tea.KeyCtrlC:
		return "ctrl-c"
	}
	return ""
}

// runTUI 启动 Bubble Tea. 程序一开始就进入 alt-screen, 之后的加载,
// 刷新与报错都发生在备用屏幕内, 不污染用户原有终端内容.
func runTUI(app *App) error {
	p := tea.NewProgram(
		program{app: app},
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	app.LastRefresh = app.Now()
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("%s: %w", app.Context.TermInitFail, err)
	}
	return nil
}
