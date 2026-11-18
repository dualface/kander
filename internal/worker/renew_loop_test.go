package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

// waitCap bounds every wait on a channel event. It only matters when the test
// is about to fail; a passing run never waits for it.
const waitCap = 30 * time.Second

// isRenewal reports whether the request renews a task, group or integration
// lease, and names the resource it renews.
func isRenewal(r *http.Request) (resource string, ok bool) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/renew") {
		return "", false
	}
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/renew"), "/")
	return parts[len(parts)-1], true
}

// serveWorker runs Serve with the options until the returned stop function is
// called; stop cancels the worker and waits for it to return.
func serveWorker(t *testing.T, o Options) (stop func()) {
	t.Helper()
	if o.Tick <= 0 {
		o.Tick = 10 * time.Millisecond
	}
	o.IdleExit = time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, o) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(waitCap):
			t.Error("Serve did not return")
		}
	}
}

// expectRenewalOf waits for one renewal event of each resource.
func expectRenewalOf(t *testing.T, events <-chan string, resources ...string) {
	t.Helper()
	missing := map[string]bool{}
	for _, r := range resources {
		missing[r] = true
	}
	timeout := time.After(waitCap)
	for len(missing) != 0 {
		select {
		case r := <-events:
			delete(missing, r)
		case <-timeout:
			t.Fatalf("no renewal reached the server for %v", missing)
		}
	}
}

// renewalEvents wraps the server so every answered renewal is announced on the
// channel.
func renewalEvents() (chan string, func(http.Handler) http.Handler) {
	events := make(chan string, 64)
	return events, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if resource, ok := isRenewal(r); ok {
				select {
				case events <- resource:
				default:
				}
			}
		})
	}
}

// requireRenewedAt checks the lease the server holds, not the local record.
func requireRenewedAt(t *testing.T, p *client.Project, c remote.Credential, want time.Time) {
	t.Helper()
	shown, err := p.Show(t.Context(), c.TaskID)
	if err != nil || shown.Claim == nil || !shown.Claim.RenewedAt.Equal(want) {
		t.Fatalf("the server did not record the renewal of %s at %v: %+v %v", c.TaskID, want, shown.Claim, err)
	}
}

// hookGate blocks a worker hook until the test lets it go, and reports the
// first time the hook is entered. The hook is let go when the test ends, so a
// test that fails early leaves no goroutine blocked in it.
type hookGate struct {
	once    sync.Once
	opened  sync.Once
	entered chan struct{}
	release chan struct{}
}

func newHookGate(t *testing.T) *hookGate {
	g := &hookGate{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(g.let)
	return g
}

func (g *hookGate) hold() {
	g.once.Do(func() { close(g.entered) })
	<-g.release
}

// let releases the hook, now and for every later call.
func (g *hookGate) let() { g.opened.Do(func() { close(g.release) }) }

func (g *hookGate) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(waitCap):
		t.Fatal("the hook was not entered")
	}
}

// requireRenewalsWhileHeld waits until both credentials are renewed, the hook
// is blocked, and then makes the leases due again: the work loop stays in the
// hook, so a renewal that reaches the server now comes from the renewal loop
// alone.
func requireRenewalsWhileHeld(t *testing.T, h *harness, events <-chan string, gate *hookGate, p *client.Project, first, second remote.Credential) {
	t.Helper()
	expectRenewalOf(t, events, first.TaskID, second.TaskID)
	gate.waitEntered(t)
	requireRenewedAt(t, p, first, h.now())
	requireRenewedAt(t, p, second, h.now())
	h.advance(16 * time.Second)
	expectRenewalOf(t, events, first.TaskID, second.TaskID)
	requireRenewedAt(t, p, first, h.now())
	requireRenewedAt(t, p, second, h.now())
}

// A slow observation of one credential must not hold back the renewal of any
// credential, neither in the first batch nor in a later one.
func TestRenewalDoesNotWaitForAnObservation(t *testing.T) {
	events, wrap := renewalEvents()
	h := newHarnessWrapped(t, 0, wrap)
	p, first := h.claimedAs("a-first", "exec-first")
	_, second := h.claimedAs("b-second", "exec-second")
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
	requireRenewalsWhileHeld(t, h, events, gate, p, first, second)
}

