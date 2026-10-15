package board

import (
	"io"
	"sync"
)

// MoveObserver lets a feature package react to a CLI move without board
// importing it. It runs before the move with the card's current entry and the
// requested state, and may return a notice that runs after a successful,
// non-replayed move. A notice writes advice only: it never changes the move
// result, its standard output, or its exit code.
type MoveObserver func(root string, entry Entry, target string) func(w io.Writer, moved Entry)

var (
	moveObserversMu sync.Mutex
	moveObservers   []MoveObserver
)

// RegisterMoveObserver adds an observer; packages call it from init.
func RegisterMoveObserver(observer MoveObserver) {
	moveObserversMu.Lock()
	defer moveObserversMu.Unlock()
	moveObservers = append(moveObservers, observer)
}

func observeMove(root string, entry Entry, target string) []func(io.Writer, Entry) {
	moveObserversMu.Lock()
	observers := append([]MoveObserver{}, moveObservers...)
	moveObserversMu.Unlock()
	var notices []func(io.Writer, Entry)
	for _, observer := range observers {
		if notice := observer(root, entry, target); notice != nil {
			notices = append(notices, notice)
		}
	}
	return notices
}
