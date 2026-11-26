package worker

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/remote"
)

const (
	expiredNote = "dispatch proposal acceptance window elapsed; a new inspected proposal needs a new ID"
	staleNote   = "dispatch proposal targets an earlier card version; inspect the current task before creating a new proposal"

	// reservationNote is the note of the answer that reserves a delivery. The
	// reservation call writes it, and a request found accepted with this note and
	// missing from the local list is read back as reserved but never typed.
	// Requests that older workers reserved carry a different note, so they are
	// never resumed: nothing tells whether their line reached the pane.
	reservationNote = "automatic delivery reserved before typing; outcome is uncertain until observed"
	// reservationKeyPrefix starts the idempotency key of a reservation. It differs
	// from the prefix older workers used, so a pending receipt of an older
	// reservation cannot clash with a new one that carries another note.
	reservationKeyPrefix = "reserve-"
)

// bounded gives one server call of a delivery its own time limit.
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, serverCallTimeout)
}

// deliver hands the open requests of this generation, and the requests it
// reserved but never typed, to the agent once each. For each request it
// resolves the target first, which types nothing; only a resolved target gets a
// durable reservation (or, for a request reserved earlier, a confirmation), and
// only a reserved request is typed. A failed resolution leaves the request as it
// was, open or reserved, and backs the execution off (delivery_block.go). An
// uncertain typing is inspected by the holder instead of being typed again.
func (w *loop) deliver(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, budget *resolveBudget) {
	if w.o.Resolve == nil || w.o.CreateRequestFile == nil {
		return
	}
	readCtx, stopRead := bounded(ctx)
	requests, err := project.Requests(readCtx, c.TaskID)
	stopRead()
	if err != nil {
		log.Warn("reading requests failed", "error", err)
		return
	}
	w.closeSettledRequestFiles(keyOf(*c), requests)
	ready, window, session, deadline, ok := w.readyRequests(ctx, log, project, c, requests)
	if !ok {
		return
	}
	key := keyOf(*c)
	var rejected []string
	// waitingNow is the deliverable requests that are still waiting: a proposal
	// found unusable during this round is not, whether or not its rejection
	// reached the server.
	waitingNow := func() []api.CollabRequest {
		return slices.DeleteFunc(slices.Clone(ready), func(r api.CollabRequest) bool { return slices.Contains(rejected, r.ID) })
	}
	for _, r := range ready {
		if !w.canResolve(key, w.o.Now(), window, session, deadline) {
			break
		}
		if budget.exhausted() {
			log.Debug("target resolution postponed: the budget of this round is spent")
			break
		}
		started := w.o.Now()
		target, err := w.o.Resolve(window, session)
		failedAt := w.o.Now()
		budget.spent += failedAt.Sub(started)
		if err != nil {
			state := w.failResolution(key, window, session, cleanReason(err.Error()), failedAt)
			pending, first := waiting(waitingNow(), c.Delivered)
			log.Warn("request delivery blocked before sending; retrying later",
				"request", first, "pending", pending, "reason", state.reason, "attempt", state.attempts,
				"blocked_since", state.since, "next_attempt", state.next)
			break
		}
		if w.deliverResolved(ctx, log, project, c, r, target, window) {
			rejected = append(rejected, r.ID)
		}
	}
	w.settleBlock(log, key, waitingNow(), c.Delivered)
}

