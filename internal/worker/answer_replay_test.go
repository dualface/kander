package worker

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/remote"
)

func TestContentKeyFollowsEveryPartOfTheAnswer(t *testing.T) {
	body := api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: 7, Note: typedNote}
	key := contentKey("exec-a", "request-1", body)
	if again := contentKey("exec-a", "request-1", body); again != key {
		t.Fatalf("the same answer gets the same key: %s and %s", key, again)
	}
	if !api.ValidIdentity(key) || !strings.HasPrefix(key, "answer-") || len(key) != len("answer-")+32 {
		t.Fatalf("the key is an identity of a fixed length: %q", key)
	}
	for name, other := range map[string]string{
		"execution": contentKey("exec-b", "request-1", body),
		"request":   contentKey("exec-a", "request-2", body),
		"revision":  contentKey("exec-a", "request-1", api.CollabAnswer{Status: body.Status, ExpectedRevision: 8, Note: body.Note}),
		"status":    contentKey("exec-a", "request-1", api.CollabAnswer{Status: api.RequestDone, ExpectedRevision: 7, Note: body.Note}),
		"note":      contentKey("exec-a", "request-1", api.CollabAnswer{Status: body.Status, ExpectedRevision: 7, Note: ackedNote}),
	} {
		if other == key {
			t.Fatalf("a different %s gets the same key", name)
		}
	}
}

func TestTransientFailuresKeepTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply gatewayReply
	}{
		{"503 not_ready", gatewayReply{mode: gatewayRefuse, status: http.StatusServiceUnavailable, code: api.CodeNotReady}},
		{"502", gatewayReply{mode: gatewayRefuse, status: http.StatusBadGateway, code: api.CodeInternal}},
		{"500 internal", gatewayReply{mode: gatewayRefuse, status: http.StatusInternalServerError, code: api.CodeInternal}},
		{"403 forbidden", gatewayReply{mode: gatewayRefuse, status: http.StatusForbidden, code: api.CodeForbidden}},
		{"transport error", gatewayReply{mode: gatewayHangUp}},
		// A 5xx is transient whatever code its body carries.
		{"503 with revision_conflict", gatewayReply{mode: gatewayRefuse, status: http.StatusServiceUnavailable, code: api.CodeRevisionConflict}},
		{"502 with not_found", gatewayReply{mode: gatewayRefuse, status: http.StatusBadGateway, code: api.CodeNotFound}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newReplayRig(t, "transient", func(send gatewaySend, _ int) gatewayReply {
				if send.answer.Note == typedNote {
					return tc.reply
				}
				return gatewayReply{}
			})
			rig.h.workAfter(16 * time.Second)
			if open := rig.h.openAttempts(rig.c); len(open) != 1 || open[0].kind != answerTyped {
				t.Fatalf("the typed note stays open: %+v", open)
			}
			rig.h.workAfter(answerBackoffFirst)
			// The HTTP transport itself may resend a request that carries an
			// idempotency key after a closed connection, so the test counts
			// the rounds that sent, not the sends.
			sends := rig.g.sendsOf(rig.r.ID, typedNote)
			if first, last := sends[0], sends[len(sends)-1]; last.at.Sub(first.at) != answerBackoffFirst {
				t.Fatalf("the typed note is replayed after its backoff: %v", sends)
			}
			requireOneAttempt(t, "the typed note", sends)
			rig.g.requireStableSends(t)
		})
	}
}

func TestDeterministicRefusalsDropTheAnswer(t *testing.T) {
	for _, code := range []string{
		api.CodeRevisionConflict, api.CodeLeaseInvalid, api.CodeTakenOver, api.CodeClaimRequired, api.CodeHeldByOther,
		api.CodeNotFound, api.CodeIdempotencyMismatch, api.CodeInvalidRequest, api.CodeEvidenceIncomplete,
	} {
		t.Run(code, func(t *testing.T) {
			rig := newReplayRig(t, "refused", func(send gatewaySend, _ int) gatewayReply {
				if send.answer.Note == typedNote {
					return gatewayReply{mode: gatewayRefuse, status: http.StatusConflict, code: code}
				}
				return gatewayReply{}
			})
			rig.h.workAfter(16 * time.Second)
			if open := rig.h.openAttempts(rig.c); len(open) != 0 {
				t.Fatalf("a refused typed note is dropped: %+v", open)
			}
			rig.h.workAfter(answerBackoffMax)
			rig.h.workAfter(answerBackoffMax)
			if sends := rig.g.sendsOf(rig.r.ID, typedNote); len(sends) != 1 {
				t.Fatalf("a refused typed note is not sent again: %v", sends)
			}
		})
	}
}

