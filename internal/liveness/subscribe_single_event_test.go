package liveness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
			args := []string{arg, "--watch", "20260907-clock-task"}
			if _, errText := parseSubscribeArgs(args); errText == "" || errText == "usage" {
				t.Fatalf("parse accepted %s: %q", arg, errText)
			}
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			oldStderr := os.Stderr
			os.Stderr = writer
			code := RunSubscribe(args)
			os.Stderr = oldStderr
			writer.Close()
			diagnostics, err := io.ReadAll(reader)
			reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			if code != 2 {
				t.Fatalf("exit code=%d want 2", code)
			}
			if !strings.HasPrefix(string(diagnostics), "kander subscribe") {
				t.Fatalf("no usage on stderr: %q", diagnostics)
			}
		})
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

// The wake-up exit joins a probe that is still running and the output worker
// that is still writing the last event. The caller's context stays alive, so
// only the subscription's own exit can have stopped them: at the moment
// SubscribeContext has joined its workers, the probe batch is collected and
// cancelled, the output worker is done, and the slow probe process is gone.
func TestSingleEventExitJoinsRunningProbe(t *testing.T) {
	root, opts, id := factsMember(t)
	if _, err := board.MoveEntry(currentEntry(t, root, id), root, "working"); err != nil {
		t.Fatal(err)
	}
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	dir := installBatchFake(t, false)
	opts.Refresh, opts.Heartbeat = .05, 3600
	var joined, probeCollected, probeCancelled, outputDone atomic.Bool
	oldHook := subscribeJoinedHook
	subscribeJoinedHook = func(probes *subscriptionProbes, output *subscriptionOutputQueue) {
		joined.Store(true)
		probeCollected.Store(!probes.running)
		probeCancelled.Store(probes.ctx.Err() != nil)
		select {
		case <-output.done:
			outputDone.Store(true)
		default:
		}
	}
	t.Cleanup(func() { subscribeJoinedHook = oldHook })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var returned atomic.Bool
	events, err := singleEventsContext(ctx, root, opts, func(event groupEvent) error {
		if returned.Load() {
			t.Errorf("event %q written after the subscription returned", event.Event)
		}
		if event.Event == "state-change" {
			// The worker is still inside the last write when the loop returns.
			time.Sleep(100 * time.Millisecond)
			return nil
		}
		if event.Event != "snapshot" {
			return nil
		}
		waitForBatchProbe(t, dir, "w1:p0")
		_, err := board.MoveEntry(currentEntry(t, root, id), root, "review")
		return err
	})
	returned.Store(true)
	requireEventNames(t, events, err, "snapshot", "state-change")
	if ctx.Err() != nil {
		t.Fatalf("caller context ended before the check: %v", ctx.Err())
	}
	if !joined.Load() || !probeCollected.Load() || !probeCancelled.Load() || !outputDone.Load() {
		t.Fatalf("workers at return: joined=%v probe collected=%v probe cancelled=%v output done=%v",
			joined.Load(), probeCollected.Load(), probeCancelled.Load(), outputDone.Load())
	}
	assertBatchPeakAndCleanup(t, dir, 1, 1)
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

// gatedProbe installs a herdr fake whose pane query answers only after the
// returned gate file exists, so a test decides when a probe completes.
func gatedProbe(t *testing.T) (open func()) {
	t.Helper()
	installPOSIXFakes(t, true)
	gate := filepath.Join(t.TempDir(), "gate")
	t.Setenv("SUBSCRIBE_PROBE_GATE", gate)
	script := `#!/bin/sh
while [ ! -f "$SUBSCRIBE_PROBE_GATE" ]; do /bin/sleep 0.01; done
echo '{"result":{"pane":{"pane_id":"w1:p0","agent":"codex","agent_status":"working","agent_session":{"value":"wanted"}}}}'
`
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.WriteFile(gate, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// Overdue at the snapshot: neither the start-up attention event nor the one
// that reports the finished deadline probe ends the subscription. Only the
// heartbeat does, after the clock passes its due time.
func TestSingleEventIgnoresDispatchOverdueAtSnapshot(t *testing.T) {
	advance := switchClock(t, time.Now().Add(10*time.Minute), 2*time.Hour)
	root, opts, id := factsMember(t)
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	openProbe := gatedProbe(t)
	prepareSubscriptionDispatchID(t, root, id, "overdue-round", "sync", time.Minute)
	opts.Refresh, opts.Heartbeat = .05, 3600
	// The clock moves at the start of the first scan after the second attention
	// event was delivered, so the scan that wrote the probe-completion attention
	// finds no heartbeat due and has to decide on the attention event alone.
	var secondDelivered atomic.Bool
	oldRead := readSubscriptionFacts
	readSubscriptionFacts = func(s *subscription, ctx context.Context, root string) (subscriptionFacts, error) {
		if secondDelivered.Load() {
			advance()
		}
		return oldRead(s, ctx, root)
	}
	t.Cleanup(func() { readSubscriptionFacts = oldRead })
	attentions := 0
	events, err := singleEvents(t, root, opts, func(event groupEvent) error {
		if event.Event != "dispatch-attention" {
			return nil
		}
		attentions++
		switch attentions {
		case 1:
			openProbe()
		case 2:
			secondDelivered.Store(true)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscription ended with %v after %v", err, eventNames(events))
	}
	// A finished deadline probe starts the next one, so a third attention event
	// can follow the second; all of them name only the baseline dispatch.
	names := eventNames(events)
	if len(names) < 4 || names[0] != "snapshot" || names[len(names)-1] != "heartbeat" || !onlyAttention(names[1:len(names)-1]) {
		t.Fatalf("events=%v", names)
	}
	if !events[0].Dispatches[id].ConfirmationOverdue {
		t.Fatalf("snapshot did not carry an overdue dispatch: %+v", events[0].Dispatches[id])
	}
	if events[1].Liveness[id].CollectionState != "pending" || events[2].Liveness[id].CollectionState != "complete" {
		t.Fatalf("probe states: %+v / %+v", events[1].Liveness[id], events[2].Liveness[id])
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
	opts.Refresh, opts.Heartbeat = 3600, 3600
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

func onlyAttention(names []string) bool {
	for _, name := range names {
		if name != "dispatch-attention" {
			return false
		}
	}
	return true
}