// readyRequests sorts the deliverable requests of the generation: the open ones
// and the ones reserved but never typed. It answers the
// dispatch proposals that cannot be delivered any more (an expired window, an
// earlier card version) and returns the others as the deliverable requests,
// with the card's WINDOW and SESSION and, when a dispatch proposal has an
// acceptance window, the time from which the delivery may try a blocked target
// again. The last result is false when the card could not be read.
func (w *loop) readyRequests(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, requests []api.CollabRequest) (ready []api.CollabRequest, window, session string, deadline time.Time, ok bool) {
	var task *api.Task
	var shown time.Time
	for _, r := range requests {
		if r.Status != api.RequestOpen && !reservedUntyped(r, c) || r.TargetGeneration != c.Generation || slices.Contains(c.Delivered, r.ID) {
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
		if w.rejectUnusable(ctx, log, project, c, r, *task) {
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

// rejectUnusable answers a dispatch proposal that cannot be delivered any more
// with a rejection, and reports whether the proposal is unusable: its acceptance
// window has elapsed on the server clock of the task read, or it binds an
// earlier card version. The rejection is a managed answer: a failed one stays in
// the replay table, and while it is open no other rejection of the request is
// sent; a rejection that was refused or abandoned is not sent again either, and
// the proposal stays open for the holder. The result does not depend on it. Any
// other request is left alone.
func (w *loop) rejectUnusable(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, r api.CollabRequest, task api.Task) bool {
	if r.Dispatch == nil {
		return false
	}
	if w.replayTableOf(keyOf(*c)).rejecting(r.ID) {
		return true
	}
	if !r.Dispatch.ConfirmBy.IsZero() && !task.ObservedAt.Before(r.Dispatch.ConfirmBy) {
		w.rejectExpired(ctx, log, project, c, r)
		return true
	}
	if r.CardRevision != task.Revision {
		body := api.CollabAnswer{Status: api.RequestRejected, ExpectedRevision: r.Revision, Note: staleNote}
		w.submitAnswer(ctx, log, project, *c, &answerAttempt{request: r.ID, kind: answerRejection, key: staleKey(c.ExecutionID, r.ID), body: body})
		return true
	}
	return false
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
	body := api.CollabAnswer{Status: api.RequestRejected, ExpectedRevision: r.Revision, Note: note}
	w.submitAnswer(ctx, log, project, *c, &answerAttempt{request: r.ID, kind: answerRejection, key: expiredKey(c.ExecutionID, r.ID, note), body: body})
}

// reservedUntyped reports a request this worker reserved and never typed, the
// reservation answer lost or the local list unsaved: the request ID goes on the
// local list before the first character is typed, so an ID that is not on it
// was never typed.
func reservedUntyped(r api.CollabRequest, c *remote.Credential) bool {
	return r.Status == api.RequestAccepted && r.Note == reservationNote &&
		r.TargetGeneration == c.Generation && !slices.Contains(c.Delivered, r.ID)
}

// markDelivered adds the request ID to the local list and saves the credential.
// When the save fails, the ID is taken back out of the in-memory list: a later
// save of the same credential must not write an ID that was never typed.
func markDelivered(c *remote.Credential, id string, save func(remote.Credential) error) error {
	c.Delivered = append(c.Delivered, id)
	if err := save(*c); err != nil {
		c.Delivered = c.Delivered[:len(c.Delivered)-1]
		return err
	}
	return nil
}

// recheckProposal checks a dispatch proposal reserved earlier against the card
// as it is after the target resolution and the confirmation, which together may
// have taken as long as the acceptance window had left. A reservation made now
// would have the server check the window and the card version just before the
// typing; the confirmation of a request that is already accepted does not, so
// the worker does. It reports whether the proposal may be typed, and whether it
// was found unusable and a rejection was tried instead (a failed rejection is
// logged); a proposal that could not be checked is neither and waits for the
// next round.
func (w *loop) recheckProposal(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, r api.CollabRequest) (usable, rejected bool) {
	showCtx, stopShow := bounded(ctx)
	task, err := project.Show(showCtx, c.TaskID)
	stopShow()
	if err != nil {
		log.Warn("reading the task failed", "request", r.ID, "error", err)
		return false, false
	}
	if w.rejectUnusable(ctx, log, project, c, r, task) {
		return false, true
	}
	return true, false
}

// deliverResolved reserves the delivery of a request whose target was resolved,
// then types it. A request found reserved but never typed (its reservation
// answer was lost, or its local list entry was not saved) is confirmed with an
// answer that changes nothing instead of being reserved again, and a dispatch
// proposal among them is checked against its window and card version once more
// before it is typed. A reservation or confirmation the server refuses types
// nothing and is tried again in the next round. A typing that fails leaves the
// request reserved and uncertain: the line may have reached the pane, so it is
// never typed again. It reports whether the request was found unusable and a
// rejection was tried instead of a delivery, which takes it out of the requests
// still waiting, whether or not the server received the rejection.
func (w *loop) deliverResolved(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential, r api.CollabRequest, target *Target, window string) (rejected bool) {
	resumed := reservedUntyped(r, c)
	key := reservationKeyPrefix + c.ExecutionID + "-" + r.ID
	if resumed {
		// The server treats the same status, note and revision as the same
		// answer: nothing changes, and it still checks that this execution holds
		// the claim and the generation is current, as a reservation would.
		key = client.NewKey()
	}
	reserveCtx, stopReserve := bounded(ctx)
	accepted, err := project.AnswerRequest(reserveCtx, key, c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: r.Revision, Note: reservationNote}, *c.Claim())
	stopReserve()
	if err != nil {
		log.Warn("reserving request delivery failed", "request", r.ID, "resumed", resumed, "error", err)
		return false
	}
	if resumed && r.Dispatch != nil {
		if usable, rejected := w.recheckProposal(ctx, log, project, c, r); !usable {
			return rejected
		}
	}
	if resumed {
		log.Warn("request reserved earlier and never typed; typing it now", "request", r.ID, "revision", accepted.Revision)
	}
	marker := newAckMarker()
	dir, path, err := w.o.CreateRequestFile(requestBody(c.TaskID, r, marker))
	if err != nil {
		log.Error("private request file could not be created; nothing was typed", "request", r.ID, "error", err)
		return false
	}
	save := func(saved remote.Credential) error { return remote.WriteCredential(w.o.Home, saved) }
	if err := markDelivered(c, r.ID, save); err != nil {
		w.closeRequestDir(log, r.ID, dir)
		log.Error("request delivery intent could not be saved", "request", r.ID, "error", err)
		return false
	}
	err = target.Prompt(requestPointer(c.TaskID, r, path))
	typedAt := w.o.Now()
	scope := w.keepRequestFile(ctx, keyOf(*c), r.ID, dir, typedAt.Add(requestFileRetention))
	if err != nil {
		log.Warn("request delivery outcome unresolved; automatic retry suppressed", "request", r.ID, "error", err)
		return false
	}
	if state, blocked := w.endBlock(keyOf(*c)); blocked {
		log.Info("request delivery unblocked", "blocked_for", w.o.Now().Sub(state.since))
	}
	body := api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: accepted.Revision, Note: typedNote}
	typed, outcome, _ := w.submitAnswer(ctx, log, project, *c, &answerAttempt{request: r.ID, kind: answerTyped, key: contentKey(c.ExecutionID, r.ID, body), body: body})
	// Without a 2xx the typed note may or may not be committed: the result
	// expects the reservation revision, and its fallback covers a committed
	// typed note.
	revision := accepted.Revision
	if outcome == answerSent {
		revision = typed.Revision
	}
	w.startAckObserver(scope, r.ID, revision, typedAt, cmp.Or(target.Window, window), marker, target.Seen)
	log.Info("request typed", "request", r.ID, "kind", r.Kind)
	return false
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