// A slow terminal delivery of one credential must not hold back the renewal of
// any credential, neither in the first batch nor in a later one.
func TestRenewalDoesNotWaitForADelivery(t *testing.T) {
	events, wrap := renewalEvents()
	h := newHarnessWrapped(t, 0, wrap)
	p, first := h.claimedAs("a-first", "exec-first", true)
	_, second := h.claimedAs("b-second", "exec-second", true)
	for _, c := range []remote.Credential{first, second} {
		if _, err := h.project("host-b").CreateRequest(t.Context(), "", c.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix it", TargetGeneration: 1}); err != nil {
			t.Fatal(err)
		}
	}
	gate := newHookGate(t)
	o := h.workerOptions()
	o.Deliver = func(string, string, string) error {
		gate.hold()
		return nil
	}
	h.advance(16 * time.Second)
	stop := serveWorker(t, o)
	defer stop()
	defer gate.let() // runs before stop: the worker cannot return from a held hook
	requireRenewalsWhileHeld(t, h, events, gate, p, first, second)
}

// renewalGate holds every renewal at the server until want of them are in
// flight at once, then lets all of them pass. peak is the highest number in
// flight together.
type renewalGate struct {
	mu       sync.Mutex
	want     int
	inflight int
	peak     int
	open     chan struct{}
	once     sync.Once
}

func newRenewalGate(want int) *renewalGate {
	return &renewalGate{want: want, open: make(chan struct{})}
}

func (g *renewalGate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := isRenewal(r); !ok {
			next.ServeHTTP(w, r)
			return
		}
		g.mu.Lock()
		g.inflight++
		g.peak = max(g.peak, g.inflight)
		if g.inflight >= g.want {
			g.once.Do(func() { close(g.open) })
		}
		g.mu.Unlock()
		select {
		case <-g.open:
		case <-time.After(waitCap):
		}
		next.ServeHTTP(w, r)
		g.mu.Lock()
		g.inflight--
		g.mu.Unlock()
	})
}

func (g *renewalGate) highest() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

// Renewals are made together: the server waits until all four are in flight.
func TestRenewalsRunConcurrently(t *testing.T) {
	gate := newRenewalGate(4)
	h := newHarnessWrapped(t, 0, gate.wrap)
	for i := range 4 {
		h.claimedAs(fmt.Sprintf("concurrent-%d", i), fmt.Sprintf("exec-%d", i))
	}
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	if got := gate.highest(); got != 4 {
		t.Fatalf("the four due renewals must be in flight together: peak %d", got)
	}
}

// No more than renewConcurrency renewals are in flight, however many are due.
// The bubble of synctest runs the calls without a network: it returns from Wait
// only when every goroutine is blocked, so a call that a missing cap would have
// started shows up in the count without any wait on the clock.
func TestRenewalConcurrencyIsLimited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const due = 12
		var mu sync.Mutex
		inflight, peak, finished := 0, 0, 0
		release := make(chan struct{})
		renew := func(context.Context, remote.Credential) {
			mu.Lock()
			inflight++
			peak = max(peak, inflight)
			mu.Unlock()
			<-release
			mu.Lock()
			inflight--
			finished++
			mu.Unlock()
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			renewBounded(t.Context(), make([]remote.Credential, due), renew)
		}()
		synctest.Wait()
		mu.Lock()
		held := inflight
		mu.Unlock()
		if held != renewConcurrency {
			t.Fatalf("%d renewals in flight with %d due, want %d", held, due, renewConcurrency)
		}
		close(release)
		<-done
		if peak != renewConcurrency || finished != due {
			t.Fatalf("peak %d (want %d), finished %d (want %d)", peak, renewConcurrency, finished, due)
		}
	})
}