// alwaysFailingTypedNote is a rig whose typed note never gets past the
// gateway.
func alwaysFailingTypedNote(t *testing.T, slug string) *replayRig {
	return newReplayRig(t, slug, func(send gatewaySend, _ int) gatewayReply {
		if send.answer.Note == typedNote {
			return gatewayReply{mode: gatewayRefuse, status: http.StatusServiceUnavailable, code: api.CodeNotReady}
		}
		return gatewayReply{}
	})
}

// A rejection that left the table without reaching Kander leaves the proposal
// open; the delivery of later renewals does not register it again.
func TestAClosedRejectionIsNotRegisteredAgain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply gatewayReply
		wait  time.Duration
	}{
		{"refused", gatewayReply{mode: gatewayRefuse, status: http.StatusConflict, code: api.CodeEvidenceIncomplete}, 0},
		{"abandoned", gatewayReply{mode: gatewayRefuse, status: http.StatusServiceUnavailable, code: api.CodeNotReady}, answerRetention},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGateway(func(send gatewaySend, _ int) gatewayReply {
				if strings.HasPrefix(send.answer.Note, expiredNote) {
					return tc.reply
				}
				return gatewayReply{}
			})
			h := g.harness(t)
			_, c := h.claimed("closed-rejection-"+tc.name, true)
			r := h.proposal(c, "closed-rejection-"+tc.name, 30*time.Second)
			h.workAfter(31 * time.Second)
			for left := tc.wait; left > 0; left -= answerBackoffMax {
				h.workAfter(answerBackoffMax)
			}
			if len(h.openAttempts(c)) != 0 {
				t.Fatalf("the rejection left the table: %+v", h.openAttempts(c))
			}
			sent := len(expiredSends(g, r.ID))
			for range 3 {
				h.workAfter(answerBackoffMax)
			}
			if got := len(expiredSends(g, r.ID)); got != sent {
				t.Fatalf("the delivery registered the rejection again: %d sends, want %d", got, sent)
			}
			if got := h.requests(c)[0]; got.Status != api.RequestOpen {
				t.Fatalf("the proposal stays open for the holder: %+v", got)
			}
			requireOneAttempt(t, "the rejection", expiredSends(g, r.ID))
			g.requireStableSends(t)
		})
	}
}

// The retention is checked before each send: a slow call earlier in the round
// does not carry a later send past it.
func TestTheRetentionIsCheckedBeforeEverySend(t *testing.T) {
	var slow atomic.Bool
	var h *harness
	g := newGateway(func(send gatewaySend, _ int) gatewayReply {
		if send.answer.Note == typedNote && slow.CompareAndSwap(true, false) {
			h.advance(2 * time.Second)
		}
		if send.answer.Note == typedNote {
			return gatewayReply{mode: gatewayRefuse, status: http.StatusServiceUnavailable, code: api.CodeNotReady}
		}
		return gatewayReply{}
	})
	h = g.harness(t)
	sink := h.logs()
	_, c := h.claimed("retention-per-send", true)
	first, second := h.request(c, "first"), h.request(c, "second")
	h.workAfter(16 * time.Second)
	typedAt := h.now()
	if open := h.openAttempts(c); len(open) != 2 {
		t.Fatalf("both typed notes are open: %+v", open)
	}
	slow.Store(true)
	h.workAfter(answerRetention - time.Second)
	var late []gatewaySend
	for _, request := range []string{first.ID, second.ID} {
		for _, send := range g.sendsOf(request, typedNote) {
			if send.at.After(typedAt) {
				late = append(late, send)
			}
		}
	}
	if len(late) != 1 {
		t.Fatalf("only the send that started before the limit went out: %v", late)
	}
	requireOneAttempt(t, "the first typed note", g.sendsOf(first.ID, typedNote))
	requireOneAttempt(t, "the second typed note", g.sendsOf(second.ID, typedNote))
	abandoned := sink.findAll("request answer abandoned: still unsettled after its retention")
	if len(abandoned) != 1 || abandoned[0]["request"] == late[0].request {
		t.Fatalf("the answer behind the slow call is abandoned unsent: %+v", abandoned)
	}
	g.requireStableSends(t)
}

