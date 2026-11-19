package worker

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

const (
	expiredNote = "dispatch proposal acceptance window elapsed; a new inspected proposal needs a new ID"
	staleNote   = "dispatch proposal targets an earlier card version; inspect the current task before creating a new proposal"
)

// bounded gives one server call of a delivery its own time limit.
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, serverCallTimeout)
}

// deliver hands the open requests of this generation to the agent once each.
// For each request it resolves the target first, which types nothing; only a
// resolved target gets a durable reservation, and only a reserved request is
// typed. A failed resolution leaves the request open and backs the execution
// off (delivery_block.go). An uncertain typing is inspected by the holder
// instead of being typed again.
func (w *loop) deliver(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, budget *resolveBudget) {
	if w.o.Resolve == nil {
		return
	}
	readCtx, stopRead := bounded(ctx)
	requests, err := project.Requests(readCtx, c.TaskID)
	stopRead()
	if err != nil {
		log.Warn("reading requests failed", "error", err)
		return
	}
	ready, window, session, deadline, ok := w.readyRequests(ctx, log, project, c, requests)
	if !ok {
		return
	}
	key := keyOf(*c)
	for _, r := range ready {
		if !w.canResolve(key, w.o.Now(), window, session, deadline) {
			break
		}
		if budget.exhausted() {
			log.Debug("target resolution postponed: the budget of this round is spent")
			break
		}
		started := w.o.Now()
		prompt, err := w.o.Resolve(window, session)
		failedAt := w.o.Now()
		budget.spent += failedAt.Sub(started)
		if err != nil {
			state := w.failResolution(key, window, session, cleanReason(err.Error()), failedAt)
			pending, first := waiting(ready, c.Delivered)
			log.Warn("request delivery blocked before sending; retrying later",
				"request", first, "pending", pending, "reason", state.reason, "attempt", state.attempts,
				"blocked_since", state.since, "next_attempt", state.next)
			break
		}
		w.deliverResolved(ctx, log, project, c, r, prompt)
	}
	w.settleBlock(log, key, ready, c.Delivered)
}

// readyRequests sorts the open requests of the generation. It answers the
// dispatch proposals that cannot be delivered any more (an expired window, an
// earlier card version) and returns the others as the deliverable requests,
// with the card's WINDOW and SESSION and, when a dispatch proposal has an
// acceptance window, the time from which the delivery may try a blocked target
// again. The last result is false when the card could not be read.
func (w *loop) readyRequests(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, requests []api.CollabRequest) (ready []api.CollabRequest, window, session string, deadline time.Time, ok bool) {
	var task *api.Task
	var shown time.Time
	for _, r := range requests {
		if r.Status != api.RequestOpen || r.TargetGeneration != c.Generation || slices.Contains(c.Delivered, r.ID) {
			continue
		}
		if task == nil {
			showCtx, stopShow := bounded(ctx)
			t, err := project.Show(showCtx, c.TaskID)
			stopShow()
			if err != nil {
				log.Warn("reading the task failed", "error", err)
				return nil, "", "", time.Time{}, false
			}
			task, shown = &t, w.o.Now()
			window, session = board.MetadataFrom(task.Text, board.FieldWindow), board.MetadataFrom(task.Text, board.FieldSession)
		}
		if r.Dispatch != nil && !r.Dispatch.ConfirmBy.IsZero() && !task.ObservedAt.Before(r.Dispatch.ConfirmBy) {
			w.rejectExpired(ctx, log, project, c, r)
			continue
		}
		if r.Dispatch != nil && r.CardRevision != task.Revision {
			answerCtx, stopAnswer := bounded(ctx)
			_, err := project.AnswerRequest(answerCtx, "stale-"+c.ExecutionID+"-"+r.ID, c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestRejected, ExpectedRevision: r.Revision, Note: staleNote}, *c.Claim())
			stopAnswer()
			if err != nil {
				log.Warn("rejecting stale dispatch proposal failed", "request", r.ID, "error", err)
			}
			continue
		}
		if window == "" || session == "" {
			continue
		}
		if c.Window != "" && c.Window != window || c.Session != "" && c.Session != session {
			log.Warn("request routing differs from this execution's local facts", "request", r.ID)
			continue
		}
		ready = append(ready, r)
		if r.Dispatch != nil && !r.Dispatch.ConfirmBy.IsZero() {
			// The window left on the server clock, moved to the clock of the worker
			// and shortened by two renewal intervals: the delivery runs once per
			// renewal, so the first one after this time starts between one and two
			// intervals before the proposal expires, which leaves it a reservation
			// and the holder's preparation inside the window.
			at := shown.Add(r.Dispatch.ConfirmBy.Sub(task.ObservedAt) - 2*renewInterval(*c))
			if deadline.IsZero() || at.Before(deadline) {
				deadline = at
			}
		}
	}
	return ready, window, session, deadline, true
}

