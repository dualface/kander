package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/remote"
)

type scheduledPoll struct {
	at  time.Time
	run chan struct{}
}

// controlPolls advances the worker and service clock only when the test allows
// a probe, so the work loop can also run while an observer is parked.
func controlPolls(h *harness, seen func(context.Context, string) (bool, error)) <-chan scheduledPoll {
	polls := make(chan scheduledPoll)
	h.loop.o.waitForAckPoll = func(ctx context.Context, at time.Time) bool {
		poll := scheduledPoll{at: at, run: make(chan struct{})}
		select {
		case <-ctx.Done():
			return false
		case polls <- poll:
		}
		select {
		case <-ctx.Done():
			return false
		case <-poll.run:
			return true
		}
	}
	h.loop.o.Resolve = func(window, session string) (*Target, error) {
		target, err := h.resolveRecording(window, session)
		target.Seen = seen
		return target, err
	}
	return polls
}

func nextAckPoll(t *testing.T, polls <-chan scheduledPoll) scheduledPoll {
	t.Helper()
	select {
	case poll := <-polls:
		return poll
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not schedule the next check")
		return scheduledPoll{}
	}
}

func runAckPoll(h *harness, poll scheduledPoll) {
	h.advance(poll.at.Sub(h.now()))
	h.loop.renewRound(h.t.Context())
	close(poll.run)
}

func ackResultOf(t *testing.T, scope *deliveryScope, id string) ackResult {
	t.Helper()
	waitFor(t, "the acknowledgement result", func() bool {
		_, ok := scope.snapshotResults()[id]
		return ok
	})
	return scope.snapshotResults()[id]
}

func TestWorkerObservesAcknowledgementAcrossTheWindow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lastPoll time.Duration
		want     string
	}{
		{"draft never submits", 120 * time.Second, unconfirmedNote},
		{"marker briefly visible", 60 * time.Second, ackedNote},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, c := h.claimed("poll-window", true)
			r := h.request(c, "the agent may not submit this")
			sink := h.logs()
			typedAt := h.now().Add(16 * time.Second)
			polls := controlPolls(h, func(_ context.Context, marker string) (bool, error) {
				elapsed := h.now().Sub(typedAt)
				if elapsed == time.Second {
					return false, errors.New("transient pane error")
				}
				return tc.want == ackedNote && elapsed >= 60*time.Second && elapsed < 65*time.Second &&
					strings.HasPrefix(marker, ackMarkerPrefix), nil
			})
			h.workAfter(16 * time.Second)
			scope := h.loop.deliveries[keyOf(c)]
			path := filepath.Join(scope.files[r.ID].dir.Path, "request.md")
			for expected := time.Second; expected <= tc.lastPoll; {
				poll := nextAckPoll(t, polls)
				if got := poll.at.Sub(typedAt); got != expected {
					t.Fatalf("poll at %s, want %s", got, expected)
				}
				if got := h.requests(c)[0]; got.Note != typedNote {
					t.Fatalf("before observation the request is only typed: %+v", got)
				}
				runAckPoll(h, poll)
				if expected < 10*time.Second {
					expected += time.Second
				} else {
					expected += 2 * time.Second
				}
			}
			if got := ackResultOf(t, scope, r.ID); got.note != tc.want {
				t.Fatalf("observer result = %q, want %q", got.note, tc.want)
			}
			h.workAfter(0)
			h.workAfter(16 * time.Second)
			if got := h.requests(c)[0]; got.Status != api.RequestAccepted || got.Note != tc.want {
				t.Fatalf("request = %+v, want accepted with %q", got, tc.want)
			}
			if len(h.lines) != 1 {
				t.Fatalf("prompt count = %d, want one", len(h.lines))
			}
			warnings := sink.findAll("request not acknowledged")
			if tc.want == unconfirmedNote {
				if len(warnings) != 1 || warnings[0]["request"] != r.ID || warnings[0]["level"] != "WARN" ||
					warnings[0]["reason"] != unconfirmedNote || warnings[0]["target"] == "" {
					t.Fatalf("expected one warning with its request and the reason: %+v", warnings)
				}
			} else if len(warnings) != 0 {
				t.Fatalf("acknowledged request warned: %v", warnings)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("file disappeared after observation: %v", err)
			}
		})
	}
}

