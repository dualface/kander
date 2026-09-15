package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/board"
)

type boardReadResult struct {
	seq     uint64
	payload BoardPayload
	err     error
}

type detailReadResult struct {
	seq  uint64
	id   string
	task Task
	err  error
}

func (a *App) writeBlocksBoardRead() bool {
	return a.TaskActions != nil && a.TaskActions.running
}

func (a *App) invalidateBoardReads() {
	if a.boardReadCancel != nil {
		a.boardReadCancel()
		a.boardReadCancel = nil
	}
	a.boardInFlightSeq = 0
}

func (a *App) invalidateSummaries(ids ...string) {
	if a.summaries != nil {
		a.summaries.Invalidate(ids...)
	}
}

func (a *App) syncSummaryStrongInterval() {
	if a.summaries != nil {
		a.summaries.SetStrongEvery(board.StrongInterval(a.RefreshSecs))
	}
}

func (a *App) invalidateDetailReads() {
	if a.detailReadCancel != nil {
		a.detailReadCancel()
		a.detailReadCancel = nil
	}
	a.detailInFlightSeq = 0
}

func (a *App) cancelOwnedReads() {
	a.invalidateBoardReads()
	a.invalidateDetailReads()
	a.boardReadQueued = false
	a.detailQueuedID = ""
	a.boardReadSeq++
	a.detailReadSeq++
}

// requestBoardRefresh schedules a board snapshot. fresh=true coalesces onto an
// in-flight read so the later result is used; periodic ticks do not.
func (a *App) requestBoardRefresh(fresh bool) {
	if a.writeBlocksBoardRead() {
		if fresh {
			a.boardReadQueued = true
		}
		return
	}
	if a.boardInFlightSeq != 0 {
		if fresh {
			a.boardReadQueued = true
		}
		return
	}
	a.boardReadQueued = true
}

func (a *App) requestDetailRead(id string) {
	if id == "" {
		return
	}
	if a.detailInFlightSeq != 0 {
		if a.detailQueuedID != id {
			a.invalidateDetailReads()
		}
		a.detailQueuedID = id
		return
	}
	a.detailQueuedID = id
}

