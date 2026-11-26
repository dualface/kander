package worker

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/remote"
)

func seenAtOnce(context.Context, string) (bool, error) { return true, nil }

// replayRig is a request typed by the harness worker behind a test gateway.
type replayRig struct {
	g     *gatewayTap
	h     *harness
	p     *client.Project
	c     remote.Credential
	r     api.CollabRequest
	files *atomic.Int32
}

func newReplayRig(t *testing.T, slug string, reply func(gatewaySend, int) gatewayReply) *replayRig {
	t.Helper()
	g := newGateway(reply)
	h := g.harness(t)
	p, c := h.claimed(slug, true)
	return &replayRig{g: g, h: h, p: p, c: c, r: h.request(c, "a request answered through the gateway"), files: h.countRequestFiles()}
}

// typeAndAcknowledge types the request in one work round, lets the observer
// see the acknowledgement at its first probe and waits for the result.
func (rig *replayRig) typeAndAcknowledge(t *testing.T) {
	t.Helper()
	polls := controlPolls(rig.h, seenAtOnce)
	rig.h.workAfter(16 * time.Second)
	runAckPoll(rig.h, nextAckPoll(t, polls))
	ackResultOf(t, rig.h.loop.deliveries[keyOf(rig.c)], rig.r.ID)
}

// requireNoRedelivery checks that replays typed nothing and created no request
// file after the first delivery.
func (rig *replayRig) requireNoRedelivery(t *testing.T) {
	t.Helper()
	if len(rig.h.lines) != 1 || rig.files.Load() != 1 {
		t.Fatalf("a replay delivered again: lines=%d request files=%d", len(rig.h.lines), rig.files.Load())
	}
}

// scenarioP brings the rig to the state of PMQA-001: the typed note committed
// with its reply lost, the result refused with a revision conflict, and the
// fallback committed by Kander with a failed audit. It returns the first send
// of the fallback.
func scenarioP(t *testing.T, slug string, fallbackMode gatewayMode) (*replayRig, gatewaySend) {
	t.Helper()
	results := 0
	rig := newReplayRig(t, slug, func(send gatewaySend, prior int) gatewayReply {
		switch {
		case send.answer.Note == typedNote && prior == 0:
			return gatewayReply{mode: gatewayLostReply}
		case send.answer.Note == ackedNote && prior == 0:
			results++
			if results == 2 {
				return gatewayReply{mode: fallbackMode, status: http.StatusServiceUnavailable, code: api.CodeNotReady}
			}
		}
		return gatewayReply{}
	})
	rig.typeAndAcknowledge(t)
	rig.h.workAfter(0)
	sends := rig.g.sendsOf(rig.r.ID, ackedNote)
	if len(sends) != 2 || sends[0].status != http.StatusConflict || sends[1].status != http.StatusServiceUnavailable {
		t.Fatalf("want a refused result and a fallback that failed with 503: %v", sends)
	}
	if sends[1].answer.ExpectedRevision != sends[0].answer.ExpectedRevision+1 {
		t.Fatalf("the fallback expects the revision of the typed note: %v", sends)
	}
	return rig, sends[1]
}

func TestScenarioPReplaysTheCommittedFallback(t *testing.T) {
	rig, fallback := scenarioP(t, "scenario-p", gatewayAuditFails)
	rig.h.workAfter(answerBackoffFirst)
	replays := rig.g.withKey(fallback.key)
	if len(replays) != 2 || !bytes.Equal(replays[0].body, replays[1].body) {
		t.Fatalf("the fallback is replayed once with the same key and body: %v", replays)
	}
	if rig.g.audited(fallback.key) != 1 {
		t.Fatalf("the fallback key is audited once: %d", rig.g.audited(fallback.key))
	}
	if got := rig.h.requests(rig.c)[0]; got.Note != ackedNote || got.Revision != fallback.answer.ExpectedRevision+1 {
		t.Fatalf("the request keeps the fallback result: %+v", got)
	}
	rig.h.workAfter(answerBackoffMax)
	if len(rig.g.withKey(fallback.key)) != 2 || len(rig.h.openAttempts(rig.c)) != 0 {
		t.Fatal("a settled fallback is not sent again")
	}
	rig.requireNoRedelivery(t)
	rig.g.requireStableSends(t)
	rig.g.requireContentKeys(t, rig.c, rig.r.ID)
}

