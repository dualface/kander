package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/focus"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
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
	TaskActions       *taskActions
	LoadTaskActions   func(string) (taskActionSource, error)
	actionSequence    uint64
	Width, Height     int
	Model             *BoardModel
	RefreshSecs       int
	Theme             string
	Columns           int
	MinColumnWidth    int
	Context           pageContext
	GetBoard          func() (BoardPayload, error)
	GetTask           func(string) (Task, error)
	GetBoardCtx       func(context.Context) (BoardPayload, error)
	GetTaskCtx        func(context.Context, string) (Task, error)
	CopyFn            copyFn
	FocusWindow       focusFn
	PrepareStart      func(string) (startRequest, error)
	StartTask         func(startRequest) (launch.StartResult, error)
	StartConfirmation *startDialog
	startSequence     uint64
	startNotice       *startNotice
	focusRunning      bool
	PersistColumns    persistFn
	Now               func() time.Time
	// PrepareTriage resolves the configured agent and launcher of an unstarted
	// takeover session; TriageIssue prepares the evidence and starts it through
	// the shared issue.StartTriage path, the same one `kander issue triage`
	// uses. cmd.go binds both and tests inject fakes.
	PrepareTriage func() (launch.TriagePreview, error)
	ResultIssue   func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error)
	TriageIssue   func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error)
	// PrepareChat resolves the agent and launcher of the chat box; StartChat
	// starts one card-less session for a typed message. cmd.go binds StartChat
	// when a board exists. A missing board offers kander init instead of
	// leaving StartChat nil for the whole process.
	PrepareChat func() (launch.ChatPreview, error)
	StartChat   func(message string) (launch.ChatResult, error)
	// missingBoard is set when startup could not locate kanban/. Board-requiring
	// actions then confirm creating it through InitBoard, the same path as
	// kander init. AttachBoard rebinds board-backed operations after success.
	missingBoard     bool
	boardRoot        string
	summaries        *board.SummaryIndex
	BoardInit        *boardInitState
	boardInitSeq     uint64
	PreviewBoardInit func() (string, error)
	InitBoard        func() (string, error)
	AttachBoard      func(root string)
	// While Chat is non-nil the chat box covers the board and owns the input;
	// chatDraft keeps the text of a closed chat box for the next open.
	Chat      *chatBox
	chatSeq   uint64
	chatDraft string
	// While Takeover is non-nil it covers the issues overlay and owns the input.
	Takeover    *takeoverState
	takeoverSeq uint64

	Searching        bool
	Detail           *Task
	DetailScroll     int
	DetailSearching  bool
	DetailQuery      string
	DetailMatchIndex int
	DetailPendingG   bool
	DetailCount      int
	DetailCountOn    bool
	DetailOp         string
	DetailFindWait   string
	DetailObjWait    string
	DetailLastFind   string
	DetailLastChar   rune
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
	// Session is the editable options config session, replaced from disk each time Options opens.
	Session *menu.Session
	// While Help is true the key reference overlay covers the board.
	Help bool
	// welcomeDismissed hides the empty-board welcome overlay for this process.
	welcomeDismissed bool
	// While Issues is non-nil the GitHub issues overlay covers the board; it
	// owns its own filters, selection and scrolling and never touches the board
	// model, so closing it leaves the board exactly as it was.
	Issues *issuesState
	// IssueProvider builds the read-only issue provider; cmd.go binds it from
	// internal/cli, while tests inject a fake.
	IssueProvider func() issue.IssueProvider
	// ImportIssue runs one issue import through the shared service; cmd.go
	// binds it from internal/cli and tests inject a fake.
	ImportIssue func(ctx context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error)
	// ImportIndex reads the board cards that already carry an imported source.
	ImportIndex func() (issue.Index, error)
	// LoadIssueCache returns the cached snapshot of one issue; ok=false is a
	// miss. cmd.go binds it to the machine-local cache below the board.
	LoadIssueCache func(repository issue.Repository, number int) (issue.IssueSnapshot, bool)
	// SaveIssueCache stores one fetched snapshot in the machine-local cache. The
	// cache is best effort; a failed write never blocks the view.
	SaveIssueCache func(snapshot issue.IssueSnapshot) error
	// OpenBrowser hands one validated issue URL to the platform opener.
	OpenBrowser     func(string) error
	issuesRepo      *issue.Repository
	issuesListSeq   uint64
	issuesDetailSeq uint64
	issuesImportSeq uint64
	issuesIndexSeq  uint64
	// pendingShell is an action that must hand the terminal back; pendingWork is a background task.
	pendingShell func()
	pendingWork  func() any
	// board/detail snapshot reads use a dedicated scheduler so they do not occupy
	// pendingWork (start, Issues, chat, task actions) and cannot block Update.
	boardReadSeq      uint64
	boardInFlightSeq  uint64
	boardReadQueued   bool
	boardReadCancel   context.CancelFunc
	detailReadSeq     uint64
	detailInFlightSeq uint64
	detailQueuedID    string
	detailReadCancel  context.CancelFunc
	// optionsLoadSeq identifies the in-flight Options config reload so a stale result cannot land on a newer panel.
	optionsLoadSeq uint64

	detailView  viewport.Model
	detailCache detailRender
}

