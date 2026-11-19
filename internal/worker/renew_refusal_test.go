package worker

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

// A refusal that proves nothing while the server still shows the credential's
// generation is retried once per renewal interval, like an outage: it
// registers a disconnect record, logs a warning, and never marks the
// credential lost or stops the agent.
func TestRefusalThatProvesNothingWaitsARenewalInterval(t *testing.T) {
	var mu sync.Mutex
	renewals := map[string]int{}
	count := func(token string) int {
		mu.Lock()
		defer mu.Unlock()
		return renewals[token]
	}
	h := newHarnessWrapped(t, 0, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := isRenewal(r); ok {
				mu.Lock()
				renewals[r.Header.Get(api.HeaderClaimToken)]++
				mu.Unlock()
			}
			next.ServeHTTP(w, r)
		})
	})
	logs := &recording{}
	h.loop.o.Logger = slog.New(logs)

	_, c := h.claimed("refusal-backoff")
	forged := c
	forged.Token = strings.Repeat("0", len(c.Token))
	forged.ExecutionID = "forged-execution"
	if err := remote.WriteCredential(h.home, forged); err != nil {
		t.Fatal(err)
	}
	interval := renewInterval(c)
	tick := time.Second

	h.advance(interval + tick)
	if live, err := h.pass(t.Context()); err != nil || live != 2 {
		t.Fatalf("the refusal pass: %d %v", live, err)
	}
	if got := count(forged.Token); got != 1 {
		t.Fatalf("the forged credential was renewed %d times in the refusal pass, want 1", got)
	}
	h.loop.mu.Lock()
	_, recorded := h.loop.disconnected[keyOf(forged)]
	h.loop.mu.Unlock()
	if !recorded {
		t.Error("the refusal registered no disconnect record for the forged credential")
	}

	// Every tick before the next interval is over leaves the credential alone.
	for elapsed := tick; elapsed < interval; elapsed += tick {
		h.advance(tick)
		if _, err := h.pass(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := count(forged.Token); got != 1 {
			t.Fatalf("the forged credential was renewed again %v after the refusal, before the interval %v: %d renewals", elapsed, interval, got)
		}
	}
	h.advance(tick)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := count(forged.Token); got != 2 {
		t.Fatalf("the forged credential was renewed %d times at the end of the interval, want 2", got)
	}

	warnings := logs.find(slog.LevelWarn, "renewal failed; retrying")
	found := false
	for _, rec := range warnings {
		if v, ok := attr(rec, "error"); ok && strings.Contains(v.String(), api.ClaimActive) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no warning names the status the server still shows: %d warnings", len(warnings))
	}
	for _, msg := range logs.errors() {
		if msg == "renewal refused while the server still shows this generation; retrying" {
			t.Fatal("the refusal was logged as an error")
		}
	}
	for _, got := range mustCredentials(t, h.home) {
		if got.Lost {
			t.Fatalf("the refusal marked a credential lost: %+v", got)
		}
	}
	if len(mustCredentials(t, h.home)) != 2 || len(h.stopped) != 0 {
		t.Fatalf("the refusal removed a credential or stopped the agent: %v", h.stopped)
	}
}
