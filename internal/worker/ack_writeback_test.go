package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

type answerTap struct {
	mu      sync.Mutex
	answers []api.CollabAnswer
	fault   func(http.ResponseWriter, *http.Request, http.Handler, api.CollabAnswer) bool
}

func (s *answerTap) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/answer") {
			body, err := io.ReadAll(r.Body)
			var answer api.CollabAnswer
			if err != nil || json.Unmarshal(body, &answer) != nil {
				http.Error(w, "unreadable test answer", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			s.mu.Lock()
			s.answers = append(s.answers, answer)
			s.mu.Unlock()
			if s.fault != nil && s.fault(w, r, next, answer) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *answerTap) revisions(note string) []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var revisions []uint64
	for _, answer := range s.answers {
		if answer.Note == note {
			revisions = append(revisions, answer.ExpectedRevision)
		}
	}
	return revisions
}

func denyTestAnswer(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Code: code, Message: "test refusal"})
}

func TestAcknowledgementAfterUncertainTypedNote(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "typed note failed before commit"
		if committed {
			name = "typed note committed but reply was lost"
		}
		t.Run(name, func(t *testing.T) {
			tap := &answerTap{fault: func(w http.ResponseWriter, r *http.Request, next http.Handler, answer api.CollabAnswer) bool {
				if answer.Note != typedNote {
					return false
				}
				if committed {
					next.ServeHTTP(httptest.NewRecorder(), r)
				}
				denyTestAnswer(w, http.StatusServiceUnavailable, api.CodeInternal)
				return true
			}}
			h := newHarnessWrapped(t, 0, tap.wrap)
			_, c := h.claimed("typed-failure", true)
			r := h.request(c, "write back the pane acknowledgement")
			polls := controlPolls(h, func(context.Context, string) (bool, error) { return true, nil })
			h.workAfter(16 * time.Second)
			before := h.requests(c)[0]
			reservation := r.Revision + 1
			want := []uint64{reservation}
			if committed {
				want = append(want, before.Revision)
			}
			runAckPoll(h, nextAckPoll(t, polls))
			ackResultOf(t, h.loop.deliveries[keyOf(c)], r.ID)
			h.workAfter(0)
			if got := tap.revisions(ackedNote); !slices.Equal(got, want) {
				t.Fatalf("acknowledgement revisions = %v, want %v", got, want)
			}
			if got := h.requests(c)[0]; got.Note != ackedNote {
				t.Fatalf("acknowledgement not recorded: %+v", got)
			}
		})
	}
}

func TestAcknowledgementDoesNotOverwriteHolderAnswer(t *testing.T) {
	for _, status := range []string{api.RequestDone, api.RequestAccepted} {
		t.Run(status, func(t *testing.T) {
			tap := &answerTap{}
			h := newHarnessWrapped(t, 0, tap.wrap)
			p, c := h.claimed("holder-first", true)
			r := h.request(c, "a request the holder answers first")
			polls := controlPolls(h, func(context.Context, string) (bool, error) { return true, nil })
			h.workAfter(16 * time.Second)
			typed := h.requests(c)[0]
			decision, err := p.AnswerRequest(t.Context(), "", c.TaskID, r.ID, api.CollabAnswer{
				ExpectedRevision: typed.Revision, Status: status, Note: "holder handled the request",
			}, *c.Claim())
			if err != nil {
				t.Fatal(err)
			}
			runAckPoll(h, nextAckPoll(t, polls))
			scope := h.loop.deliveries[keyOf(c)]
			ackResultOf(t, scope, r.ID)
			h.workAfter(0)
			h.workAfter(16 * time.Second)
			if got := tap.revisions(ackedNote); !slices.Equal(got, []uint64{typed.Revision}) {
				t.Fatalf("only the fenced stale answer may be attempted, got revisions %v", got)
			}
			if got := h.requests(c)[0]; got.Note != decision.Note || got.Status != decision.Status || got.Revision != decision.Revision {
				t.Fatalf("holder answer was changed: %+v", got)
			}
			if len(scope.snapshotResults()) != 0 {
				t.Fatal("discarded acknowledgement was retained")
			}
		})
	}
}

func TestAcknowledgementStopsAfterClaimRefusal(t *testing.T) {
	for _, code := range []string{api.CodeLeaseInvalid, api.CodeTakenOver, api.CodeClaimRequired} {
		t.Run(code, func(t *testing.T) {
			tap := &answerTap{fault: func(w http.ResponseWriter, _ *http.Request, _ http.Handler, answer api.CollabAnswer) bool {
				if answer.Note != ackedNote {
					return false
				}
				denyTestAnswer(w, http.StatusConflict, code)
				return true
			}}
			h := newHarnessWrapped(t, 0, tap.wrap)
			_, c := h.claimed("claim-refusal", true)
			r := h.request(c, "an older claim does not write a result")
			polls := controlPolls(h, func(context.Context, string) (bool, error) { return true, nil })
			h.workAfter(16 * time.Second)
			runAckPoll(h, nextAckPoll(t, polls))
			scope := h.loop.deliveries[keyOf(c)]
			ackResultOf(t, scope, r.ID)
			h.workAfter(0)
			h.workAfter(16 * time.Second)
			if len(tap.revisions(ackedNote)) != 1 || len(scope.snapshotResults()) != 0 {
				t.Fatal("a fenced acknowledgement must be attempted once and then discarded")
			}
		})
	}
}

func TestAcknowledgementRetriesTransientWriteFailureWithoutTyping(t *testing.T) {
	var attempts int
	tap := &answerTap{fault: func(w http.ResponseWriter, _ *http.Request, _ http.Handler, answer api.CollabAnswer) bool {
		if answer.Note == ackedNote {
			attempts++
			if attempts == 1 {
				denyTestAnswer(w, http.StatusBadGateway, api.CodeInternal)
				return true
			}
		}
		return false
	}}
	h := newHarnessWrapped(t, 0, tap.wrap)
	_, c := h.claimed("retry-result", true)
	r := h.request(c, "retry the receipt, not the typing")
	polls := controlPolls(h, func(context.Context, string) (bool, error) { return true, nil })
	h.workAfter(16 * time.Second)
	runAckPoll(h, nextAckPoll(t, polls))
	scope := h.loop.deliveries[keyOf(c)]
	ackResultOf(t, scope, r.ID)
	h.workAfter(0)
	if len(scope.snapshotResults()) != 1 || h.requests(c)[0].Note != typedNote {
		t.Fatal("transient error must retain the result for another round")
	}
	h.workAfter(0)
	if len(tap.revisions(ackedNote)) != 2 || len(h.lines) != 1 || h.requests(c)[0].Note != ackedNote {
		t.Fatal("result was not retried once without typing again")
	}
}
