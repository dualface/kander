package server

import (
	"bufio"
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

// eventLog is the per-project append-only change log. The epoch changes when
// the log is recreated, so a cursor from a lost log never resumes silently.
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
	l := &eventLog{root: root, rel: filepath.Join("control", "events.jsonl"), changed: make(chan struct{})}
	var meta struct {
		Epoch string `json:"epoch"`
	}
	found, err := readJSON(root, filepath.Join("control", "events-epoch.json"), &meta)
	if err != nil {
		return nil, err
	}
	if !found || meta.Epoch == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		meta.Epoch = hex.EncodeToString(b)
		if err := writeJSON(root, filepath.Join("control", "events-epoch.json"), meta); err != nil {
			return nil, err
		}
	}
	l.epoch = meta.Epoch
	l.next = 1
	data, ok, err := fs.ReadRegularFileIfExists(root, filepath.Join(root, l.rel))
	if err != nil {
		return nil, err
	}
	if ok {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			var e api.Event
			// A torn last line from a crash is skipped; the sequence resumes
			// after the last complete event.
			if json.Unmarshal(scanner.Bytes(), &e) != nil {
				continue
			}
			l.push(e)
			l.next = e.Seq + 1
		}
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
func (l *eventLog) append(events ...api.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
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
