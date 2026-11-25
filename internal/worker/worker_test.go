package worker

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

type harness struct {
	t    *testing.T
	at   atomic.Int64
	srv  *server.Server
	http *httptest.Server
	home string
	loop *loop
	// mu guards stopped and lines, which the work loop writes while a Serve
	// level test reads them. Tests that drive a pass read the fields directly
	// after it returns; the others use stoppedPIDs and deliveredLines.
	mu      sync.Mutex
	stopped []int
	lines   []string
}

func (h *harness) now() time.Time          { return time.Unix(0, h.at.Load()) }
func (h *harness) advance(d time.Duration) { h.at.Add(int64(d)) }

func newHarness(t *testing.T) *harness { return newHarnessRenewing(t, 0) }

// newHarnessRenewing grants leases with the given renew interval; zero keeps
// the server default.
func newHarnessRenewing(t *testing.T, renew time.Duration) *harness {
	return newHarnessWrapped(t, renew, nil)
}

// newHarnessWrapped is newHarnessRenewing with a wrapper around the server
// handler. The wrapper is installed before the test server starts: assigning
// the handler of a running server is a data race.
func newHarnessWrapped(t *testing.T, renew time.Duration, wrap func(http.Handler) http.Handler) *harness {
	h := &harness{t: t, home: t.TempDir()}
	h.at.Store(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC).UnixNano())
	s, err := server.Open(server.Options{DataDir: t.TempDir(), Now: h.now, RenewInterval: renew, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	if wrap != nil {
		handler = wrap(handler)
	}
	h.srv, h.http = s, httptest.NewServer(handler)
	t.Cleanup(func() { h.http.Close(); h.srv.Close() })
	h.loop = newLoop(h.workerOptions())
	return h
}

// workerOptions are the options of the harness worker: a quiet logger, the
// fake clock, and hooks that record what the worker did.
func (h *harness) workerOptions() Options {
	return Options{Home: h.home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: h.now, Stop: func(pid int) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.stopped = append(h.stopped, pid)
		return nil
	}, Resolve: h.resolveRecording}
}

// resolveRecording is the default target resolution of the harness: it always
// resolves, and the prompt it returns records the lines typed.
func (h *harness) resolveRecording(window, session string) (Prompt, error) {
	return func(text string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.lines = append(h.lines, text)
		return nil
	}, nil
}

// pass is one tick of the worker with the loops run in turn: the renewals
// first, then the work they owe. A test drives it where it needs to step both
// loops without a clock or a goroutine.
func (h *harness) pass(ctx context.Context) (int, error) {
	h.loop.renewRound(ctx)
	return h.loop.workRound(ctx)
}

func (h *harness) stoppedPIDs() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.stopped...)
}

func (h *harness) deliveredLines() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.lines...)
}

func (h *harness) project(id string) *client.Project {
	c, err := client.New(h.http.URL, id)
	if err != nil {
		h.t.Fatal(err)
	}
	c.Backoff = time.Millisecond
	info, err := c.Register(h.t.Context(), "demo", "")
	if err != nil {
		h.t.Fatal(err)
	}
	return c.Project(info.ID)
}

// claimed creates a todo task, claims it for host-a, and stores the
// credential the worker renews.
func (h *harness) claimed(slug string, routing ...bool) (*client.Project, remote.Credential) {
	return h.claimedAs(slug, "exec-a", routing...)
}

// claimedAs is claimed with the execution ID of the claim.
func (h *harness) claimedAs(slug, execution string, routing ...bool) (*client.Project, remote.Credential) {
	var start *api.StartInput
	if len(routing) != 0 && routing[0] {
		start = &api.StartInput{Window: "tmux:$1:@1:%1", Session: "claude local-session"}
	}
	return h.claimedWith(slug, execution, start)
}

// claimedWith is claimedAs with the start metadata of the claim; a nil start
// claims without routing facts.
func (h *harness) claimedWith(slug, execution string, start *api.StartInput) (*client.Project, remote.Credential) {
	ctx := h.t.Context()
	p := h.project("host-a")
	task, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: slug, Title: slug, Language: "en"})
	if err != nil {
		h.t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if task, err = p.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		h.t.Fatal(err)
	}
	if _, err = p.Move(ctx, "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		h.t.Fatal(err)
	}
	input := api.ClaimRequest{ClientID: "host-a", ExecutionID: execution, Owner: "claude", Start: start}
	g, err := p.Claim(ctx, "", task.TaskID, input)
	if err != nil {
		h.t.Fatal(err)
	}
	c := remote.Credential{
		ServerURL: h.http.URL, ClientID: "host-a", ProjectID: p.ID(), TaskID: task.TaskID, ExecutionID: execution,
		Generation: g.Claim.Generation, Token: g.Token, RenewInterval: g.RenewInterval, TTL: g.TTL, RenewedAt: g.Claim.RenewedAt, AgentPID: 4242,
	}
	if input.Start != nil {
		c.Window, c.Session = input.Start.Window, input.Start.Session
		if _, err := p.Execution(ctx, "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, *c.Claim()); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := remote.WriteCredential(h.home, c); err != nil {
		h.t.Fatal(err)
	}
	return p, c
}

