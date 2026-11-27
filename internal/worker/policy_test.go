package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// policyReads counts the policy reads of the worker and fails them while
// failing is set.
type policyReads struct {
	count   atomic.Int32
	failing atomic.Bool
}

func (r *policyReads) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/policy") && req.Header.Get(api.HeaderClientID) == "host-a" {
			r.count.Add(1)
			if r.failing.Load() {
				http.Error(w, "policy store unavailable", http.StatusInternalServerError)
				return
			}
		}
		next.ServeHTTP(w, req)
	})
}

// writeWorkerPolicy writes the next policy revision from another host.
func writeWorkerPolicy(t *testing.T, p *client.Project, mode string) api.ProjectPolicy {
	t.Helper()
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	values := config.PolicyValuesOf(config.DefaultConfig())
	values.Git.BranchMode = mode
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	written, err := p.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: current.Revision, Values: raw})
	if err != nil {
		t.Fatal(err)
	}
	return written
}

// policyAfter is workAfter followed by the step of the policy loop, which a
// harness drives by hand like the other loops.
func (h *harness) policyAfter(d time.Duration) {
	h.t.Helper()
	h.workAfter(d)
	h.loop.refreshPolicies(h.t.Context())
}

func cachedRevision(t *testing.T, h *harness, c remote.Credential) uint64 {
	t.Helper()
	return config.ReadPolicyCache(h.home, c.ServerURL, c.ProjectID).Snapshot.Revision
}

func TestWorkerRefreshesThePolicyCacheWhenARenewalNamesANewerVersion(t *testing.T) {
	reads := &policyReads{}
	h := newHarnessWrapped(t, 0, reads.wrap)
	_, c := h.claimed("worker-policy")
	h.policyAfter(15 * time.Second)
	if reads.count.Load() != 0 || config.ReadPolicyCache(h.home, c.ServerURL, c.ProjectID).Status != config.PolicyCacheMissing {
		t.Fatalf("an unset policy was read %d times", reads.count.Load())
	}
	other := h.project("host-b")
	written := writeWorkerPolicy(t, other, "main-only")
	h.policyAfter(15 * time.Second)
	cache := config.ReadPolicyCache(h.home, c.ServerURL, c.ProjectID)
	if cache.Status != config.PolicyCacheSet || cache.Snapshot.Revision != written.Revision || cache.Snapshot.Epoch != written.Epoch || cache.Values.Git.BranchMode != "main-only" {
		t.Fatalf("cache after the renewal: %+v", cache)
	}
	// A renewal that names the cached version reads nothing.
	h.policyAfter(15 * time.Second)
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("the worker read the policy %d times, want 1", got)
	}
}

func TestWorkerRefreshesAProjectOnceForSeveralCredentials(t *testing.T) {
	reads := &policyReads{}
	h := newHarnessWrapped(t, 0, reads.wrap)
	_, first := h.claimedAs("worker-policy-one", "exec-one")
	h.claimedAs("worker-policy-two", "exec-two")
	writeWorkerPolicy(t, h.project("host-b"), "main-only")
	h.policyAfter(15 * time.Second)
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("two leases of one project read the policy %d times", got)
	}
	if cachedRevision(t, h, first) != 1 {
		t.Fatal("the cache was not refreshed")
	}
}

func TestWorkerPolicyRefreshFailureIsOnlyAWarning(t *testing.T) {
	reads := &policyReads{}
	h := newHarnessWrapped(t, 0, reads.wrap)
	p, c := h.claimed("worker-policy-fail")
	sink := h.logs()
	writeWorkerPolicy(t, h.project("host-b"), "main-only")
	reads.failing.Store(true)
	h.policyAfter(15 * time.Second)
	if warnings := sink.findAll("project policy refresh failed; retried at the next renewal"); len(warnings) != 1 || warnings[0]["level"] != "WARN" {
		t.Fatalf("refresh failure log %v", warnings)
	}
	if cachedRevision(t, h, c) != 0 {
		t.Fatal("a failed read wrote the cache")
	}
	// The lease stays renewed and the credential live.
	requireRenewedAt(t, p, h.credential(), h.now())
	if h.credential().Lost {
		t.Fatal("a failed policy read marked the lease lost")
	}
	// Within the same renewal nothing is read again; the next renewal retries.
	h.loop.refreshPolicies(t.Context())
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("the failed read was retried before the next renewal: %d reads", got)
	}
	reads.failing.Store(false)
	h.policyAfter(15 * time.Second)
	if cachedRevision(t, h, c) != 1 {
		t.Fatal("the next renewal did not retry the refresh")
	}
}

// A work step held in a slow terminal operation must not hold back the policy
// read that a renewal asked for.
func TestWorkerRefreshesThePolicyWhileTheWorkLoopIsBusy(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("worker-policy-busy")
	gate := newHookGate(t)
	o := h.workerOptions()
	o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		gate.hold()
		return api.AgentObservation{Status: "alive"}
	}
	h.advance(16 * time.Second)
	stop := serveWorker(t, o)
	defer stop()
	defer gate.let() // runs before stop: the worker cannot return from a held hook
	gate.waitEntered(t)
	written := writeWorkerPolicy(t, h.project("host-b"), "main-only")
	h.advance(16 * time.Second)
	deadline := time.Now().Add(waitCap)
	for cachedRevision(t, h, c) != written.Revision {
		if time.Now().After(deadline) {
			t.Fatal("the policy cache waited for the held work step")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
