package server

import (
	"context"

	"github.com/dualface/kander/v2/api"
)

const subscriptionBuffer = 64

// eventSink keeps the fan-out independent of the Go envelope chosen by the
// subscriber. The log mutex serializes queue writes, resets, and closure.
type eventSink interface {
	publish(*eventLog, []api.Event)
	reset(string)
	close()
}

type eventSubscriber[T any] struct {
	ch           chan T
	notification func(api.Event, string, bool) T
}

// Subscribe watches the same events as the project's HTTP event stream.
// notification maps each event or reset into the caller's Go envelope.
// The mapping runs under the log lock and must not block or call the server.
func Subscribe[T any](ctx context.Context, s *Server, projectID, cursor string, notification func(api.Event, string, bool) T) (<-chan T, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.shutdown.Err(); err != nil {
		return nil, err
	}
	p, err := s.project(projectID)
	if err != nil {
		return nil, err
	}
	return subscribeEvents(p.events, ctx, s.shutdown, cursor, notification), nil
}

func subscribeEvents[T any](l *eventLog, ctx, shutdown context.Context, cursor string, notification func(api.Event, string, bool) T) <-chan T {
	sub := &eventSubscriber[T]{ch: make(chan T, subscriptionBuffer), notification: notification}
	l.mu.Lock()
	if l.subs == nil {
		l.subs = make(map[eventSink]struct{})
	}
	l.subs[sub] = struct{}{}
	page, _ := l.pageLocked(cursor)
	if page.Reset {
		sub.reset(page.Cursor)
	} else {
		sub.publish(l, page.Events)
	}
	l.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stopShutdown := context.AfterFunc(shutdown, cancel)
	context.AfterFunc(ctx, func() {
		stopShutdown()
		l.mu.Lock()
		delete(l.subs, sub)
		sub.close()
		l.mu.Unlock()
	})
	return sub.ch
}

func (s *eventSubscriber[T]) publish(l *eventLog, events []api.Event) {
	// Only the consumer can change the length while the lock is held, and
	// it can only shrink. None of the following sends can block a writer.
	if len(events) > cap(s.ch)-len(s.ch) {
		s.reset(l.cursorLocked(l.next - 1))
		return
	}
	for _, event := range events {
		s.ch <- s.notification(event, l.cursorLocked(event.Seq), false)
	}
}

func (l *eventLog) resetSubscribersLocked() {
	cursor := l.cursorLocked(l.next - 1)
	for sub := range l.subs {
		sub.reset(cursor)
	}
}

func (s *eventSubscriber[T]) reset(cursor string) {
	for {
		select {
		case <-s.ch:
		default:
			s.ch <- s.notification(api.Event{}, cursor, true)
			return
		}
	}
}

func (s *eventSubscriber[T]) close() { close(s.ch) }
