package worker

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/remote"
)

const (
	ackWindow       = 120 * time.Second
	ackFastPeriod   = 10 * time.Second
	ackProbeTimeout = 2 * time.Second

	typedNote       = "typed into the agent pane; waiting for the agent's acknowledgement"
	ackedNote       = "submitted: the agent acknowledged the request file"
	unconfirmedNote = "typed into the agent pane but not acknowledged within 120s: the agent may be busy with the message queued, or the line may still sit in its input box"
)

type ackResult struct {
	revision uint64
	typedAt  time.Time
	window   string
	note     string
}

func (w *loop) startAckObserver(scope *deliveryScope, id string, revision uint64, typedAt time.Time, window, marker string, seen func(context.Context, string) (bool, error)) {
	if seen == nil {
		return
	}
	ctx, cancel := context.WithCancel(scope.ctx)
	file := scope.files[id]
	file.cancel = cancel
	scope.files[id] = file
	go func() {
		defer cancel()
		note := w.observeAcknowledgement(ctx, typedAt, marker, seen)
		if note == "" {
			return
		}
		scope.mu.Lock()
		defer scope.mu.Unlock()
		// File removal cancels first and then takes mu to delete the
		// result, so cancellation cannot leave a result behind.
		if ctx.Err() == nil {
			scope.results[id] = ackResult{revision: revision, typedAt: typedAt, window: window, note: note}
		}
	}()
}

func (w *loop) observeAcknowledgement(ctx context.Context, typedAt time.Time, marker string, seen func(context.Context, string) (bool, error)) string {
	deadline := typedAt.Add(ackWindow)
	fastUntil := typedAt.Add(ackFastPeriod)
	after := func(poll time.Time) time.Time {
		if poll.Before(fastUntil) {
			return poll.Add(time.Second)
		}
		return poll.Add(2 * time.Second)
	}
	for next := typedAt.Add(time.Second); w.o.waitForAckPoll(ctx, next); next = after(next) {
		now := w.o.Now()
		if !now.Before(deadline) {
			return unconfirmedNote
		}
		// Coalesce missed slots into this probe. An overrun of only a few
		// milliseconds starts the next probe immediately, not a full slot late.
		for at := after(next); !at.After(now); at = after(at) {
			next = at
		}
		probeCtx, cancel := context.WithTimeout(ctx, min(ackProbeTimeout, deadline.Sub(now)))
		ok, err := seen(probeCtx, marker)
		cancel()
		if err == nil && ok && w.o.Now().Before(deadline) {
			return ackedNote
		}
	}
	return ""
}

func waitForAckPoll(ctx context.Context, at time.Time) bool {
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func (s *deliveryScope) dropResult(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.results, id)
}

func (s *deliveryScope) snapshotResults() map[string]ackResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.results)
}

// flushAcknowledgements runs in the work loop, not in the observers, once per
// round for each credential of a task. It rebuilds the answers of the
// credential once per process, replays the open answers whose backoff has
// elapsed, and then sends the results the observers published for request
// files still retained.
func (w *loop) flushAcknowledgements(ctx context.Context, c remote.Credential) {
	if ctx.Err() != nil || c.ResourceKind == "group" || c.ResourceKind == "integration" {
		return
	}
	log := w.credentialLog(c)
	cl, err := w.client(c)
	if err != nil {
		log.Warn("acknowledgement client unavailable", "error", err)
		return
	}
	project := cl.Project(c.ProjectID)
	w.rebuildAnswers(ctx, log, project, c)
	w.replayAnswers(ctx, log, project, c)
	scope := w.deliveries[keyOf(c)]
	if scope == nil {
		return
	}
	results := scope.snapshotResults()
	for _, id := range slices.Sorted(maps.Keys(results)) {
		if ctx.Err() != nil {
			return
		}
		result := results[id]
		file, retained := scope.files[id]
		if !retained {
			continue
		}
		if !w.o.Now().Before(file.expires) {
			w.closeRequestFile(scope, id)
			continue
		}
		scope.dropResult(id)
		body := api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: result.revision, Note: result.note}
		w.submitAnswer(ctx, log, project, c, &answerAttempt{
			request: id, kind: answerResult, key: contentKey(c.ExecutionID, id, body), body: body,
			typedAt: result.typedAt, window: result.window,
		})
	}
}