func TestScenarioPReplaysTheFallbackAfterTheHolderAnswered(t *testing.T) {
	rig, fallback := scenarioP(t, "scenario-p-holder", gatewayAuditFails)
	current := rig.h.requests(rig.c)[0]
	decision, err := rig.p.AnswerRequest(t.Context(), "", rig.c.TaskID, rig.r.ID, api.CollabAnswer{
		ExpectedRevision: current.Revision, Status: api.RequestDone, Note: "holder handled the request",
	}, *rig.c.Claim())
	if err != nil {
		t.Fatal(err)
	}
	rig.h.workAfter(answerBackoffFirst)
	if replays := rig.g.withKey(fallback.key); len(replays) != 2 {
		t.Fatalf("the fallback is replayed once after the holder answered: %v", replays)
	}
	if rig.g.audited(fallback.key) != 1 {
		t.Fatalf("the fallback key is audited once: %d", rig.g.audited(fallback.key))
	}
	if got := rig.h.requests(rig.c)[0]; got.Status != decision.Status || got.Note != decision.Note || got.Revision != decision.Revision {
		t.Fatalf("the holder answer stays: %+v, want %+v", got, decision)
	}
	rig.requireNoRedelivery(t)
	rig.g.requireStableSends(t)
}

func TestFallbackFailedBeforeCommitIsDroppedAfterTheHolderAnswered(t *testing.T) {
	rig, fallback := scenarioP(t, "fallback-before-commit", gatewayRefuse)
	if got := rig.h.requests(rig.c)[0]; got.Note != typedNote {
		t.Fatalf("the refused fallback never reached Kander: %+v", got)
	}
	decision, err := rig.p.AnswerRequest(t.Context(), "", rig.c.TaskID, rig.r.ID, api.CollabAnswer{
		ExpectedRevision: rig.h.requests(rig.c)[0].Revision, Status: api.RequestDone, Note: "holder handled the request",
	}, *rig.c.Claim())
	if err != nil {
		t.Fatal(err)
	}
	rig.h.workAfter(answerBackoffFirst)
	replays := rig.g.withKey(fallback.key)
	if len(replays) != 2 || replays[1].status != http.StatusConflict {
		t.Fatalf("the replay of the fallback gets a revision conflict: %v", replays)
	}
	if len(rig.h.openAttempts(rig.c)) != 0 {
		t.Fatalf("the fallback is dropped: %+v", rig.h.openAttempts(rig.c))
	}
	sent := len(rig.g.ofRequest(rig.r.ID))
	rig.h.workAfter(answerBackoffMax)
	rig.h.workAfter(answerBackoffMax)
	if got := len(rig.g.ofRequest(rig.r.ID)); got != sent {
		t.Fatalf("no answer follows the dropped fallback: %d sends, want %d", got, sent)
	}
	if got := rig.h.requests(rig.c)[0]; got.Status != decision.Status || got.Note != decision.Note || got.Revision != decision.Revision {
		t.Fatalf("the holder answer stays: %+v", got)
	}
	rig.g.requireStableSends(t)
}

func TestTypedNoteIsReplayedAfterAFailedAudit(t *testing.T) {
	rig := newReplayRig(t, "typed-audit", func(send gatewaySend, prior int) gatewayReply {
		if send.answer.Note == typedNote && prior == 0 {
			return gatewayReply{mode: gatewayAuditFails}
		}
		return gatewayReply{}
	})
	rig.typeAndAcknowledge(t)
	rig.h.workAfter(0)
	rig.h.workAfter(answerBackoffFirst)
	typed := rig.g.sendsOf(rig.r.ID, typedNote)
	if len(typed) != 2 || typed[0].key != typed[1].key || !bytes.Equal(typed[0].body, typed[1].body) {
		t.Fatalf("the typed note is replayed once with the same key and body: %v", typed)
	}
	if rig.g.audited(typed[0].key) != 1 {
		t.Fatalf("the typed key is audited once: %d", rig.g.audited(typed[0].key))
	}
	if got := rig.h.requests(rig.c)[0]; got.Note != ackedNote {
		t.Fatalf("the request ends acknowledged: %+v", got)
	}
	rig.requireNoRedelivery(t)
	rig.g.requireStableSends(t)
	rig.g.requireContentKeys(t, rig.c, rig.r.ID)
}

