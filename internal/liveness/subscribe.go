package liveness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

const (
	defaultRefresh   = 1.0
	defaultHeartbeat = 900.0
)

type subscribeOptions struct {
	Group     string
	Members   []string
	Watch     []string
	Refresh   float64
	Heartbeat float64
	// Follow keeps the subscription writing events until it is stopped. The
	// zero value ends it after the scan that wrote the first wake-up event.
	Follow bool
}

type changeEvent struct {
	From   string `json:"from"`
	TaskID string `json:"task_id"`
	To     string `json:"to"`
}

type livenessJSON struct {
	Agent            string              `json:"agent"`
	Status           string              `json:"status"`
	Channel          string              `json:"channel"`
	Detail           string              `json:"detail"`
	ObservedAt       *time.Time          `json:"observed_at,omitempty"`
	AgeSeconds       float64             `json:"age_seconds"`
	RuntimeState     string              `json:"runtime_state"`
	ObservationValid bool                `json:"observation_valid"`
	Identity         ObservationIdentity `json:"identity"`
	Revision         uint64              `json:"revision"`
	CollectionState  string              `json:"collection_state"`
	Collecting       bool                `json:"collecting"`
	Stale            bool                `json:"stale"`
	NewWindow        string              `json:"new_window,omitempty"`
}

type groupEvent struct {
	Coordinators           map[string]api.CoordinatorSummary `json:"coordinators,omitempty"`
	Claims                 map[string]*api.ClaimView         `json:"claims,omitempty"`
	Cursor                 string                            `json:"cursor,omitempty"`
	Dispatches             map[string]dispatchJSON           `json:"dispatches,omitempty"`
	Attention              []string                          `json:"attention,omitempty"`
	Blocked                map[string]string                 `json:"blocked,omitempty"`
	SchemaVersion          int                               `json:"schema_version"`
	SubscriptionID         string                            `json:"subscription_id"`
	Seq                    uint64                            `json:"seq"`
	ObservedAt             time.Time                         `json:"observed_at"`
	TaskRevisions          map[string]uint64                 `json:"task_revisions"`
	Updated                []string                          `json:"updated,omitempty"`
	WatchReferences        []string                          `json:"watch_references,omitempty"`
	MembershipVersions     map[string]string                 `json:"membership_versions,omitempty"`
	Memberships            map[string][]string               `json:"memberships,omitempty"`
	MembershipComplete     bool                              `json:"membership_complete"`
	ReconciliationRequired bool                              `json:"reconciliation_required"`
	ReadStatus             string                            `json:"read_status"`
	Detail                 string                            `json:"detail,omitempty"`
	Removed                []string                          `json:"removed,omitempty"`
	Event                  string                            `json:"event"`
	GroupID                string                            `json:"group_id"`
	Tasks                  map[string]string                 `json:"tasks"`
	Changed                []changeEvent                     `json:"changed,omitempty"`
	Watched                []string                          `json:"watched,omitempty"`
	Liveness               map[string]livenessJSON           `json:"liveness,omitempty"`
}

var nowFn = time.Now

// subscriptionInterval rejects values that would overflow or truncate to zero.
func subscriptionInterval(seconds float64) (time.Duration, bool) {
	nanoseconds := seconds * float64(time.Second)
	// float64(MaxInt64) rounds up to 2^63, so the upper bound is exclusive.
	if math.IsNaN(nanoseconds) || nanoseconds < 1 || nanoseconds >= float64(math.MaxInt64) {
		return 0, false
	}
	return time.Duration(nanoseconds), true
}

func validInterval(seconds float64) bool {
	_, valid := subscriptionInterval(seconds)
	return valid
}

// NewSubscribeCommand builds kander subscribe against the server.
func NewSubscribeCommand() *cobra.Command {
	return command.Streaming(command.RequireProject(newSubscribeCommand(runSubscribe)))
}

