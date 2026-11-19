// Package worker is the per-user background process of a v2 client. It
// renews the leases of this host's claims, and when a lease is lost it stops
// the managed agent of that execution and keeps the worktree as it is. One
// worker runs per client home; it outlives the TUI and the command that
// started it, and exits once no live claim is left.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/version"
)

// idleExit is how long a worker without live claims stays before exiting.
const idleExit = 2 * time.Minute

// Options configure one worker loop; tests shorten the timings.
type Options struct {
	Observe  func(context.Context, remote.Credential) api.AgentObservation
	Home     string
	Tick     time.Duration
	IdleExit time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
	// Stop overrides process termination in tests. The default verifies the
	// recorded process incarnation before signaling its kernel handle.
	Stop func(pid int) error
	// Resolve finds the agent recorded by the card's WINDOW and SESSION and
	// returns the way to type a line into it; the default is Resolve.
	Resolve func(window, session string) (Prompt, error)
	// The fields below drive the handover to a replaced binary; the defaults
	// are the production behavior and tests replace them to start no process.
	//
	// Executable names the file this worker started from; the default is
	// os.Executable.
	Executable func() (string, error)
	// Identity reads the identity of the file at a path; the default reads it
	// through an open handle.
	Identity func(path string) (os.FileInfo, error)
	// Probe checks that the replaced binary works; the default runs
	// `<executable> version` with a bounded timeout.
	Probe func(ctx context.Context, executable string) error
	// Spawn starts the worker of the replaced binary for a client home; the
	// default is remote.StartWorker.
	Spawn func(home, executable string) (Successor, error)
	// Held reports whether a worker holds the lock of a client home; the
	// default is remote.WorkerHeld.
	Held func(home string) (bool, error)
	// ConfirmTimeout and ConfirmPoll bound the wait for the started worker to
	// take the lock and set its pace.
	ConfirmTimeout time.Duration
	ConfirmPoll    time.Duration
}

// Prompt types one line into a resolved agent pane. An error leaves the outcome
// uncertain: the line may have reached the pane.
type Prompt func(text string) error

// Resolve is the terminal side of the delivery of collaboration requests. It
// finds the agent pane that a card's WINDOW and SESSION name and checks that the
// pane takes a line, without typing anything: an error means nothing was typed.
// The notify package sets it at init, so this package stays free of terminal
// code; a binary without it keeps requests open for the holder to read.
var Resolve func(window, session string) (Prompt, error)

// Run is `kander worker`: it holds the worker lock of the client home and
// renews until idle or signaled.
func Run(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: kander worker")
		return 2
	}
	home, err := remote.Home()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kander worker:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = Serve(ctx, Options{Home: home, Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil))})
	if errors.Is(err, ErrRunning) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kander worker:", err)
		return 1
	}
	return 0
}

// ErrRunning reports that another worker holds the lock.
var ErrRunning = errors.New("another worker is running")

