package worker

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

// oldReservationNote is the note that workers before the recovery wrote on a
// reservation. Requests that carry it are never resumed.
const oldReservationNote = "automatic delivery reserved; outcome is uncertain until observed"

// reservationLoss wraps the server handler of a test. It loses the answer of
// one reservation: the server commits the reservation, and the worker sees a
// 504. It can also refuse the next answer that is not a reservation with a 503
// before the server sees it, and it counts the reservations it saw.
type reservationLoss struct {
	mu           sync.Mutex
	lose         bool
	refuse       bool
	before       func()
	reservations int
	answers      int
}

func (l *reservationLoss) loseNext() { l.mu.Lock(); l.lose = true; l.mu.Unlock() }

// beforeNextConfirmation runs f once, while the next answer that is not a
// reservation is on its way to the server.
func (l *reservationLoss) beforeNextConfirmation(f func()) { l.mu.Lock(); l.before = f; l.mu.Unlock() }

func (l *reservationLoss) refuseNextConfirmation() { l.mu.Lock(); l.refuse = true; l.mu.Unlock() }

func (l *reservationLoss) count() int { l.mu.Lock(); defer l.mu.Unlock(); return l.reservations }

// answerCount is the number of answers of any kind the server was asked for.
func (l *reservationLoss) answerCount() int { l.mu.Lock(); defer l.mu.Unlock(); return l.answers }

func (l *reservationLoss) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/answer") {
			next.ServeHTTP(w, r)
			return
		}
		reservation := strings.HasPrefix(r.Header.Get(api.HeaderIdempotencyKey), reservationKeyPrefix)
		l.mu.Lock()
		l.answers++
		if reservation {
			l.reservations++
		}
		lose := reservation && l.lose
		refuse := !reservation && l.refuse
		var before func()
		if !reservation {
			before, l.before = l.before, nil
		}
		if lose {
			l.lose = false
		}
		if refuse {
			l.refuse = false
		}
		l.mu.Unlock()
		if before != nil {
			before()
		}
		switch {
		case lose:
			// The server commits; the answer never reaches the worker.
			next.ServeHTTP(httptest.NewRecorder(), r)
			w.WriteHeader(http.StatusGatewayTimeout)
		case refuse:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeInternal, Message: "refused by the test"})
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// lostReservation builds a worker whose first reservation answer is lost, and
// runs the round that loses it. The request is accepted with the reservation
// note, the local list is empty and nothing was typed.
func lostReservation(t *testing.T) (*harness, *reservationLoss, *fakeTarget, api.CollabRequest) {
	t.Helper()
	loss := &reservationLoss{}
	h := newHarnessWrapped(t, 0, loss.wrap)
	_, c := h.claimed("lost-reservation", true)
	h.request(c, "fix the test")
	pr := h.target(nil)
	loss.loseNext()
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := h.requests(c)[0]
	if got.Status != api.RequestAccepted || got.Note != reservationNote || len(h.credential().Delivered) != 0 || len(pr.typedLines()) != 0 || loss.count() != 1 {
		t.Fatalf("the lost reservation was not set up: %+v delivered=%v typed=%q reservations=%d", got, h.credential().Delivered, pr.typedLines(), loss.count())
	}
	return h, loss, pr, got
}

