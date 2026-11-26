package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/remote"
)

// The managed answers of the worker are the typed note, the acknowledgement
// result (and its fallback attempt) and the two rejections of a dispatch
// proposal. An embedder may append its audit only after Kander answered 2xx and
// fail the call afterwards, so a write can be committed while the worker sees
// an error. Every managed answer therefore goes through the replay table: it is
// kept until it gets a 2xx or a deterministic refusal, and it is sent again
// with the same idempotency key and the same body, so the server returns the
// stored receipt and the embedder can complete its audit.
const (
	// answerRetention bounds how long an attempt is replayed, counted from its
	// first send. It matches the request file retention.
	answerRetention = 30 * time.Minute
	// answerBackoffFirst is the wait after the first transient failure of an
	// attempt; every further failure doubles it up to answerBackoffMax.
	answerBackoffFirst = 5 * time.Second
	answerBackoffMax   = 60 * time.Second
	// answerWarnEvery throttles the WARN of an attempt that keeps failing.
	answerWarnEvery = 5 * time.Minute
)

// answerKind is the kind of a managed answer. A request has at most one open
// attempt of each kind.
type answerKind string

const (
	answerTyped     answerKind = "typed"
	answerResult    answerKind = "acknowledgement"
	answerRejection answerKind = "rejection"
)

// answerAttempt is one managed answer. key and body never change once the
// attempt exists: every send of the attempt uses both as they are.
type answerAttempt struct {
	request   string
	kind      answerKind
	key       string
	body      api.CollabAnswer
	firstSent time.Time
	// rebuilt marks an attempt derived from the server state at startup.
	rebuilt bool
	// failures counts the transient failures in a row; next is the earliest
	// time of the next send.
	failures int
	next     time.Time
	warnedAt time.Time
	// typedAt and window fill the log of an acknowledgement result.
	typedAt time.Time
	window  string
}

// replayTable holds the open attempts of one credential in the order they were
// added, and whether the attempts of this credential were rebuilt in this
// process. Only the work loop uses it.
type replayTable struct {
	attempts []*answerAttempt
	rebuilt  bool
	// closedRejections holds the requests whose rejection left the table: a
	// rejection that was refused or abandoned leaves the proposal open, and the
	// delivery must not register it again.
	closedRejections map[string]struct{}
}

