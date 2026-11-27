package liveness

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// subscriberPolicyReads counts the policy reads of the observer and fails them
// while failing is set.
type subscriberPolicyReads struct {
	count   atomic.Int32
	failing atomic.Bool
}

func (r *subscriberPolicyReads) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/policy") && req.Header.Get(api.HeaderClientID) == "observer-host" {
			r.count.Add(1)
			if r.failing.Load() {
				http.Error(w, "policy store unavailable", http.StatusInternalServerError)
				return
			}
		}
		next.ServeHTTP(w, req)
	})
}

// writeSubscriberPolicy writes the next policy revision from the executor host.
func writeSubscriberPolicy(t *testing.T, p *client.Project, review bool) api.ProjectPolicy {
	t.Helper()
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Error(err)
		return api.ProjectPolicy{}
	}
	values := config.PolicyValuesOf(config.DefaultConfig())
	values.Rules[config.RuleReview] = review
	raw, err := json.Marshal(values)
	if err != nil {
		t.Error(err)
		return api.ProjectPolicy{}
	}
	written, err := p.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: current.Revision, Values: raw})
	if err != nil {
		t.Error(err)
	}
	return written
}

func subscriberCache(service *remote.Session) config.PolicyCache {
	return config.ReadPolicyCache(service.Home, service.ServerURL, service.Info.ID)
}

func TestSubscribeWritesPolicyChangeAfterTheCacheAndExits(t *testing.T) {
	service, executor := remoteSubscriptionService(t)
	const group = "20261128-policy-change-group"
	subscriptionRemoteCard(t, executor, "policy-change", group)
	cachedAtEvent := uint64(0)
	events, err := runSingleRemoteObserved(t, service, subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 3600}, func() {
		writeSubscriberPolicy(t, executor, false)
	}, func(event groupEvent) {
		if event.Event == "policy-change" {
			cachedAtEvent = subscriberCache(service).Snapshot.Revision
		}
	})
	requireEventNames(t, events, err, "snapshot", "policy-change")
	change := events[1]
	if change.PolicyRevision == nil || *change.PolicyRevision != 1 {
		t.Fatalf("policy-change carries revision %v, want 1", change.PolicyRevision)
	}
	if change.SchemaVersion != 1 || change.SubscriptionID != events[0].SubscriptionID || change.Seq != 2 || change.ObservedAt.IsZero() || change.Tasks == nil {
		t.Fatalf("policy-change lacks the common fields: %+v", change)
	}
	if events[0].PolicyRevision != nil {
		t.Fatalf("the snapshot carries a policy revision: %v", *events[0].PolicyRevision)
	}
	if cachedAtEvent != 1 {
		t.Fatalf("the cache held revision %d when policy-change was written, want 1", cachedAtEvent)
	}
	if service.PolicyRevision() != 1 {
		t.Fatalf("the session works with revision %d after the change", service.PolicyRevision())
	}
}

func TestSubscribeIgnoresAPolicyEventItAlreadyKnows(t *testing.T) {
	reads := &subscriberPolicyReads{}
	service, executor := remoteSubscriptionServiceBehind(t, reads.wrap)
	const group = "20261128-policy-known-group"
	task := subscriptionRemoteCard(t, executor, "policy-known", group)
	// The session already works with a later revision of the same epoch than
	// the next event announces.
	first := writeSubscriberPolicy(t, executor, true)
	service.Info.Policy = &api.ProjectPolicy{Epoch: first.Epoch, Revision: 5}
	events, err := runSingleRemote(t, service, subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 3600}, func() {
		writeSubscriberPolicy(t, executor, false)
		if _, err := executor.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
			t.Error(err)
		}
	})
	requireEventNames(t, events, err, "snapshot", "state-change")
	if got := reads.count.Load(); got != 0 {
		t.Fatalf("a known policy revision was read %d times", got)
	}
}

func TestSubscribeReadsThePolicyAfterAReset(t *testing.T) {
	var polls atomic.Int32
	service, executor := remoteSubscriptionServiceBehind(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/events") && r.URL.Query().Get("cursor") != "" && polls.Add(1) == 1 {
				query := r.URL.Query()
				query.Set("cursor", "unknown-epoch:1")
				r.URL.RawQuery = query.Encode()
			}
			next.ServeHTTP(w, r)
		})
	})
	const group = "20261128-policy-reset-group"
	subscriptionRemoteCard(t, executor, "policy-reset", group)
	// The policy changed while the session was not looking; only the reset tells.
	written := writeSubscriberPolicy(t, executor, false)
	events, err := runSingleRemote(t, service, subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 3600}, func() {})
	requireEventNames(t, events, err, "snapshot", "snapshot-reset", "policy-change")
	if got := events[2].PolicyRevision; got == nil || *got != written.Revision {
		t.Fatalf("policy-change after the reset carries %v, want %d", got, written.Revision)
	}
	if cache := subscriberCache(service); cache.Snapshot.Revision != written.Revision {
		t.Fatalf("the reset did not refresh the cache: %+v", cache)
	}
}

func TestSubscribeWritesPolicyChangeWhenTheReadFails(t *testing.T) {
	reads := &subscriberPolicyReads{}
	reads.failing.Store(true)
	service, executor := remoteSubscriptionServiceBehind(t, reads.wrap)
	const group = "20261128-policy-failed-group"
	subscriptionRemoteCard(t, executor, "policy-failed", group)
	events, err := runSingleRemote(t, service, subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 3600}, func() {
		writeSubscriberPolicy(t, executor, false)
	})
	requireEventNames(t, events, err, "snapshot", "policy-change")
	if got := events[1].PolicyRevision; got == nil || *got != 1 {
		t.Fatalf("policy-change carries %v, want the announced revision 1", got)
	}
	if reads.count.Load() == 0 || subscriberCache(service).Status != config.PolicyCacheMissing {
		t.Fatal("the failed read was not attempted, or it wrote the cache")
	}
}