func (h *harness) credential() remote.Credential {
	all, err := remote.Credentials(h.home)
	if err != nil || len(all) != 1 {
		h.t.Fatalf("want one credential: %+v %v", all, err)
	}
	return all[0]
}

func TestWorkerRenewsBeyondTheTTL(t *testing.T) {
	h := newHarness(t)
	p, c := h.claimed("renew")
	for range 10 {
		h.advance(16 * time.Second)
		if live, err := h.pass(t.Context()); err != nil || live != 1 {
			t.Fatalf("pass: live=%d err=%v", live, err)
		}
	}
	shown, err := p.Show(t.Context(), c.TaskID)
	if err != nil || shown.Claim.Status != api.ClaimActive {
		t.Fatalf("160s of renewals must keep the 90s lease active: %+v %v", shown.Claim, err)
	}
	if len(h.stopped) != 0 {
		t.Fatalf("no agent may be stopped while the lease is live: %v", h.stopped)
	}
}

func TestWorkerStopsTheAgentWhenTakenOver(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("lost")
	h.advance(100 * time.Second)
	b := h.project("host-b")
	if _, err := b.Takeover(t.Context(), "", c.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-b", Owner: "codex", Decision: "user", ExpectedGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	if live, _ := h.pass(t.Context()); live != 0 {
		t.Fatalf("a taken-over credential is not live")
	}
	got := h.credential()
	if !got.Lost || len(h.stopped) != 1 || h.stopped[0] != 4242 {
		t.Fatalf("the managed agent must be stopped and the credential kept as lost: %+v %v", got, h.stopped)
	}
	if live, _ := h.pass(t.Context()); live != 0 || len(h.stopped) != 1 {
		t.Fatal("a lost credential is never renewed or stopped again")
	}
}

func TestWorkerRemovesReleasedClaims(t *testing.T) {
	h := newHarness(t)
	p, c := h.claimed("released")
	if _, err := p.Move(t.Context(), "", c.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "test"}, c.Claim()); err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if live, _ := h.pass(t.Context()); live != 0 {
		t.Fatal("a released claim is not live")
	}
	if all, _ := remote.Credentials(h.home); len(all) != 0 || len(h.stopped) != 0 {
		t.Fatalf("a released claim's credential is removed without stopping anything: %+v %v", all, h.stopped)
	}
}

func TestSecondWorkerExits(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		done <- Serve(ctx, Options{Home: home, Tick: 10 * time.Millisecond, IdleExit: time.Hour, Logger: quiet})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		held, err := remote.WorkerHeld(home)
		if err != nil {
			t.Fatal(err)
		}
		if held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first worker never took the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := Serve(t.Context(), Options{Home: home, Logger: quiet}); err != ErrRunning {
		t.Fatalf("second worker must exit, got %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkerDeliversRequestsOnce(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("deliver", true)
	b := h.project("host-b")
	r, err := b.CreateRequest(t.Context(), "", c.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix\nthe test", TargetGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		h.advance(16 * time.Second)
		if _, err := h.pass(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.lines) != 1 || !strings.Contains(h.lines[0], r.ID) || strings.Contains(h.lines[0], "\n") {
		t.Fatalf("one single-line delivery expected: %q", h.lines)
	}
	if !strings.HasSuffix(h.lines[0], "done|rejected [note])") {
		t.Fatalf("worker appended text after the request protocol: %q", h.lines[0])
	}
	requests, err := b.Requests(t.Context(), c.TaskID)
	if err != nil || requests[0].Status != api.RequestAccepted {
		t.Fatalf("a delivered request is accepted: %+v %v", requests, err)
	}
}

// An observation slower than the renew interval must not use up the time the
// same pass has for delivering requests.
func TestWorkerDeliversAfterASlowObservation(t *testing.T) {
	// The interval leaves the real HTTP calls a budget of a second; only the
	// observation is slower than it.
	const renew = time.Second
	h := newHarnessRenewing(t, renew)
	var observed atomic.Bool
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		time.Sleep(renew + renew/2)
		observed.Store(true)
		return api.AgentObservation{Status: "alive"}
	}
	_, c := h.claimed("slow-observation", true)
	r, err := h.project("host-b").CreateRequest(t.Context(), "", c.TaskID, api.NewCollabRequest{Kind: api.RequestSync, Message: "status", TargetGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	h.advance(2 * renew)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !h.credential().RenewedAt.Equal(h.now()) || !observed.Load() {
		t.Fatalf("the pass did not renew and observe: renewed=%v observed=%v", h.credential().RenewedAt, observed.Load())
	}
	if len(h.lines) != 1 || !strings.Contains(h.lines[0], r.ID) {
		t.Fatalf("the request was not delivered after a slow observation: %q", h.lines)
	}
}