// staleProposal creates a dispatch proposal and moves the card past the version
// it binds.
func staleProposal(t *testing.T, h *harness, p *client.Project, c remote.Credential, id string) api.CollabRequest {
	t.Helper()
	r := h.proposal(c, id, 0)
	card, err := p.Show(t.Context(), c.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.PutDocument(t.Context(), "progress-"+id, c.TaskID, api.Document{Path: "progress.md", Text: "Later version", ExpectedRevision: card.Revision}, c.Claim()); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStaleRejectionIsReplayedAfterAFailedAudit(t *testing.T) {
	g := newGateway(func(send gatewaySend, prior int) gatewayReply {
		if send.answer.Note == staleNote && prior == 0 {
			return gatewayReply{mode: gatewayAuditFails}
		}
		return gatewayReply{}
	})
	h := g.harness(t)
	p, c := h.claimed("stale-audit", true)
	r := staleProposal(t, h, p, c, "stale-audit")
	h.workAfter(16 * time.Second)
	h.workAfter(answerBackoffFirst)
	sends := g.sendsOf(r.ID, staleNote)
	if len(sends) != 2 || sends[0].key != "stale-"+c.ExecutionID+"-"+r.ID {
		t.Fatalf("the stale rejection is replayed once under its key: %v", sends)
	}
	if g.audited(sends[0].key) != 1 {
		t.Fatalf("the stale key is audited once: %d", g.audited(sends[0].key))
	}
	if got := h.requests(c)[0]; got.Status != api.RequestRejected || got.Note != staleNote {
		t.Fatalf("the proposal stays rejected as stale: %+v", got)
	}
	g.requireStableSends(t)
}

func expiredSends(g *gatewayTap, request string) []gatewaySend {
	var found []gatewaySend
	for _, send := range g.ofRequest(request) {
		if strings.HasPrefix(send.answer.Note, expiredNote) {
			found = append(found, send)
		}
	}
	return found
}

func TestExpiredRejectionIsReplayedAfterAFailedAudit(t *testing.T) {
	g := newGateway(func(send gatewaySend, prior int) gatewayReply {
		if strings.HasPrefix(send.answer.Note, expiredNote) && prior == 0 {
			return gatewayReply{mode: gatewayAuditFails}
		}
		return gatewayReply{}
	})
	h := g.harness(t)
	_, c := h.claimed("expired-audit", true)
	r := h.proposal(c, "expired-audit", 30*time.Second)
	h.workAfter(31 * time.Second)
	h.workAfter(answerBackoffFirst)
	sends := expiredSends(g, r.ID)
	if len(sends) != 2 || sends[0].key != expiredKey(c.ExecutionID, r.ID, sends[0].answer.Note) {
		t.Fatalf("the expired rejection is replayed once under its key: %v", sends)
	}
	if g.audited(sends[0].key) != 1 {
		t.Fatalf("the expired key is audited once: %d", g.audited(sends[0].key))
	}
	if got := h.requests(c)[0]; got.Status != api.RequestRejected || got.Note != sends[0].answer.Note {
		t.Fatalf("the proposal stays rejected with the original note: %+v", got)
	}
	g.requireStableSends(t)
}

// While a rejection is open, a later round neither sends another one nor
// sends the open one twice, even when the note of a new rejection would carry
// the blocked suffix the first one lacked.
func TestAnOpenRejectionIsNotSentAgainByTheDelivery(t *testing.T) {
	g := newGateway(func(send gatewaySend, prior int) gatewayReply {
		if strings.HasPrefix(send.answer.Note, expiredNote) {
			return gatewayReply{mode: gatewayRefuse, status: http.StatusServiceUnavailable, code: api.CodeNotReady}
		}
		return gatewayReply{}
	})
	h := g.harness(t)
	_, c := h.claimed("expired-dedupe", true)
	h.request(c, "a plain request that blocks the delivery")
	h.target(errNoIdentity)
	r := h.proposal(c, "expired-dedupe", 30*time.Second)
	h.workAfter(31 * time.Second)
	for rounds := 1; len(expiredSends(g, r.ID)) < 10; rounds++ {
		before := len(expiredSends(g, r.ID))
		h.workAfter(answerBackoffMax)
		if sent := len(expiredSends(g, r.ID)) - before; sent > 1 {
			t.Fatalf("round %d sent %d rejections", rounds, sent)
		}
		open := 0
		for _, a := range h.openAttempts(c) {
			if a.request == r.ID && a.kind == answerRejection {
				open++
			}
		}
		if open != 1 {
			t.Fatalf("round %d keeps %d open rejections, want one", rounds, open)
		}
		if rounds > 20 {
			t.Fatal("the rejection is not replayed")
		}
	}
	if _, blocked := h.blockOf(); !blocked {
		t.Fatal("the delivery blocked while the rejection was replayed")
	}
	// The note of a rejection made now would carry the blocked suffix; every
	// send is still the first rejection.
	for _, send := range expiredSends(g, r.ID) {
		if first := expiredSends(g, r.ID)[0]; send.key != first.key || !bytes.Equal(send.body, first.body) {
			t.Fatalf("the replayed rejection changed: %v then %v", first, send)
		}
	}
	g.requireStableSends(t)
}

// Without an embedder, a fallback whose reply was lost is replayed, and the
// replay still logs the unconfirmed result.
func TestPureKanderLogsTheReplayedUnconfirmedFallback(t *testing.T) {
	var typed, unconfirmed atomic.Int32
	lose := func(w http.ResponseWriter, r *http.Request, next http.Handler) {
		next.ServeHTTP(httptest.NewRecorder(), r)
		denyTestAnswer(w, http.StatusBadGateway, api.CodeInternal)
	}
	tap := &answerTap{fault: func(w http.ResponseWriter, r *http.Request, next http.Handler, answer api.CollabAnswer) bool {
		switch {
		case answer.Note == typedNote && typed.Add(1) == 1:
			lose(w, r, next)
			return true
		case answer.Note == unconfirmedNote && unconfirmed.Add(1) == 2:
			lose(w, r, next)
			return true
		}
		return false
	}}
	h := newHarnessWrapped(t, 0, tap.wrap)
	sink := h.logs()
	_, c := h.claimed("pure-kander", true)
	r := h.request(c, "never acknowledged")
	polls := controlPolls(h, func(context.Context, string) (bool, error) { return false, nil })
	h.workAfter(16 * time.Second)
	typedAt := h.now()
	for {
		poll := nextAckPoll(t, polls)
		runAckPoll(h, poll)
		if !poll.at.Before(typedAt.Add(ackWindow)) {
			break
		}
	}
	ackResultOf(t, h.loop.deliveries[keyOf(c)], r.ID)
	h.workAfter(0)
	if got := h.requests(c)[0]; got.Note != unconfirmedNote || unconfirmed.Load() != 2 {
		t.Fatalf("the fallback committed while its reply was lost: %+v, %d sends", got, unconfirmed.Load())
	}
	h.workAfter(answerBackoffFirst)
	warnings := sink.findAll("request not acknowledged")
	if unconfirmed.Load() != 3 || len(warnings) != 1 || warnings[0]["request"] != r.ID || warnings[0]["level"] != "WARN" {
		t.Fatalf("the replay logs the unconfirmed result once: %d sends, %+v", unconfirmed.Load(), warnings)
	}
	tap.requireStableSends(t)
}
