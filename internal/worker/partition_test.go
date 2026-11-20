package worker

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

// partition makes the server unreachable at its address; heal serves it there
// again, so stored credentials keep their server URL.
func (h *harness) partition() (heal func()) {
	addr := h.http.Listener.Addr().String()
	h.http.Close()
	return func() {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			h.t.Fatal(err)
		}
		h.http = &httptest.Server{Listener: l, Config: &http.Server{Handler: h.srv.Handler()}}
		h.http.Start()
	}
}

// groupClaimed claims a group lease for host-a and stores its credential.
func (h *harness) groupClaimed() remote.Credential { return h.groupClaimedAs("partition-coordinator") }

// groupClaimedAs is groupClaimed with the execution ID of the coordinator.
func (h *harness) groupClaimedAs(execution string) remote.Credential {
	c, _ := h.groupClaimedFor("20261117-partition-group", execution)
	return c
}

// groupClaimedFor claims the named group and returns the claim request, which
// a successor needs to take the lease over.
func (h *harness) groupClaimedFor(group, execution string) (remote.Credential, api.GroupClaim) {
	t := h.t
	project := h.project("host-a")
	task, err := project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "partition-group", Title: "Partition group fixture", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	if task, err = project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	request := api.GroupClaim{ExecutionID: execution, Request: board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "public-label", Basis: "partition fixture", Members: []string{task.TaskID}}}
	grant, err := project.ClaimGroup(t.Context(), "", group, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := remote.Credential{ResourceKind: "group", ServerURL: h.http.URL, ClientID: "host-a", ProjectID: project.ID(), TaskID: group, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: grant.Claim.RenewedAt, AgentPID: 4242}
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	return c, request
}

// An outage longer than the lease TTL ends nothing: the credential stays live,
// the agent keeps running, and the lease is renewed at its generation once the
// server is back.
func TestWorkerKeepsEveryLeaseThroughAPartition(t *testing.T) {
	for name, setup := range map[string]func(*harness) remote.Credential{
		"task":        func(h *harness) remote.Credential { _, c := h.claimed("partition"); return c },
		"group":       func(h *harness) remote.Credential { return h.groupClaimed() },
		"integration": func(h *harness) remote.Credential { _, c := h.integration(); return c },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			logs := &recording{}
			h.loop.o.Logger = slog.New(logs)
			h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
				return api.AgentObservation{Status: "alive"}
			}
			c := setup(h)
			heal := h.partition()
			for _, wait := range []time.Duration{91 * time.Second, 30 * time.Second} {
				h.advance(wait)
				if live, _ := h.pass(t.Context()); live != 1 {
					t.Fatal("a partition ended the credential")
				}
			}
			if got := h.credential(); got.Lost || len(h.stopped) != 0 {
				t.Fatalf("a partition stopped the agent: %+v %v", got, h.stopped)
			}
			heal()
			h.advance(16 * time.Second)
			if live, err := h.pass(t.Context()); err != nil || live != 1 {
				t.Fatalf("renewal after the partition: %d %v", live, err)
			}
			if got := h.credential(); got.Lost || got.Generation != c.Generation || !got.RenewedAt.Equal(h.now()) || len(h.stopped) != 0 {
				t.Fatalf("lease after the partition: %+v %v", got, h.stopped)
			}
			if len(logs.find(slog.LevelInfo, "renewal confirmed again")) == 0 {
				t.Fatal("the renewal after the partition was not logged as confirmed again")
			}
			h.loop.mu.Lock()
			outages := len(h.loop.disconnected)
			h.loop.mu.Unlock()
			if outages != 0 {
				t.Fatalf("the renewal after the partition left %d disconnect records", outages)
			}
		})
	}
}

// A credential whose client cannot be built is retried, never lost.
func TestWorkerRetriesWhenItCannotCreateAClient(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("bad-client")
	c.ServerURL = "http://[::1"
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if live, _ := h.pass(t.Context()); live != 2 {
		t.Fatalf("a client that cannot be built ended a credential: %d", live)
	}
	for _, got := range mustCredentials(t, h.home) {
		if got.Lost || len(h.stopped) != 0 {
			t.Fatalf("a client that cannot be built stopped the agent: %+v %v", got, h.stopped)
		}
	}
}

// A refusal is read against the server's view: while the view cannot be read,
// or still shows the credential's generation, the agent keeps running.
func TestWorkerKeepsTheAgentWhenARefusalProvesNothing(t *testing.T) {
	for name, failShow := range map[string]bool{"unreadable view": true, "same generation": false} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			_, c := h.claimed("refusal")
			if failShow {
				h.http.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/tasks/"+c.TaskID) {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					h.srv.Handler().ServeHTTP(w, r)
				})
			}
			c.Token = strings.Repeat("0", len(c.Token))
			forged := c
			forged.ExecutionID = "forged-execution"
			if err := remote.WriteCredential(h.home, forged); err != nil {
				t.Fatal(err)
			}
			h.advance(16 * time.Second)
			if live, _ := h.pass(t.Context()); live != 2 {
				t.Fatalf("a refusal that proves nothing ended a credential: %d", live)
			}
			for _, got := range mustCredentials(t, h.home) {
				if got.Lost {
					t.Fatalf("a refusal that proves nothing marked a credential lost: %+v", got)
				}
			}
			if len(h.stopped) != 0 {
				t.Fatalf("a refusal that proves nothing stopped the agent: %v", h.stopped)
			}
		})
	}
}

func mustCredentials(t *testing.T, home string) []remote.Credential {
	t.Helper()
	all, err := remote.Credentials(home)
	if err != nil {
		t.Fatal(err)
	}
	return all
}
