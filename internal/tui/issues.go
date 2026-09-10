package tui

import (
	"context"
	"strings"
	"time"

	"github.com/dualface/kander/internal/issue"
)

const (
	issuesRequestTimeout = 60 * time.Second
	issuesListLimit      = 50
	// Below this width the overlay switches from list/detail columns to list
	// and detail pages.
	issuesWideMinWidth = 100
	issuesMaxWidth     = 140
	issuesItemLines    = 3
	issuesListHeader   = 1
)

// issuesState is the whole state of the issues overlay. The board model is
// never touched, so closing the overlay leaves selection, scrolling and layout
// exactly as they were.
type issuesState struct {
	state  string
	label  string
	search string

	// editing is "" while browsing, or "search" / "label" while the filter
	// input line is open.
	editing string
	input   string

	repository *issue.Repository

	items      []issue.IssueSummary
	limit      int
	more       bool
	loading    bool
	listErr    string
	selected   int
	listScroll int

	// index maps a canonical source key to the local card already bound to it.
	// It is read through pendingWork together with the list; a failed read only
	// means the imported markers are missing, never that imports are refused.
	index    issue.Index
	indexErr string
	// importing is true while an import request is in flight. It is a UI hint
	// only: the board transaction remains the authority on duplicates.
	importing bool

	detailNumber  int
	detail        *issue.IssueSnapshot
	detailLoading bool
	detailErr     string
	detailScroll  int
	detailStamp   uint64
	showDetail    bool
	// detailReload carries a detail number that has to be fetched again after
	// the list request finishes; pendingWork holds only one task at a time.
	detailReload int
	renderKey    string
	renderLines  []string

	notice      string
	noticeUntil time.Time
}

// Background results carry the request sequence; a result whose sequence is no
// longer the latest is dropped, which keeps out-of-order async replies from
// landing on a newer query or a reopened overlay.
type issuesListResult struct {
	seq        uint64
	repository *issue.Repository
	page       issue.IssuePage
	index      issue.Index
	indexErr   error
	err        error
}

type issuesDetailResult struct {
	seq      uint64
	number   int
	snapshot issue.IssueSnapshot
	err      error
}

// issuesImportResult carries the request sequence and the exact issue identity
// of one import; a result that no longer matches the current selection is
// dropped without reporting anything for another issue.
type issuesImportResult struct {
	seq        uint64
	repository issue.Repository
	number     int
	result     issue.ImportResult
	index      issue.Index
	indexErr   error
	err        error
}

func (a *App) applyIssuesImport(result issuesImportResult) {
	st := a.Issues
	if st == nil || result.seq != a.issuesImportSeq {
		return
	}
	st.importing = false
	if result.index != nil {
		st.index = result.index
		st.indexErr = ""
		a.LastRefresh = time.Time{}
	}
	if result.err != nil {
		a.issuesSetNotice(a.Context.IssuesImportFailed + ": " + issue.Message(result.err))
		return
	}
	if result.number != a.issuesSelectedNumber() {
		return
	}
	key := "tui.issues_import_created"
	if result.result.Existing {
		key = "tui.issues_import_existing"
	}
	a.issuesSetNotice(t(key, result.result.TaskID))
}

func (a *App) openIssues() {
	if a.Issues != nil {
		return
	}
	a.Issues = &issuesState{state: issue.IssueStateOpen}
	a.issuesReloadList()
}

func (a *App) closeIssues() {
	a.Issues = nil
}

func nextIssueState(state string) string {
	switch state {
	case issue.IssueStateOpen:
		return issue.IssueStateClosed
	case issue.IssueStateClosed:
		return issue.IssueStateAll
	default:
		return issue.IssueStateOpen
	}
}