// contentKey is the idempotency key of a typed note or an acknowledgement
// result: it follows from the execution, the request and the whole body, so the
// same key always carries the same body and a restarted worker can compute it
// again from the request as the server shows it.
func contentKey(execution, request string, answer api.CollabAnswer) string {
	material := strings.Join([]string{execution, request, strconv.FormatUint(answer.ExpectedRevision, 10), answer.Status, answer.Note}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return "answer-" + hex.EncodeToString(sum[:])[:32]
}

func staleKey(execution, request string) string { return "stale-" + execution + "-" + request }

// answerBackoff is the wait after the failures-th transient failure in a row.
func answerBackoff(failures int) time.Duration {
	wait := answerBackoffFirst
	for i := 1; i < failures && wait < answerBackoffMax; i++ {
		wait *= 2
	}
	return min(wait, answerBackoffMax)
}

// answerRefusal returns the code of a deterministic refusal of Kander: the
// answer was not applied and sending it again changes nothing. Every other
// failure is transient: a transport error, any HTTP 5xx whatever its body says
// (an embedder may wrap an upstream body in a 503), and an error code of an
// embedder.
func answerRefusal(err error) (string, bool) {
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Status >= 500 {
		return "", false
	}
	switch failure.Code {
	case api.CodeRevisionConflict, api.CodeLeaseInvalid, api.CodeTakenOver, api.CodeClaimRequired, api.CodeHeldByOther,
		api.CodeNotFound, api.CodeIdempotencyMismatch, api.CodeInvalidRequest, api.CodeEvidenceIncomplete:
		return failure.Code, true
	default:
		return "", false
	}
}

func (w *loop) replayTableOf(key credentialKey) *replayTable {
	table := w.replays[key]
	if table == nil {
		table = &replayTable{}
		w.replays[key] = table
	}
	return table
}

func (t *replayTable) pending(request string, kind answerKind) *answerAttempt {
	i := slices.IndexFunc(t.attempts, func(a *answerAttempt) bool { return a.request == request && a.kind == kind })
	if i == -1 {
		return nil
	}
	return t.attempts[i]
}

func (t *replayTable) remove(a *answerAttempt) {
	t.attempts = slices.DeleteFunc(t.attempts, func(open *answerAttempt) bool { return open == a })
	if a.kind == answerRejection {
		if t.closedRejections == nil {
			t.closedRejections = map[string]struct{}{}
		}
		t.closedRejections[a.request] = struct{}{}
	}
}

// rejecting reports whether a rejection of the request is open or already
// left the table, so the delivery treats the proposal as rejected.
func (t *replayTable) rejecting(request string) bool {
	_, closed := t.closedRejections[request]
	return closed || t.pending(request, answerRejection) != nil
}

func (t *replayTable) replace(old, next *answerAttempt) {
	if i := slices.Index(t.attempts, old); i != -1 {
		t.attempts[i] = next
		return
	}
	t.attempts = append(t.attempts, next)
}

// forgetReplays drops the attempts of a credential that is gone or lost: no
// answer is written for it any more.
func (w *loop) forgetReplays(key credentialKey) { delete(w.replays, key) }

// answerOutcome is the result of one send of an attempt.
type answerOutcome int

const (
	answerSent answerOutcome = iota
	answerRefusedNow
	answerKept
	// answerAbandoned means the attempt reached its retention before the send;
	// nothing was sent.
	answerAbandoned
	// answerDuplicate means an attempt of the same kind is already open for
	// the request; nothing was sent.
	answerDuplicate
)

// submitAnswer adds a new managed answer to the replay table and sends it.
func (w *loop) submitAnswer(ctx context.Context, log *slog.Logger, project *client.Project, c remote.Credential, a *answerAttempt) (api.CollabRequest, answerOutcome, error) {
	table := w.replayTableOf(keyOf(c))
	if table.pending(a.request, a.kind) != nil {
		return api.CollabRequest{}, answerDuplicate, nil
	}
	a.firstSent = w.o.Now()
	table.attempts = append(table.attempts, a)
	return w.sendAnswer(ctx, log, project, c, table, a, true)
}

// sendAnswer sends an open attempt and classifies the result: a 2xx or a
// deterministic refusal settles it, anything else keeps it for a send after
// its backoff. With fallback set, a revision conflict of an acknowledgement
// result goes through fallBack.
func (w *loop) sendAnswer(ctx context.Context, log *slog.Logger, project *client.Project, c remote.Credential, table *replayTable, a *answerAttempt, fallback bool) (api.CollabRequest, answerOutcome, error) {
	// Checked before every send, not once per round: an earlier call of the
	// same round, or the read of a fallback, can take the clock past the
	// retention.
	if w.abandonExpired(log, table, a) {
		return api.CollabRequest{}, answerAbandoned, nil
	}
	callCtx, cancel := bounded(ctx)
	answered, err := project.AnswerRequest(callCtx, a.key, c.TaskID, a.request, a.body, *c.Claim())
	cancel()
	code, refused := answerRefusal(err)
	switch {
	case err == nil:
		table.remove(a)
		w.logSettled(log, a)
		return answered, answerSent, nil
	case refused && fallback && a.kind == answerResult && !a.rebuilt && code == api.CodeRevisionConflict:
		w.fallBack(ctx, log, project, c, table, a)
		return answered, answerRefusedNow, err
	case refused:
		table.remove(a)
		if code == api.CodeIdempotencyMismatch {
			log.Error("request answer key reused with another body", "request", a.request, "kind", a.kind, "key", a.key, "error", err)
		} else {
			log.Debug("request answer refused", "request", a.request, "kind", a.kind, "rebuilt", a.rebuilt, "error", err)
		}
		return answered, answerRefusedNow, err
	default:
		w.keepFailed(log, a, err)
		return answered, answerKept, err
	}
}

// keepFailed schedules the next send of an attempt after a transient failure
// and logs it: a WARN on the first failure and then at most one per
// answerWarnEvery, DEBUG otherwise.
func (w *loop) keepFailed(log *slog.Logger, a *answerAttempt, err error) {
	now := w.o.Now()
	a.failures++
	a.next = now.Add(answerBackoff(a.failures))
	if a.failures == 1 || now.Sub(a.warnedAt) >= answerWarnEvery {
		a.warnedAt = now
		log.Warn("writing the request answer failed; retrying later", "request", a.request, "kind", a.kind,
			"attempt", a.failures, "next_attempt", a.next, "error", err)
		return
	}
	log.Debug("writing the request answer failed; retrying later", "request", a.request, "kind", a.kind,
		"attempt", a.failures, "next_attempt", a.next, "error", err)
}

func (w *loop) logSettled(log *slog.Logger, a *answerAttempt) {
	switch {
	case a.rebuilt:
		log.Debug("rebuilt request answer settled", "request", a.request, "kind", a.kind)
	case a.kind != answerResult:
	case a.body.Note == ackedNote:
		log.Info("request acknowledged", "request", a.request)
	default:
		log.Warn("request not acknowledged", "request", a.request, "reason", unconfirmedNote,
			"typed_at", a.typedAt, "window", ackWindow, "target", a.window)
	}
}

// abandonExpired drops an attempt once its retention, counted from its first
// send, has passed, and reports whether it did.
func (w *loop) abandonExpired(log *slog.Logger, table *replayTable, a *answerAttempt) bool {
	if w.o.Now().Before(a.firstSent.Add(answerRetention)) {
		return false
	}
	table.remove(a)
	log.Warn("request answer abandoned: still unsettled after its retention", "request", a.request, "kind", a.kind,
		"key", a.key, "first_sent", a.firstSent, "retention", answerRetention)
	return true
}

// replayAnswers drops the attempts past their retention and sends the ones
// whose backoff has elapsed, in the order they were added.
func (w *loop) replayAnswers(ctx context.Context, log *slog.Logger, project *client.Project, c remote.Credential) {
	table := w.replays[keyOf(c)]
	if table == nil {
		return
	}
	for _, a := range slices.Clone(table.attempts) {
		if ctx.Err() != nil {
			return
		}
		if w.abandonExpired(log, table, a) || w.o.Now().Before(a.next) {
			continue
		}
		w.sendAnswer(ctx, log, project, c, table, a, true)
	}
}

// fallBack handles an acknowledgement result the server refused with a
// revision conflict. A request still accepted with the note this worker wrote
// before the result (typed, or reserved when the typed note was lost) gets the
// result again at its current revision, as a new attempt in place of the old
// one, sent at once; any other request has been answered by someone else and
// keeps its answer. A failed read keeps the old attempt as a transient failure.
func (w *loop) fallBack(ctx context.Context, log *slog.Logger, project *client.Project, c remote.Credential, table *replayTable, a *answerAttempt) {
	readCtx, stopRead := bounded(ctx)
	requests, err := project.Requests(readCtx, c.TaskID)
	stopRead()
	if err != nil {
		w.keepFailed(log, a, err)
		return
	}
	w.closeSettledRequestFiles(keyOf(c), requests)
	next := fallbackAttempt(c, a, requests)
	if next == nil {
		table.remove(a)
		log.Debug("request acknowledgement dropped: the request was answered otherwise", "request", a.request)
		return
	}
	table.replace(a, next)
	// One fallback per request and round: a second conflict means the request
	// moved again, and the result is not written.
	w.sendAnswer(ctx, log, project, c, table, next, false)
}

// fallbackAttempt is the attempt that replaces an acknowledgement result after
// a revision conflict, or nil when the request is no longer one this worker
// may acknowledge. The replacement keeps the first send time, so the retention
// is not restarted, and starts its backoff afresh.
func fallbackAttempt(c remote.Credential, a *answerAttempt, requests []api.CollabRequest) *answerAttempt {
	i := slices.IndexFunc(requests, func(r api.CollabRequest) bool { return r.ID == a.request })
	if i == -1 {
		return nil
	}
	current := requests[i]
	if current.TargetGeneration != c.Generation || current.Status != api.RequestAccepted ||
		(current.Note != typedNote && current.Note != reservationNote) {
		return nil
	}
	body := api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: current.Revision, Note: a.body.Note}
	return &answerAttempt{
		request: a.request, kind: answerResult, key: contentKey(c.ExecutionID, a.request, body), body: body,
		firstSent: a.firstSent, typedAt: a.typedAt, window: a.window,
	}
}