// takeOver lets host-b take the task over from the stale host-a execution.
func (h *harness) takeOver(c remote.Credential) {
	h.t.Helper()
	if _, err := h.project("host-b").Takeover(h.t.Context(), "", c.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-b-" + c.TaskID, Owner: "codex", Decision: "user", ExpectedGeneration: c.Generation}); err != nil {
		h.t.Fatal(err)
	}
}

// takeOverGroup lets host-b take the group lease over from host-a.
func (h *harness) takeOverGroup(c remote.Credential, request api.GroupClaim) {
	h.t.Helper()
	b := h.project("host-b")
	view, err := b.Group(h.t.Context(), c.TaskID)
	if err != nil {
		h.t.Fatal(err)
	}
	request.ExecutionID = "successor-" + c.TaskID
	request.ExpectedGeneration = c.Generation
	request.Decision = "explicit takeover"
	request.Request.ExpectedRevision = view.Checkpoint.Revision
	request.Request.ExpectedEpoch = view.Checkpoint.Authority.Epoch
	if _, err := b.ClaimGroup(h.t.Context(), "", c.TaskID, request, nil); err != nil {
		h.t.Fatal(err)
	}
}

// A taken-over lease is only marked by the renewal loop; the work loop stops
// the agent, once.
func TestRenewalLoopMarksALostLeaseAndTheWorkLoopStopsTheAgent(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("lost")
	h.advance(100 * time.Second)
	h.takeOver(c)
	h.loop.renewRound(t.Context())
	if got := h.credential(); !got.Lost || len(h.stopped) != 0 {
		t.Fatalf("the renewal loop marks the credential and stops nothing: %+v %v", got, h.stopped)
	}
	for range 2 {
		if live, err := h.loop.workRound(t.Context()); err != nil || live != 0 {
			t.Fatalf("a lost credential is not live: %d %v", live, err)
		}
		if len(h.stopped) != 1 || h.stopped[0] != 4242 {
			t.Fatalf("the work loop stops the agent exactly once: %v", h.stopped)
		}
	}
}

// serveInOrder makes the loops list the credentials sorted by task ID and fails
// the test unless that is the given order. The directory order of the client
// home depends on the file system, and a test that blocks one work step needs
// to know which credentials come after it.
func serveInOrder(t *testing.T, h *harness, ids ...string) {
	t.Helper()
	h.loop.scan = func(home string) ([]remote.Credential, error) {
		all, err := remote.Credentials(home)
		slices.SortFunc(all, func(a, b remote.Credential) int { return strings.Compare(a.TaskID, b.TaskID) })
		return all, err
	}
	all, err := h.loop.scan(h.home)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range all {
		got = append(got, c.TaskID)
	}
	if !slices.Equal(got, ids) {
		t.Fatalf("the work loop serves credentials in the order %v, not %v", got, ids)
	}
}

// stepGate is a hookGate for the observation of one task.
type stepGate struct {
	*hookGate
	task string
}

func newStepGate(t *testing.T, task string) *stepGate {
	return &stepGate{hookGate: newHookGate(t), task: task}
}

func (g *stepGate) hold(c remote.Credential) {
	if c.TaskID == g.task {
		g.hookGate.hold()
	}
}

// background runs fn in a goroutine; the returned function waits for it.
func background(t *testing.T, fn func()) (wait func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	return func() {
		t.Helper()
		select {
		case <-done:
		case <-time.After(waitCap):
			t.Fatal("the background call did not return")
		}
	}
}

// A lease lost during a work round is stopped before the next work step, not
// after the round.
func TestLostCredentialIsStoppedBetweenWorkSteps(t *testing.T) {
	h := newHarness(t)
	_, x := h.claimedAs("a-x", "exec-x")
	_, y := h.claimedAs("b-y", "exec-y")
	_, z := h.claimedAs("c-z", "exec-z")
	serveInOrder(t, h, x.TaskID, y.TaskID, z.TaskID)
	gate := newStepGate(t, y.TaskID)
	var stoppedBeforeZ atomic.Int64
	stoppedBeforeZ.Store(-1)
	h.loop.o.Observe = func(_ context.Context, c remote.Credential) api.AgentObservation {
		gate.hold(c)
		if c.TaskID == z.TaskID {
			stoppedBeforeZ.Store(int64(len(h.stoppedPIDs())))
		}
		return api.AgentObservation{Status: "alive"}
	}
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	wait := background(t, func() { _, _ = h.loop.workRound(t.Context()) })
	gate.waitEntered(t)
	h.advance(100 * time.Second)
	h.takeOver(x)
	h.loop.renewRound(t.Context())
	gate.let()
	wait()
	if got := stoppedBeforeZ.Load(); got != 1 {
		t.Fatalf("the agent of the lost credential must be stopped before the next work step: %d stopped when it began", got)
	}
}

