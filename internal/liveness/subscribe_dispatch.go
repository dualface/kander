package liveness

import (
	"io"
	"time"

	"github.com/dualface/kander/internal/board"
)

type dispatchJSON struct {
	board.DispatchSummary
	AgeSeconds          float64 `json:"age_seconds"`
	ConfirmationPending bool    `json:"confirmation_pending"`
	ConfirmationOverdue bool    `json:"confirmation_overdue"`
}

type dispatchAttentionKey struct {
	id    string
	epoch uint64
}

func summarizeDispatch(summary board.DispatchSummary, observed time.Time) dispatchJSON {
	pending := summary.State == board.DispatchPrepared || summary.State == board.DispatchUnknown
	return dispatchJSON{DispatchSummary: summary, AgeSeconds: max(0, observed.Sub(summary.CreatedAt).Seconds()),
		ConfirmationPending: pending, ConfirmationOverdue: pending && !observed.Before(summary.ConfirmBy)}
}

func (f subscriptionFacts) needsProbe(id string) bool {
	return f.states[id] == "working" || f.states[id] == "review" && f.dispatches[id].ConfirmationPending
}

// A persisted confirmation deadline is independent of refresh, heartbeat and
// unrelated events. Attention tracking is bounded by current monitored cards.
func (s *subscription) dispatchWait(facts subscriptionFacts, now time.Time, wait time.Duration) time.Duration {
	for id, dispatch := range facts.dispatches {
		key := dispatchAttentionKey{dispatch.ID, dispatch.Epoch}
		if dispatch.ConfirmationPending && s.attention[id] != key {
			wait = min(wait, max(0, dispatch.ConfirmBy.Sub(now)))
		}
	}
	return wait
}

func (s *subscription) dispatchAttention(w io.Writer, facts subscriptionFacts, probeCompleted bool) error {
	if s.probes.poll() {
		probeCompleted = true
	}
	var overdue []string
	newAttention := false
	for id := range s.attention {
		if !facts.dispatches[id].ConfirmationOverdue {
			delete(s.attention, id)
		}
	}
	for _, id := range facts.monitored {
		dispatch := facts.dispatches[id]
		if !dispatch.ConfirmationOverdue {
			continue
		}
		overdue = append(overdue, id)
		key := dispatchAttentionKey{dispatch.ID, dispatch.Epoch}
		if s.attention[id] != key {
			s.attention[id] = key
			newAttention = true
			s.dispatchProbePending = true
		}
	}
	if len(overdue) == 0 {
		s.dispatchProbePending = false
		return nil
	}
	// If another bounded batch owns the worker, defer the deadline probe until
	// it joins. No additional worker or unbounded retry queue is created.
	if s.dispatchProbePending && !s.probes.running {
		s.probes.start(facts)
		s.dispatchProbePending = false
	}
	if !newAttention && !probeCompleted {
		return nil
	}
	payload := s.payload("dispatch-attention", facts)
	payload.Attention = overdue
	payload.ReconciliationRequired = true
	payload.Detail = t("liveness.subscription_dispatch_overdue")
	payload.Liveness = s.probes.liveness(facts, s.heartbeat)
	if err := emitEvent(w, payload); err != nil {
		return err
	}
	s.last = facts
	return nil
}