// issuesReloadList starts a fresh list request for the current filters. The
// repository resolution is cached for the session; a failed list keeps the
// resolved identity so a retry does not have to resolve again.
func (a *App) issuesReloadList() {
	st := a.Issues
	if st == nil {
		return
	}
	a.issuesListSeq++
	a.issuesDetailSeq++
	seq := a.issuesListSeq
	query := issue.IssueQuery{State: st.state, Search: st.search, Limit: issuesListLimit}
	if st.label != "" {
		query.Labels = []string{st.label}
	}
	provider := a.IssueProvider
	loader := a.ImportIndex
	cached := a.issuesRepo
	st.loading = true
	st.listErr = ""
	// A new list request supersedes the previous background task, so a lost
	// import result must not leave the overlay unable to start another import.
	st.importing = false
	a.pendingWork = func() any {
		if provider == nil {
			return issuesListResult{seq: seq, err: issue.NewError(issue.ErrorCLIUnavailable, "tui", "no issue provider is registered")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), issuesRequestTimeout)
		defer cancel()
		instance := provider()
		repository := cached
		if repository == nil {
			resolved, err := instance.ResolveRepository(ctx, ".", "")
			if err != nil {
				return issuesListResult{seq: seq, err: err}
			}
			repository = &resolved
		}
		page, err := instance.ListIssues(ctx, *repository, query)
		result := issuesListResult{seq: seq, repository: repository, page: page, err: err}
		if err == nil && loader != nil {
			result.index, result.indexErr = loader()
		}
		return result
	}
}

// issuesLoadDetail starts the snapshot request of one issue. Comments are
// requested only here, so the list never pays for them.
func (a *App) issuesLoadDetail(number int) {
	st := a.Issues
	if st == nil || number <= 0 {
		return
	}
	repository := a.issuesRepository()
	if repository == nil {
		st.detailErr = a.Context.IssuesLoadFailed
		return
	}
	a.issuesDetailSeq++
	seq := a.issuesDetailSeq
	st.detailNumber = number
	st.detail = nil
	st.detailErr = ""
	st.detailLoading = true
	st.detailScroll = 0
	st.detailStamp++
	provider := a.IssueProvider
	a.pendingWork = func() any {
		if provider == nil {
			return issuesDetailResult{seq: seq, number: number, err: issue.NewError(issue.ErrorCLIUnavailable, "tui", "no issue provider is registered")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), issuesRequestTimeout)
		defer cancel()
		snapshot, err := provider().GetIssue(ctx, *repository, number, true)
		return issuesDetailResult{seq: seq, number: number, snapshot: snapshot, err: err}
	}
}

func (a *App) applyIssuesList(result issuesListResult) {
	st := a.Issues
	if st == nil || result.seq != a.issuesListSeq {
		return
	}
	st.loading = false
	if result.repository != nil {
		a.issuesRepo = result.repository
		st.repository = result.repository
	}
	if result.err != nil {
		st.listErr = issue.Message(result.err)
		st.detailReload = 0
		return
	}
	st.items = append([]issue.IssueSummary{}, result.page.Issues...)
	st.limit = result.page.Limit
	st.more = result.page.More
	st.listErr = ""
	if result.index != nil {
		st.index = result.index
		st.indexErr = ""
	}
	if st.selected > len(st.items)-1 {
		st.selected = max(0, len(st.items)-1)
	}
	if st.listScroll > len(st.items)-1 {
		st.listScroll = max(0, len(st.items)-1)
	}
	if st.detailReload > 0 {
		number := st.detailReload
		st.detailReload = 0
		a.issuesLoadDetail(number)
	}
}

func (a *App) applyIssuesDetail(result issuesDetailResult) {
	st := a.Issues
	if st == nil || result.seq != a.issuesDetailSeq {
		return
	}
	if result.number != st.detailNumber {
		return
	}
	st.detailLoading = false
	if result.err != nil {
		st.detail = nil
		st.detailErr = issue.Message(result.err)
		return
	}
	snapshot := result.snapshot
	st.detail = &snapshot
	st.detailErr = ""
	st.detailScroll = 0
}

func (a *App) issuesRepository() *issue.Repository {
	if a.Issues != nil && a.Issues.repository != nil {
		return a.Issues.repository
	}
	return a.issuesRepo
}

// issuesLocalCard reports the local card bound to one issue. A missing index or
// an unresolved repository simply means "not known to be imported".
func (a *App) issuesLocalCard(number int) (issue.LocalCard, bool) {
	st := a.Issues
	if st == nil || len(st.index) == 0 || number <= 0 {
		return issue.LocalCard{}, false
	}
	repository := a.issuesRepository()
	if repository == nil {
		return issue.LocalCard{}, false
	}
	key, err := repository.IssueSourceKey(number)
	if err != nil {
		return issue.LocalCard{}, false
	}
	local, ok := st.index[key]
	return local, ok
}

