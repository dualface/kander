package liveness

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

// singleEvents runs a subscription without Follow to its end and returns every
// event written. The 10 second context turns a subscription that never exits
// into a test failure instead of a hang.
func singleEvents(t *testing.T, root string, opts subscribeOptions, onEvent func(groupEvent) error) ([]groupEvent, error) {
	t.Helper()
	opts.Follow = false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	writer := &clockEvents{onEvent: func(event groupEvent) error {
		if onEvent == nil {
			return nil
		}
		return onEvent(event)
	}}
	err := SubscribeContext(ctx, root, opts, writer)
	return writer.events, err
}

func eventNames(events []groupEvent) []string {
	names := make([]string, len(events))
	for i, event := range events {
		names[i] = event.Event
	}
	return names
}

func requireEventNames(t *testing.T, events []groupEvent, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("single-event subscription ended with %v after %v", err, eventNames(events))
	}
	if got := eventNames(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("events=%v want %v", got, want)
	}
}

func TestParseSubscribeFollow(t *testing.T) {
	const group, member = "20260907-clock-group", "20260907-clock-task"
	cases := []struct {
		name   string
		args   []string
		follow bool
	}{
		{"absent", []string{group, member}, false},
		{"alone", []string{"--follow", group, member}, true},
		{"after members", []string{group, member, "--follow"}, true},
		{"with watch", []string{"--follow", "--watch", member}, true},
		{"watch before follow", []string{"--watch", member, "--follow"}, true},
		{"with group members", []string{"--follow", group, member, "--watch", "other-task"}, true},
		{"with heartbeat and refresh", []string{"--heartbeat", "30", "--follow", "--refresh", "2", group, member}, true},
		{"watch only without follow", []string{"--heartbeat", "600", "--watch", member}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			opts, errText := parseSubscribeArgs(test.args)
			if errText != "" || opts.Follow != test.follow {
				t.Fatalf("follow=%v err=%q want follow=%v", opts.Follow, errText, test.follow)
			}
		})
	}
	opts, _ := parseSubscribeArgs([]string{"--follow", "--heartbeat", "30", "--refresh", "2", group, member, "--watch", "other-task"})
	if opts.Heartbeat != 30 || opts.Refresh != 2 || !reflect.DeepEqual(opts.Watch, []string{"other-task"}) || opts.Group != group || !reflect.DeepEqual(opts.Members, []string{member}) {
		t.Fatalf("--follow disturbed other options: %+v", opts)
	}
}

func TestRunSubscribeRejectsFollowValueAndUntilEvent(t *testing.T) {
	tempBoard(t)
	for _, arg := range []string{"--follow=1", "--follow=", "--until-event"} {
		t.Run(arg, func(t *testing.T) {
			if _, errText := parseSubscribeArgs([]string{arg, "--watch", "20260907-clock-task"}); errText == "" || errText == "usage" {
				t.Fatalf("parse accepted %s: %q", arg, errText)
			}
			if code := RunSubscribe([]string{arg, "--watch", "20260907-clock-task"}); code != 2 {
				t.Fatalf("exit code=%d want 2", code)
			}
		})
	}
}

func TestSingleEventExitsAfterStateChange(t *testing.T) {
	root, opts, id := factsMember(t)
	opts.Refresh, opts.Heartbeat = .25, 3600
	baseline := runtime.NumGoroutine()
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event == "snapshot" {
			_, err := board.MoveEntry(currentEntry(t, root, id), root, "working")
			return err
		}
		return nil
	})
	requireEventNames(t, events, err, "snapshot", "state-change")
	if events[1].Tasks[id] != "working" || len(events[1].Changed) != 1 {
		t.Fatalf("state-change: %+v", events[1])
	}
	// The probe task and the output worker are joined before the call returns.
	for deadline := time.Now().Add(300 * time.Millisecond); runtime.NumGoroutine() > baseline && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline {
		t.Fatalf("goroutines before=%d after=%d", baseline, n)
	}
}

