package remote

import (
	"context"
	"io"
	"slices"
	"sync"

	"github.com/dualface/kander/internal/api"
)

// MoveObserver prepares a CLI-only notice from service data. Failures must not
// change the domain result. The TUI does not invoke these presentation hooks.
type MoveObserver func(context.Context, *Session, api.Task, string) func(io.Writer, api.Task)

var moveNotices struct {
	sync.Mutex
	observers []MoveObserver
}

func RegisterMoveObserver(observer MoveObserver) {
	moveNotices.Lock()
	defer moveNotices.Unlock()
	moveNotices.observers = append(moveNotices.observers, observer)
}
func observeServiceMove(ctx context.Context, s *Session, task api.Task, target string) []func(io.Writer, api.Task) {
	moveNotices.Lock()
	observers := slices.Clone(moveNotices.observers)
	moveNotices.Unlock()
	var notices []func(io.Writer, api.Task)
	for _, observer := range observers {
		if notice := observer(ctx, s, task, target); notice != nil {
			notices = append(notices, notice)
		}
	}
	return notices
}