// newSubscribeCommand parses the subscription and hands it to run.
func newSubscribeCommand(run func(subscribeOptions) int) *cobra.Command {
	opts := subscribeOptions{}
	c := &cobra.Command{
		Use: "subscribe [<task-group> <task-id>...]",
		// Cards outside any task group are monitored through --watch alone, so a
		// subscription may name no group and no members when it watches something.
		Args: func(c *cobra.Command, args []string) error {
			if len(args) == 0 && len(opts.Watch) > 0 {
				return nil
			}
			return command.Args(2, -1)(c, args)
		},
	}
	c.RunE = func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			opts.Group, opts.Members = args[0], args[1:]
		}
		return command.Exit(run(opts))
	}
	command.Float(c.Flags(), &opts.Refresh, "refresh", defaultRefresh, validInterval, "", "liveness.refresh_interval_must_be_greater_than_0")
	command.Float(c.Flags(), &opts.Heartbeat, "heartbeat", defaultHeartbeat, validInterval, "", "liveness.heartbeat_interval_must_be_greater_than_0")
	c.Flags().StringArrayVar(&opts.Watch, "watch", nil, "")
	c.Flags().BoolVar(&opts.Follow, "follow", false, "")
	return c
}

func uniqueStrings(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func emitEvent(w io.Writer, payload groupEvent) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// Subscribe preserves the stop-channel API; a closed stop is a successful exit.
// Writer requirements and resource ownership follow subscribeContext.
func Subscribe(root string, opts subscribeOptions, w io.Writer, stop <-chan struct{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	err := subscribeContext(ctx, root, opts, w, nil)
	stopped := ctx.Err() != nil
	cancel()
	<-joined
	if stopped && err == context.Canceled {
		return nil
	}
	return err
}

// subscribeContext runs one subscription. Without opts.Follow it returns nil
// after the scan that wrote the first wake-up event: a membership, state, task,
// blocked, coordinator, claim, reset or heartbeat event, or a dispatch-attention
// event that dispatchAttention reports as a wake-up.
func subscribeContext(ctx context.Context, root string, opts subscribeOptions, w io.Writer, service *remote.Session) (result error) {
	refresh, valid := subscriptionInterval(opts.Refresh)
	if !valid {
		return fmt.Errorf("%s", t("liveness.refresh_interval_must_be_greater_than_0"))
	}
	heartbeat, valid := subscriptionInterval(opts.Heartbeat)
	if !valid {
		return fmt.Errorf("%s", t("liveness.heartbeat_interval_must_be_greater_than_0"))
	}
	session, err := newSubscription(opts)
	if err != nil {
		return err
	}
	session.service = service
	writer, cleanup, err := subscriptionWriter(w)
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil {
			result = errors.Join(result, cleanupErr)
		}
	}()
	output := newSubscriptionOutput(ctx, writer)
	probes := newSubscriptionProbes(ctx)
	defer func() { probes.finish(); result = subscriptionResult(ctx, result, output.finish()) }()
	session.probes = probes
	session.heartbeat = heartbeat
	w = output
	snapshot, err := session.readTolerant(ctx, root, refresh, w, output)
	if err != nil {
		return err
	}
	if err := session.emit(w, "snapshot", snapshot, nil, nil, nil); err != nil {
		return err
	}
	probes.start(snapshot)
	session.recordOverdueBaseline(snapshot)
	// Every overdue dispatch here is in the baseline, so this write never wakes.
	if _, err := session.dispatchAttention(w, snapshot, false); err != nil {
		return err
	}
	heartbeatDue := nowFn().Add(heartbeat)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-output.done:
			return output.err
		default:
		}
		heartbeatRemaining := heartbeatDue.Sub(nowFn())
		wait := refresh
		if heartbeatRemaining < wait {
			if heartbeatRemaining < 0 {
				wait = 0
			} else {
				wait = heartbeatRemaining
			}
		}
		wait = session.dispatchWait(snapshot, nowFn(), wait)
		probeCompleted := false
		reset := false
		timerWait := wait
		if service != nil {
			timerWait = 0
		}
		timer := time.NewTimer(timerWait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-output.done:
			timer.Stop()
			return output.err
		case batch := <-probes.results:
			timer.Stop()
			probes.accept(batch)
			if len(session.attention) == 0 {
				continue
			}
			probeCompleted = true
		case <-timer.C:
			if service != nil {
				page, err := service.Project.Events(output.ctx, session.cursor, min(wait, time.Minute))
				if err != nil {
					return session.unavailable(w, snapshot, err)
				}
				if page.Reset {
					session.reconciliation = true
					reset = true
				}
			}
		}
		current, err := session.readTolerant(ctx, root, refresh, w, output)
		if err != nil {
			return err
		}
		if service != nil {
			probes.start(current)
		}
		woke := false
		if reset {
			if err := session.emit(w, "snapshot-reset", current, nil, nil, nil); err != nil {
				return err
			}
			woke = true
		}

		var changed []changeEvent
		var updated []string
		for _, taskID := range current.monitored {
			if previous, ok := snapshot.states[taskID]; ok {
				if current.states[taskID] != previous {
					changed = append(changed, changeEvent{From: previous, TaskID: taskID, To: current.states[taskID]})
				}
				if current.revisions[taskID] != snapshot.revisions[taskID] {
					updated = append(updated, taskID)
				}
			}
		}
		if membershipChanged(snapshot, current) {
			removed := removedMembers(snapshot, current)
			if len(removed) > 0 {
				session.reconciliation = true
			}
			if err := session.emit(w, "membership-change", current, nil, nil, removed); err != nil {
				return err
			}
			woke = true
		}
		if len(changed) > 0 {
			if err := session.emit(w, "state-change", current, changed, updated, nil); err != nil {
				return err
			}
			woke = true
		} else if len(updated) > 0 {
			if err := session.emit(w, "task-update", current, nil, updated, nil); err != nil {
				return err
			}
			woke = true
		}
		if changed, takenOver := changedCoordinators(snapshot, current); changed {
			session.reconciliation = session.reconciliation || takenOver
			if err := session.emit(w, "coordinator-change", current, nil, nil, nil); err != nil {
				return err
			}
			woke = true
		}
		if ids := changedClaims(snapshot, current); len(ids) > 0 {
			if err := session.emit(w, "claim-change", current, nil, ids, nil); err != nil {
				return err
			}
			woke = true
		}
		if !maps.Equal(snapshot.blocked, current.blocked) {
			if err := session.emit(w, "member-blocked", current, nil, nil, nil); err != nil {
				return err
			}
			woke = true
		}
		attentionWoke, err := session.dispatchAttention(w, current, probeCompleted)
		if err != nil {
			return err
		}
		woke = woke || attentionWoke
		snapshot = current
		if !nowFn().Before(heartbeatDue) {
			if err := session.emit(w, "heartbeat", current, nil, nil, nil); err != nil {
				return err
			}
			woke = true
			probes.start(current)
			heartbeatDue = nowFn().Add(heartbeat)
		}
		// The scan wrote all of its events above; the deferred output.finish
		// then waits for the queue, so the last line is complete on return.
		if woke && !opts.Follow {
			return nil
		}
	}
}