// issuesImportOrJump imports the selected issue, or focuses the card that
// already carries it. The jump never fetches anything; the import runs in
// pendingWork because it touches the network and writes the board.
func (a *App) issuesImportOrJump(withComments bool) {
	st := a.Issues
	if st == nil {
		return
	}
	number := a.issuesSelectedNumber()
	if local, ok := a.issuesLocalCard(number); ok {
		if a.issuesFocusLocalCard(local.TaskID) {
			return
		}
		a.issuesSetNotice(a.Context.IssuesImportNoCard + ": " + local.TaskID)
		return
	}
	a.issuesImport(withComments)
}

// issuesImport starts one import request. The request is bound to the resolved
// repository and the issue number; the result is dropped when either moved on.
func (a *App) issuesImport(withComments bool) {
	st := a.Issues
	if st == nil || st.importing {
		return
	}
	number := a.issuesSelectedNumber()
	repository := a.issuesRepository()
	if number <= 0 || repository == nil {
		a.issuesSetNotice(a.Context.IssuesNoTarget)
		return
	}
	importer := a.ImportIssue
	if importer == nil {
		a.issuesSetNotice(a.Context.IssuesImportFailed)
		return
	}
	loader := a.ImportIndex
	resolved := *repository
	a.issuesImportSeq++
	seq := a.issuesImportSeq
	st.importing = true
	a.issuesSetNotice(a.Context.IssuesImporting)
	a.pendingWork = func() any {
		ctx, cancel := context.WithTimeout(context.Background(), issuesRequestTimeout)
		defer cancel()
		result, err := importer(ctx, resolved, number, issue.ImportOptions{Comments: withComments})
		out := issuesImportResult{seq: seq, repository: resolved, number: number, result: result, err: err}
		if err == nil && loader != nil {
			out.index, out.indexErr = loader()
		}
		return out
	}
}

// issuesFocusLocalCard moves the board selection onto one card and closes the
// overlay. The archive column is opened when the card lives there.
func (a *App) issuesFocusLocalCard(taskID string) bool {
	if a.selectLocalCard(taskID) {
		a.closeIssues()
		return true
	}
	if !a.Model.ShowArchived {
		a.Model.ToggleArchived()
		if a.selectLocalCard(taskID) {
			a.closeIssues()
			return true
		}
	}
	return false
}

func (a *App) selectLocalCard(taskID string) bool {
	for _, state := range a.Model.States() {
		for index, task := range a.Model.TasksFor(state) {
			if task.TaskID == taskID {
				a.Model.FocusState(state)
				a.Model.SelectTaskIndex(state, index)
				return true
			}
		}
	}
	if a.Model.Query != "" {
		a.Model.Query = ""
		return a.selectLocalCard(taskID)
	}
	return false
}

// issuesSelectedNumber returns the issue the user is acting on: the open detail
// on the detail page, otherwise the selected list item.
func (a *App) issuesSelectedNumber() int {
	st := a.Issues
	if st == nil {
		return 0
	}
	if st.detailNumber > 0 && (st.showDetail || st.detail != nil || st.detailLoading) {
		return st.detailNumber
	}
	if st.selected >= 0 && st.selected < len(st.items) {
		return st.items[st.selected].Number
	}
	return 0
}

func (a *App) issuesMoveSelection(delta int) {
	st := a.Issues
	if st == nil || len(st.items) == 0 {
		return
	}
	next := st.selected + delta
	if next < 0 {
		next = 0
	}
	if next > len(st.items)-1 {
		next = len(st.items) - 1
	}
	if next == st.selected {
		return
	}
	st.selected = next
	// A detail that belongs to another issue would otherwise sit next to a new
	// selection.
	if st.detailNumber > 0 && st.detailNumber != st.items[next].Number {
		st.detail = nil
		st.detailNumber = 0
		st.detailErr = ""
		st.detailLoading = false
		st.detailScroll = 0
		st.showDetail = false
		st.detailReload = 0
	}
	a.issuesEnsureSelectionVisible()
}