func TestWorkerDoesNotWaitForAnAcknowledgementBeforeTheNextRound(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("no-wait", true)
	first := h.request(c, "first request")
	polls := controlPolls(h, func(context.Context, string) (bool, error) { return true, nil })
	h.workAfter(16 * time.Second)
	poll := nextAckPoll(t, polls)
	second := h.request(c, "next request")
	if _, err := h.loop.workRound(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.workAfter(16 * time.Second)
	if lines := h.lines; len(lines) != 2 || !strings.Contains(lines[0], first.ID) || !strings.Contains(lines[1], second.ID) {
		t.Fatalf("the next round did not deliver while the first observer waited: %q", lines)
	}
	if len(h.loop.deliveries[keyOf(c)].snapshotResults()) != 0 {
		t.Fatal("the parked observers must not have results")
	}
	close(poll.run)
}

func TestWorkerCancelsAcknowledgementsWhenCredentialGoesAway(t *testing.T) {
	for _, action := range []string{"absent", "lost"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t)
			_, c := h.claimed("gone", true)
			sink := h.logs()
			first := h.request(c, "result already waiting")
			second := h.request(c, "probe in progress")
			entered, exited := make(chan struct{}), make(chan struct{})
			var seen int
			polls := controlPolls(h, func(ctx context.Context, _ string) (bool, error) {
				seen++
				if seen == 1 {
					return true, nil
				}
				close(entered)
				<-ctx.Done()
				close(exited)
				return false, ctx.Err()
			})
			h.workAfter(16 * time.Second)
			scope := h.loop.deliveries[keyOf(c)]
			poll := nextAckPoll(t, polls)
			runAckPoll(h, poll)
			waitFor(t, "one result", func() bool { return len(scope.snapshotResults()) == 1 })
			runAckPoll(h, nextAckPoll(t, polls))
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("second observer never entered its probe")
			}
			var dirs []string
			for _, entry := range scope.files {
				dirs = append(dirs, entry.dir.Path)
			}

			if action == "absent" {
				h.loop.forgetAbsent(nil)
			} else {
				h.loop.stopLost(t.Context(), h.loop.o.Logger, remote.Credential{
					ServerURL: c.ServerURL, ProjectID: c.ProjectID, TaskID: c.TaskID, ExecutionID: c.ExecutionID, Lost: true,
				})
			}
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				t.Fatal("observer did not cancel")
			}
			if h.loop.deliveries[keyOf(c)] != nil || len(scope.snapshotResults()) != 0 {
				t.Fatal("lost credential retained acknowledgement state")
			}
			h.loop.flushAcknowledgements(t.Context(), c)
			for _, r := range h.requests(c) {
				if (r.ID == first.ID || r.ID == second.ID) && r.Note != typedNote {
					t.Fatalf("a result was written after cancellation: %+v", r)
				}
			}
			if len(sink.findAll("request not acknowledged")) != 0 {
				t.Fatal("a canceled observer logged a timeout")
			}
			for _, dir := range dirs {
				requireRequestDirRemoved(t, dir)
			}
		})
	}
}

func requireRequestDirRemoved(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private directory survived cleanup: %v", err)
	}
}

func TestWorkerClosesRequestFilesOnTerminalStatusOrRetention(t *testing.T) {
	h := newHarness(t)
	p, c := h.claimed("request-file-cleanup", true)
	items := []api.CollabRequest{
		h.request(c, "done"), h.request(c, "rejected"), h.request(c, "superseded"), h.request(c, "unanswered"),
	}
	h.workAfter(16 * time.Second)
	scope := h.loop.deliveries[keyOf(c)]
	dir := make(map[string]string)
	for id, entry := range scope.files {
		dir[id] = entry.dir.Path
	}
	if len(dir) != len(items) {
		t.Fatalf("request files created = %d, want %d", len(dir), len(items))
	}
	for i, state := range []string{api.RequestDone, api.RequestRejected} {
		if _, err := p.AnswerRequest(t.Context(), "", c.TaskID, items[i].ID, api.CollabAnswer{Status: state}, *c.Claim()); err != nil {
			t.Fatal(err)
		}
	}
	h.workAfter(16 * time.Second)
	// A takeover normally removes the credential before a superseded request
	// is read. Exercise that status explicitly as well.
	h.loop.closeSettledRequestFiles(keyOf(c), []api.CollabRequest{{ID: items[2].ID, Status: api.RequestSuperseded}})
	for _, item := range items[:3] {
		requireRequestDirRemoved(t, dir[item.ID])
	}
	last := items[3].ID
	h.loop.expireRequestFiles(scope.files[last].expires.Add(-time.Nanosecond))
	if _, err := os.Stat(dir[last]); err != nil {
		t.Fatalf("file removed before retention elapsed: %v", err)
	}
	h.loop.expireRequestFiles(scope.files[last].expires)
	requireRequestDirRemoved(t, dir[last])
	if len(scope.files) != 0 || len(scope.snapshotResults()) != 0 {
		t.Fatal("expired or settled request files still tracked")
	}
	if !slices.Contains(h.credential().Delivered, last) {
		t.Fatal("expiration must not clear the durable at-most-once flag")
	}
}