func TestAnAnswerBacksOffBetweenSends(t *testing.T) {
	rig := alwaysFailingTypedNote(t, "backoff")
	rig.h.workAfter(16 * time.Second)
	start := rig.h.now()
	for rig.h.now().Before(start.Add(200 * time.Second)) {
		rig.h.workAfter(time.Second)
	}
	sends := rig.g.sendsOf(rig.r.ID, typedNote)
	requireOneAttempt(t, "the typed note", sends)
	want := []time.Duration{5, 10, 20, 40, 60, 60}
	if len(sends) != len(want)+1 {
		t.Fatalf("%d sends in 200 seconds, want %d", len(sends), len(want)+1)
	}
	for i, gap := range want {
		// Rounds run every second, so a send that came later than its backoff
		// allows would show as a longer gap.
		if got := sends[i+1].at.Sub(sends[i].at); got != gap*time.Second {
			t.Fatalf("gap %d is %s, want %s", i+1, got, gap*time.Second)
		}
	}
	rig.g.requireStableSends(t)
}

func TestAFailingAnswerWarnsAtMostEveryFiveMinutes(t *testing.T) {
	rig := alwaysFailingTypedNote(t, "warn-throttle")
	sink := rig.h.debugLogs()
	rig.h.workAfter(16 * time.Second)
	first := rig.h.now()
	for rig.h.now().Before(first.Add(answerRetention - 5*time.Second)) {
		rig.h.workAfter(5 * time.Second)
	}
	sends := rig.g.sendsOf(rig.r.ID, typedNote)
	var records []map[string]any
	for _, record := range sink.findAll("writing the request answer failed; retrying later") {
		if record["request"] == rig.r.ID {
			records = append(records, record)
		}
	}
	requireOneAttempt(t, "the typed note", sends)
	if len(sends) < 20 || len(records) != len(sends) {
		t.Fatalf("one record per failed send: %d sends, %d records", len(sends), len(records))
	}
	var warned time.Time
	for i, send := range sends {
		want := "DEBUG"
		if i == 0 || send.at.Sub(warned) >= answerWarnEvery {
			want, warned = "WARN", send.at
		}
		if records[i]["level"] != want {
			t.Fatalf("failure %d at %s logs %v, want %s", i+1, send.at.Sub(first), records[i]["level"], want)
		}
	}
	rig.g.requireStableSends(t)
}

func TestAnAnswerIsAbandonedAfterItsRetention(t *testing.T) {
	rig := alwaysFailingTypedNote(t, "retention")
	sink := rig.h.logs()
	rig.h.workAfter(16 * time.Second)
	first := rig.h.now()
	for rig.h.now().Before(first.Add(answerRetention)) {
		rig.h.workAfter(answerBackoffMax)
	}
	if open := rig.h.openAttempts(rig.c); len(open) != 0 {
		t.Fatalf("the answer is dropped after its retention: %+v", open)
	}
	abandoned := sink.findAll("request answer abandoned: still unsettled after its retention")
	if len(abandoned) != 1 || abandoned[0]["request"] != rig.r.ID || abandoned[0]["level"] != "WARN" {
		t.Fatalf("one warning names the request: %+v", abandoned)
	}
	sent := len(rig.g.ofRequest(rig.r.ID))
	rig.h.workAfter(answerBackoffMax)
	rig.h.workAfter(answerBackoffMax)
	if got := len(rig.g.ofRequest(rig.r.ID)); got != sent {
		t.Fatalf("an abandoned answer is not sent again: %d sends, want %d", got, sent)
	}
	requireOneAttempt(t, "the typed note", rig.g.sendsOf(rig.r.ID, typedNote))
	for _, send := range rig.g.ofRequest(rig.r.ID) {
		if !send.at.Before(first.Add(answerRetention)) {
			t.Fatalf("a send after the retention: %v", send)
		}
	}
	rig.g.requireStableSends(t)
}