func (a *App) issuesEnsureSelectionVisible() {
	st := a.Issues
	if st == nil {
		return
	}
	capacity := a.issuesListCapacity()
	if st.selected < st.listScroll {
		st.listScroll = st.selected
	}
	if st.selected >= st.listScroll+capacity {
		st.listScroll = st.selected - capacity + 1
	}
	if st.listScroll < 0 {
		st.listScroll = 0
	}
	maxScroll := len(st.items) - capacity
	if maxScroll < 0 {
		maxScroll = 0
	}
	if st.listScroll > maxScroll {
		st.listScroll = maxScroll
	}
}

func (a *App) issuesListCapacity() int {
	layout := a.issuesLayout()
	capacity := (layout.bodyHeight - issuesListHeader) / issuesItemLines
	if capacity < 1 {
		capacity = 1
	}
	return capacity
}

func (a *App) issuesSetNotice(message string) {
	st := a.Issues
	if st == nil {
		return
	}
	st.notice = message
	st.noticeUntil = a.Now().Add(4 * time.Second)
}

func (a *App) issuesNotice() string {
	st := a.Issues
	if st == nil || st.notice == "" {
		return ""
	}
	if a.Now().After(st.noticeUntil) {
		return ""
	}
	return st.notice
}

// issuesOpenBrowser builds the URL from the confirmed identity only, validates
// every part of it and hands it to the platform opener as a direct argv
// element. A URL from remote content is never trusted.
func (a *App) issuesOpenBrowser() {
	number := a.issuesSelectedNumber()
	repository := a.issuesRepository()
	if number <= 0 || repository == nil {
		a.issuesSetNotice(a.Context.IssuesNoTarget)
		return
	}
	target, err := repository.IssueURL(number)
	if err != nil || !validIssueURL(target, *repository) {
		a.issuesSetNotice(a.Context.IssuesNoTarget)
		return
	}
	opener := a.OpenBrowser
	if opener == nil {
		opener = openExternalURL
	}
	if err := opener(target); err != nil {
		a.issuesSetNotice(a.Context.IssuesBrowserFailed + ": " + err.Error())
		return
	}
	a.issuesSetNotice(a.Context.IssuesBrowserOpened)
}

func validIssueURL(target string, repository issue.Repository) bool {
	if !strings.HasPrefix(target, "https://") {
		return false
	}
	rest := strings.TrimPrefix(target, "https://")
	host, path, ok := strings.Cut(rest, "/")
	if !ok || !strings.EqualFold(host, repository.Host) {
		return false
	}
	return strings.HasPrefix(path, repository.Owner+"/"+repository.Name+"/issues/")
}

func (a *App) handleIssuesKey(key string) {
	st := a.Issues
	if st == nil {
		return
	}
	if st.editing != "" {
		a.handleIssuesEditKey(key)
		return
	}
	switch key {
	case "esc":
		if a.issuesDetailPageActive() {
			st.showDetail = false
			return
		}
		a.closeIssues()
	case "q", "Q":
		a.closeIssues()
	case "tab":
		st.state = nextIssueState(st.state)
		a.issuesResetDetail()
		a.issuesReloadList()
	case "/":
		st.editing = "search"
		st.input = st.search
	case "l", "L":
		st.editing = "label"
		st.input = st.label
	case "r", "R":
		a.issuesRefresh()
	case "enter":
		number := a.issuesSelectedNumber()
		if number > 0 {
			st.showDetail = true
			a.issuesLoadDetail(number)
		}
	case "o", "O":
		a.issuesOpenBrowser()
	case "i":
		a.issuesImportOrJump(false)
	case "I":
		a.issuesImportOrJump(true)
	case "up", "k", "K":
		if a.issuesDetailPageActive() {
			a.issuesScrollDetail(-1)
			return
		}
		a.issuesMoveSelection(-1)
	case "down", "j", "J":
		if a.issuesDetailPageActive() {
			a.issuesScrollDetail(1)
			return
		}
		a.issuesMoveSelection(1)
	case "pgup", "ctrl-b":
		if a.issuesScrollKeysActive() {
			a.issuesScrollDetail(-a.issuesDetailBodyHeight())
			return
		}
		a.issuesMoveSelection(-a.issuesListCapacity())
	case "pgdn", "ctrl-f":
		if a.issuesScrollKeysActive() {
			a.issuesScrollDetail(a.issuesDetailBodyHeight())
			return
		}
		a.issuesMoveSelection(a.issuesListCapacity())
	case "home":
		if a.issuesScrollKeysActive() {
			st.detailScroll = 0
			return
		}
		st.selected = 0
		a.issuesEnsureSelectionVisible()
	case "end":
		if a.issuesScrollKeysActive() {
			st.detailScroll = 1 << 30
			a.issuesClampDetailScroll()
			return
		}
		if len(st.items) > 0 {
			st.selected = len(st.items) - 1
		}
		a.issuesEnsureSelectionVisible()
	case "?":
		a.Help = true
	}
}

