package worker

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/i18n"
	"github.com/dualface/kander/v2/internal/remote"
)

// proposal creates a dispatch proposal for the card. A zero window gives it no
// acceptance window.
func (h *harness) proposal(c remote.Credential, id string, window time.Duration) api.CollabRequest {
	h.t.Helper()
	card, err := h.project("host-a").Show(h.t.Context(), c.TaskID)
	if err != nil {
		h.t.Fatal(err)
	}
	input := board.DispatchInput{ID: id, TaskID: c.TaskID, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Inspect this version"}
	r, err := h.project("host-b").CreateRequest(h.t.Context(), "proposal-"+id, c.TaskID, api.NewCollabRequest{Kind: api.RequestSync, Message: input.Message, Dispatch: &input, CardRevision: card.Revision, TargetGeneration: c.Generation, AcceptWithin: api.DurationOf(window)})
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

// offsets returns the times of the calls as offsets from start.
func offsets(calls []time.Time, start time.Time) []time.Duration {
	var out []time.Duration
	for _, at := range calls {
		out = append(out, at.Sub(start))
	}
	return out
}

func TestAProposalIsTriedInTheLastTwoIntervalsOfItsWindow(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("window", true)
	start := h.now()
	r := h.proposal(c, "window", 120*time.Second)
	pr, sink := h.target(errNoIdentity), h.logs()
	for range 8 {
		h.workAfter(15 * time.Second)
	}
	var got []int
	for _, d := range offsets(pr.calls, start) {
		got = append(got, int(d/time.Second))
	}
	// 15: first try. 45: the backoff of 30 seconds ends. 90 and 105: the window
	// of 120 seconds minus two renewal intervals has come.
	if want := []int{15, 45, 90, 105}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("resolutions at %v seconds, want %v", got, want)
	}
	rejected := h.requests(c)[0]
	if rejected.ID != r.ID || rejected.Status != api.RequestRejected {
		t.Fatalf("the expired proposal is rejected: %+v", rejected)
	}
	since := start.Add(15 * time.Second).UTC().Format(time.RFC3339)
	if !strings.Contains(rejected.Note, "acceptance window elapsed") || !strings.Contains(rejected.Note, since) || !strings.Contains(rejected.Note, "no session identity value") {
		t.Fatalf("the note names the block start and the reason: %q", rejected.Note)
	}
	warn := sink.findAll("dispatch proposal expired while request delivery was blocked")
	if len(warn) != 1 || warn[0]["request"] != r.ID || warn[0]["blocked_since"] == nil || warn[0]["reason"] == nil {
		t.Fatalf("one WARN with the request, the start and the reason: %v", warn)
	}
	if state, blocked := h.blockOf(); !blocked || !state.since.Equal(start.Add(15*time.Second)) {
		t.Fatalf("an expired proposal does not end or restart the block: %+v %v", state, blocked)
	}
}

func TestAProposalWithoutAWindowFollowsTheBackoff(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("no-window", true)
	h.proposal(c, "no-window", 0)
	pr := h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	h.workAfter(15 * time.Second)
	if pr.count() != 1 {
		t.Fatalf("no acceptance window gives no deadline: %d resolutions", pr.count())
	}
	h.workAfter(15 * time.Second)
	if pr.count() != 2 {
		t.Fatalf("the backoff ends after 30 seconds: %d resolutions", pr.count())
	}
	if request := h.requests(c)[0]; request.Status != api.RequestOpen {
		t.Fatalf("the proposal stays open: %+v", request)
	}
}

func TestAStaleProposalIsRejectedWhileTheDeliveryBacksOff(t *testing.T) {
	h := newHarness(t)
	p, c := h.claimed("stale-in-backoff", true)
	h.request(c, "plain")
	pr := h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	r := h.proposal(c, "stale-in-backoff", 0)
	card, err := p.Show(t.Context(), c.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.PutDocument(t.Context(), "progress", c.TaskID, api.Document{Path: "progress.md", Text: "Later version", ExpectedRevision: card.Revision}, c.Claim()); err != nil {
		t.Fatal(err)
	}
	h.workAfter(5 * time.Second)
	if pr.count() != 1 {
		t.Fatalf("the backoff still holds: %d resolutions", pr.count())
	}
	for _, request := range h.requests(c) {
		if request.ID == r.ID && request.Status != api.RequestRejected {
			t.Fatalf("the stale proposal is rejected during the backoff: %+v", request)
		}
	}
}

func TestExpiredRejectionKeyHasAFixedLengthAndFollowsTheNote(t *testing.T) {
	long := strings.Repeat("e", 128)
	key := expiredKey(long, "request-1", "note")
	if len(key) != len("expired-")+32 || !strings.HasPrefix(key, "expired-") || !api.ValidIdentity(key) {
		t.Fatalf("key %q", key)
	}
	if key != expiredKey(long, "request-1", "note") || key == expiredKey(long, "request-1", "other note") || key == expiredKey("e", "request-1", "note") || key == expiredKey(long, "request-2", "note") {
		t.Fatal("the key is the digest of the execution, the request and the note")
	}
}

// A rejection that ends in a server error, followed by a worker that starts
// again, still ends with the proposal rejected: the second note differs from the
// first and gets its own key.
func TestAnExpiredProposalIsRejectedAfterAServerErrorAndARestart(t *testing.T) {
	var armed atomic.Bool
	failExpired := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if armed.Load() && strings.HasPrefix(r.Header.Get(api.HeaderIdempotencyKey), "expired-") {
				http.Error(w, `{"code":"internal","message":"injected"}`, http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	h := newHarnessWrapped(t, 0, failExpired)
	_, c := h.claimed("expired-error", true)
	r := h.proposal(c, "expired-error", 30*time.Second)
	h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	armed.Store(true)
	h.workAfter(15 * time.Second)
	if h.requests(c)[0].Status != api.RequestOpen {
		t.Fatal("the failed rejection leaves the proposal open")
	}
	h.restartLoop()
	armed.Store(false)
	h.workAfter(time.Second)
	if got := h.requests(c)[0]; got.ID != r.ID || got.Status != api.RequestRejected {
		t.Fatalf("the proposal is rejected after the restart: %+v", got)
	}
}

func TestTheBlockStaysWhenNoRequestWaits(t *testing.T) {
	h := newHarness(t)
	p, c := h.claimed("no-waiting", true)
	r := h.request(c, "x")
	h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	want, _ := h.blockOf()
	if _, err := p.AnswerRequest(t.Context(), "holder-answer", c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestRejected, ExpectedRevision: r.Revision}, *c.Claim()); err != nil {
		t.Fatal(err)
	}
	h.workAfter(5 * time.Second)
	got, blocked := h.blockOf()
	if !blocked || got.pending != 0 || !got.since.Equal(want.since) || got.attempts != want.attempts {
		t.Fatalf("no waiting request: the block stays with nothing pending: %+v", got)
	}
}

func TestBlockedDeliveryIsReportedInTheObservationReason(t *testing.T) {
	h := newHarness(t)
	p, c := h.claimed("suffix", true)
	r := h.request(c, "x")
	base := "agent is waiting for input"
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		return api.AgentObservation{Status: "alive", Reason: base}
	}
	pr := h.target(errNoIdentity)
	reported := func() string {
		view, err := p.Show(t.Context(), c.TaskID)
		if err != nil || view.Claim == nil || view.Claim.Observation == nil {
			t.Fatalf("no observation: %+v %v", view.Claim, err)
		}
		return view.Claim.Observation.Reason
	}
	h.workAfter(15 * time.Second)
	if reported() != base {
		t.Fatalf("the report precedes the first failure: %q", reported())
	}
	state, _ := h.blockOf()
	suffix := config.Text("worker.request_delivery_blocked", 1, r.ID, state.since.UTC().Format(time.RFC3339), state.reason)
	h.workAfter(5 * time.Second)
	first := reported()
	if first != base+"; "+suffix {
		t.Fatalf("the reason carries the suffix after the base: %q, want %q", first, base+"; "+suffix)
	}
	h.workAfter(5 * time.Second)
	if reported() != first {
		t.Fatalf("an unchanged block gives an unchanged suffix: %q", reported())
	}
	base = ""
	h.workAfter(5 * time.Second)
	if reported() != suffix {
		t.Fatalf("an empty base gives the suffix alone: %q", reported())
	}
	base = "agent is waiting for input"
	pr.set(nil)
	h.workAfter(30 * time.Second)
	h.workAfter(5 * time.Second)
	if reported() != base {
		t.Fatalf("a typed line ends the suffix: %q", reported())
	}
}

func TestTheObservationReasonKeepsToItsLimitAndHoldsNoControlCharacters(t *testing.T) {
	h := newHarness(t)
	p, c := h.claimed("limit", true)
	r := h.request(c, "x")
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		return api.AgentObservation{Status: "alive", Reason: strings.Repeat("é", 3000)}
	}
	h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	h.workAfter(5 * time.Second)
	view, err := p.Show(t.Context(), c.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	reason := view.Claim.Observation.Reason
	state, _ := h.blockOf()
	if len(reason) > 4096 || !strings.Contains(reason, r.ID) || !strings.HasSuffix(reason, state.reason) {
		t.Fatalf("%d bytes, suffix kept whole: %q", len(reason), reason[max(0, len(reason)-200):])
	}
	// A reason that is long and holds control characters is cut and cleaned.
	cleaned := cleanReason("bad\x00thing\x1b[31m\nline two " + strings.Repeat("x", 2000))
	if strings.ContainsFunc(cleaned, func(r rune) bool { return r < ' ' || r == 0x7f || r == ' ' }) || len(cleaned) > blockReasonLimit {
		t.Fatalf("not cleaned: %q", cleaned)
	}
}

func TestAnErrorIsLoggedOnceAfterTenMinutesWithRequestsWaiting(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("error-log", true)
	h.request(c, "x")
	sink := h.logs()
	h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	h.workAfter(300 * time.Second)
	if len(sink.findAll("request delivery still blocked")) != 0 {
		t.Fatal("no ERROR before ten minutes")
	}
	h.workAfter(300 * time.Second)
	errs := sink.findAll("request delivery still blocked")
	if len(errs) != 1 || errs[0]["level"] != "ERROR" {
		t.Fatalf("one ERROR at ten minutes: %v", errs)
	}
	h.workAfter(300 * time.Second)
	if len(sink.findAll("request delivery still blocked")) != 1 {
		t.Fatal("at most one ERROR in a blocked period")
	}
}

func TestAProposalSentAgainAfterTenMinutesGetsTheError(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("resent", true)
	h.proposal(c, "first", 30*time.Second)
	sink := h.logs()
	h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	h.workAfter(15 * time.Second)
	if h.requests(c)[0].Status != api.RequestRejected {
		t.Fatal("the first proposal expired")
	}
	if len(sink.findAll("request delivery still blocked")) != 0 {
		t.Fatal("nothing waits, so no ERROR yet")
	}
	h.workAfter(600 * time.Second)
	h.proposal(c, "second", 120*time.Second)
	h.workAfter(5 * time.Second)
	if len(sink.findAll("request delivery still blocked")) != 1 {
		t.Fatalf("the block is over ten minutes old: %v", sink.buf.String())
	}
}

func TestTheBlockedSuffixIsInEveryCatalog(t *testing.T) {
	args := []any{2, "req-77", "2026-11-04T12:00:15Z", "no session identity value"}
	for _, lang := range []string{"en", "ja", "cn"} {
		text := i18n.Text(lang, "worker.request_delivery_blocked", args...)
		for _, want := range []string{"2", "req-77", "2026-11-04T12:00:15Z", "no session identity value"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s text %q lacks %q", lang, text, want)
			}
		}
	}
	if got, want := i18n.Text("en", "worker.request_delivery_blocked", args...), "delivery blocked before sending since 2026-11-04T12:00:15Z; 2 request(s) waiting (first req-77): no session identity value"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
