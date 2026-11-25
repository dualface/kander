package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/api"
)

func TestEventAppendFailureInvalidatesTheObservableCursor(t *testing.T) {
	root := t.TempDir()
	log, err := openEventLog(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.append(api.Event{Kind: api.EventTaskChanged, TaskID: "20261112-event-task"}); err != nil {
		t.Fatal(err)
	}
	cursor := log.cursor()
	subscriber := subscribeForTest(log, t.Context(), cursor)
	log.rel = filepath.Join("missing-directory", "events.jsonl")
	if err := log.append(api.Event{Kind: api.EventTaskChanged, TaskID: "20261112-event-task"}); err == nil {
		t.Fatal("injected persistence failure did not occur")
	}
	page := log.read(t.Context(), cursor, 0)
	if !page.Reset || page.Cursor == cursor {
		t.Fatalf("a missed commit was invisible: %+v", page)
	}
	if got := receiveNotification(t, subscriber); !got.Reset || got.Cursor != page.Cursor {
		t.Fatalf("subscriber = %+v, long poll = %+v", got, page)
	}
}
func TestEventStreamRestartResetsEvenWithoutAnAppendedEvent(t *testing.T) {
	root := t.TempDir()
	log, err := openEventLog(root)
	if err != nil {
		t.Fatal(err)
	}
	cursor := log.cursor()
	// A card transaction may commit here and the process may die before append.
	restarted, err := openEventLog(root)
	if err != nil {
		t.Fatal(err)
	}
	page := restarted.read(t.Context(), cursor, 0)
	if !page.Reset {
		t.Fatal("restart silently resumed across an unobservable commit")
	}
}

func TestEventSubscribersReplayAndResetWithLongPoll(t *testing.T) {
	log, err := openEventLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cursor := log.cursor()
	if err := log.append(api.Event{Kind: api.EventTaskChanged}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	replay := subscribeForTest(log, ctx, cursor)
	page := log.read(t.Context(), cursor, 0)
	if got := receiveNotification(t, replay); got.Reset || got.Cursor != page.Cursor || got.Event != page.Events[0] {
		t.Fatalf("subscriber = %+v, long poll = %+v", got, page)
	}
	fresh := subscribeForTest(log, t.Context(), "")
	if got := receiveNotification(t, fresh); !got.Reset || got.Cursor != page.Cursor {
		t.Fatalf("empty cursor did not reset: %+v", got)
	}
	if err := log.reset(); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []<-chan testNotification{replay, fresh} {
		if got := receiveNotification(t, sub); !got.Reset || got.Cursor == page.Cursor || got.Cursor != log.cursor() {
			t.Fatalf("epoch change = %+v", got)
		}
	}
	cancel()
	select {
	case _, ok := <-replay:
		if ok {
			t.Fatal("unexpected queued event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close the subscription")
	}
}

func TestSlowEventSubscriberResetsWithoutBlockingOtherReaders(t *testing.T) {
	log, err := openEventLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cursor := log.cursor()
	slow := subscribeForTest(log, t.Context(), cursor)
	fast := subscribeForTest(log, t.Context(), cursor)
	appended := make(chan error, 1)
	for i := range subscriptionBuffer + 1 {
		go func() { appended <- log.append(api.Event{Kind: api.EventTaskChanged}) }()
		select {
		case err := <-appended:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("event append blocked on a subscriber")
		}
		if got := receiveNotification(t, fast); got.Reset || got.Event.Seq != uint64(i+1) {
			t.Fatalf("fast reader event %d = %+v", i, got)
		}
	}
	if got := receiveNotification(t, slow); !got.Reset || got.Event != (api.Event{}) || got.Cursor != log.cursor() {
		t.Fatalf("slow reader = %+v, want reset at %s", got, log.cursor())
	}
	if err := log.append(api.Event{Kind: api.EventTaskChanged}); err != nil {
		t.Fatal(err)
	}
	if got := receiveNotification(t, slow); got.Reset || got.Event.Seq != subscriptionBuffer+2 {
		t.Fatalf("slow reader did not resume after reset: %+v", got)
	}
}

type testNotification struct {
	Event  api.Event
	Cursor string
	Reset  bool
}

func subscribeForTest(log *eventLog, ctx context.Context, cursor string) <-chan testNotification {
	return subscribeEvents(log, ctx, ctx, cursor, func(event api.Event, cursor string, reset bool) testNotification {
		return testNotification{Event: event, Cursor: cursor, Reset: reset}
	})
}

func receiveNotification(t *testing.T, ch <-chan testNotification) testNotification {
	t.Helper()
	select {
	case notification, ok := <-ch:
		if !ok {
			t.Fatal("subscription closed before notification")
		}
		return notification
	case <-time.After(time.Second):
		t.Fatal("no subscription notification")
		return testNotification{}
	}
}
