package worker

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
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
	key      string
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
			scope.results[id] = ackResult{revision: revision, key: client.NewKey(), typedAt: typedAt, window: window, note: note}
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

func answerDelivery(ctx context.Context, project *client.Project, c remote.Credential, id, key string, revision uint64, note string) (api.CollabRequest, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	answer := api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: revision, Note: note}
	return project.AnswerRequest(ctx, key, c.TaskID, id, answer, *c.Claim())
}

// flushAcknowledgements runs in the work loop, not in the observers. Only
// transient failures keep a result for a later round, within the file lifetime.
func (w *loop) flushAcknowledgements(ctx context.Context, c remote.Credential) {
	scope := w.deliveries[keyOf(c)]
	if scope == nil || ctx.Err() != nil {
		return
	}
	results := scope.snapshotResults()
	if len(results) == 0 {
		return
	}
	log := w.credentialLog(c)
	cl, err := w.client(c)
	if err != nil {
		log.Warn("acknowledgement client unavailable", "error", err)
		return
	}
	project := cl.Project(c.ProjectID)
	for id, result := range results {
		file, retained := scope.files[id]
		if !retained {
			continue
		}
		if !w.o.Now().Before(file.expires) {
			w.closeRequestFile(scope, id)
			continue
		}
		applied, err := w.writeAcknowledgement(ctx, project, c, id, result)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if acknowledgementRefused(err) {
				scope.dropResult(id)
				log.Debug("request acknowledgement refused", "request", id, "error", err)
			} else {
				log.Warn("writing the request acknowledgement failed; retrying later", "request", id, "error", err)
			}
			continue
		}
		scope.dropResult(id)
		if applied && result.note == ackedNote {
			log.Info("request acknowledged", "request", id)
		} else if applied {
			log.Warn("request not acknowledged", "request", id, "reason", unconfirmedNote,
				"typed_at", result.typedAt, "window", ackWindow, "target", result.window)
		}
	}
}

func acknowledgementRefused(err error) bool {
	var failure *api.Error
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.Code {
	case api.CodeLeaseInvalid, api.CodeTakenOver, api.CodeClaimRequired, api.CodeHeldByOther, api.CodeNotFound:
		return true
	default:
		return false
	}
}

func (w *loop) writeAcknowledgement(ctx context.Context, project *client.Project, c remote.Credential, id string, result ackResult) (bool, error) {
	_, err := answerDelivery(ctx, project, c, id, result.key, result.revision, result.note)
	if !api.IsCode(err, api.CodeRevisionConflict) {
		return err == nil, err
	}
	readCtx, stopRead := bounded(ctx)
	requests, err := project.Requests(readCtx, c.TaskID)
	stopRead()
	if err != nil {
		return false, err
	}
	w.closeSettledRequestFiles(keyOf(c), requests)
	i := slices.IndexFunc(requests, func(r api.CollabRequest) bool { return r.ID == id })
	if i == -1 {
		return false, nil
	}
	current := requests[i]
	if current.TargetGeneration != c.Generation || current.Status != api.RequestAccepted ||
		(current.Note != typedNote && current.Note != reservationNote) {
		return false, nil
	}
	// Keep the idempotency key for this revision if a retry's response is
	// lost. The original key is bound to a different expected revision.
	key := result.key + "-" + strconv.FormatUint(current.Revision, 10)
	_, err = answerDelivery(ctx, project, c, id, key, current.Revision, result.note)
	return err == nil, err
}
