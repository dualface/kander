package liveness

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

// runSingleRemote runs a subscription through the service without Follow. It
// calls act once the snapshot event was written and returns every event and the
// result when the subscription ends on its own. A subscription that does not end
// fails the test.
func runSingleRemote(t *testing.T, service *remote.Session, opts subscribeOptions, act func()) ([]groupEvent, error) {
	t.Helper()
	return runSingleRemoteObserved(t, service, opts, act, nil)
}

// runSingleRemoteObserved is runSingleRemote that also calls observe, when it
// is set, as each event is written.
func runSingleRemoteObserved(t *testing.T, service *remote.Session, opts subscribeOptions, act func(), observe func(groupEvent)) ([]groupEvent, error) {
	t.Helper()
	opts.Follow = false
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	seen := make(chan struct{})
	writer := &clockEvents{onEvent: func(event groupEvent) error {
		if observe != nil {
			observe(event)
		}
		if event.Event == "snapshot" {
			closeOnce(seen)
		}
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- subscribeContext(ctx, service.Location(""), opts, writer, service) }()
	select {
	case <-seen:
	case err := <-done:
		t.Fatalf("subscription ended before its snapshot: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("no snapshot event")
	}
	act()
	select {
	case err := <-done:
		return writer.events, err
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatalf("single-event subscription did not end; events=%v", eventNames(writer.events))
	}
	return nil, nil
}

func TestSingleEventExitsAfterCoordinatorChange(t *testing.T) {
	service, executor := remoteSubscriptionService(t)
	const group = "20261121-single-coordinator-group"
	task := subscriptionRemoteCard(t, executor, "single-coordinator", group)
	events, err := runSingleRemote(t, service, subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 3600}, func() {
		_, claimErr := executor.ClaimGroup(t.Context(), "", group, api.GroupClaim{BranchMode: "main-develop", ExecutionID: "single-coordinator-run", Request: board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "public-label", Basis: "confirmed subscription fixture", Members: []string{task.TaskID}}}, nil)
		if claimErr != nil {
			t.Error(claimErr)
		}
	})
	requireEventNames(t, events, err, "snapshot", "coordinator-change")
	if events[1].Coordinators[group].Claim == nil {
		t.Fatalf("coordinator-change lost the claim: %+v", events[1].Coordinators[group])
	}
}

func TestSingleEventExitsAfterClaimChange(t *testing.T) {
	service, executor := remoteSubscriptionService(t)
	const group = "20261121-single-claim-group"
	task := subscriptionRemoteCard(t, executor, "single-claim", group)
	if _, err := executor.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := executor.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{BranchMode: "main-develop", ClientID: "executor-host", ExecutionID: "single-claim-run", Owner: "codex", Start: &api.StartInput{Session: "codex single-session", Window: "tmux:$999:@999:%999"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	events, err := runSingleRemote(t, service, subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 3600}, func() {
		_, observeErr := executor.ObserveAgent(t.Context(), "", task.TaskID, api.AgentObservation{Status: "alive", Reason: "confirmed by the owning worker"}, proof)
		if observeErr != nil {
			t.Error(observeErr)
		}
	})
	requireEventNames(t, events, err, "snapshot", "claim-change")
	if got := events[1].Updated; len(got) != 1 || got[0] != task.TaskID {
		t.Fatalf("claim-change names %v, want %s", got, task.TaskID)
	}
}

// A reset cursor asks the consumer to read everything again, so it wakes a
// single-event subscription although no card changed.
func TestSingleEventExitsAfterSnapshotReset(t *testing.T) {
	var polls atomic.Int32
	// The first event request of the observer carries a cursor the server does
	// not know, which the server answers with a reset page.
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
	const group = "20261121-single-reset-group"
	subscriptionRemoteCard(t, executor, "single-reset", group)
	events, err := runSingleRemote(t, service, subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 3600}, func() {})
	requireEventNames(t, events, err, "snapshot", "snapshot-reset")
	if !events[1].ReconciliationRequired {
		t.Fatalf("snapshot-reset does not require reconciliation: %+v", events[1])
	}
}

// A start order of a monitored card wakes the subscription with its order.
func TestSingleEventExitsAfterStartOrderChange(t *testing.T) {
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "service"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	observer, err := client.New(httpServer.URL, "c-00000000000a-hosta")
	if err != nil {
		t.Fatal(err)
	}
	info, err := observer.Register(t.Context(), "orders", "https://example.invalid/orders.git")
	if err != nil {
		t.Fatal(err)
	}
	host, _ := client.New(httpServer.URL, "c-00000000000b-hostb")
	if _, err := host.Heartbeat(t.Context(), api.HostHeartbeat{ClientID: "c-00000000000b-hostb", Name: "hostb", WorkerID: "w-1", Version: "test", Accepting: true, OrderProtocol: api.StartOrderProtocol, Projects: []api.HostProject{{Project: "orders", Agents: []string{"claude"}, Launchers: []string{"tmux-session"}}}}, 0); err != nil {
		t.Fatal(err)
	}
	project := observer.Project(info.ID)
	task := subscriptionRemoteCard(t, project, "ordered", "")
	if _, err := project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	service := &remote.Session{ServerURL: httpServer.URL, Client: observer, Project: project, Info: info, Home: t.TempDir()}
	var order api.StartOrder
	events, err := runSingleRemote(t, service, subscribeOptions{Watch: []string{task.TaskID}, Refresh: 0.01, Heartbeat: 3600}, func() {
		var createErr error
		if order, createErr = project.CreateStartOrder(t.Context(), "", api.NewStartOrder{TaskID: task.TaskID, Host: "hostb"}); createErr != nil {
			t.Error(createErr)
		}
	})
	requireEventNames(t, events, err, "snapshot", "start-order-change")
	got := events[1].StartOrders
	if len(got) != 1 || got[0].OrderID != order.ID || got[0].State != api.StartOrderPending || got[0].TaskID != task.TaskID {
		t.Fatalf("start-order-change = %+v, want order %s", got, order.ID)
	}
}