func TestSingleEventExitsAfterEachWakeEvent(t *testing.T) {
	const group = "20260908-wake-group"
	cases := []struct {
		name   string
		setup  func(t *testing.T, root string, opts *subscribeOptions)
		mutate func(t *testing.T, root string, opts subscribeOptions, id string)
		want   []string
	}{
		{
			name: "task-update",
			mutate: func(t *testing.T, root string, _ subscribeOptions, id string) {
				if err := factsUpdate(root, id, func(s string) string { return s + "\nChanged.\n" }); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{"snapshot", "task-update"},
		},
		{
			name: "membership-change",
			setup: func(t *testing.T, root string, opts *subscribeOptions) {
				addFactsExternal(t, root, "wake-first", group)
				opts.Watch = []string{group}
			},
			mutate: func(t *testing.T, root string, _ subscribeOptions, _ string) {
				addFactsExternal(t, root, "wake-added", group)
			},
			want: []string{"snapshot", "membership-change"},
		},
		{
			name: "member-blocked",
			mutate: func(t *testing.T, root string, opts subscribeOptions, id string) {
				snapshot, err := board.ReadSnapshot(root, id)
				if err != nil {
					t.Fatal(err)
				}
				claim, err := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: opts.Group, Owner: "coordinator", Token: "session-one", Basis: "fixture", Members: []string{id}})
				if err != nil {
					t.Fatal(err)
				}
				stale := board.CoordinatorReconcile{GroupID: claim.GroupID, ExpectedRevision: claim.Revision, Authority: claim.Authority,
					Members: map[string]board.CoordinatorObservation{id: {Revision: snapshot.Revision, DispatchID: "stale-round", Epoch: 1}}}
				// The reconcile reports the stale dispatch as an error, but the
				// checkpoint it returns records the block the subscription shows.
				checkpoint, _ := board.ReconcileCoordinator(context.Background(), root, stale, nil)
				if checkpoint.Members[id].Blocked == nil {
					t.Fatalf("no block: %+v", checkpoint.Members[id])
				}
			},
			want: []string{"snapshot", "member-blocked"},
		},
		{
			name: "heartbeat",
			setup: func(t *testing.T, root string, opts *subscribeOptions) {
				opts.Refresh, opts.Heartbeat = 3600, .2
			},
			want: []string{"snapshot", "heartbeat"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, opts, id := factsMember(t)
			opts.Refresh, opts.Heartbeat = .25, 3600
			if test.setup != nil {
				test.setup(t, root, &opts)
			}
			events, err := singleEvents(t, root, opts, func(event groupEvent) error {
				if event.Event == "snapshot" && test.mutate != nil {
					test.mutate(t, root, opts, id)
				}
				return nil
			})
			requireEventNames(t, events, err, test.want...)
		})
	}
}

func TestSingleEventWritesWholeScanBeforeExit(t *testing.T) {
	const group = "20260908-whole-scan-group"
	root, opts, id := factsMember(t)
	addFactsExternal(t, root, "whole-first", group)
	opts.Watch = []string{group}
	opts.Refresh, opts.Heartbeat = .25, 3600
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event != "snapshot" {
			return nil
		}
		addFactsExternal(t, root, "whole-added", group)
		_, err := board.MoveEntry(currentEntry(t, root, id), root, "working")
		return err
	})
	requireEventNames(t, events, err, "snapshot", "membership-change", "state-change")
}

// prepareDispatchWithID prepares a sync dispatch whose deadline lies `deadline`
// from now; a negative deadline value is not accepted by the board, so callers
// that need an overdue dispatch wait for it to pass.
func prepareDispatchWithID(t *testing.T, root, taskID, dispatchID string, deadline time.Duration) board.Dispatch {
	t.Helper()
	now := time.Now().UTC()
	dispatch, err := board.PrepareDispatch(root, board.DispatchInput{ID: dispatchID, TaskID: taskID, Kind: "sync",
		Message: "private dispatch payload", Base: strings.Repeat("a", 40), CreatedAt: now.Add(-time.Minute), ConfirmBy: now.Add(deadline)})
	if err != nil {
		t.Fatal(err)
	}
	return dispatch
}

