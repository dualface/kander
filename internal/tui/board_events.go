package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/api"
)

type boardEventWatch struct {
	poll            func(context.Context, string) (api.EventPage, error)
	cancel          context.CancelFunc
	seq, inFlight   uint64
	retryAt         time.Time
	waitingSnapshot bool
}
type boardEventResult struct {
	seq  uint64
	page api.EventPage
	err  error
}

func (a *App) cancelBoardEvents() {
	if a.boardEvents.cancel != nil {
		a.boardEvents.cancel()
		a.boardEvents.cancel = nil
	}
	a.boardEvents.seq++
	a.boardEvents.inFlight = 0
}

// takeBoardEvents starts at most one bounded, cancellable read. Events are
// invalidation hints; only a subsequent snapshot changes the displayed board.
func (a *App) takeBoardEvents() tea.Cmd {
	watch := &a.boardEvents
	if watch.poll == nil || !a.Running || watch.inFlight != 0 || watch.waitingSnapshot || a.Now().Before(watch.retryAt) {
		return nil
	}
	if a.Model.Stale {
		watch.retryAt = a.Now().Add(2 * time.Second)
		a.requestBoardRefresh(false)
		return nil
	}
	if a.Model.Cursor == "" {
		watch.waitingSnapshot = true
		a.requestBoardRefresh(false)
		return nil
	}
	watch.seq++
	seq := watch.seq
	watch.inFlight = seq
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	watch.cancel = cancel
	poll, cursor := watch.poll, a.Model.Cursor
	return func() tea.Msg {
		defer cancel()
		page, err := poll(ctx, cursor)
		return workMsg{payload: boardEventResult{seq: seq, page: page, err: err}}
	}
}
func (a *App) applyBoardEvents(result boardEventResult) {
	watch := &a.boardEvents
	if result.seq != watch.inFlight || !a.Running {
		return
	}
	watch.inFlight = 0
	watch.cancel = nil
	if result.err != nil {
		watch.retryAt = a.Now().Add(2 * time.Second)
		a.requestBoardRefresh(false)
		return
	}
	if result.page.Reset || len(result.page.Events) > 0 || result.page.Cursor == "" {
		// Never adopt a reset page cursor before obtaining the complete snapshot.
		watch.waitingSnapshot = true
		a.requestBoardRefresh(true)
	}
}
func (a *App) boardEventsSnapshot(payload BoardPayload, err error) {
	watch := &a.boardEvents
	watch.waitingSnapshot = false
	if err != nil || payload.Stale || payload.Cursor == "" {
		watch.retryAt = a.Now().Add(2 * time.Second)
	}
}