// Without a confirmed renewal the work loop leaves the credential alone.
func TestWorkLoopSkipsACredentialWhoseRenewalDidNotConfirm(t *testing.T) {
	h := newHarness(t)
	var observed atomic.Int32
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		observed.Add(1)
		return api.AgentObservation{Status: "alive"}
	}
	h.claimed("unconfirmed")
	h.partition()
	h.advance(16 * time.Second)
	if live, err := h.pass(t.Context()); err != nil || live != 1 {
		t.Fatalf("an unconfirmed renewal ends nothing: %d %v", live, err)
	}
	if observed.Load() != 0 || h.credential().Lost {
		t.Fatalf("the credential was served or lost: observed %d", observed.Load())
	}
}

// recording is a log handler that keeps every record. onHandle, when set, runs
// before the record is kept and may block the logging call.
type recording struct {
	mu       sync.Mutex
	records  []slog.Record
	onHandle func(slog.Record)
}

func (r *recording) Enabled(context.Context, slog.Level) bool { return true }
func (r *recording) Handle(_ context.Context, rec slog.Record) error {
	if r.onHandle != nil {
		r.onHandle(rec)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}
func (r *recording) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recording) WithGroup(string) slog.Handler      { return r }

// find returns the records of the level with the message.
func (r *recording) find(level slog.Level, msg string) []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []slog.Record
	for _, rec := range r.records {
		if rec.Level == level && rec.Message == msg {
			out = append(out, rec)
		}
	}
	return out
}

// errors returns the records at ERROR level.
func (r *recording) errors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, rec := range r.records {
		if rec.Level >= slog.LevelError {
			out = append(out, rec.Message)
		}
	}
	return out
}

func attr(rec slog.Record, key string) (slog.Value, bool) {
	var found slog.Value
	var ok bool
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found, ok = a.Value, true
			return false
		}
		return true
	})
	return found, ok
}

func TestSlowWorkRoundIsLogged(t *testing.T) {
	h := newHarness(t)
	logs := &recording{}
	h.loop.o.Logger = slog.New(logs)
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		h.advance(16 * time.Second)
		return api.AgentObservation{Status: "alive"}
	}
	h.claimed("slow-round")
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	slow := logs.find(slog.LevelWarn, "work round exceeded the renewal interval")
	if len(slow) != 1 {
		t.Fatalf("one warning for the slow work round expected: %d", len(slow))
	}
	if d, ok := attr(slow[0], "duration"); !ok || d.Duration() < 16*time.Second {
		t.Fatalf("the warning must carry the duration: %v", d)
	}
	if n := len(logs.find(slog.LevelWarn, "renewal round exceeded the renewal interval")); n != 0 {
		t.Fatalf("the renewal batch was quick: %d warnings", n)
	}
}

func TestSlowRenewalRoundIsLogged(t *testing.T) {
	var current atomic.Pointer[harness]
	h := newHarnessWrapped(t, 0, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if _, ok := isRenewal(r); ok {
				current.Load().advance(16 * time.Second)
			}
		})
	})
	current.Store(h)
	logs := &recording{}
	h.loop.o.Logger = slog.New(logs)
	h.claimed("slow-renewal")
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	if n := len(logs.find(slog.LevelWarn, "renewal round exceeded the renewal interval")); n != 1 {
		t.Fatalf("one warning for the slow renewal batch expected: %d", n)
	}
	if n := len(logs.find(slog.LevelWarn, "work round exceeded the renewal interval")); n != 0 {
		t.Fatalf("no work round ran: %d warnings", n)
	}
}