func TestSingleEventIgnoresDispatchOverdueAtSnapshot(t *testing.T) {
	root, opts, id := factsMember(t)
	prepareDispatchWithID(t, root, id, "overdue-round", 60*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	opts.Refresh, opts.Heartbeat = 3600, 1
	events, err := singleEvents(t, root, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := eventNames(events)
	if len(names) < 3 || names[0] != "snapshot" || names[len(names)-1] != "heartbeat" {
		t.Fatalf("events=%v", names)
	}
	attention := 0
	for _, name := range names[1 : len(names)-1] {
		if name != "dispatch-attention" {
			t.Fatalf("events=%v", names)
		}
		attention++
	}
	// One at start-up and one after the deadline probe finished.
	if attention < 2 {
		t.Fatalf("dispatch-attention written %d times: %v", attention, names)
	}
	if !events[0].Dispatches[id].ConfirmationOverdue {
		t.Fatalf("snapshot did not carry an overdue dispatch: %+v", events[0].Dispatches[id])
	}
}

func TestSingleEventWakesForNewDispatchOnSameCard(t *testing.T) {
	root, opts, id := factsMember(t)
	first := prepareDispatchWithID(t, root, id, "first-round", time.Minute)
	// Both dispatches count as overdue at the fixed observation time.
	fixed := time.Now().Add(10 * time.Minute)
	oldNow, oldRead := nowFn, readSubscriptionFacts
	nowFn = func() time.Time { return fixed }
	// Hold every scan after the snapshot until the dispatch swap is complete, so
	// that no scan sees the card between the two board writes.
	swapped := make(chan struct{})
	var reads atomic.Int32
	readSubscriptionFacts = func(s *subscription, ctx context.Context, root string) (subscriptionFacts, error) {
		if reads.Add(1) > 1 {
			select {
			case <-swapped:
			case <-ctx.Done():
			}
		}
		return oldRead(s, ctx, root)
	}
	t.Cleanup(func() { nowFn, readSubscriptionFacts = oldNow, oldRead })
	opts.Refresh, opts.Heartbeat = 3600, 3600
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event != "snapshot" {
			return nil
		}
		defer close(swapped)
		if err := board.EndDispatch(root, id, first.Input.ID, first.Revision, board.DispatchCancelled, "replaced in test"); err != nil {
			return err
		}
		prepareDispatchWithID(t, root, id, "second-round", time.Minute)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	names := eventNames(events)
	last := events[len(events)-1]
	if len(names) < 3 || names[0] != "snapshot" || names[len(names)-2] != "task-update" || last.Event != "dispatch-attention" {
		t.Fatalf("events=%v", names)
	}
	if events[0].Dispatches[id].ID != "first-round" || !events[0].Dispatches[id].ConfirmationOverdue {
		t.Fatalf("snapshot dispatch: %+v", events[0].Dispatches[id])
	}
	if last.Dispatches[id].ID != "second-round" || !last.Dispatches[id].ConfirmationOverdue {
		t.Fatalf("last dispatch: %+v", last.Dispatches[id])
	}
	for _, event := range events[1 : len(events)-2] {
		if event.Event != "dispatch-attention" || event.Dispatches[id].ID != "first-round" {
			t.Fatalf("event before the swap woke the subscription: %v", names)
		}
	}
}

func TestSingleEventWakesForDispatchOverdueDuringSubscription(t *testing.T) {
	root, opts, id := factsMember(t)
	prepareDispatchWithID(t, root, id, "late-round", 300*time.Millisecond)
	opts.Refresh, opts.Heartbeat = 3600, 3600
	events, err := singleEvents(t, root, opts, nil)
	requireEventNames(t, events, err, "snapshot", "dispatch-attention")
	if events[0].Dispatches[id].ConfirmationOverdue || !events[1].Dispatches[id].ConfirmationOverdue {
		t.Fatalf("overdue flags: %+v / %+v", events[0].Dispatches[id], events[1].Dispatches[id])
	}
}

// Terminal events behave as under --follow: the same events, the same error.
func TestSingleEventTerminalEventsMatchFollow(t *testing.T) {
	cases := map[string]func(t *testing.T, root string, opts *subscribeOptions, id string) func(groupEvent) error{
		"read-error": func(t *testing.T, root string, _ *subscribeOptions, id string) func(groupEvent) error {
			prepareDispatchWithID(t, root, id, "broken-round", time.Minute)
			if err := os.Remove(filepath.Join(currentEntry(t, root, id).Path, "dispatches", "broken-round", "state.json")); err != nil {
				t.Fatal(err)
			}
			return nil
		},
		"membership-unknown": func(t *testing.T, root string, opts *subscribeOptions, _ string) func(groupEvent) error {
			const group = "20260908-terminal-group"
			addFactsExternal(t, root, "terminal-readable", group)
			bad := addFactsExternal(t, root, "terminal-unreadable", group)
			opts.Watch = []string{group}
			return func(event groupEvent) error {
				if event.Event == "snapshot" {
					return os.WriteFile(currentEntry(t, root, bad).Document, []byte{0xff}, 0600)
				}
				return nil
			}
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			run := func(follow bool) ([]groupEvent, string) {
				root, opts, id := factsMember(t)
				opts.Refresh, opts.Heartbeat = .25, 3600
				onEvent := arrange(t, root, &opts, id)
				opts.Follow = follow
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				writer := &clockEvents{onEvent: func(event groupEvent) error {
					if onEvent == nil {
						return nil
					}
					return onEvent(event)
				}}
				err := SubscribeContext(ctx, root, opts, writer)
				if err == nil {
					t.Fatalf("follow=%v: terminal event ended with status 0", follow)
				}
				// Each run has its own temporary board.
				return writer.events, strings.ReplaceAll(err.Error(), root, "<root>")
			}
			followed, followErr := run(true)
			single, singleErr := run(false)
			if followErr != singleErr {
				t.Fatalf("follow err=%q single err=%q", followErr, singleErr)
			}
			if !reflect.DeepEqual(eventNames(followed), eventNames(single)) {
				t.Fatalf("follow=%v single=%v", eventNames(followed), eventNames(single))
			}
			last := single[len(single)-1]
			if last.ReadStatus != "invalid" || last.MembershipComplete {
				t.Fatalf("terminal event: %+v", last)
			}
		})
	}
}

func TestFollowSurvivesAllCardsDone(t *testing.T) {
	root, opts, id := factsMember(t)
	if err := factsUpdate(root, id, func(text string) string {
		body, _ := board.SectionBody(text, board.SectionSummary)
		return strings.Replace(text, "## SUMMARY\n\n"+body, "## SUMMARY\n\nLifecycle fixture completed.", 1)
	}); err != nil {
		t.Fatal(err)
	}
	closeSubscriptionFixture(t, root, id)
	dispatch := prepareSubscriptionDispatch(t, root, id, "wrap-up", time.Minute)
	opts.Refresh, opts.Heartbeat = .05, 3600
	events := make(runtimeEvents, 32)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "snapshot" })
	completeSubscriptionDispatch(t, root, id, dispatch, "done")
	nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "state-change" && e.Tasks[id] == "done" })
	select {
	case err := <-done:
		t.Fatalf("follow subscription ended after all cards were done: %v", err)
	case <-time.After(400 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("cancel=%v", err)
	}
}
