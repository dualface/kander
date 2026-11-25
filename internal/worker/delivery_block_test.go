package worker

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/remote"
)

// fakeTarget is the target resolution of a test: it fails with err while err is set,
// takes cost of the fake clock, and records when it was called and what its
// prompt typed.
type fakeTarget struct {
	h       *harness
	mu      sync.Mutex
	err     error
	typeErr error
	cost    time.Duration
	calls   []time.Time
	typed   []string
	onType  func()
}

func (h *harness) target(err error) *fakeTarget {
	p := &fakeTarget{h: h, err: err}
	h.loop.o.Resolve = p.resolve
	return p
}

func (p *fakeTarget) resolve(window, session string) (Prompt, error) {
	p.mu.Lock()
	p.calls = append(p.calls, p.h.now())
	err, cost := p.err, p.cost
	p.mu.Unlock()
	if cost > 0 {
		p.h.advance(cost)
	}
	if err != nil {
		return nil, err
	}
	return func(text string) error {
		if p.onType != nil {
			p.onType()
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.typed = append(p.typed, text)
		return p.typeErr
	}, nil
}

func (p *fakeTarget) set(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *fakeTarget) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func (p *fakeTarget) typedLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.typed)
}

// workAfter lets d of the fake clock pass and runs one work step of every
// credential. The renewals continue through the wait, so the lease stays active;
// the step runs once, at the end, whatever the renewal interval.
func (h *harness) workAfter(d time.Duration) {
	h.t.Helper()
	for left := d; left > 0; {
		step := min(left, 15*time.Second)
		h.advance(step)
		left -= step
		h.loop.renewRound(h.t.Context())
	}
	credentials, err := remote.Credentials(h.home)
	if err != nil {
		h.t.Fatal(err)
	}
	h.loop.mu.Lock()
	for _, c := range credentials {
		h.loop.owed[keyOf(c)] = struct{}{}
	}
	h.loop.mu.Unlock()
	if _, err := h.loop.workRound(h.t.Context()); err != nil {
		h.t.Fatal(err)
	}
}

// logs routes the log of the harness worker into a sink.
func (h *harness) logs() *logSink {
	sink := &logSink{}
	h.loop.o.Logger = slog.New(slog.NewJSONHandler(sink, nil))
	return sink
}

// findAll returns every log record with the message.
func (s *logSink) findAll(msg string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []map[string]any
	for _, line := range strings.Split(s.buf.String(), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil && record["msg"] == msg {
			found = append(found, record)
		}
	}
	return found
}

// blockOf is the blocked state of the only credential.
func (h *harness) blockOf() (blockState, bool) {
	return h.loop.blockOf(keyOf(h.credential()))
}

// request creates a plain request for the task and returns it.
func (h *harness) request(c remote.Credential, message string) api.CollabRequest {
	h.t.Helper()
	r, err := h.project("host-b").CreateRequest(h.t.Context(), "", c.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: message, TargetGeneration: c.Generation})
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *harness) requests(c remote.Credential) []api.CollabRequest {
	h.t.Helper()
	requests, err := h.project("host-b").Requests(h.t.Context(), c.TaskID)
	if err != nil {
		h.t.Fatal(err)
	}
	return requests
}

var errNoIdentity = errors.New("Session identity could not be verified: pane reported no session identity value")

func TestResolutionFailureLeavesTheRequestOpenAndUntyped(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("pre-send", true)
	r := h.request(c, "look at this")
	before := h.requests(c)[0]
	pr, sink := h.target(errNoIdentity), h.logs()
	h.workAfter(16 * time.Second)
	after := h.requests(c)
	if len(after) != 1 || after[0].Status != api.RequestOpen || after[0].Revision != before.Revision {
		t.Fatalf("the request must stay open and unanswered: %+v", after)
	}
	if slices.Contains(h.credential().Delivered, r.ID) || len(pr.typedLines()) != 0 || len(h.deliveredLines()) != 0 {
		t.Fatalf("nothing may be reserved or typed: %v %v", h.credential().Delivered, pr.typedLines())
	}
	warn := sink.findAll("request delivery blocked before sending; retrying later")
	if len(warn) != 1 {
		t.Fatalf("one WARN per failure: %v", warn)
	}
	for _, field := range []string{"request", "pending", "reason", "attempt", "blocked_since", "next_attempt"} {
		if _, ok := warn[0][field]; !ok {
			t.Fatalf("the WARN lacks %q: %v", field, warn[0])
		}
	}
	if warn[0]["request"] != r.ID || warn[0]["pending"] != float64(1) || !strings.Contains(warn[0]["reason"].(string), "no session identity value") {
		t.Fatalf("the WARN names the request, the count and the reason: %v", warn[0])
	}
}

