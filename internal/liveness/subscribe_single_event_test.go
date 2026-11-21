package liveness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command/commandtest"
)

// singleEvents runs a subscription without Follow to its end and returns every
// event written. The 10 second context turns a subscription that never exits
// into a test failure instead of a hang.
func singleEvents(t *testing.T, root string, opts subscribeOptions, onEvent func(groupEvent) error) ([]groupEvent, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return singleEventsContext(ctx, root, opts, onEvent)
}

// singleEventsContext leaves the caller's context alive, for tests that check
// the state of the subscription's resources at the moment it returns.
func singleEventsContext(ctx context.Context, root string, opts subscribeOptions, onEvent func(groupEvent) error) ([]groupEvent, error) {
	opts.Follow = false
	writer := &clockEvents{onEvent: func(event groupEvent) error {
		if onEvent == nil {
			return nil
		}
		return onEvent(event)
	}}
	err := subscribeContext(ctx, root, opts, writer, nil)
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

// holdScansUntil blocks every facts read after the snapshot until ready is
// closed, so that a test can make several board writes without a scan seeing
// the board between them.
func holdScansUntil(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	oldRead := readSubscriptionFacts
	var reads atomic.Int32
	readSubscriptionFacts = func(s *subscription, ctx context.Context, root string) (subscriptionFacts, error) {
		if reads.Add(1) > 1 {
			select {
			case <-ready:
			case <-ctx.Done():
			}
		}
		return oldRead(s, ctx, root)
	}
	t.Cleanup(func() { readSubscriptionFacts = oldRead })
}

// switchClock replaces nowFn with a clock that reads `at` until the returned
// function is called and `at + jump` afterwards.
func switchClock(t *testing.T, at time.Time, jump time.Duration) (advance func()) {
	t.Helper()
	var advanced atomic.Bool
	oldNow := nowFn
	nowFn = func() time.Time {
		if advanced.Load() {
			return at.Add(jump)
		}
		return at
	}
	t.Cleanup(func() { nowFn = oldNow })
	return func() { advanced.Store(true) }
}

// attentionSession returns a subscription that can call dispatchAttention
// directly, with the facts read of the board's current state.
func attentionSession(t *testing.T, opts subscribeOptions, root string) (*subscription, func() subscriptionFacts) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	session, err := newSubscription(opts)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	session.probes = newSubscriptionProbes(ctx)
	session.heartbeat = time.Hour
	t.Cleanup(func() { session.probes.finish(); cancel() })
	return session, func() subscriptionFacts {
		t.Helper()
		facts, err := session.readContext(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		return facts
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
		{"explicit true", []string{"--follow=true", group, member}, true},
		{"explicit false", []string{"--follow=false", group, member}, false},
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

// --until-event is not an option: the default already ends after the first
// wake-up event, and a rejected spelling never starts a subscription.
func TestParseSubscribeRejectsUntilEvent(t *testing.T) {
	called := false
	run := func(subscribeOptions) int { called = true; return 0 }
	code, _, stderr := commandtest.Capture(func() int {
		return commandtest.Run(newSubscribeCommand(run), "--until-event", "--watch", "20260907-clock-task")
	})
	if code != 2 || called || !strings.Contains(stderr, "until-event") {
		t.Fatalf("code=%d called=%v stderr=%q", code, called, stderr)
	}
}

func TestSingleEventExitsAfterStateChange(t *testing.T) {
	root, opts, id := factsMember(t)
	opts.Refresh, opts.Heartbeat = .25, 3600
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
}

// The wake-up exit waits for the probe task and for the output worker. Both
// are held by a barrier, the probe task after it sent its result and the
// output worker inside the write of the last event. subscribeContext must not
// return while either barrier is closed, whichever one is released first; the
// caller's context stays alive, so only the exit's own joins can hold it.
func TestSingleEventExitWaitsForWorkers(t *testing.T) {
	for _, first := range []string{"output", "probe"} {
		t.Run("release "+first+" first", func(t *testing.T) {
			root, opts, id := factsMember(t)
			if _, err := board.MoveEntry(currentEntry(t, root, id), root, "working"); err != nil {
				t.Fatal(err)
			}
			setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
			dir := installBatchFake(t, false)
			opts.Refresh, opts.Heartbeat = .05, 3600
			releaseOutput, releaseProbe := make(chan struct{}), make(chan struct{})
			outputHeld, probeHeld := make(chan struct{}), make(chan struct{})
			release := closeOnce
			oldHook := probeWorkerTailHook
			probeWorkerTailHook = func() {
				closeOnce(probeHeld)
				<-releaseProbe
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			done := make(chan struct{})
			// Every exit path, including a t.Fatal on a barrier timeout, runs
			// this cleanup. The hook is restored only after the subscription
			// goroutine ended, since that goroutine reads it. When the join
			// times out the hook stays, which is harmless: both barriers are
			// open by then.
			t.Cleanup(func() {
				cancel()
				closeOnce(releaseOutput)
				closeOnce(releaseProbe)
				select {
				case <-done:
					probeWorkerTailHook = oldHook
				case <-time.After(10 * time.Second):
					t.Error("the subscription goroutine did not end after cancel and release")
				}
			})
			var events []groupEvent
			var err error
			go func() {
				defer close(done)
				// This callback runs outside the test goroutine, so it reports
				// failures by returning them and never calls a testing.T method.
				events, err = singleEventsContext(ctx, root, opts, func(event groupEvent) error {
					switch event.Event {
					case "snapshot":
						if err := batchProbeStarted(dir, "w1:p0"); err != nil {
							return err
						}
						snapshot, err := board.ReadSnapshot(root, id)
						if err != nil {
							return err
						}
						_, err = board.MoveEntry(snapshot.Entry, root, "review")
						return err
					case "state-change":
						closeOnce(outputHeld)
						<-releaseOutput
					}
					return nil
				})
			}()
			for _, held := range []chan struct{}{outputHeld, probeHeld} {
				select {
				case <-held:
				case <-time.After(10 * time.Second):
					t.Fatal("a worker barrier was never reached")
				}
			}
			releases := map[string]chan struct{}{"output": releaseOutput, "probe": releaseProbe}
			second := map[string]string{"output": "probe", "probe": "output"}[first]
			release(releases[first])
			select {
			case <-done:
				t.Fatalf("subscribeContext returned while the %s worker was still held", second)
			case <-time.After(300 * time.Millisecond):
			}
			release(releases[second])
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("subscribeContext did not return after both workers were released")
			}
			requireEventNames(t, events, err, "snapshot", "state-change")
			assertBatchPeakAndCleanup(t, dir, 1, 1)
		})
	}
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
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
	// Both writes finish before any scan reads the board, so the first scan
	// carries both changes.
	written := make(chan struct{})
	holdScansUntil(t, written)
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event != "snapshot" {
			return nil
		}
		defer close(written)
		addFactsExternal(t, root, "whole-added", group)
		_, err := board.MoveEntry(currentEntry(t, root, id), root, "working")
		return err
	})
	requireEventNames(t, events, err, "snapshot", "membership-change", "state-change")
}

// The key of a dispatch is its ID and epoch, never the card: only an overdue
// dispatch the snapshot did not report wakes a single-event subscription.
func TestDispatchAttentionWakesOnlyForNewOverdueKeys(t *testing.T) {
	// Every dispatch below counts as overdue at this observation time.
	switchClock(t, time.Now().Add(10*time.Minute), 0)
	root, opts, id := factsMember(t)
	session, read := attentionSession(t, opts, root)
	attention := func(facts subscriptionFacts, probeCompleted bool) (written, woke bool) {
		t.Helper()
		var out bytes.Buffer
		woke, err := session.dispatchAttention(&out, facts, probeCompleted)
		if err != nil {
			t.Fatal(err)
		}
		return out.Len() > 0, woke
	}

	first := prepareSubscriptionDispatchID(t, root, id, "first-round", "sync", time.Minute)
	atSnapshot := read()
	if !atSnapshot.dispatches[id].ConfirmationOverdue {
		t.Fatalf("fixture dispatch is not overdue: %+v", atSnapshot.dispatches[id])
	}
	session.recordOverdueBaseline(atSnapshot)
	if written, woke := attention(atSnapshot, false); !written || woke {
		t.Fatalf("start-up attention written=%v woke=%v", written, woke)
	}
	// The probe-completion attention names the same dispatch key.
	if written, woke := attention(atSnapshot, true); !written || woke {
		t.Fatalf("probe-completion attention written=%v woke=%v", written, woke)
	}

	// The same card gets a new overdue dispatch: a new key.
	if err := board.EndDispatch(root, id, first.Input.ID, first.Revision, board.DispatchCancelled, "replaced in test"); err != nil {
		t.Fatal(err)
	}
	prepareSubscriptionDispatchID(t, root, id, "second-round", "sync", time.Minute)
	replaced := read()
	if replaced.dispatches[id].ID != "second-round" || !replaced.dispatches[id].ConfirmationOverdue {
		t.Fatalf("replacement dispatch: %+v", replaced.dispatches[id])
	}
	if written, woke := attention(replaced, false); !written || !woke {
		t.Fatalf("new dispatch on the same card written=%v woke=%v", written, woke)
	}
}

// gatedProbes installs a herdr fake that answers the n-th pane query only when
// the file gate<n> exists, so a test decides when each probe batch completes.
func gatedProbes(t *testing.T) (open func(n int), started func() int) {
	t.Helper()
	installPOSIXFakes(t, true)
	dir := t.TempDir()
	t.Setenv("SUBSCRIBE_PROBE_DIR", dir)
	script := `#!/bin/sh
n=0
[ -f "$SUBSCRIBE_PROBE_DIR/count" ] && read n < "$SUBSCRIBE_PROBE_DIR/count"
n=$((n + 1))
echo $n > "$SUBSCRIBE_PROBE_DIR/count"
while [ ! -f "$SUBSCRIBE_PROBE_DIR/gate$n" ]; do /bin/sleep 0.01; done
echo '{"result":{"pane":{"pane_id":"w1:p0","agent":"codex","agent_status":"working","agent_session":{"value":"wanted"}}}}'
`
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	open = func(n int) {
		if err := os.WriteFile(filepath.Join(dir, "gate"+strconv.Itoa(n)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	started = func() int {
		data, _ := os.ReadFile(filepath.Join(dir, "count"))
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return n
	}
	return open, started
}

// Overdue at the snapshot: neither the start-up attention event nor the one
// written when the first deadline probe finishes ends the subscription. The
// refresh interval is far longer than the test, so the only scan before the
// heartbeat is the one the probe result channel starts: the second attention
// event comes from the loop's own probe-completion branch, not from a timed
// scan. The second probe stays blocked; the heartbeat, which falls due on the
// real clock long after that scan, ends the subscription.
func TestSingleEventIgnoresDispatchOverdueAtSnapshot(t *testing.T) {
	// Every dispatch counts as overdue; the clock keeps running so that the
	// heartbeat becomes due.
	start := time.Now()
	oldNow := nowFn
	nowFn = func() time.Time { return start.Add(10*time.Minute + time.Since(start)) }
	t.Cleanup(func() { nowFn = oldNow })
	root, opts, id := factsMember(t)
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	open, started := gatedProbes(t)
	prepareSubscriptionDispatchID(t, root, id, "overdue-round", "sync", time.Minute)
	opts.Refresh, opts.Heartbeat = 3600, 1.5
	attentions := 0
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event == "dispatch-attention" {
			attentions++
			if attentions == 1 {
				open(1)
			}
		}
		return nil
	})
	// The first finished probe starts a second batch, which stays blocked until
	// the subscription ends, so exactly two attention events are written.
	requireEventNames(t, events, err, "snapshot", "dispatch-attention", "dispatch-attention", "heartbeat")
	if n := started(); n != 2 {
		t.Fatalf("probe batches started=%d, want 2 (the second one blocked)", n)
	}
	if !events[0].Dispatches[id].ConfirmationOverdue {
		t.Fatalf("snapshot did not carry an overdue dispatch: %+v", events[0].Dispatches[id])
	}
	if events[1].Liveness[id].CollectionState != "pending" || events[2].Liveness[id].CollectionState != "complete" {
		t.Fatalf("probe states: %+v / %+v", events[1].Liveness[id], events[2].Liveness[id])
	}
	// Events of one scan share the observation time, so a different time proves
	// the heartbeat came from a later scan than the probe-completion attention.
	if !events[3].ObservedAt.After(events[2].ObservedAt) {
		t.Fatalf("heartbeat shares the scan of the last attention: %v / %v", events[3].ObservedAt, events[2].ObservedAt)
	}
}

func TestSingleEventWakesForDispatchOverdueDuringSubscription(t *testing.T) {
	advance := switchClock(t, time.Now(), 10*time.Minute)
	root, opts, id := factsMember(t)
	prepareSubscriptionDispatchID(t, root, id, "late-round", "sync", time.Minute)
	opts.Refresh, opts.Heartbeat = .1, 3600
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event == "snapshot" {
			advance()
		}
		return nil
	})
	requireEventNames(t, events, err, "snapshot", "dispatch-attention")
	if events[0].Dispatches[id].ConfirmationOverdue || !events[1].Dispatches[id].ConfirmationOverdue {
		t.Fatalf("overdue flags: %+v / %+v", events[0].Dispatches[id], events[1].Dispatches[id])
	}
}

