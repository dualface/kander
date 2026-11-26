package worker

import (
	"slices"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

// A Codex execution whose card and credential both record the discovered
// session, as a remote start on a pane metadata launcher now writes them, gets
// its open requests delivered to the recorded pane and session.
func TestCodexExecutionWithTheConfirmedSessionGetsRequestsDelivered(t *testing.T) {
	h := newHarness(t)
	const window, session = "herdr-core:t5f", "codex 01a20fe4-5f0f-7a42-a040-cd6e31cc675c"
	_, c := h.claimedWith("codex-routing", "exec-a", &api.StartInput{Window: window, Session: session})
	if c.Session != session || c.Window != window {
		t.Fatalf("credential routing=%q %q", c.Window, c.Session)
	}
	r := h.request(c, "look at this")
	pr, sink := h.target(nil), h.logs()
	var resolved [][2]string
	resolve := pr.resolve
	h.loop.o.Resolve = func(w, s string) (*Target, error) {
		resolved = append(resolved, [2]string{w, s})
		return resolve(w, s)
	}
	h.workAfter(16 * time.Second)
	if len(resolved) == 0 || resolved[0] != [2]string{window, session} {
		t.Fatalf("resolved targets=%q, want %q %q", resolved, window, session)
	}
	if len(pr.typedLines()) == 0 || !slices.Contains(h.credential().Delivered, r.ID) {
		t.Fatalf("the request was not delivered: typed=%q delivered=%v", pr.typedLines(), h.credential().Delivered)
	}
	if delivered := sink.findAll("request typed"); len(delivered) != 1 || delivered[0]["request"] != r.ID {
		t.Fatalf("delivery log=%v", delivered)
	}
	if differs := sink.findAll("request routing differs from this execution's local facts"); len(differs) != 0 {
		t.Fatalf("routing WARN logged: %v", differs)
	}
}