func TestALostReservationIsRecoveredAndTypedOnce(t *testing.T) {
	h, loss, pr, reserved := lostReservation(t)
	c := h.credential()
	var atTyping api.CollabRequest
	var listed bool
	pr.onType = func() {
		atTyping = h.requests(c)[0]
		listed = slices.Contains(h.credential().Delivered, reserved.ID)
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if lines := pr.typedLines(); len(lines) != 1 || !strings.Contains(lines[0], reserved.ID) {
		t.Fatalf("one line typed after the lost reservation: %q", lines)
	}
	if !listed {
		t.Fatal("the request ID must be on the local list when the line is typed")
	}
	if atTyping.Revision != reserved.Revision {
		t.Fatalf("the confirmation changed the request before typing: revision %d, reserved at %d", atTyping.Revision, reserved.Revision)
	}
	if final := h.requests(c)[0]; final.Status != api.RequestAccepted || final.Note != "delivered to the agent" {
		t.Fatalf("the final answer: %+v", final)
	}
	if loss.count() != 1 {
		t.Fatalf("recovery must not reserve again: %d reservation answers", loss.count())
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(pr.typedLines()) != 1 {
		t.Fatalf("a recovered request is typed once: %q", pr.typedLines())
	}
}

func TestAFailedConfirmationTypesNothingAndIsTriedAgain(t *testing.T) {
	h, loss, pr, reserved := lostReservation(t)
	loss.refuseNextConfirmation()
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(pr.typedLines()) != 0 || slices.Contains(h.credential().Delivered, reserved.ID) {
		t.Fatalf("a failed confirmation must not record or type the request: %q %v", pr.typedLines(), h.credential().Delivered)
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(pr.typedLines()) != 1 {
		t.Fatalf("the next round types the request once: %q", pr.typedLines())
	}
}

func TestARecoveredRequestWhoseTypingFailsStaysUncertain(t *testing.T) {
	h, _, pr, _ := lostReservation(t)
	pr.typeErr = errors.New("transport closed after possible terminal delivery")
	for range 3 {
		h.advance(16 * time.Second)
		if _, err := h.pass(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(pr.typedLines()) != 1 {
		t.Fatalf("an uncertain typing is not repeated: %q", pr.typedLines())
	}
	if got := h.requests(h.credential())[0]; got.Status != api.RequestAccepted || !strings.Contains(got.Note, "uncertain") {
		t.Fatalf("the request stays uncertain: %+v", got)
	}
}

// A worker that starts again recovers the request in the first round that
// confirms a renewal: the work step follows a confirmed renewal, so the test
// advances the clock to the next due renewal and runs the renewal round instead
// of owing the work step by hand. A worker that restarts right after a renewal
// therefore types up to one renewal interval later.
func TestAWorkerThatStartsAgainRecoversALostReservation(t *testing.T) {
	h, _, _, reserved := lostReservation(t)
	h.loop = newLoop(h.workerOptions())
	pr := h.target(nil)
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if lines := pr.typedLines(); len(lines) != 1 || !strings.Contains(lines[0], reserved.ID) {
		t.Fatalf("the first round of the new worker types the request once: %q", lines)
	}
}

func TestRequestsThatAreNotReservedUntypedAreLeftAlone(t *testing.T) {
	for _, tc := range []struct{ name, note string }{
		{"an older reservation", oldReservationNote},
		{"a note written by hand", "handled by hand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			p, c := h.claimed("left-alone", true)
			r := h.request(c, "x")
			if _, err := p.AnswerRequest(t.Context(), "by-hand", c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: r.Revision, Note: tc.note}, *c.Claim()); err != nil {
				t.Fatal(err)
			}
			before := h.requests(c)[0]
			pr := h.target(nil)
			for range 3 {
				h.advance(16 * time.Second)
				if _, err := h.pass(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if after := h.requests(c)[0]; len(pr.typedLines()) != 0 || after.Status != before.Status || after.Revision != before.Revision || after.Note != tc.note {
				t.Fatalf("typed=%q before=%+v after=%+v", pr.typedLines(), before, after)
			}
		})
	}
}

func TestReservedUntypedNeedsEveryCondition(t *testing.T) {
	c := &remote.Credential{Generation: 3, Delivered: []string{"listed"}}
	for _, tc := range []struct {
		name string
		r    api.CollabRequest
		want bool
	}{
		{"reserved and never typed", api.CollabRequest{ID: "r", Status: api.RequestAccepted, Note: reservationNote, TargetGeneration: 3}, true},
		{"still open", api.CollabRequest{ID: "r", Status: api.RequestOpen, Note: reservationNote, TargetGeneration: 3}, false},
		{"already done", api.CollabRequest{ID: "r", Status: api.RequestDone, Note: reservationNote, TargetGeneration: 3}, false},
		{"another generation", api.CollabRequest{ID: "r", Status: api.RequestAccepted, Note: reservationNote, TargetGeneration: 2}, false},
		{"on the local list", api.CollabRequest{ID: "listed", Status: api.RequestAccepted, Note: reservationNote, TargetGeneration: 3}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reservedUntyped(tc.r, c); got != tc.want {
				t.Fatalf("reservedUntyped = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAFailedSaveTakesTheRequestBackOffTheLocalList(t *testing.T) {
	c := &remote.Credential{Delivered: []string{"first"}}
	if err := markDelivered(c, "second", func(remote.Credential) error { return errors.New("disk full") }); err == nil {
		t.Fatal("the save error must be returned")
	}
	if !slices.Equal(c.Delivered, []string{"first"}) {
		t.Fatalf("an ID that was never saved must leave the list: %v", c.Delivered)
	}
	var saved []string
	if err := markDelivered(c, "second", func(c remote.Credential) error { saved = slices.Clone(c.Delivered); return nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Delivered, []string{"first", "second"}) || !slices.Equal(saved, c.Delivered) {
		t.Fatalf("a saved ID stays on the list: %v saved %v", c.Delivered, saved)
	}
}

func TestResumingALostReservationLogsAWarning(t *testing.T) {
	h, _, _, reserved := lostReservation(t)
	sink := h.logs()
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	warn := sink.findAll("request reserved earlier and never typed; typing it now")
	if len(warn) != 1 || warn[0]["request"] != reserved.ID || warn[0]["level"] != "WARN" {
		t.Fatalf("one WARN naming the request: %v", warn)
	}
}

// lostProposal loses the reservation of a dispatch proposal with the given
// acceptance window.
func lostProposal(t *testing.T, window time.Duration) (*harness, *reservationLoss, *fakeTarget, api.CollabRequest) {
	t.Helper()
	loss := &reservationLoss{}
	h := newHarnessWrapped(t, 0, loss.wrap)
	_, c := h.claimed("lost-proposal", true)
	proposal := h.proposal(c, "lost", window)
	pr := h.target(nil)
	loss.loseNext()
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.requests(c)[0]; got.Status != api.RequestAccepted || got.Note != reservationNote || len(pr.typedLines()) != 0 {
		t.Fatalf("the lost reservation of the proposal was not set up: %+v typed=%q", got, pr.typedLines())
	}
	return h, loss, pr, proposal
}

func TestAReservedProposalInsideItsWindowAndOnItsCardVersionIsTypedOnce(t *testing.T) {
	h, _, pr, proposal := lostProposal(t, 120*time.Second)
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if lines := pr.typedLines(); len(lines) != 1 || !strings.Contains(lines[0], proposal.ID) {
		t.Fatalf("the proposal is typed once: %q", lines)
	}
}

func TestAReservedProposalWhoseWindowElapsedIsRejectedWithoutTyping(t *testing.T) {
	h, _, pr, proposal := lostProposal(t, 30*time.Second)
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.requests(h.credential())[0]; got.ID != proposal.ID || got.Status != api.RequestRejected || len(pr.typedLines()) != 0 {
		t.Fatalf("an elapsed window rejects the proposal: %+v typed=%q", got, pr.typedLines())
	}
}

func TestAReservedProposalOnAnEarlierCardVersionIsRejectedWithoutTyping(t *testing.T) {
	h, _, pr, proposal := lostProposal(t, 120*time.Second)
	c := h.credential()
	p := h.project("host-a")
	card, err := p.Show(t.Context(), c.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.PutDocument(t.Context(), "progress", c.TaskID, api.Document{Path: "progress.md", Text: "Later version", ExpectedRevision: card.Revision}, c.Claim()); err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.requests(c)[0]; got.ID != proposal.ID || got.Status != api.RequestRejected || len(pr.typedLines()) != 0 {
		t.Fatalf("a changed card version rejects the proposal: %+v typed=%q", got, pr.typedLines())
	}
}

func TestAReservedRequestWhoseTargetCannotBeResolvedWaitsAsPending(t *testing.T) {
	loss := &reservationLoss{}
	h := newHarnessWrapped(t, 0, loss.wrap)
	_, c := h.claimed("lost-then-blocked", true)
	r := h.request(c, "x")
	pr := h.target(nil)
	loss.loseNext()
	h.workAfter(16 * time.Second)
	if got := h.requests(c)[0]; got.Status != api.RequestAccepted || got.Note != reservationNote {
		t.Fatalf("the reservation was not lost: %+v", got)
	}
	pr.set(errNoIdentity)
	sink := h.logs()
	answers := loss.answerCount()
	h.workAfter(16 * time.Second)
	if loss.answerCount() != answers || slices.Contains(h.credential().Delivered, r.ID) || len(pr.typedLines()) != 0 {
		t.Fatalf("a failed resolution sends no answer, records nothing and types nothing: answers %d to %d", answers, loss.answerCount())
	}
	warn := sink.findAll("request delivery blocked before sending; retrying later")
	if len(warn) != 1 || warn[0]["pending"] != float64(1) || warn[0]["request"] != r.ID {
		t.Fatalf("the failed resolution counts the request as pending: %v", warn)
	}
	pr.set(nil)
	h.workAfter(30 * time.Second)
	if len(pr.typedLines()) != 1 {
		t.Fatalf("the first attempt after the cause is gone types the request once: %q", pr.typedLines())
	}
	if final := h.requests(c)[0]; final.Status != api.RequestAccepted || final.Note != "delivered to the agent" {
		t.Fatalf("the final answer: %+v", final)
	}
}

// The target resolution and the confirmation of a recovered proposal may take
// as long as its window had left. The confirmation of an accepted request checks
// neither the window nor the card version, so the worker checks them again
// before it types.
func TestAReservedProposalThatTurnsUnusableBeforeTypingIsRejectedWithoutTyping(t *testing.T) {
	for _, phase := range []string{"resolution", "confirmation"} {
		for _, change := range []string{"window", "card"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				h, loss, pr, proposal := lostProposal(t, 40*time.Second)
				c := h.credential()
				p := h.project("host-a")
				apply := func() {
					if change == "window" {
						h.advance(10 * time.Second)
						return
					}
					card, err := p.Show(t.Context(), c.TaskID)
					if err != nil {
						t.Error(err)
						return
					}
					if _, err := p.PutDocument(t.Context(), "progress", c.TaskID, api.Document{Path: "progress.md", Text: "Version saved while the delivery ran", ExpectedRevision: card.Revision}, c.Claim()); err != nil {
						t.Error(err)
					}
				}
				if phase == "resolution" {
					h.loop.o.Resolve = func(window, session string) (Prompt, error) {
						apply()
						return pr.resolve(window, session)
					}
				} else {
					loss.beforeNextConfirmation(apply)
				}
				h.advance(16 * time.Second)
				if _, err := h.pass(t.Context()); err != nil {
					t.Fatal(err)
				}
				if got := h.requests(c)[0]; got.ID != proposal.ID || got.Status != api.RequestRejected || len(pr.typedLines()) != 0 || slices.Contains(h.credential().Delivered, proposal.ID) {
					t.Fatalf("the proposal must be rejected before anything is recorded or typed: %+v typed=%q", got, pr.typedLines())
				}
			})
		}
	}
}

// A proposal that the recheck rejects is not waiting any more: the observation
// of the execution must not list it, and the ERROR of a long block must not
// name it.
func TestARejectedRecheckLeavesNoPendingRequest(t *testing.T) {
	h, _, pr, proposal := lostProposal(t, 60*time.Second)
	pr.set(errNoIdentity)
	h.workAfter(16 * time.Second)
	if state, blocked := h.blockOf(); !blocked || state.pending != 1 {
		t.Fatalf("the proposal waits behind the failed resolution: %+v %v", state, blocked)
	}
	pr.set(nil)
	pr.cost = 15 * time.Second
	h.workAfter(15 * time.Second)
	if got := h.requests(h.credential())[0]; got.ID != proposal.ID || got.Status != api.RequestRejected || len(pr.typedLines()) != 0 {
		t.Fatalf("the window elapsed during the resolution: %+v typed=%q", got, pr.typedLines())
	}
	if state, blocked := h.blockOf(); !blocked || state.pending != 0 || state.first != "" {
		t.Fatalf("a rejected proposal is not pending: %+v %v", state, blocked)
	}
}