// The request file and the answer have retentions of their own: a result
// observed late is still replayed once its file is gone.
func TestAnAnswerOutlivesItsRequestFile(t *testing.T) {
	g := newGateway(func(send gatewaySend, _ int) gatewayReply {
		if send.answer.Note == ackedNote {
			return gatewayReply{mode: gatewayRefuse, status: http.StatusServiceUnavailable, code: api.CodeNotReady}
		}
		return gatewayReply{}
	})
	h := g.harness(t)
	_, c := h.claimed("outlives-file", true)
	r := h.request(c, "acknowledged late")
	typedAt := h.now().Add(16 * time.Second)
	// Seen late in the window, the result is first sent almost two minutes
	// after the request file started its retention.
	seenAfter := 110 * time.Second
	polls := controlPolls(h, func(_ context.Context, _ string) (bool, error) {
		return h.now().Sub(typedAt) >= seenAfter, nil
	})
	h.workAfter(16 * time.Second)
	for {
		poll := nextAckPoll(t, polls)
		runAckPoll(h, poll)
		if poll.at.Sub(typedAt) >= seenAfter {
			break
		}
	}
	scope := h.loop.deliveries[keyOf(c)]
	ackResultOf(t, scope, r.ID)
	h.workAfter(0)
	first := g.sendsOf(r.ID, ackedNote)[0].at
	closed := typedAt.Add(requestFileRetention)
	for h.now().Before(closed.Add(2 * answerBackoffMax)) {
		h.workAfter(answerBackoffMax)
	}
	if _, retained := scope.files[r.ID]; retained {
		t.Fatal("the request file is closed after its retention")
	}
	var late int
	for _, send := range g.sendsOf(r.ID, ackedNote) {
		if send.at.After(closed) && send.at.Before(first.Add(answerRetention)) {
			late++
		}
	}
	if late == 0 {
		t.Fatalf("the result is replayed after its file closed: %v", g.sendsOf(r.ID, ackedNote))
	}
	requireOneAttempt(t, "the result", g.sendsOf(r.ID, ackedNote))
	g.requireStableSends(t)
}

func TestAnswersEndWithTheirCredential(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(*replayRig)
	}{
		{"removed", func(rig *replayRig) {
			if _, err := rig.p.Move(rig.h.t.Context(), "", rig.c.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "test"}, rig.c.Claim()); err != nil {
				rig.h.t.Fatal(err)
			}
		}},
		{"lost", func(rig *replayRig) {
			rig.h.advance(100 * time.Second)
			rig.h.takeOver(rig.c)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := alwaysFailingTypedNote(t, "credential-"+tc.name)
			rig.h.workAfter(16 * time.Second)
			if len(rig.h.openAttempts(rig.c)) != 1 {
				t.Fatal("the typed note is open")
			}
			tc.end(rig)
			rig.h.workAfter(16 * time.Second)
			if _, kept := rig.h.loop.replays[keyOf(rig.c)]; kept {
				t.Fatalf("the open answers end with the credential: %+v", rig.h.openAttempts(rig.c))
			}
			sent := len(rig.g.ofRequest(rig.r.ID))
			rig.h.workAfter(answerBackoffMax)
			rig.h.workAfter(answerBackoffMax)
			if got := len(rig.g.ofRequest(rig.r.ID)); got != sent {
				t.Fatalf("no answer follows: %d sends, want %d", got, sent)
			}
			requireOneAttempt(t, "the typed note", rig.g.sendsOf(rig.r.ID, typedNote))
			rig.g.requireStableSends(t)
		})
	}
}

func TestFallbackRule(t *testing.T) {
	c := remote.Credential{ExecutionID: "exec-a", Generation: 2}
	first := time.Date(2026, 11, 27, 1, 0, 0, 0, time.UTC)
	original := &answerAttempt{request: "r1", kind: answerResult, key: "answer-old", firstSent: first, failures: 3,
		body: api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: 2, Note: ackedNote}}
	current := api.CollabRequest{ID: "r1", TargetGeneration: 2, Status: api.RequestAccepted, Note: typedNote, Revision: 3}
	for _, tc := range []struct {
		name   string
		change func(*api.CollabRequest)
		want   bool
	}{
		{"typed", func(*api.CollabRequest) {}, true},
		{"reserved", func(r *api.CollabRequest) { r.Note = reservationNote }, true},
		{"answered done", func(r *api.CollabRequest) { r.Status = api.RequestDone }, false},
		{"another generation", func(r *api.CollabRequest) { r.TargetGeneration = 3 }, false},
		{"another note", func(r *api.CollabRequest) { r.Note = "holder handled the request" }, false},
		{"missing", func(r *api.CollabRequest) { r.ID = "r2" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := current
			tc.change(&r)
			next := fallbackAttempt(c, original, []api.CollabRequest{r})
			if (next != nil) != tc.want {
				t.Fatalf("fallback = %+v, want one: %v", next, tc.want)
			}
			if next == nil {
				return
			}
			want := api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: r.Revision, Note: ackedNote}
			if next.body != want || next.key != contentKey(c.ExecutionID, "r1", want) || !next.firstSent.Equal(first) || next.failures != 0 {
				t.Fatalf("the fallback expects the current revision under its content key: %+v", next)
			}
		})
	}
}

