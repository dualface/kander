package liveness

import (
	"context"
	"errors"
	"io"
	"time"
)

// subscriptionContentionLimit bounds how long consecutive lock wait timeouts
// may keep a subscription retrying before it reports a terminal read-error.
const subscriptionContentionLimit = 30 * time.Second

// readSubscriptionFacts is the single read seam; tests replace it to inject
// lock wait timeouts without holding the board lock for the whole limit.
var readSubscriptionFacts = (*subscription).readContext

// readTolerant returns the next committed facts. A lock wait timeout (the
// read budget expired while the caller context is still live) is retried
// after refresh until consecutive timeouts span subscriptionContentionLimit.
// The contention start lives only in this call, so any success clears it.
// Every other failure, and the end of the caller context during a read, is
// reported through unavailable exactly as a single failed read was before.
func (s *subscription) readTolerant(ctx context.Context, root string, refresh time.Duration, w io.Writer, output *subscriptionOutputQueue) (subscriptionFacts, error) {
	var contendedSince time.Time
	for {
		facts, err := readSubscriptionFacts(s, ctx, root)
		if err == nil {
			return facts, nil
		}
		if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return facts, s.unavailable(w, facts, err)
		}
		now := nowFn()
		if contendedSince.IsZero() {
			contendedSince = now
		}
		if now.Sub(contendedSince) >= subscriptionContentionLimit {
			return facts, s.unavailable(w, facts, err)
		}
		if err := waitContentionRetry(ctx, refresh, output); err != nil {
			return facts, err
		}
	}
}

// waitContentionRetry waits a full refresh. Due heartbeats and dispatch
// confirmation deadlines do not shorten it, and probe results stay in their
// buffered slot until the next successful read polls them.
func waitContentionRetry(ctx context.Context, refresh time.Duration, output *subscriptionOutputQueue) error {
	timer := time.NewTimer(refresh)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-output.done:
		return output.err
	case <-timer.C:
		return nil
	}
}
