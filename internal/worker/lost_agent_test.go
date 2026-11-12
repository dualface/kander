package worker

import (
	"errors"
	"github.com/dualface/kander/internal/api"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/remote"
)

func TestWorkerStopsLateAgentFactsWithoutRepeatingUncertainEffect(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("late-agent")
	c.AgentPID = 0
	// The claim can expire before a slow launcher returns its process facts.
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	// The merge preserves an already known PID, so use a fresh credential with
	// no launch observation to represent this timing window.
	c.ExecutionID = "late-observation"
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	c.Lost = true
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	h.loop.stopLost(t.Context(), h.loop.o.Logger, c)
	if len(h.stopped) != 0 {
		t.Fatal("stopped an unknown target")
	}
	c.AgentPID = 5252
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	h.loop.o.Stop = func(pid int) error {
		attempts++
		all, err := remote.Credentials(h.home)
		if err != nil {
			t.Fatal(err)
		}
		for _, saved := range all {
			if saved.ExecutionID == c.ExecutionID && (!saved.Lost || len(saved.StopAttempts) != 1) {
				t.Fatal("external stop preceded durable loss/intent")
			}
		}
		return errors.New("uncertain process stop")
	}
	h.loop.stopLost(t.Context(), h.loop.o.Logger, c)
	all, err := remote.Credentials(h.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, saved := range all {
		if saved.ExecutionID == c.ExecutionID {
			if !strings.HasPrefix(saved.StopResult, "unresolved:") {
				t.Fatal(saved.StopResult)
			}
			h.loop.stopLost(t.Context(), h.loop.o.Logger, saved)
		}
	}
	if attempts != 1 {
		t.Fatalf("uncertain stop repeated %d times", attempts)
	}
	h.advance(100 * time.Second)
}

func TestWorkerDoesNotRepeatAnUncertainRequestDelivery(t *testing.T) {
	h := newHarness(t)
	_, credential := h.claimed("uncertain-delivery", true)
	sender := h.project("host-b")
	request, err := sender.CreateRequest(t.Context(), "", credential.TaskID, api.NewCollabRequest{Kind: api.RequestFix, TargetGeneration: credential.Generation, Message: "check this once"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	h.loop.o.Deliver = func(window, session, text string) error {
		calls++
		saved := h.credential()
		if !slices.Contains(saved.Delivered, request.ID) {
			t.Fatal("typed before saving the delivery intent")
		}
		return errors.New("transport closed after possible terminal delivery")
	}
	for range 2 {
		h.advance(16 * time.Second)
		if _, err := h.loop.pass(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("uncertain delivery was replayed %d times", calls)
	}
	requests, err := sender.Requests(t.Context(), credential.TaskID)
	if err != nil || requests[0].Status != api.RequestAccepted || !strings.Contains(requests[0].Note, "uncertain") {
		t.Fatalf("lost delivery uncertainty: %+v %v", requests, err)
	}
}
