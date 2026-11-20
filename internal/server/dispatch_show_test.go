package server_test

import (
	"strings"
	"testing"
)

// A dispatch that has no record is not found. When a proposal carries its ID,
// the error points at the request, because only the holder's prepare turns a
// proposal into a dispatch.
func TestDispatchShowPointsAtTheProposalOfAMissingDispatch(t *testing.T) {
	m := newReviewMember(t, "show-proposal")
	if _, err := m.holder.Dispatch(t.Context(), m.task, "dispatch-1"); err == nil || !strings.Contains(err.Error(), "dispatch does not exist") || strings.Contains(err.Error(), "kander request show") {
		t.Fatalf("no proposal: %v", err)
	}
	request := m.propose(t, "fix", "proposal-key", "dispatch-1")
	_, err := m.holder.Dispatch(t.Context(), m.task, "dispatch-1")
	if err == nil || !strings.Contains(err.Error(), "request "+request.ID+" proposed it") || !strings.Contains(err.Error(), "kander request show "+m.task+" "+request.ID) {
		t.Fatalf("with a proposal: %v", err)
	}
	// A proposal of another dispatch ID does not explain this one.
	if _, err := m.holder.Dispatch(t.Context(), m.task, "dispatch-2"); err == nil || strings.Contains(err.Error(), "kander request show") {
		t.Fatalf("other ID: %v", err)
	}
}
