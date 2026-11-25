package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/config"
)

const (
	// blockBackoffFirst is the wait after the first failure of a target
	// resolution; every further failure doubles it up to blockBackoffMax. The
	// first wait is two renewal intervals, long enough for a short cause (an
	// agent that is blocked on a confirmation for a moment) to pass, and the cap
	// keeps a recovered terminal from waiting longer than five minutes.
	blockBackoffFirst = 30 * time.Second
	blockBackoffMax   = 300 * time.Second
	// blockErrorAfter is how long a delivery stays blocked, with requests still
	// waiting, before the worker log gets an ERROR.
	blockErrorAfter = 10 * time.Minute
	// resolveRoundBudget caps the target resolutions of one work round. A
	// resolution waits up to ten seconds for a busy agent, and the work loop
	// serves the credentials one after the other: without the cap a round with
	// several due executions would delay the observation reports and the stop of
	// a lost agent of the credentials behind them. The budget is checked before
	// each resolution, so a round holds at most about 30 + 10 seconds of them.
	resolveRoundBudget = 30 * time.Second
	// serverCallTimeout bounds each server call of a delivery. A call does not
	// share a deadline with the resolution before it: a resolution may take the
	// whole of it, and the reservation after it must still go through.
	serverCallTimeout = 10 * time.Second
	// blockReasonLimit bounds the failure reason kept in a blocked state, in
	// bytes.
	blockReasonLimit = 512
	// observationReasonLimit is the limit the server puts on an observation reason.
	observationReasonLimit = 4096
)

// blockState is the delivery state of an execution after a failed target
// resolution. It lives in the memory of the worker only: nothing was typed
// before the failure, so a worker that starts again resolves at once.
type blockState struct {
	// since is the failure time of the first failure of this blocked period. It
	// is not reset by an expired proposal or a changed target.
	since time.Time
	// attempts counts the failures in a row against the current target; next is
	// the earliest time of the next resolution the backoff allows.
	attempts int
	next     time.Time
	reason   string
	// window and session are the card values of the latest failure. A card that
	// names another target is not waited for.
	window, session string
	// pending and first describe the deliverable requests at the end of the last
	// round: their number and the ID of the first.
	pending     int
	first       string
	errorLogged bool
}

// blockBackoff is the wait after the n-th failure in a row: 30, 60, 120, 240,
// then 300 seconds.
func blockBackoff(attempts int) time.Duration {
	wait := blockBackoffFirst
	for i := 1; i < attempts && wait < blockBackoffMax; i++ {
		wait *= 2
	}
	return min(wait, blockBackoffMax)
}

// resolveBudget is the time the target resolutions of one work round have used.
type resolveBudget struct{ spent time.Duration }

func (b *resolveBudget) exhausted() bool { return b.spent >= resolveRoundBudget }

// canResolve reports whether the delivery of the execution may resolve its
// target now. An execution that is not blocked may. A blocked one may when the
// card names another target, when the backoff is over, or when the deadline of
// a dispatch proposal has come; deadline is zero without such a proposal.
func (w *loop) canResolve(key credentialKey, now time.Time, window, session string, deadline time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, blocked := w.blocked[key]
	switch {
	case !blocked, state.window != window, state.session != session, !now.Before(state.next):
		return true
	}
	return !deadline.IsZero() && !now.Before(deadline)
}

// failResolution records a failed target resolution of the execution and
// returns the state after it.
func (w *loop) failResolution(key credentialKey, window, session, reason string, now time.Time) blockState {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, blocked := w.blocked[key]
	if !blocked {
		state = blockState{since: now}
	}
	if blocked && state.window == window && state.session == session {
		state.attempts++
	} else {
		state.attempts = 1
	}
	state.window, state.session, state.reason = window, session, reason
	state.next = now.Add(blockBackoff(state.attempts))
	w.blocked[key] = state
	return state
}

// endBlock ends the blocked period of the execution and returns it.
func (w *loop) endBlock(key credentialKey) (blockState, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, blocked := w.blocked[key]
	delete(w.blocked, key)
	return state, blocked
}

func (w *loop) blockOf(key credentialKey) (blockState, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, blocked := w.blocked[key]
	return state, blocked
}

// noteWaiting stores the deliverable requests the last round left, when the
// execution is blocked, and returns the state. The second result is whether an
// ERROR is due: the delivery has been blocked for blockErrorAfter with requests
// waiting, and the period has not logged one yet.
func (w *loop) noteWaiting(key credentialKey, pending int, first string, now time.Time) (blockState, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, blocked := w.blocked[key]
	if !blocked {
		return state, false
	}
	state.pending, state.first = pending, first
	due := pending > 0 && !state.errorLogged && now.Sub(state.since) >= blockErrorAfter
	if due {
		state.errorLogged = true
	}
	w.blocked[key] = state
	return state, due
}

// forgetBlocks drops the blocked state of every execution outside live.
func (w *loop) forgetBlocks(live map[credentialKey]struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key := range w.blocked {
		if _, ok := live[key]; !ok {
			delete(w.blocked, key)
		}
	}
}

// withBlockedSuffix adds the delivery state to the reason of an agent
// observation: a blocked execution with requests waiting says so to the
// readers of the observation on the server. The suffix depends on the blocked
// state only, so it is the same in two reports while that state is.
func (w *loop) withBlockedSuffix(key credentialKey, reason string) string {
	state, blocked := w.blockOf(key)
	if !blocked || state.pending == 0 {
		return reason
	}
	suffix := config.Text("worker.request_delivery_blocked", state.pending, state.first, state.since.UTC().Format(time.RFC3339), state.reason)
	if reason == "" {
		return suffix
	}
	room := observationReasonLimit - len(suffix) - len("; ")
	if room <= 0 {
		return suffix
	}
	return truncateUTF8(reason, room) + "; " + suffix
}

// expiredKey is the idempotency key of the rejection of an expired dispatch
// proposal. The note is part of it: the note names the blocked state, which
// changes between two attempts, and a fixed key would answer the second one with
// an idempotency mismatch whenever the first ended in a server error.
func expiredKey(execution, request, note string) string {
	sum := sha256.Sum256([]byte(execution + "\x00" + request + "\x00" + note))
	return "expired-" + hex.EncodeToString(sum[:])[:32]
}

// cleanReason makes an error text fit a log field and an observation: control
// characters become spaces, white space is compressed, and the text is cut at a
// character boundary.
func cleanReason(text string) string {
	return truncateUTF8(strings.Join(strings.Fields(stripControl(text)), " "), blockReasonLimit)
}

// stripControl replaces the control characters and the Unicode line separators
// of text with spaces.
func stripControl(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, text)
}

// truncateUTF8 cuts text to at most limit bytes without splitting a character.
func truncateUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

// waiting returns the number of requests of ready whose ID is not on the local
// list, so that nothing was typed for them, and the ID of the first.
func waiting(ready []api.CollabRequest, delivered []string) (int, string) {
	count, first := 0, ""
	for _, r := range ready {
		if slices.Contains(delivered, r.ID) {
			continue
		}
		if count == 0 {
			first = r.ID
		}
		count++
	}
	return count, first
}