// Update routes messages to the active popup, board, or detail view.
func (a *App) Update(msg tea.Msg) tea.Cmd {
	if event, ok := msg.(tea.KeyMsg); ok && mapKey(event) == "ctrl-c" && !a.confirmCapturesKeys() && (a.TaskActions == nil || !a.TaskActions.running) && (a.Chat == nil || a.Chat.phase != chatRunning) {
		a.requestQuit()
		return nil
	}
	if a.TaskActions != nil {
		return a.updateTaskActions(msg)
	}
	if a.Chat != nil {
		return a.updateChat(msg)
	}
	if a.Options != nil {
		if event, ok := msg.(tea.MouseMsg); ok {
			return a.Options.HandleMouse(event.X, event.Y, a.mouse.mapButtons(event.X, event.Y, neutralButtons(event), time.Now()))
		}
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

// View renders the board or detail view with the active popup on top.
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
	if a.Issues != nil {
		a.ShowCursor = a.Issues.editing != ""
	}
	if a.Takeover != nil {
		a.ShowCursor = false
	}
	h, w := a.size()
	p := themePalette(a.Theme)
	switch {
	case a.TaskActions != nil:
		a.ShowCursor = false
		box, popup := a.renderTaskActions()
		base = overlay(base, popup, box.X, box.Y, p)
	case a.Chat != nil:
		a.ShowCursor = false
		box, popup := a.renderChat()
		base = overlay(base, popup, box.X, box.Y, p)
	case a.Options != nil:
		box, popup := a.Options.view()
		base = overlay(base, popup, box.X, box.Y, p)
		if a.Options.confirm != nil {
			a.ShowCursor = false
			cbox, cpopup := a.Options.renderConfirm()
			base = overlay(base, cpopup, cbox.X, cbox.Y, p)
		}
	case a.StartConfirmation != nil:
		box, popup := a.renderStartConfirmation()
		base = overlay(base, popup, box.X, box.Y, p)
	case a.BoardInit != nil:
		if a.Issues != nil {
			issuesBox, issuesPopup := a.renderIssues()
			base = overlay(base, issuesPopup, issuesBox.X, issuesBox.Y, p)
		}
		box, popup := a.renderBoardInit()
		base = overlay(base, popup, box.X, box.Y, p)
	case a.Takeover != nil:
		if a.Issues != nil {
			issuesBox, issuesPopup := a.renderIssues()
			base = overlay(base, issuesPopup, issuesBox.X, issuesBox.Y, p)
		}
		box, popup := a.renderTakeover()
		base = overlay(base, popup, box.X, box.Y, p)
		if a.Help {
			helpBox, helpPopup := a.renderHelp()
			base = overlay(base, helpPopup, helpBox.X, helpBox.Y, p)
		}
	case a.Issues != nil:
		box, popup := a.renderIssues()
		base = overlay(base, popup, box.X, box.Y, p)
		if a.Help {
			helpBox, helpPopup := a.renderHelp()
			base = overlay(base, helpPopup, helpBox.X, helpBox.Y, p)
		}
	case a.Help:
		box, popup := a.renderHelp()
		base = overlay(base, popup, box.X, box.Y, p)
	case a.shouldShowWelcome():
		box, popup := a.renderWelcome()
		base = overlay(base, popup, box.X, box.Y, p)
	}
	if a.TaskActions == nil && a.Chat == nil && a.Options == nil && !a.Help && a.StartConfirmation == nil && a.BoardInit == nil && !a.shouldShowWelcome() && a.startNoticeOverflows(w) {
		box, popup := a.renderStartPopup([]string{a.startNotice.full})
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
		Model:           newBoardModel(single),
		RefreshSecs:     refresh,
		Theme:           theme,
		Columns:         clampColumns(columns),
		MinColumnWidth:  minColumnWidth,
		Context:         ctx,
		GetBoard:        getBoard,
		GetTask:         getTask,
		CopyFn:          copy,
		LoadTaskActions: loadTaskActionSource,
		FocusWindow:     focus.Window,
		PrepareStart:    prepareTaskStart,
		StartTask:       runTaskStart,
		PersistColumns:  persist,
		Now:             time.Now,
		Running:         true,
		Glyphs:          map[string]string{"vbar": "│", "bar": "▎", "hbar": "─", "dot": "·", "left": "‹", "right": "›"},
		LastRefresh:     time.Now(),
		detailView:      newDetailViewport(),
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

func (a *App) resetDetailSearch() {
	a.DetailSearching = false
	a.DetailQuery = ""
	a.DetailMatchIndex = 0
	a.resetDetailPending()
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
	n := a.boardBodyHeight() / cardHeight
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
	if a.Session != nil {
		a.Session.SyncTUI(written, true)
	}
	a.PrefsError = ""
}

func (a *App) handleBoardKey(key string) {
	switch key {
	case "q", "Q":
		a.requestQuit()
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
		a.requestBoardRefresh(true)
	case "o", "O":
		a.openOptions()
	case "?":
		a.Help = true
	case "y":
		a.copySelectedTaskID()
	case "m":
		a.openTaskActions()
	case "g":
		a.openIssues()
	case "c":
		a.openChat()
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

func (a *App) HandleKey(key string) {
	if a.StartConfirmation != nil {
		a.handleStartConfirmation(key)
		return
	}
	if a.BoardInit != nil {
		a.handleBoardInitKey(key)
		return
	}
	if a.Takeover != nil {
		a.handleTakeoverKey(key)
		return
	}
	if key == "ctrl-c" {
		a.requestQuit()
		return
	}
	// The help overlay is read-only and any key closes it.
	if a.Help {
		a.Help = false
		return
	}
	if a.shouldShowWelcome() {
		switch key {
		case "esc":
			a.dismissWelcome()
			return
		case "q", "Q", "c", "g", "?", "o", "O", "r", "R":
		default:
			return
		}
	}
	if a.Issues != nil {
		a.handleIssuesKey(key)
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