// issuesDetailPageActive reports whether the narrow detail page is on screen;
// only there do the movement keys belong to the detail instead of the list.
func (a *App) issuesDetailPageActive() bool {
	return a.Issues != nil && a.Issues.showDetail && !a.issuesLayout().wide
}

// issuesScrollKeysActive reports whether a page key should scroll an already
// loaded detail body rather than paging the list.
func (a *App) issuesScrollKeysActive() bool {
	st := a.Issues
	if st == nil || st.detail == nil {
		return false
	}
	return st.showDetail || a.issuesLayout().wide
}

func (a *App) handleIssuesEditKey(key string) {
	st := a.Issues
	switch key {
	case "esc":
		st.editing = ""
		st.input = ""
	case "enter":
		value := strings.TrimSpace(st.input)
		switch st.editing {
		case "search":
			st.search = value
		case "label":
			st.label = value
		}
		st.editing = ""
		st.input = ""
		a.issuesResetDetail()
		a.issuesReloadList()
	case "backspace":
		if st.input != "" {
			runes := []rune(st.input)
			st.input = string(runes[:len(runes)-1])
		}
	default:
		if isPrintableKey(key) {
			st.input += key
		}
	}
}

// issuesRefresh reloads the list and, when a detail is open, its snapshot too.
func (a *App) issuesRefresh() {
	st := a.Issues
	if st == nil {
		return
	}
	number := st.detailNumber
	a.issuesReloadList()
	if number > 0 {
		st.detailReload = number
	}
}

// issuesResetDetail drops the loaded snapshot; used when the result set the
// snapshot belonged to is replaced.
func (a *App) issuesResetDetail() {
	st := a.Issues
	if st == nil {
		return
	}
	st.detail = nil
	st.detailNumber = 0
	st.detailErr = ""
	st.detailLoading = false
	st.detailScroll = 0
	st.showDetail = false
	st.detailReload = 0
	st.renderKey = ""
	st.renderLines = nil
}

func (a *App) handleIssuesMouse(x, y, bstate int) {
	st := a.Issues
	if st == nil {
		return
	}
	layout := a.issuesLayout()
	if delta := mouseWheelDelta(bstate); delta != 0 {
		if layout.wide && x >= layout.body.X+layout.listWidth+1 {
			a.issuesScrollDetail(delta * mouseScrollStep)
			return
		}
		a.issuesMoveSelection(delta * mouseScrollStep)
		return
	}
	if !mouseLeftClicked(bstate) {
		return
	}
	if st.editing != "" {
		return
	}
	index, ok := layout.listIndexAt(x, y, len(st.items))
	if !ok {
		return
	}
	if st.showDetail && !layout.wide {
		return
	}
	if index != st.selected {
		st.selected = index
		if st.detailNumber > 0 && st.detailNumber != st.items[index].Number {
			st.detail = nil
			st.detailNumber = 0
			st.detailErr = ""
			st.detailLoading = false
			st.detailScroll = 0
			st.detailReload = 0
		}
	}
	if mouseLeftDoubleClicked(bstate) {
		st.showDetail = true
		a.issuesLoadDetail(st.items[index].Number)
	}
}