func TestRebuildConditions(t *testing.T) {
	c := remote.Credential{ExecutionID: "exec-a", Generation: 2}
	now := time.Date(2026, 11, 27, 1, 0, 0, 0, time.UTC)
	base := api.CollabRequest{ID: "r1", TargetGeneration: 2, Status: api.RequestAccepted, Note: ackedNote, Revision: 4, UpdatedAt: now.Add(-10 * time.Minute)}
	expired := expiredNote + "; delivery was blocked before sending since 2026-11-27T00:00:00Z: busy"
	for _, tc := range []struct {
		name   string
		change func(*api.CollabRequest)
		want   []answerKind
	}{
		{"acknowledged", func(*api.CollabRequest) {}, []answerKind{answerTyped, answerResult}},
		{"unconfirmed", func(r *api.CollabRequest) { r.Note = unconfirmedNote }, []answerKind{answerTyped, answerResult}},
		{"acknowledged at revision 2", func(r *api.CollabRequest) { r.Revision = 2 }, []answerKind{answerResult}},
		{"typed", func(r *api.CollabRequest) { r.Note = typedNote }, []answerKind{answerTyped}},
		{"stale", func(r *api.CollabRequest) { r.Status, r.Note = api.RequestRejected, staleNote }, []answerKind{answerRejection}},
		{"expired", func(r *api.CollabRequest) { r.Status, r.Note = api.RequestRejected, expired }, []answerKind{answerRejection}},
		{"another generation", func(r *api.CollabRequest) { r.TargetGeneration = 1 }, nil},
		{"older than the retention", func(r *api.CollabRequest) { r.UpdatedAt = now.Add(-answerRetention - time.Second) }, nil},
		{"revision 1", func(r *api.CollabRequest) { r.Revision = 1 }, nil},
		{"accepted by the reservation", func(r *api.CollabRequest) { r.Note = reservationNote }, nil},
		{"accepted by the holder", func(r *api.CollabRequest) { r.Note = "holder handled the request" }, nil},
		{"done", func(r *api.CollabRequest) { r.Status = api.RequestDone }, nil},
		{"rejected by the holder", func(r *api.CollabRequest) { r.Status, r.Note = api.RequestRejected, "not now" }, nil},
		{"open", func(r *api.CollabRequest) { r.Status, r.Note = api.RequestOpen, "" }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.change(&r)
			attempts := rebuiltAttempts(c, r, now)
			var kinds []answerKind
			for _, a := range attempts {
				kinds = append(kinds, a.kind)
			}
			if len(kinds) != len(tc.want) {
				t.Fatalf("kinds = %v, want %v", kinds, tc.want)
			}
			for i, a := range attempts {
				if a.kind != tc.want[i] {
					t.Fatalf("kinds = %v, want %v", kinds, tc.want)
				}
				wantRevision, wantNote := r.Revision-1, r.Note
				if a.kind == answerTyped && r.Note != typedNote {
					wantRevision, wantNote = r.Revision-2, typedNote
				}
				if a.body.ExpectedRevision != wantRevision || a.body.Note != wantNote || a.body.Status != r.Status {
					t.Fatalf("attempt %d body = %+v", i, a.body)
				}
				var wantKey string
				switch {
				case a.kind != answerRejection:
					wantKey = contentKey(c.ExecutionID, r.ID, a.body)
				case r.Note == staleNote:
					wantKey = staleKey(c.ExecutionID, r.ID)
				default:
					wantKey = expiredKey(c.ExecutionID, r.ID, r.Note)
				}
				if a.key != wantKey {
					t.Fatalf("attempt %d key = %s, want %s", i, a.key, wantKey)
				}
			}
		})
	}
}

func TestRebuildRunsOncePerCredential(t *testing.T) {
	var failing atomic.Bool
	var reads atomic.Int32
	h := newHarnessWrapped(t, 0, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/requests") {
				reads.Add(1)
				if failing.Load() {
					denyTestAnswer(w, http.StatusServiceUnavailable, api.CodeNotReady)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	_, c := h.claimed("rebuild-once")
	round := func() {
		t.Helper()
		if _, err := h.loop.workRound(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	failing.Store(true)
	round()
	if reads.Load() != 1 || h.loop.replays[keyOf(c)].rebuilt {
		t.Fatalf("a failed read leaves the rebuild for the next round: reads=%d", reads.Load())
	}
	failing.Store(false)
	round()
	round()
	round()
	if reads.Load() != 2 || !h.loop.replays[keyOf(c)].rebuilt {
		t.Fatalf("the rebuild reads once more and then never again: reads=%d", reads.Load())
	}
}