func TestBackoffGrowsPerFailureInARow(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("backoff", true)
	h.request(c, "once")
	pr := h.target(errNoIdentity)
	h.workAfter(15 * time.Second)
	for n, wait := range []time.Duration{30, 60, 120, 240, 300, 300} {
		wait *= time.Second
		calls := pr.count()
		h.workAfter(wait - time.Second)
		if pr.count() != calls {
			t.Fatalf("failure %d: resolved %v before the backoff of %v ended", n+1, time.Second, wait)
		}
		h.workAfter(time.Second)
		if pr.count() != calls+1 {
			t.Fatalf("failure %d: the round after %v must resolve", n+1, wait)
		}
		if state, _ := h.blockOf(); state.attempts != n+2 {
			t.Fatalf("failure %d: attempts = %d", n+1, state.attempts)
		}
	}
}

// The failure counts of the first six rows are covered by
// TestBackoffGrowsPerFailureInARow; this pins that the wait stays at the cap.
func TestBackoffStaysAtTheCap(t *testing.T) {
	if got := blockBackoff(40); got != blockBackoffMax {
		t.Fatalf("backoff after 40 failures = %v, want %v", got, blockBackoffMax)
	}
}

func TestOnlyTheFirstResolutionOfAnExecutionRunsPerRound(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("many", true)
	for _, message := range []string{"a", "b", "c"} {
		h.request(c, message)
	}
	pr := h.target(errNoIdentity)
	h.workAfter(16 * time.Second)
	if pr.count() != 1 {
		t.Fatalf("a failed resolution ends the delivery of the execution for the round: %d calls", pr.count())
	}
	if state, _ := h.blockOf(); state.pending != 3 {
		t.Fatalf("three requests wait: %+v", state)
	}
}

func TestRecoveryReservesThenTypesOnceThenAcknowledges(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("recover", true)
	r := h.request(c, "after the outage")
	sink := h.logs()
	pr := h.target(errNoIdentity)
	h.workAfter(16 * time.Second)
	pr.set(nil)
	var atTyping api.CollabRequest
	pr.onType = func() { atTyping = h.requests(c)[0] }
	h.workAfter(30 * time.Second)
	if lines := pr.typedLines(); len(lines) != 1 || !strings.Contains(lines[0], r.ID) {
		t.Fatalf("one line typed after the recovery: %q", lines)
	}
	if atTyping.Status != api.RequestAccepted || !strings.Contains(atTyping.Note, "reserved") {
		t.Fatalf("the request is reserved before it is typed: %+v", atTyping)
	}
	if final := h.requests(c)[0]; final.Status != api.RequestAccepted || final.Note != "delivered to the agent" {
		t.Fatalf("the final answer: %+v", final)
	}
	if _, blocked := h.blockOf(); blocked {
		t.Fatal("a typed line ends the block")
	}
	if unblocked := sink.findAll("request delivery unblocked"); len(unblocked) != 1 || unblocked[0]["blocked_for"] == nil {
		t.Fatalf("one INFO with blocked_for: %v", unblocked)
	}
}

func TestTypingFailureStaysUncertainAndKeepsTheBlock(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("typing", true)
	h.request(c, "x")
	pr := h.target(errNoIdentity)
	h.workAfter(16 * time.Second)
	want, _ := h.blockOf()
	pr.set(nil)
	pr.typeErr = errors.New("transport closed")
	h.workAfter(30 * time.Second)
	got, blocked := h.blockOf()
	if !blocked || got.since != want.since || got.attempts != want.attempts || !got.next.Equal(want.next) || got.reason != want.reason {
		t.Fatalf("a failed typing does not touch the block: %+v then %+v", want, got)
	}
	if request := h.requests(c)[0]; request.Status != api.RequestAccepted || !strings.Contains(request.Note, "uncertain") {
		t.Fatalf("an uncertain typing stays reserved: %+v", request)
	}
}

// refuseAnswers fails every request answer at the transport while armed, as a
// server that is busy or down would.
func refuseAnswers(armed *atomic.Bool, status int, code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if armed.Load() && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/answer") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(api.Error{Code: code, Message: "refused by the test"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func TestRefusedReservationTypesNothingAndLeavesTheBlockAlone(t *testing.T) {
	var armed atomic.Bool
	h := newHarnessWrapped(t, 0, refuseAnswers(&armed, http.StatusConflict, api.CodeRevisionConflict))
	_, c := h.claimed("refused", true)
	r := h.request(c, "x")
	pr := h.target(errNoIdentity)
	h.workAfter(16 * time.Second)
	want, _ := h.blockOf()
	pr.set(nil)
	armed.Store(true)
	h.workAfter(30 * time.Second)
	if len(pr.typedLines()) != 0 || slices.Contains(h.credential().Delivered, r.ID) || h.requests(c)[0].Status != api.RequestOpen {
		t.Fatal("a refused reservation must not type, record the request, or answer it")
	}
	got, blocked := h.blockOf()
	if !blocked || got.since != want.since || got.attempts != want.attempts || !got.next.Equal(want.next) {
		t.Fatalf("a refused reservation does not touch the block: %+v then %+v", want, got)
	}
	armed.Store(false)
	h.workAfter(16 * time.Second)
	if len(pr.typedLines()) != 1 {
		t.Fatalf("the next round tries again: %q", pr.typedLines())
	}
}