// A merged stdout/stderr pipe must not turn an output failure into another
// unbounded write. When diagnostics also fail, the nonzero exit is the signal.
func writeSubscriptionDiagnostic(w io.Writer, message string) error {
	return writeSubscriptionDiagnosticContext(context.Background(), w, message)
}

// writeSubscriptionDiagnosticContext is writeSubscriptionDiagnostic for a write that must stop
// when the subscription is canceled: it keeps the write limit and ends earlier with the context.
func writeSubscriptionDiagnosticContext(parent context.Context, w io.Writer, message string) error {
	writer, cleanup, err := subscriptionWriter(w)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, subscriptionWriteTimeout)
	defer cancel()
	data := []byte(message + "\n")
	n, err := writer.WriteContext(ctx, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return errors.Join(err, cleanup())
}

// bindStreamNotices makes the remarks of the stream use the bounded diagnostic writer, and
// returns the function that undoes it. While the subscription writes to a shared stdout and
// stderr, the descriptor is non-blocking, and a plain stderr write would fail with EAGAIN. The
// write ends with ctx, so canceling the subscription does not wait for a reader that stopped.
func bindStreamNotices(ctx context.Context) (restore func()) {
	return command.SetStreamLineWriter(func(line string) error {
		return writeSubscriptionDiagnosticContext(ctx, os.Stderr, line)
	})
}

// runSubscribe implements kander subscribe against the server. Every failure is written by the
// bounded diagnostic writer: as the error object with --json, otherwise as "kander: message".
func runSubscribe(opts subscribeOptions) int {
	fail := func(err error) int {
		_ = writeSubscriptionDiagnostic(os.Stderr, command.ErrorLine(err))
		return command.FailReported(err)
	}
	if _, err := config.Load(false); err != nil {
		return fail(err)
	}
	cfg, err := config.Effective(nil)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	// The remarks of the stream go through the bounded writer too: stdout and stderr may be
	// one full, non-blocking pipe.
	ctx, cancel := subscriptionSignalContext()
	defer cancel()
	defer bindStreamNotices(ctx)()
	service, err := remote.Open(ctx, cfg, cwd)
	if err == nil {
		err = subscribeContext(ctx, service.Location(""), opts, os.Stdout, service)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return fail(err)
	}
	return 0
}
