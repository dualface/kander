package tui

import (
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

type copyFn func(string) (bool, string)
type persistFn func(int) (config.TUI, error)

type mouseSel struct {
	Kind         string
	TaskID       string
	Line, Col    int
	ContentWidth int
}

type boardHit struct {
	Kind  string
	State string
	Index int
	Delta int
}

type App struct {
	Width, Height  int
	Model          *BoardModel
	RefreshSecs    int
	Theme          string
	Columns        int
	MinColumnWidth int
	Context        pageContext
	GetBoard       func() (BoardPayload, error)
	GetTask        func(string) (Task, error)
	CopyFn         copyFn
	PersistColumns persistFn
	Now            func() time.Time

	Searching        bool
	Detail           *Task
	DetailScroll     int
	DetailSearching  bool
	DetailQuery      string
	DetailMatchIndex int
	DetailPendingG   bool
	DetailSelectMode string
	DetailAnchor     *[2]int
	DetailCursor     [2]int
	MouseSelecting   bool
	MouseAnchor      *mouseSel
	MouseCursor      *mouseSel
	CopyNotice       string
	CopyNoticeUntil  time.Time
	SuppressClick    bool
	LastRefresh      time.Time
	Running          bool
	PrefsError       string
	Glyphs           map[string]string
	CursorX, CursorY int
	ShowCursor       bool
	mouse            mouseTracker

	// While Options is non-nil the options popup covers the board and takes over input.
	Options *optionsPanel
	// Session is the config session, loaded on demand when the options are first opened.
	Session *menu.Session
	// While Help is true the key reference overlay covers the board.
	Help bool
	// pendingShell is an action that must hand the terminal back; pendingWork is a background task.
	pendingShell func()
	pendingWork  func() any

	detailView  viewport.Model
	detailCache detailRender
}

// Update is the message entry point of App: the options panel takes over while open, otherwise the board and detail view handle it.
func (a *App) Update(msg tea.Msg) tea.Cmd {
	if event, ok := msg.(tea.KeyMsg); ok && mapKey(event) == "ctrl-c" {
		a.Running = false
		return nil
	}
	if a.Options != nil {
		return a.Options.Update(msg)
	}
	switch event := msg.(type) {
	case tea.KeyMsg:
		a.HandleKey(mapKey(event))
		if a.Options != nil {
			return a.Options.Init()
		}
	case tea.MouseMsg:
		a.HandleMouse(event.X, event.Y, a.mouse.mapButtons(event.X, event.Y, neutralButtons(event), time.Now()))
	}
	return nil
}

// View renders the whole screen: the board or the detail view underneath, with the options popup on top.
func (a *App) View() string {
	var base string
	switch {
	case a.Detail != nil && a.Options == nil:
		a.ShowCursor = a.DetailSearching
		base = a.renderDetailView()
	default:
		a.ShowCursor = a.Searching
		base = a.renderBoardView()
	}
	h, w := a.size()
	p := themePalette(a.Theme)
	switch {
	case a.Options != nil:
		box, popup := a.Options.view()
		base = overlay(base, popup, box.X, box.Y, p)
	case a.Help:
		box, popup := a.renderHelp()
		base = overlay(base, popup, box.X, box.Y, p)
	}
	return paintScreen(base, w, h, p)
}

func newApp(single bool, refresh int, ctx pageContext, getBoard func() (BoardPayload, error), getTask func(string) (Task, error), theme string, columns int, persist persistFn, copy copyFn) *App {
	if copy == nil {
		copy = copyToClipboard
	}
	if persist == nil {
		persist = saveColumns
	}
	return &App{
		Model:          newBoardModel(single),
		RefreshSecs:    refresh,
		Theme:          theme,
		Columns:        clampColumns(columns),
		MinColumnWidth: minColumnWidth,
		Context:        ctx,
		GetBoard:       getBoard,
		GetTask:        getTask,
		CopyFn:         copy,
		PersistColumns: persist,
		Now:            time.Now,
		Running:        true,
		Glyphs:         map[string]string{"vbar": "│", "bar": "▎", "hbar": "─", "dot": "·", "left": "‹", "right": "›"},
		LastRefresh:    time.Now(),
		detailView:     newDetailViewport(),
	}
}

func (a *App) size() (h, w int) {
	if a.Height < 1 {
		a.Height = 24
	}
	if a.Width < 1 {
		a.Width = 80
	}
	return a.Height, a.Width
}

func (a *App) refreshBoard() bool {
	previous := a.Model.RefreshError
	payload, err := a.GetBoard()
	if err != nil {
		a.Model.RefreshError = err.Error()
		a.LastRefresh = a.Now()
		return a.Model.RefreshError != previous
	}
	changed := a.Model.SetBoard(payload)
	a.LastRefresh = a.Now()
	return changed || a.refreshOpenDetail()
}

func (a *App) refreshOpenDetail() bool {
	if a.Detail == nil {
		return false
	}
	taskID := a.Detail.TaskID
	if taskID == "" {
		return false
	}
	previous := a.Model.DetailError
	next, err := a.GetTask(taskID)
	if err != nil {
		a.Model.DetailError = err.Error()
		return a.Model.DetailError != previous
	}
	a.Model.DetailError = ""
	changed := a.Detail.Document != next.Document ||
		a.Detail.Title != next.Title ||
		a.Detail.Time != next.Time ||
		a.Detail.State != next.State ||
		a.Detail.Assignee != next.Assignee ||
		a.Detail.Kind != next.Kind ||
		a.Detail.TaskGroup != next.TaskGroup ||
		a.Detail.Type != next.Type
	a.Detail = &next
	a.clampDetailCursor()
	matches := a.detailMatches(nil)
	if len(matches) > 0 && a.DetailMatchIndex > len(matches)-1 {
		a.DetailMatchIndex = len(matches) - 1
	} else if len(matches) == 0 {
		a.DetailMatchIndex = 0
	}
	return changed || previous != ""
}

func (a *App) openDetail() {
	selected := a.Model.SelectedTask()
	if selected == nil {
		return
	}
	task, err := a.GetTask(selected.TaskID)
	if err != nil {
		a.Model.DetailError = err.Error()
		return
	}
	a.Detail = &task
	a.DetailScroll = 0
	a.DetailCursor = [2]int{0, 0}
	a.resetDetailSearch()
	a.resetMouseSelection()
	a.Model.DetailError = ""
}

func (a *App) closeDetail() {
	a.Detail = nil
	a.DetailScroll = 0
	a.resetDetailSearch()
	a.resetMouseSelection()
}

func (a *App) resetDetailSearch() {
	a.DetailSearching = false
	a.DetailQuery = ""
	a.DetailMatchIndex = 0
	a.DetailPendingG = false
	a.resetDetailSelection()
	a.ShowCursor = false
}

func (a *App) resetDetailSelection() {
	a.DetailSelectMode = ""
	a.DetailAnchor = nil
}

func (a *App) resetMouseSelection() {
	a.MouseSelecting = false
	a.MouseAnchor = nil
	a.MouseCursor = nil
	a.SuppressClick = false
}

func (a *App) mouseSelectionMoved() bool {
	if a.MouseAnchor == nil || a.MouseCursor == nil {
		return false
	}
	return *a.MouseAnchor != *a.MouseCursor
}

func (a *App) notifyCopy(text string, success bool, err string) {
	if success {
		preview := clipText(strings.ReplaceAll(text, "\n", " "), 40)
		a.CopyNotice = a.Context.Copied + ": " + preview
	} else {
		if err == "" {
			err = a.Context.ClipboardNA
		}
		a.CopyNotice = a.Context.CopyFailed + ": " + err
	}
	a.CopyNoticeUntil = a.Now().Add(time.Duration(copyNoticeSeconds * float64(time.Second)))
}

func (a *App) copyText(text string) bool {
	ok, err := a.CopyFn(text)
	a.notifyCopy(text, ok, err)
	return ok
}

func (a *App) copySelectedTaskID() {
	selected := a.Model.SelectedTask()
	if selected == nil || selected.TaskID == "" {
		return
	}
	a.copyText(selected.TaskID)
}

func (a *App) cardMetaLine(task Task) string {
	var groupOrType string
	if task.TaskGroup != "" {
		groupOrType = compactGroup(task.TaskGroup)
	} else {
		parts := []string{}
		if task.Type != "" {
			parts = append(parts, task.Type)
		}
		parts = append(parts, a.Context.sizeLabel(task.Kind))
		groupOrType = strings.Join(parts, " / ")
	}
	assignee := task.Assignee
	if assignee == "" {
		assignee = a.Context.Unassigned
	}
	return compactTime(orDash(task.Time)) + " " + a.Glyphs["dot"] + " " + assignee + " " + a.Glyphs["dot"] + " " + groupOrType
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func (a *App) boardCardLines(task Task, contentWidth int) []string {
	title := task.Title
	if title == "" {
		title = task.TaskID
	}
	return []string{
		clipText(title, contentWidth),
		clipText(task.TaskID, contentWidth),
		clipText(a.cardMetaLine(task), contentWidth),
	}
}

// detailBodyHeight is the number of body lines visible in the detail panel:
// the header, the blank line, the panel top border, the metadata, the separator, the panel bottom border and the status bar are all subtracted.
func (a *App) detailBodyHeight() int {
	h, _ := a.size()
	n := h - detailBodyTop - 2
	if n < 1 {
		return 1
	}
	return n
}

func (a *App) detailMatches(lines []string) []int {
	if lines == nil {
		lines = a.detailLines()
	}
	return lineMatchIndexes(lines, a.DetailQuery)
}

func (a *App) clampDetailCursor() {
	lines := a.detailLines()
	if len(lines) == 0 {
		a.DetailCursor = [2]int{0, 0}
		return
	}
	line, col := a.DetailCursor[0], a.DetailCursor[1]
	if line > len(lines)-1 {
		line = len(lines) - 1
	}
	if line < 0 {
		line = 0
	}
	n := runeCount(lines[line])
	if col > n {
		col = n
	}
	if col < 0 {
		col = 0
	}
	a.DetailCursor = [2]int{line, col}
}

func (a *App) scrollDetailBy(delta int) {
	lines := a.detailLines()
	body := a.detailBodyHeight()
	maxScroll := len(lines) - body
	if maxScroll < 0 {
		maxScroll = 0
	}
	a.DetailScroll += delta
	if a.DetailScroll < 0 {
		a.DetailScroll = 0
	}
	if a.DetailScroll > maxScroll {
		a.DetailScroll = maxScroll
	}
	line, col := a.DetailCursor[0], a.DetailCursor[1]
	if line < a.DetailScroll {
		line = a.DetailScroll
	}
	if line >= a.DetailScroll+body {
		line = a.DetailScroll + body - 1
	}
	if line < 0 {
		line = 0
	}
	if len(lines) > 0 && line > len(lines)-1 {
		line = len(lines) - 1
	}
	if len(lines) > 0 {
		n := runeCount(lines[line])
		if col > n {
			col = n
		}
	}
	a.DetailCursor = [2]int{line, col}
}

func (a *App) ensureDetailCursorVisible(lines []string) {
	body := a.detailBodyHeight()
	line := a.DetailCursor[0]
	if line < a.DetailScroll {
		a.DetailScroll = line
	} else if line >= a.DetailScroll+body {
		a.DetailScroll = line - body + 1
	}
	maxScroll := len(lines) - body
	if maxScroll < 0 {
		maxScroll = 0
	}
	if a.DetailScroll < 0 {
		a.DetailScroll = 0
	}
	if a.DetailScroll > maxScroll {
		a.DetailScroll = maxScroll
	}
}

func (a *App) jumpDetailMatch(direction int) {
	matches := a.detailMatches(nil)
	if len(matches) == 0 {
		return
	}
	a.DetailMatchIndex = (a.DetailMatchIndex + direction) % len(matches)
	if a.DetailMatchIndex < 0 {
		a.DetailMatchIndex += len(matches)
	}
	line := matches[a.DetailMatchIndex]
	a.DetailCursor = [2]int{line, 0}
	a.ensureDetailCursorVisible(a.detailLines())
}

func (a *App) applyDetailSearch() {
	a.DetailSearching = false
	a.ShowCursor = false
	matches := a.detailMatches(nil)
	if len(matches) == 0 {
		return
	}
	a.DetailMatchIndex = 0
	a.DetailCursor = [2]int{matches[0], 0}
	a.ensureDetailCursorVisible(a.detailLines())
}

func (a *App) pageSize() int {
	h, _ := a.size()
	n := (h - bodyTop) / cardHeight
	if n < 1 {
		return 1
	}
	return n
}

func (a *App) page(direction int) {
	page := a.pageSize()
	state := a.Model.CurrentState()
	a.Model.MoveTask(direction * page)
	taskCount := len(a.Model.TasksFor(state))
	next := a.Model.Scrolls[state] + direction*page
	maxScroll := taskCount - page
	if maxScroll < 0 {
		maxScroll = 0
	}
	if next < 0 {
		next = 0
	}
	if next > maxScroll {
		next = maxScroll
	}
	a.Model.Scrolls[state] = next
}

func (a *App) visibleColumnLayout() []boardLayout {
	_, width := a.size()
	count := visibleColumnCount(width, len(a.Model.States()), a.Model.Single, a.Columns, a.MinColumnWidth)
	states := a.Model.VisibleStates(count)
	geom := columnGeometry(width, len(states))
	out := make([]boardLayout, len(states))
	for i, state := range states {
		out[i] = boardLayout{State: state, X: geom[i].X, Width: geom[i].Width, HasSeparator: geom[i].HasSeparator}
	}
	return out
}

// adjustColumns changes how many columns the user wants on screen. How many are actually shown also depends on the terminal width
// and is settled by visibleColumnCount.
func (a *App) adjustColumns(delta int) {
	next := clampColumns(a.Columns + delta)
	if next == a.Columns {
		return
	}
	a.Columns = next
	if a.PersistColumns == nil {
		a.PrefsError = ""
		return
	}
	written, err := a.PersistColumns(a.Columns)
	if err != nil {
		a.PrefsError = err.Error()
		return
	}
	// The preference already reached disk, so the cached baseline of the options session is synced, otherwise the next save would blame another process for the change.
	a.Session.SyncTUI(written, true)
	a.PrefsError = ""
}

func (a *App) handleBoardKey(key string) {
	switch key {
	case "q", "Q":
		a.Running = false
	case "left", "h", "H":
		a.Model.MoveColumn(-1)
	case "right", "l", "L", "tab":
		a.Model.MoveColumn(1)
	case "up", "k", "K":
		a.Model.MoveTask(-1)
	case "down", "j", "J":
		a.Model.MoveTask(1)
	case "pgup":
		a.page(-1)
	case "pgdn":
		a.page(1)
	case "home":
		a.Model.MoveTask(-len(a.Model.Tasks))
	case "end":
		a.Model.MoveTask(len(a.Model.Tasks))
	case "/":
		a.Searching = true
		a.ShowCursor = true
	case "a", "A":
		a.Model.ToggleArchived()
	case "t", "T":
		a.Theme = themes[(themeIndex(a.Theme)+1)%len(themes)]
	case "-", "_":
		a.adjustColumns(-1)
	case "=", "+":
		a.adjustColumns(1)
	case "r", "R":
		a.refreshBoard()
	case "o", "O":
		a.openOptions()
	case "?":
		a.Help = true
	case "y":
		a.copySelectedTaskID()
	case "enter":
		a.openDetail()
	}
}

func (a *App) handleSearchKey(key string) {
	switch key {
	case "enter":
		a.Searching = false
		a.ShowCursor = false
	case "esc":
		a.Model.Query = ""
		a.Model.Normalize()
		a.Searching = false
		a.ShowCursor = false
	case "backspace":
		if a.Model.Query != "" {
			runes := []rune(a.Model.Query)
			a.Model.Query = string(runes[:len(runes)-1])
			a.Model.Normalize()
		}
	default:
		if isPrintableKey(key) {
			a.Model.Query += key
			a.Model.Normalize()
		}
	}
}

func (a *App) handleDetailSearchKey(key string) {
	switch key {
	case "enter":
		a.applyDetailSearch()
	case "esc":
		a.DetailQuery = ""
		a.DetailMatchIndex = 0
		a.DetailSearching = false
		a.ShowCursor = false
	case "backspace":
		if a.DetailQuery != "" {
			runes := []rune(a.DetailQuery)
			a.DetailQuery = string(runes[:len(runes)-1])
		}
	default:
		if isPrintableKey(key) {
			a.DetailQuery += key
		}
	}
}

func isPrintableKey(key string) bool {
	if key == "" || len([]rune(key)) != 1 {
		return false
	}
	r := []rune(key)[0]
	return unicode.IsPrint(r)
}

func (a *App) detailSelectionActive() bool {
	return a.DetailSelectMode != "" && a.DetailAnchor != nil
}

func (a *App) detailToggleSelect(mode string) {
	if a.DetailSelectMode == mode {
		a.resetDetailSelection()
		return
	}
	a.DetailSelectMode = mode
	a.resetMouseSelection()
	line, col := a.DetailCursor[0], a.DetailCursor[1]
	lines := a.detailLines()
	if mode == "line" {
		if len(lines) == 0 {
			a.DetailAnchor = &[2]int{0, 0}
			a.DetailCursor = [2]int{0, 0}
			return
		}
		if line > len(lines)-1 {
			line = len(lines) - 1
		}
		if line < 0 {
			line = 0
		}
		a.DetailAnchor = &[2]int{line, 0}
		a.DetailCursor = [2]int{line, runeCount(lines[line])}
		return
	}
	a.DetailAnchor = &[2]int{line, col}
}

func (a *App) detailYank() {
	if a.DetailSelectMode == "" || a.DetailAnchor == nil {
		return
	}
	lines := a.detailLines()
	var text string
	if a.DetailSelectMode == "line" {
		text = extractLineSelection(lines, *a.DetailAnchor, a.DetailCursor)
	} else {
		text = extractCharSelection(lines, *a.DetailAnchor, a.DetailCursor)
	}
	if text != "" {
		a.copyText(text)
	}
	a.resetDetailSelection()
}

func (a *App) detailMoveCursor(deltaLine, deltaCol int) {
	lines := a.detailLines()
	if len(lines) == 0 {
		return
	}
	line, col := a.DetailCursor[0], a.DetailCursor[1]
	line += deltaLine
	if line < 0 {
		line = 0
	}
	if line > len(lines)-1 {
		line = len(lines) - 1
	}
	lineText := lines[line]
	if deltaCol != 0 {
		col += deltaCol
		n := runeCount(lineText)
		if col < 0 {
			col = 0
		}
		if col > n {
			col = n
		}
	} else if deltaLine != 0 {
		n := runeCount(lineText)
		if col > n {
			col = n
		}
	}
	a.DetailCursor = [2]int{line, col}
	a.ensureDetailCursorVisible(lines)
}

func (a *App) handleDetailKey(key string) {
	if a.DetailSearching {
		a.handleDetailSearchKey(key)
		return
	}
	if a.DetailPendingG {
		a.DetailPendingG = false
		if key == "g" {
			a.DetailScroll = 0
			a.DetailCursor = [2]int{0, 0}
			return
		}
	}
	switch key {
	case "up", "k", "K":
		a.detailMoveCursor(-1, 0)
		return
	case "down", "j", "J":
		a.detailMoveCursor(1, 0)
		return
	case "left", "h", "H":
		a.detailMoveCursor(0, -1)
		return
	case "right", "l", "L":
		a.detailMoveCursor(0, 1)
		return
	}
	if a.detailSelectionActive() {
		switch key {
		case "y":
			a.detailYank()
			return
		case "v":
			a.detailToggleSelect("char")
			return
		case "V":
			a.detailToggleSelect("line")
			return
		case "q", "Q", "esc", "backspace":
			a.resetDetailSelection()
			return
		}
	}
	pageHeight := a.detailBodyHeight()
	half := pageHeight / 2
	if half < 1 {
		half = 1
	}
	switch key {
	case "q", "Q", "esc", "backspace":
		a.closeDetail()
	case "pgup", "ctrl-b":
		a.scrollDetailBy(-pageHeight)
	case "pgdn", "ctrl-f":
		a.scrollDetailBy(pageHeight)
	case "ctrl-u":
		a.scrollDetailBy(-half)
	case "ctrl-d":
		a.scrollDetailBy(half)
	case "g":
		a.DetailPendingG = true
	case "G", "end":
		lines := a.detailLines()
		if len(lines) > 0 {
			last := len(lines) - 1
			a.DetailCursor = [2]int{last, runeCount(lines[last])}
		}
		a.DetailScroll = 1 << 30
		a.ensureDetailCursorVisible(a.detailLines())
	case "home":
		a.DetailCursor = [2]int{0, 0}
		a.DetailScroll = 0
	case "/":
		a.DetailSearching = true
		a.DetailPendingG = false
		a.ShowCursor = true
	case "?":
		a.Help = true
	case "n":
		a.jumpDetailMatch(1)
	case "N":
		a.jumpDetailMatch(-1)
	case "v":
		a.detailToggleSelect("char")
	case "V":
		a.detailToggleSelect("line")
	case "y":
		a.detailYank()
	}
}

func (a *App) HandleKey(key string) {
	if key == "ctrl-c" {
		a.Running = false
		return
	}
	// The help overlay is read-only and any key closes it.
	if a.Help {
		a.Help = false
		return
	}
	if a.Detail != nil {
		a.handleDetailKey(key)
		return
	}
	if a.Searching {
		a.handleSearchKey(key)
		return
	}
	a.handleBoardKey(key)
}