// A claim released while its work step runs is not an error of the step.
func TestWorkStepToleratesAReleasedCredential(t *testing.T) {
	h := newHarness(t)
	logs := &recording{}
	h.loop.o.Logger = slog.New(logs)
	p, c := h.claimed("released-midway")
	gate := newStepGate(t, c.TaskID)
	h.loop.o.Observe = func(_ context.Context, c remote.Credential) api.AgentObservation {
		gate.hold(c)
		return api.AgentObservation{Status: "alive"}
	}
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	wait := background(t, func() { _, _ = h.loop.workRound(t.Context()) })
	gate.waitEntered(t)
	if _, err := p.Move(t.Context(), "", c.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "test"}, c.Claim()); err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	if all := mustCredentials(t, h.home); len(all) != 0 {
		t.Fatalf("the renewal loop removes a released credential: %+v", all)
	}
	gate.let()
	wait()
	if got := logs.errors(); len(got) != 0 {
		t.Fatalf("a released credential is not an error of the work step: %v", got)
	}
}

// Two writers of one credential file keep each other's facts.
func TestConcurrentWritersKeepEveryFact(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("writers")
	delivered := c
	delivered.Delivered = []string{"request-1"}
	newer := c
	newer.RenewedAt = c.RenewedAt.Add(time.Minute)
	lost := c
	lost.Lost, lost.LostReason = true, "taken over"
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, writer := range []remote.Credential{delivered, newer, lost, c} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 25 {
				if err := remote.WriteCredential(h.home, writer); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	got := h.credential()
	if !slices.Contains(got.Delivered, "request-1") || !got.RenewedAt.Equal(newer.RenewedAt) || !got.Lost {
		t.Fatalf("a writer erased a fact of another: delivered=%v renewed=%v lost=%v", got.Delivered, got.RenewedAt, got.Lost)
	}
}

// credentialOf reads the stored credential of c.
func (h *harness) credentialOf(c remote.Credential) remote.Credential {
	for _, got := range mustCredentials(h.t, h.home) {
		if keyOf(got) == keyOf(c) {
			return got
		}
	}
	h.t.Fatalf("credential of %s is gone", c.TaskID)
	return remote.Credential{}
}

// A task and the group it coordinates share an execution ID and are served as
// two credentials.
func TestCredentialsSharingAnExecutionAreServedSeparately(t *testing.T) {
	h := newHarness(t)
	_, task := h.claimed("shared", true)
	group := h.groupClaimedAs("exec-a")
	observed := map[string]int{}
	h.loop.o.Observe = func(_ context.Context, c remote.Credential) api.AgentObservation {
		observed[c.TaskID]++
		return api.AgentObservation{Status: "alive"}
	}
	r, err := h.project("host-b").CreateRequest(t.Context(), "", task.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix it", TargetGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if live, err := h.pass(t.Context()); err != nil || live != 2 {
		t.Fatalf("both credentials are live: %d %v", live, err)
	}
	if observed[task.TaskID] != 1 || observed[group.TaskID] != 1 {
		t.Fatalf("each credential is observed once: %v", observed)
	}
	if len(h.lines) != 1 || !strings.Contains(h.lines[0], r.ID) {
		t.Fatalf("the request of the task is delivered once: %q", h.lines)
	}
}

// The failed renewal of one credential does not hold back another credential
// of the same execution.
func TestAFailingRenewalDoesNotPaceACredentialOfTheSameExecution(t *testing.T) {
	var failing atomic.Pointer[string]
	h := newHarnessWrapped(t, 0, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if resource, ok := isRenewal(r); ok && failing.Load() != nil && resource == *failing.Load() {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	_, task := h.claimed("failing")
	group := h.groupClaimedAs("exec-a")
	failing.Store(&task.TaskID)
	observed := map[string]int{}
	h.loop.o.Observe = func(_ context.Context, c remote.Credential) api.AgentObservation {
		observed[c.TaskID]++
		return api.AgentObservation{Status: "alive"}
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if observed[group.TaskID] != 1 || observed[task.TaskID] != 0 {
		t.Fatalf("only the credential whose renewal was confirmed is served: %v", observed)
	}
	if got := h.credentialOf(group); !got.RenewedAt.Equal(h.now()) {
		t.Fatalf("the group lease was not renewed: %v", got.RenewedAt)
	}
}

// A credential the work round read before it was lost is not served from that
// stale copy, and its agent is stopped once.
func TestALostCredentialBehindABlockedStepIsStoppedOnce(t *testing.T) {
	h := newHarness(t)
	_, a := h.claimed("a-task")
	g, request := h.groupClaimedFor("29991231-late-group", "exec-group")
	serveInOrder(t, h, a.TaskID, g.TaskID)
	gate := newStepGate(t, a.TaskID)
	h.loop.o.Observe = func(_ context.Context, c remote.Credential) api.AgentObservation {
		if c.TaskID == g.TaskID {
			// A stale copy of the lost group would be observed as stopped and
			// stopped a second time.
			return api.AgentObservation{Status: "stopped", Reason: "test"}
		}
		gate.hold(c)
		return api.AgentObservation{Status: "alive"}
	}
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	var live atomic.Int64
	wait := background(t, func() { n, _ := h.loop.workRound(t.Context()); live.Store(int64(n)) })
	gate.waitEntered(t)
	h.advance(100 * time.Second)
	h.takeOverGroup(g, request)
	h.loop.renewRound(t.Context())
	gate.let()
	wait()
	if got := h.stoppedPIDs(); len(got) != 1 {
		t.Fatalf("the agent of the lost group is stopped once: %v", got)
	}
	if live.Load() != 1 {
		t.Fatalf("only the task is live: %d", live.Load())
	}
	if n, err := h.loop.workRound(t.Context()); err != nil || n != 1 || len(h.stoppedPIDs()) != 1 {
		t.Fatalf("a later round neither serves nor stops the lost group again: live=%d err=%v stopped=%v", n, err, h.stoppedPIDs())
	}
	if got := h.credentialOf(g); !strings.HasPrefix(got.StopResult, "stopped:") {
		t.Fatalf("the stop result is kept: %q", got.StopResult)
	}
}

// The renewal loop and the work loop both scan the credentials; one failed scan
// is retried.
func TestCredentialScanRetriesOnce(t *testing.T) {
	h := newHarness(t)
	logs := &recording{}
	h.loop.o.Logger = slog.New(logs)
	_, c := h.claimed("scan")
	var scans atomic.Int32
	h.loop.scan = func(home string) ([]remote.Credential, error) {
		if scans.Add(1) == 1 {
			return nil, errors.New("credential vanished during the scan")
		}
		return remote.Credentials(home)
	}
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	if got := h.credentialOf(c); !got.RenewedAt.Equal(h.now()) {
		t.Fatalf("the round did not renew after the retried scan: %v", got.RenewedAt)
	}
	if got := logs.errors(); len(got) != 0 {
		t.Fatalf("a retried scan is not an error: %v", got)
	}
}

// Stopping the renewal loop cancels the renewal in flight; the cancellation is
// no outage of the server.
func TestStoppingTheRenewalLoopRecordsNoOutage(t *testing.T) {
	var held atomic.Int32
	inflight := make(chan struct{}, 1)
	h := newHarnessWrapped(t, 0, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := isRenewal(r); ok && held.Add(1) == 1 {
				inflight <- struct{}{}
				<-r.Context().Done()
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	logs := &recording{}
	h.loop.o.Logger = slog.New(logs)
	h.loop.o.Tick = 10 * time.Millisecond
	_, c := h.claimed("stopped-midway")
	h.advance(16 * time.Second)
	h.loop.startRenewals(t.Context())
	t.Cleanup(h.loop.stopRenewals)
	select {
	case <-inflight:
	case <-time.After(waitCap):
		t.Fatal("no renewal reached the server")
	}
	h.loop.stopRenewals()
	h.loop.mu.Lock()
	outages := len(h.loop.disconnected)
	h.loop.mu.Unlock()
	if outages != 0 || len(logs.find(slog.LevelWarn, "renewal failed; retrying")) != 0 {
		t.Fatalf("a cancelled renewal is not an outage: %d recorded", outages)
	}
	h.loop.startRenewals(t.Context())
	waitFor(t, "the renewal of the restarted loop", func() bool { return h.credentialOf(c).RenewedAt.Equal(h.now()) })
}

// observedBy counts the observations per task.
type observedBy struct {
	mu sync.Mutex
	n  map[string]int
}

func (o *observedBy) add(task string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.n == nil {
		o.n = map[string]int{}
	}
	o.n[task]++
}

func (o *observedBy) count(task string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.n[task]
}

// A confirmed renewal owes one work step. A later renewal that the server did
// not confirm, or the release of the claim, withdraws it: the work loop must not
// serve the credential when it gets to it behind a slow step. The log call that
// reports the outcome is held, so the work must be withdrawn before it.
func TestLaterRenewalOutcomeWithdrawsTheOwedWork(t *testing.T) {
	for outcome, message := range map[string]string{"failure": "renewal failed; retrying", "release": "claim released; credential removed"} {
		t.Run(outcome, func(t *testing.T) {
			var failing atomic.Pointer[string]
			h := newHarnessWrapped(t, 0, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if resource, ok := isRenewal(r); ok && failing.Load() != nil && resource == *failing.Load() {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					next.ServeHTTP(w, r)
				})
			})
			_, a := h.claimedAs("a-slow", "exec-a")
			pb, b := h.claimedAs("b-later", "exec-b")
			serveInOrder(t, h, a.TaskID, b.TaskID)
			gate := newStepGate(t, a.TaskID)
			logGate := newHookGate(t)
			logs := &recording{onHandle: func(rec slog.Record) {
				if rec.Message == message {
					logGate.hold()
				}
			}}
			h.loop.o.Logger = slog.New(logs)
			var observed observedBy
			h.loop.o.Observe = func(_ context.Context, c remote.Credential) api.AgentObservation {
				observed.add(c.TaskID)
				gate.hold(c)
				return api.AgentObservation{Status: "alive"}
			}
			h.advance(16 * time.Second)
			h.loop.renewRound(t.Context())
			wait := background(t, func() { _, _ = h.loop.workRound(t.Context()) })
			gate.waitEntered(t)
			// b was renewed and is owed a work step behind the blocked step of a.
			if outcome == "failure" {
				failing.Store(&b.TaskID)
			} else if _, err := pb.Move(t.Context(), "", b.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "test"}, b.Claim()); err != nil {
				t.Fatal(err)
			}
			h.advance(16 * time.Second)
			renewed := background(t, func() { h.loop.renewRound(t.Context()) })
			logGate.waitEntered(t)
			// The renewal of b is inside its log call; a goes on to b now.
			gate.let()
			wait()
			if got := observed.count(b.TaskID); got != 0 {
				t.Fatalf("the work owed by an earlier renewal ran after a %s: %d observations", outcome, got)
			}
			logGate.let()
			renewed()
		})
	}
}

// A lease lost during the last work step of a round is stopped in that round.
func TestLostMarkOfTheLastWorkStepIsStoppedInTheSameRound(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("last-step")
	gate := newStepGate(t, c.TaskID)
	h.loop.o.Observe = func(_ context.Context, c remote.Credential) api.AgentObservation {
		gate.hold(c)
		return api.AgentObservation{Status: "alive"}
	}
	h.advance(16 * time.Second)
	h.loop.renewRound(t.Context())
	wait := background(t, func() { _, _ = h.loop.workRound(t.Context()) })
	gate.waitEntered(t)
	h.advance(100 * time.Second)
	h.takeOver(c)
	h.loop.renewRound(t.Context())
	gate.let()
	wait()
	if got := h.stoppedPIDs(); len(got) != 1 {
		t.Fatalf("the agent must be stopped before the round ends: %v", got)
	}
}