func (a *App) takeQueuedReads() tea.Cmd {
	var cmds []tea.Cmd
	if cmd := a.takeBoardRead(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := a.takeDetailRead(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

func (a *App) takeBoardRead() tea.Cmd {
	if a.writeBlocksBoardRead() || a.boardInFlightSeq != 0 || !a.boardReadQueued {
		return nil
	}
	a.boardReadQueued = false
	a.boardReadSeq++
	seq := a.boardReadSeq
	a.boardInFlightSeq = seq
	ctx, cancel := context.WithCancel(context.Background())
	a.boardReadCancel = cancel
	get, getCtx := a.GetBoard, a.GetBoardCtx
	return func() tea.Msg {
		payload, err := runBoardRead(ctx, get, getCtx)
		return workMsg{payload: boardReadResult{seq: seq, payload: payload, err: err}}
	}
}

func runBoardRead(ctx context.Context, get func() (BoardPayload, error), getCtx func(context.Context) (BoardPayload, error)) (BoardPayload, error) {
	if getCtx != nil {
		return getCtx(ctx)
	}
	if get != nil {
		return get()
	}
	return BoardPayload{}, errors.New("board reader is not bound")
}

func (a *App) takeDetailRead() tea.Cmd {
	id := a.detailQueuedID
	if id == "" || a.detailInFlightSeq != 0 {
		return nil
	}
	a.detailQueuedID = ""
	a.detailReadSeq++
	seq := a.detailReadSeq
	a.detailInFlightSeq = seq
	ctx, cancel := context.WithCancel(context.Background())
	a.detailReadCancel = cancel
	get, getCtx := a.GetTask, a.GetTaskCtx
	return func() tea.Msg {
		task, err := runTaskRead(ctx, id, get, getCtx)
		return workMsg{payload: detailReadResult{seq: seq, id: id, task: task, err: err}}
	}
}

func runTaskRead(ctx context.Context, id string, get func(string) (Task, error), getCtx func(context.Context, string) (Task, error)) (Task, error) {
	if getCtx != nil {
		return getCtx(ctx, id)
	}
	if get != nil {
		return get(id)
	}
	return Task{}, errors.New("task reader is not bound")
}

func (a *App) applyBoardRead(result boardReadResult) {
	if result.seq != a.boardInFlightSeq {
		return
	}
	a.boardInFlightSeq = 0
	a.boardReadCancel = nil
	if a.boardReadQueued {
		return
	}
	a.applyBoardPayload(result.payload, result.err)
}

func (a *App) applyBoardPayload(payload BoardPayload, err error) bool {
	previous := a.Model.RefreshError
	a.LastRefresh = a.Now()
	if err != nil {
		a.Model.RefreshError = err.Error()
		return a.Model.RefreshError != previous
	}
	changed := a.Model.SetBoard(payload)
	a.Model.RefreshError = ""
	a.showJournalWarnings(payload.Warnings)
	if a.Detail != nil && a.Detail.TaskID != "" {
		a.requestDetailRead(a.Detail.TaskID)
	}
	return changed
}

func (a *App) applyDetailRead(result detailReadResult) {
	if result.seq != a.detailInFlightSeq {
		return
	}
	a.detailInFlightSeq = 0
	a.detailReadCancel = nil
	if a.detailQueuedID != "" {
		return
	}
	if a.Detail == nil || a.Detail.TaskID != result.id {
		return
	}
	a.applyDetailTask(result.task, result.err)
}

func (a *App) applyDetailTask(next Task, err error) bool {
	previous := a.Model.DetailError
	if err != nil {
		a.Model.DetailError = err.Error()
		return a.Model.DetailError != previous
	}
	a.Model.DetailError = ""
	changed := a.Detail == nil ||
		a.Detail.Document != next.Document ||
		a.Detail.Title != next.Title ||
		a.Detail.Time != next.Time ||
		a.Detail.State != next.State ||
		a.Detail.Assignee != next.Assignee ||
		a.Detail.Kind != next.Kind ||
		a.Detail.TaskGroup != next.TaskGroup ||
		a.Detail.Type != next.Type
	a.Detail = &next
	a.showJournalWarnings(next.Warnings)
	a.clampDetailCursor()
	matches := a.detailMatches(nil)
	if len(matches) > 0 && a.DetailMatchIndex > len(matches)-1 {
		a.DetailMatchIndex = len(matches) - 1
	} else if len(matches) == 0 {
		a.DetailMatchIndex = 0
	}
	return changed || previous != ""
}

func (a *App) refreshBoard() bool {
	if a.writeBlocksBoardRead() {
		return false
	}
	if a.GetBoard == nil {
		return false
	}
	payload, err := a.GetBoard()
	return a.applyBoardPayload(payload, err)
}

func (a *App) refreshOpenDetail() bool {
	if a.Detail == nil || a.GetTask == nil {
		return false
	}
	taskID := a.Detail.TaskID
	if taskID == "" {
		return false
	}
	next, err := a.GetTask(taskID)
	return a.applyDetailTask(next, err)
}

func (a *App) openDetail() {
	selected := a.Model.SelectedTask()
	if selected == nil {
		return
	}
	task := *selected
	a.Detail = &task
	a.DetailScroll = 0
	a.DetailCursor = [2]int{0, 0}
	a.resetDetailSearch()
	a.resetMouseSelection()
	a.Model.DetailError = ""
	a.requestDetailRead(task.TaskID)
}

func (a *App) closeDetail() {
	a.invalidateDetailReads()
	a.detailQueuedID = ""
	a.detailReadSeq++
	a.Detail = nil
	a.DetailScroll = 0
	a.resetDetailSearch()
	a.resetMouseSelection()
}
