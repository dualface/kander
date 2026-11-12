package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/fs"
)

// eventRetention bounds the in-memory window a cursor can resume from. An
// older cursor reads as reset, and the client re-reads the task list.
const eventRetention = 4096

// eventLog is a bounded per-project change stream. Restart or append failure
// changes its epoch, so an observer always refreshes after an uncertain gap.
type eventLog struct {
	mu      sync.Mutex
	root    string
	rel     string
	epoch   string
	next    uint64
	window  []api.Event
	changed chan struct{}
}

func openEventLog(root string) (*eventLog, error) {
	if err := ensureDir(root, "control"); err != nil {
		return nil, err
	}
	l := &eventLog{root: root, rel: filepath.Join("control", "events.jsonl"), changed: make(chan struct{})}
	// Events are invalidation hints, not the board's durable source of truth.
	// Every service restart resets cursors, including a crash after a domain
	// commit but before its event append.
	if err := l.rotate(); err != nil {
		return nil, err
	}
	if err := fs.WriteTextAtomic(root, filepath.Join(root, l.rel), "", true); err != nil {
		return nil, err
	}

	return l, nil
}

func (l *eventLog) push(e api.Event) {
	l.window = append(l.window, e)
	if len(l.window) > eventRetention {
		l.window = append([]api.Event(nil), l.window[len(l.window)-eventRetention:]...)
	}
}

// append records events durably, then wakes long-polling readers.
func (l *eventLog) append(events ...api.Event) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer func() {
		if err != nil {
			// Any observer that could have missed a committed write must re-read.
			_ = l.rotate()
			close(l.changed)
			l.changed = make(chan struct{})
		}
	}()
	file, err := fs.OpenAppendFile(l.root, filepath.Join(l.root, l.rel))
	if err != nil {
		return err
	}
	var b strings.Builder
	for i := range events {
		events[i].Seq = l.next
		if events[i].At.IsZero() {
			events[i].At = time.Now().UTC()
		}
		l.next++
		line, err := json.Marshal(events[i])
		if err != nil {
			file.Close()
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if _, err := file.WriteString(b.String()); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	for _, e := range events {
		l.push(e)
	}
	if l.next%eventRetention == 0 {
		var compact strings.Builder
		for _, event := range l.window {
			data, _ := json.Marshal(event)
			compact.Write(data)
			compact.WriteByte('\n')
		}
		if err := fs.WriteTextAtomic(l.root, filepath.Join(l.root, l.rel), compact.String(), true); err != nil {
			return err
		}
	}
	close(l.changed)
	l.changed = make(chan struct{})
	return nil
}

// cursor returns the position after the last recorded event.
func (l *eventLog) cursor() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.epoch + ":" + strconv.FormatUint(l.next-1, 10)
}

// read returns the events after cursor, waiting up to wait for at least one.
func (l *eventLog) read(ctx context.Context, cursor string, wait time.Duration) api.EventPage {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		l.mu.Lock()
		page, ready := l.pageLocked(cursor)
		changed := l.changed
		l.mu.Unlock()
		if ready || wait <= 0 {
			return page
		}
		select {
		case <-changed:
		case <-deadline.C:
			return page
		case <-ctx.Done():
			return page
		}
	}
}

func (l *eventLog) pageLocked(cursor string) (api.EventPage, bool) {
	current := l.epoch + ":" + strconv.FormatUint(l.next-1, 10)
	epoch, raw, ok := strings.Cut(cursor, ":")
	after, err := strconv.ParseUint(raw, 10, 64)
	if !ok || err != nil || epoch != l.epoch || after >= l.next {
		return api.EventPage{Cursor: current, Reset: true, Events: []api.Event{}}, true
	}
	if after == l.next-1 {
		return api.EventPage{Cursor: current, Events: []api.Event{}}, false
	}
	if len(l.window) == 0 || l.window[0].Seq > after+1 {
		return api.EventPage{Cursor: current, Reset: true, Events: []api.Event{}}, true
	}
	out := []api.Event{}
	for _, e := range l.window {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return api.EventPage{Cursor: current, Events: out}, true
}

// rotate runs only during startup or under mu after an append failure.
func (l *eventLog) rotate() error {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return err
	}
	l.epoch = hex.EncodeToString(entropy[:])
	l.next = 1
	l.window = nil
	return nil
}