// Serve runs the renewal loop and the work loop until ctx ends or the worker
// is idle.
func Serve(ctx context.Context, o Options) error {
	if o.Tick <= 0 {
		o.Tick = time.Second
	}
	if o.IdleExit <= 0 {
		o.IdleExit = idleExit
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Resolve == nil {
		o.Resolve = Resolve
	}
	o.defaultHandover()
	if err := os.MkdirAll(o.Home, 0o700); err != nil {
		return err
	}
	lock := &workerLock{home: o.Home}
	if err := lock.acquire(ctx); err != nil {
		return err
	}
	defer lock.release()
	w := newLoop(o)
	self, err := newHandover(o)
	if err != nil {
		o.Logger.Warn("the executable of this worker cannot be identified; handover to a replaced binary is off", "error", err)
	}
	o.Logger.Info("worker started", "version", version.String(), "pid", os.Getpid(), "executable", self.path())
	// Registered after the lock release, so it runs first: only the worker that
	// holds worker.lock acts on credentials.
	w.startRenewals(ctx)
	defer w.stopRenewals()
	idleSince := o.Now()
	ticker := time.NewTicker(o.Tick)
	defer ticker.Stop()
	for {
		live, err := w.workRound(ctx)
		if err != nil {
			o.Logger.Error("worker pass failed", "error", err)
		}
		if live > 0 {
			idleSince = o.Now()
		} else if o.Now().Sub(idleSince) >= o.IdleExit {
			return nil
		}
		result, err := w.tryHandover(ctx, self, lock)
		if err != nil {
			return err
		}
		if result != handoverNone {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// loop holds the state of the two loops of one worker: the renewal loop in
// renew.go and the work loop below. The work loop observes, reports, delivers
// and stops agents, one credential after the other; it never renews, so no
// slow terminal can delay a renewal.
type loop struct {
	o Options
	// scan lists the credentials; tests replace it.
	scan func(home string) ([]remote.Credential, error)

	// mu guards every field below. It is held for map reads and writes only,
	// never across a call to the server or the file system.
	mu      sync.Mutex
	clients map[string]*client.Client
	// disconnected holds, per credential, the first and the latest renewal
	// attempt that reached no decision of the server since the last confirmed
	// renewal: retries are paced by the renewal interval and logged.
	disconnected map[credentialKey]outage
	// owed holds the credentials whose renewal was confirmed and whose work has
	// not run since. A credential gets one work step per confirmed renewal, so
	// nothing is observed, reported or delivered for a lease the server did not
	// confirm.
	owed map[credentialKey]struct{}
	// lostMarks holds the credentials the renewal loop marked lost and the work
	// loop has not stopped yet.
	lostMarks map[credentialKey]struct{}
	// blocked holds the executions whose request delivery failed before it typed
	// anything and has not succeeded since (delivery_block.go).
	blocked  map[credentialKey]blockState
	renewing *renewals
}

type outage struct{ since, last time.Time }

func newLoop(o Options) *loop {
	return &loop{
		o:            o,
		scan:         remote.Credentials,
		clients:      map[string]*client.Client{},
		disconnected: map[credentialKey]outage{},
		owed:         map[credentialKey]struct{}{},
		lostMarks:    map[credentialKey]struct{}{},
		blocked:      map[credentialKey]blockState{},
	}
}

func (w *loop) client(c remote.Credential) (*client.Client, error) {
	key := c.ServerURL + "\x00" + c.ClientID
	w.mu.Lock()
	defer w.mu.Unlock()
	if existing, ok := w.clients[key]; ok {
		return existing, nil
	}
	cl, err := client.New(c.ServerURL, c.ClientID)
	if err != nil {
		return nil, err
	}
	cl.Retries = 0
	w.clients[key] = cl
	return cl, nil
}

// credentials lists the credentials of the client home. The renewal loop can
// remove a released credential while the other loop is scanning, which fails
// the scan once; one retry reads the directory again.
func (w *loop) credentials() ([]remote.Credential, error) {
	all, err := w.scan(w.o.Home)
	if err == nil {
		return all, nil
	}
	w.o.Logger.Debug("credential scan retried", "error", err)
	return w.scan(w.o.Home)
}

// workRound serves every credential once and returns the number still live. It
// replaces the earlier serial pass: the renewals it used to make are the job of
// the renewal loop.
func (w *loop) workRound(ctx context.Context) (int, error) {
	started := w.o.Now()
	credentials, err := w.credentials()
	if err != nil {
		return 0, err
	}
	for i := range credentials {
		w.refreshGroupContext(&credentials[i])
	}
	w.forgetAbsent(credentials)
	stopped := map[credentialKey]struct{}{}
	budget := &resolveBudget{}
	live := 0
	for _, c := range credentials {
		w.stopMarked(ctx, stopped)
		key := keyOf(c)
		if _, done := stopped[key]; done {
			continue
		}
		if c.Lost {
			w.stopLost(ctx, w.o.Logger, c)
			continue
		}
		if !w.takeOwed(key) || w.work(ctx, c, budget) {
			live++
		}
	}
	// A lease lost during the last work step is stopped in this round, not the
	// next one, which may be a handover away.
	w.stopMarked(ctx, stopped)
	w.warnSlow("work round exceeded the renewal interval", started, credentials)
	return live, nil
}

// refreshGroupContext completes a group credential with the facts of the
// execution that coordinates the group: the agent of a task that also
// coordinates its own group is recorded on the task credential only.
func (w *loop) refreshGroupContext(c *remote.Credential) {
	if c.ResourceKind != "group" {
		return
	}
	if err := remote.RefreshExecutionContext(w.o.Home, c); err != nil {
		w.o.Logger.Error("group execution context could not be read", "group", c.TaskID, "error", err)
	} else if err := remote.WriteCredential(w.o.Home, *c); err != nil && !errors.Is(err, remote.ErrCredentialReleased) {
		w.o.Logger.Error("group execution context could not be saved", "group", c.TaskID, "error", err)
	}
}

// takeOwed reports whether the credential has a work step owed and clears it,
// so a work step that fails halfway is not repeated before the next confirmed
// renewal.
func (w *loop) takeOwed(key credentialKey) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, owed := w.owed[key]
	delete(w.owed, key)
	return owed
}

// forgetAbsent drops the bookkeeping of credentials that no longer exist. A lost
// credential keeps its marks but loses its delivery state: its agent is stopped,
// so nothing is delivered to it any more.
func (w *loop) forgetAbsent(credentials []remote.Credential) {
	present := make(map[credentialKey]struct{}, len(credentials))
	live := make(map[credentialKey]struct{}, len(credentials))
	for _, c := range credentials {
		present[keyOf(c)] = struct{}{}
		if !c.Lost {
			live[keyOf(c)] = struct{}{}
		}
	}
	w.forgetBlocks(live)
	w.mu.Lock()
	defer w.mu.Unlock()
	for key := range w.owed {
		if _, ok := present[key]; !ok {
			delete(w.owed, key)
		}
	}
	for key := range w.lostMarks {
		if _, ok := present[key]; !ok {
			delete(w.lostMarks, key)
		}
	}
	for key := range w.disconnected {
		if _, ok := present[key]; !ok {
			delete(w.disconnected, key)
		}
	}
}

// stopMarked stops the agents of the credentials the renewal loop marked lost
// since the last check. The work loop runs it before every work step, so a
// lost lease waits for at most the step in progress. The credentials it stops
// are added to stopped, which keeps the copy of the credential that was read at
// the start of the round from being served again. It reports whether it had
// marks to work on and read the credentials; a failed read leaves the marks for
// the next check and reports false.
func (w *loop) stopMarked(ctx context.Context, stopped map[credentialKey]struct{}) bool {
	w.mu.Lock()
	marks := w.lostMarks
	w.lostMarks = map[credentialKey]struct{}{}
	w.mu.Unlock()
	if len(marks) == 0 {
		return false
	}
	credentials, err := w.credentials()
	if err != nil {
		w.o.Logger.Error("lost credentials could not be read; their agents are stopped later", "error", err)
		w.mu.Lock()
		for key := range marks {
			w.lostMarks[key] = struct{}{}
		}
		w.mu.Unlock()
		return false
	}
	for _, c := range credentials {
		key := keyOf(c)
		if _, marked := marks[key]; !marked || !c.Lost {
			continue
		}
		w.refreshGroupContext(&c)
		w.stopLost(ctx, w.o.Logger, c)
		stopped[key] = struct{}{}
	}
	return true
}

// marked reports whether a lease is marked lost and not stopped yet.
func (w *loop) marked() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.lostMarks) > 0
}

// warnSlow logs a round that took longer than the shortest renewal interval of
// its credentials: a lease can lapse once a round takes longer than the TTL.
func (w *loop) warnSlow(msg string, started time.Time, credentials []remote.Credential) {
	interval := 15 * time.Second
	for i, c := range credentials {
		if i == 0 || renewInterval(c) < interval {
			interval = renewInterval(c)
		}
	}
	if duration := w.o.Now().Sub(started); duration > interval {
		w.o.Logger.Warn(msg, "duration", duration, "interval", interval, "credentials", len(credentials))
	}
}

// work is the half of a credential's step that follows a confirmed renewal: it
// observes the agent, reports the observation and delivers requests. It
// reports whether the credential is still live.
func (w *loop) work(ctx context.Context, c remote.Credential, budget *resolveBudget) bool {
	log := w.credentialLog(c)
	cl, err := w.client(c)
	if err != nil {
		log.Warn("worker client unavailable", "error", err)
		return true
	}
	if c.ResourceKind == "integration" {
		return w.integrationWork(ctx, log, c)
	}
	project := cl.Project(c.ProjectID)
	observeCtx, stopObservation := context.WithTimeout(ctx, 2*time.Second)
	observation := w.observe(observeCtx, c)
	stopObservation()
	if observation.Status != "unknown" {
		if c.ResourceKind != "group" {
			observation.Reason = w.withBlockedSuffix(keyOf(c), observation.Reason)
		}
		reportCtx, stopReport := context.WithTimeout(ctx, 5*time.Second)
		var reportErr error
		if c.ResourceKind == "group" {
			_, reportErr = project.ObserveGroup(reportCtx, client.NewKey(), c.TaskID, observation, *c.Claim())
		} else {
			_, reportErr = project.ObserveAgent(reportCtx, client.NewKey(), c.TaskID, observation, *c.Claim())
		}
		stopReport()
		if reportErr != nil {
			log.Warn("agent observation report failed", "error", reportErr)
		}
		if c.ResourceKind == "group" && observation.Status == "stopped" {
			w.lose(log, c, "agent stopped: "+observation.Reason)
			return false
		}
	}
	if c.ResourceKind != "group" && observation.Status != "stopped" {
		// The renewal runs in another loop, so a slow delivery cannot cost a
		// lease; each server call of the delivery has a time limit of its own.
		w.deliver(ctx, log, project, &c, budget)
	}
	if err := remote.WriteCredential(w.o.Home, c); errors.Is(err, remote.ErrCredentialReleased) {
		// The renewal loop removed the credential while this step ran.
		log.Debug("claim released during the work step")
	} else if err != nil {
		log.Error("credential write failed", "error", err)
	}
	return true
}

// lose marks the credential lost and stops the managed agent. The worktree,
// branch, and uncommitted changes are left untouched for manual handling. Only
// the work loop calls it: the renewal loop marks a lease lost and leaves the
// stop to the work loop.
func (w *loop) lose(log *slog.Logger, c remote.Credential, reason string) {
	if !w.markLost(log, c, reason) {
		return
	}
	c.Lost, c.LostReason = true, reason
	w.stopLost(context.Background(), log, c)
	w.mu.Lock()
	delete(w.lostMarks, keyOf(c))
	w.mu.Unlock()
}