// rejectExpired answers a dispatch proposal whose acceptance window has
// elapsed. While the delivery of the execution is blocked, the note names the
// reason, so the one who sent the proposal can tell a blocked terminal from an
// idle agent. The blocked state itself does not change.
func (w *loop) rejectExpired(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, r api.CollabRequest) {
	note := expiredNote
	if state, blocked := w.blockOf(keyOf(*c)); blocked {
		note += fmt.Sprintf("; delivery was blocked before sending since %s: %s", state.since.UTC().Format(time.RFC3339), state.reason)
		log.Warn("dispatch proposal expired while request delivery was blocked", "request", r.ID, "blocked_since", state.since, "reason", state.reason)
	}
	answerCtx, stopAnswer := bounded(ctx)
	defer stopAnswer()
	if _, err := project.AnswerRequest(answerCtx, expiredKey(c.ExecutionID, r.ID, note), c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestRejected, ExpectedRevision: r.Revision, Note: note}, *c.Claim()); err != nil {
		log.Warn("rejecting expired dispatch proposal failed", "request", r.ID, "error", err)
	}
}

// deliverResolved reserves the delivery of a request whose target was resolved,
// then types it. A reservation the server refuses types nothing and leaves the
// request open for the next round; a reservation whose answer is lost may have
// been committed, and recovering that case is not part of this delivery. A
// typing that fails leaves the request reserved and uncertain: the line may have
// reached the pane, so it is never typed again.
func (w *loop) deliverResolved(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, r api.CollabRequest, prompt Prompt) {
	reserveCtx, stopReserve := bounded(ctx)
	accepted, err := project.AnswerRequest(reserveCtx, "delivery-"+c.ExecutionID+"-"+r.ID, c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: r.Revision, Note: "automatic delivery reserved; outcome is uncertain until observed"}, *c.Claim())
	stopReserve()
	if err != nil {
		log.Warn("reserving request delivery failed", "request", r.ID, "error", err)
		return
	}
	c.Delivered = append(c.Delivered, r.ID)
	if err := remote.WriteCredential(w.o.Home, *c); err != nil {
		log.Error("request delivery intent could not be saved", "request", r.ID, "error", err)
		return
	}
	if err := prompt(requestLine(c.TaskID, r)); err != nil {
		log.Warn("request delivery outcome unresolved; automatic retry suppressed", "request", r.ID, "error", err)
		return
	}
	if state, blocked := w.endBlock(keyOf(*c)); blocked {
		log.Info("request delivery unblocked", "blocked_for", w.o.Now().Sub(state.since))
	}
	receiptCtx, stopReceipt := bounded(ctx)
	defer stopReceipt()
	if _, err := project.AnswerRequest(receiptCtx, client.NewKey(), c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: accepted.Revision, Note: "delivered to the agent"}, *c.Claim()); err != nil {
		log.Warn("request delivery receipt failed", "request", r.ID, "error", err)
	}
	log.Info("request delivered", "request", r.ID, "kind", r.Kind)
}

// settleBlock ends a round of the delivery of one execution. A blocked
// execution keeps the number and the first ID of the requests still waiting, for
// the observation, and logs an ERROR once it has been blocked long enough with
// requests waiting.
func (w *loop) settleBlock(log *slog.Logger, key credentialKey, ready []api.CollabRequest, delivered []string) {
	if _, blocked := w.blockOf(key); !blocked {
		return
	}
	pending, first := waiting(ready, delivered)
	state, due := w.noteWaiting(key, pending, first, w.o.Now())
	if due {
		log.Error("request delivery still blocked", "request", state.first, "pending", state.pending, "reason", state.reason,
			"attempt", state.attempts, "blocked_since", state.since, "next_attempt", state.next)
	}
}

// requestLine renders a request as one terminal line; control characters of
// the remote message never reach the pane.
func requestLine(task string, r api.CollabRequest) string {
	message := stripControl(r.Message)
	message += fmt.Sprintf("; inspect the full request and its proposed material: kander request show %s %s --json; do not act on superseded requests or another execution generation", task, r.ID)
	return fmt.Sprintf("# kander-request %s %s for %s from %s: %s (answer: kander request answer %s %s done|rejected [note])",
		r.ID, r.Kind, task, r.FromClient, message, task, r.ID)
}