// A dispatch-attention event names every overdue card. One that was overdue at the snapshot comes
// first, so a check of the first name alone would miss the card that became overdue later.
func TestSingleEventWakesForOtherCardOverdueDuringSubscription(t *testing.T) {
	now := time.Now()
	// The first dispatch is overdue at the snapshot (10 minutes on, deadline 1 minute); the second,
	// with a deadline of 20 minutes, becomes overdue when the clock jumps.
	advance := switchClock(t, now.Add(10*time.Minute), 20*time.Minute)
	root, opts, id := factsMember(t)
	late, latePath := makeWorking(t, "facts-zlate", "Late member")
	setTaskGroup(t, latePath, opts.Group)
	if _, err := board.MoveEntry(currentEntry(t, root, late), root, "review"); err != nil {
		t.Fatal(err)
	}
	opts.Members = append(opts.Members, late)
	prepareSubscriptionDispatchID(t, root, id, "early-round", "sync", time.Minute)
	prepareSubscriptionDispatchID(t, root, late, "late-round", "sync", 20*time.Minute)
	opts.Refresh, opts.Heartbeat = .1, 3600
	// No scan reads the board before the clock has moved, so a finished start-up probe cannot
	// cause one more attention event at the old time.
	moved := make(chan struct{})
	holdScansUntil(t, moved)
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event == "snapshot" {
			advance()
			close(moved)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	names := eventNames(events)
	if !reflect.DeepEqual(names, []string{"snapshot", "dispatch-attention", "dispatch-attention"}) {
		t.Fatalf("events=%v", names)
	}
	if !events[0].Dispatches[id].ConfirmationOverdue || events[0].Dispatches[late].ConfirmationOverdue {
		t.Fatalf("snapshot overdue flags: %+v", events[0].Dispatches)
	}
	if !reflect.DeepEqual(events[1].Attention, []string{id}) || !reflect.DeepEqual(events[2].Attention, []string{id, late}) {
		t.Fatalf("attention: start-up %v, wake-up %v", events[1].Attention, events[2].Attention)
	}
}

// A card whose overdue dispatch is replaced during the subscription: the scan
// that sees the new dispatch writes task-update and the attention event, and
// the attention event is the one that names a new key.
func TestSingleEventWakesForNewDispatchOnSameCard(t *testing.T) {
	// Both dispatches count as overdue at the fixed observation time.
	switchClock(t, time.Now().Add(10*time.Minute), 0)
	root, opts, id := factsMember(t)
	first := prepareSubscriptionDispatchID(t, root, id, "first-round", "sync", time.Minute)
	swapped := make(chan struct{})
	holdScansUntil(t, swapped)
	// A timed scan follows the swap, whichever side takes the start-up probe result.
	opts.Refresh, opts.Heartbeat = .25, 3600
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event != "snapshot" {
			return nil
		}
		defer close(swapped)
		if err := board.EndDispatch(root, id, first.Input.ID, first.Revision, board.DispatchCancelled, "replaced in test"); err != nil {
			return err
		}
		prepareSubscriptionDispatchID(t, root, id, "second-round", "sync", time.Minute)
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
	if last.Dispatches[id].ID != "second-round" || !last.Dispatches[id].ConfirmationOverdue {
		t.Fatalf("last dispatch: %+v", last.Dispatches[id])
	}
}

// writePreparedOperation leaves a prepared, uncommitted board transaction, the
// state a recoverable read error reports.
func writePreparedOperation(t *testing.T, root, id string) {
	t.Helper()
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	operation := "abcdefabcdefabcdefabcdefabcdefab"
	text := snapshot.Text
	record := board.OperationRecord{Schema: 1, ID: operation, Phase: "prepared", Revisions: map[string]uint64{id: snapshot.Revision + 1},
		Files: []board.FileChange{{Path: filepath.Join("review", id, "spec.md"), Before: &text, After: text + "\nnot committed\n"}}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, ".kander", "operations", operation+".json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// Terminal events behave as under --follow: the same events, the same error.
func TestSingleEventTerminalEventsMatchFollow(t *testing.T) {
	cases := []struct {
		event      string
		readStatus string
		arrange    func(t *testing.T, root string, opts *subscribeOptions, id string) func(groupEvent) error
	}{
		{"read-error", "recoverable", func(t *testing.T, root string, _ *subscribeOptions, id string) func(groupEvent) error {
			writePreparedOperation(t, root, id)
			return nil
		}},
		{"membership-unknown", "invalid", func(t *testing.T, root string, opts *subscribeOptions, _ string) func(groupEvent) error {
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
		}},
	}
	for _, test := range cases {
		t.Run(test.event, func(t *testing.T) {
			run := func(follow bool) ([]groupEvent, string) {
				root, opts, id := factsMember(t)
				opts.Refresh, opts.Heartbeat = .25, 3600
				onEvent := test.arrange(t, root, &opts, id)
				opts.Follow = follow
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				writer := &clockEvents{onEvent: func(event groupEvent) error {
					if onEvent == nil {
						return nil
					}
					return onEvent(event)
				}}
				err := subscribeContext(ctx, root, opts, writer, nil)
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
			if len(single) == 0 {
				t.Fatal("no terminal event written")
			}
			last := single[len(single)-1]
			if last.Event != test.event || last.ReadStatus != test.readStatus || last.MembershipComplete || !last.ReconciliationRequired {
				t.Fatalf("terminal event: %+v", last)
			}
		})
	}
}

// An output failure, a cancellation, and a closed stop channel end a single-event subscription as
// they end a --follow one: the same events, the same result.
func TestSingleEventOutputFailureCancellationAndStopMatchFollow(t *testing.T) {
	failure := errors.New("output refused")
	cases := []struct {
		name string
		run  func(t *testing.T, root string, opts subscribeOptions) ([]groupEvent, error)
		want func(err error) bool
	}{
		{"output failure", func(t *testing.T, root string, opts subscribeOptions) ([]groupEvent, error) {
			writer := &clockEvents{onEvent: func(groupEvent) error { return failure }}
			err := subscribeContext(t.Context(), root, opts, writer, nil)
			return writer.events, err
		}, func(err error) bool { return errors.Is(err, failure) }},
		{"cancellation", func(t *testing.T, root string, opts subscribeOptions) ([]groupEvent, error) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			writer := &clockEvents{onEvent: func(event groupEvent) error {
				if event.Event == "snapshot" {
					cancel()
				}
				return nil
			}}
			err := subscribeContext(ctx, root, opts, writer, nil)
			return writer.events, err
		}, func(err error) bool { return errors.Is(err, context.Canceled) }},
		{"closed stop channel", func(t *testing.T, root string, opts subscribeOptions) ([]groupEvent, error) {
			stop := make(chan struct{})
			writer := &clockEvents{onEvent: func(event groupEvent) error {
				if event.Event == "snapshot" {
					close(stop)
				}
				return nil
			}}
			err := Subscribe(root, opts, writer, stop)
			return writer.events, err
		}, func(err error) bool { return err == nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			run := func(follow bool) ([]string, error) {
				root, opts, _ := factsMember(t)
				opts.Refresh, opts.Heartbeat, opts.Follow = .25, 3600, follow
				events, err := test.run(t, root, opts)
				return eventNames(events), err
			}
			followed, followErr := run(true)
			single, singleErr := run(false)
			if !test.want(followErr) || !test.want(singleErr) {
				t.Fatalf("follow err=%v single err=%v", followErr, singleErr)
			}
			if !reflect.DeepEqual(followed, single) || len(single) == 0 || single[0] != "snapshot" {
				t.Fatalf("follow=%v single=%v", followed, single)
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