// rebuildAnswers runs once per credential and process. It derives from the
// requests as the server shows them the managed answers this execution may
// have written last and whose audit may still be open, and adds them as
// attempts. Each derived attempt expects a revision below the current one, so
// the server never applies it: a committed key returns its stored receipt, any
// other key a refusal or a transient failure, and the request does not change.
// A failed read is tried again in the next round.
func (w *loop) rebuildAnswers(ctx context.Context, log *slog.Logger, project *client.Project, c remote.Credential) {
	table := w.replayTableOf(keyOf(c))
	if table.rebuilt {
		return
	}
	readCtx, stopRead := bounded(ctx)
	requests, err := project.Requests(readCtx, c.TaskID)
	stopRead()
	if err != nil {
		log.Debug("reading requests to rebuild answers failed; retrying next round", "error", err)
		return
	}
	table.rebuilt = true
	now := w.o.Now()
	for _, r := range requests {
		for _, a := range rebuiltAttempts(c, r, now) {
			if table.pending(a.request, a.kind) != nil {
				continue
			}
			a.firstSent, a.rebuilt = now, true
			table.attempts = append(table.attempts, a)
		}
	}
}

// rebuiltAttempts derives the attempts of one request; see rebuildAnswers.
// The typed note before an acknowledgement result comes first.
func rebuiltAttempts(c remote.Credential, r api.CollabRequest, now time.Time) []*answerAttempt {
	if r.TargetGeneration != c.Generation || now.Sub(r.UpdatedAt) > answerRetention || r.Revision < 2 {
		return nil
	}
	last := api.CollabAnswer{Status: r.Status, ExpectedRevision: r.Revision - 1, Note: r.Note}
	switch {
	case r.Status == api.RequestAccepted && r.Note == typedNote:
		return []*answerAttempt{{request: r.ID, kind: answerTyped, key: contentKey(c.ExecutionID, r.ID, last), body: last}}
	case r.Status == api.RequestAccepted && (r.Note == ackedNote || r.Note == unconfirmedNote):
		var attempts []*answerAttempt
		if r.Revision >= 3 {
			typed := api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: r.Revision - 2, Note: typedNote}
			attempts = append(attempts, &answerAttempt{request: r.ID, kind: answerTyped, key: contentKey(c.ExecutionID, r.ID, typed), body: typed})
		}
		return append(attempts, &answerAttempt{request: r.ID, kind: answerResult, key: contentKey(c.ExecutionID, r.ID, last), body: last})
	case r.Status == api.RequestRejected && r.Note == staleNote:
		return []*answerAttempt{{request: r.ID, kind: answerRejection, key: staleKey(c.ExecutionID, r.ID), body: last}}
	case r.Status == api.RequestRejected && strings.HasPrefix(r.Note, expiredNote):
		return []*answerAttempt{{request: r.ID, kind: answerRejection, key: expiredKey(c.ExecutionID, r.ID, r.Note), body: last}}
	}
	return nil
}