// A resolution may take as long as the work step allows; the reservation and the
// acknowledgement after it still get their own time.
func TestReservationFollowsASlowResolution(t *testing.T) {
	const renew = 300 * time.Millisecond
	h := newHarnessRenewing(t, renew)
	_, c := h.claimed("slow-resolution", true)
	h.request(c, "x")
	typed := atomic.Int32{}
	h.loop.o.Resolve = func(string, string) (Prompt, error) {
		time.Sleep(2 * renew)
		return func(string) error { typed.Add(1); return nil }, nil
	}
	h.advance(2 * renew)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if request := h.requests(c)[0]; typed.Load() != 1 || request.Status != api.RequestAccepted || request.Note != "delivered to the agent" {
		t.Fatalf("typed=%d request=%+v", typed.Load(), request)
	}
}

func TestAWorkerThatStartsAgainResolvesAtOnce(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("restart", true)
	h.request(c, "x")
	h.target(errNoIdentity)
	h.workAfter(16 * time.Second)
	if _, blocked := h.blockOf(); !blocked {
		t.Fatal("the first failure blocks")
	}
	h.loop = newLoop(h.workerOptions())
	pr := h.target(errNoIdentity)
	h.workAfter(time.Second)
	if pr.count() != 1 {
		t.Fatalf("a new worker has no memory of the block: %d", pr.count())
	}
}

// The block follows the credential: a credential that is gone or lost takes its
// state with it.
func TestTheBlockEndsWithItsCredential(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("gone", true)
	h.request(c, "x")
	h.target(errNoIdentity)
	h.workAfter(16 * time.Second)
	key := keyOf(c)
	if _, blocked := h.loop.blockOf(key); !blocked {
		t.Fatal("blocked")
	}
	if err := remote.RemoveCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	h.workAfter(time.Second)
	if _, blocked := h.loop.blockOf(key); blocked {
		t.Fatal("a removed credential keeps no block")
	}
}

func TestTargetChangeAndBackoffDecideTheGate(t *testing.T) {
	h := newHarness(t)
	key := credentialKey{TaskID: "t"}
	start := h.now()
	h.loop.failResolution(key, "w1", "s1", "first", start)
	state := h.loop.failResolution(key, "w1", "s1", "again", start.Add(30*time.Second))
	if state.attempts != 2 || !state.since.Equal(start) {
		t.Fatalf("a failure against the same target counts up: %+v", state)
	}
	if h.loop.canResolve(key, start.Add(31*time.Second), "w1", "s1", time.Time{}) {
		t.Fatal("the backoff of 60 seconds is still running")
	}
	if !h.loop.canResolve(key, start.Add(31*time.Second), "w2", "s1", time.Time{}) || !h.loop.canResolve(key, start.Add(31*time.Second), "w1", "s2", time.Time{}) {
		t.Fatal("a card that names another target is tried at once")
	}
	state = h.loop.failResolution(key, "w2", "s1", "other", start.Add(31*time.Second))
	if state.attempts != 1 || !state.since.Equal(start) || !state.next.Equal(start.Add(61*time.Second)) {
		t.Fatalf("a new target counts from one and keeps the start of the block: %+v", state)
	}
	if !h.loop.canResolve(key, start.Add(32*time.Second), "w2", "s1", start.Add(32*time.Second)) {
		t.Fatal("a dispatch deadline that has come opens the gate")
	}
}

func TestDelayedExecutionsKeepTheirStateAndGoFirstNextRound(t *testing.T) {
	h := newHarness(t)
	var creds []remote.Credential
	for _, name := range []string{"a-one", "b-two", "c-three", "d-four"} {
		_, c := h.claimedAs(name, "exec-"+name, true)
		h.request(c, name)
		creds = append(creds, c)
	}
	pr := h.target(errNoIdentity)
	pr.cost = 10 * time.Second
	h.workAfter(16 * time.Second)
	if pr.count() != 3 {
		t.Fatalf("the budget of 30 seconds allows three resolutions of 10 seconds: %d", pr.count())
	}
	var postponed []remote.Credential
	for _, c := range creds {
		if _, ok := h.loop.blockOf(keyOf(c)); !ok {
			postponed = append(postponed, c)
		}
	}
	if len(postponed) != 1 {
		t.Fatalf("the postponed execution keeps no state of a failure: %d without one", len(postponed))
	}
	pr.cost = 0
	h.workAfter(15 * time.Second)
	if _, ok := h.loop.blockOf(keyOf(postponed[0])); !ok {
		t.Fatal("the postponed execution resolves in the next round, with a fresh budget")
	}
}
