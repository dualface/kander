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
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
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
	// Deliver types one line into the agent recorded by the card's WINDOW
	// and SESSION; the default is Deliver.
	Deliver func(window, session, text string) error
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

// Deliver is the terminal delivery of collaboration requests. The notify
// package sets it at init, so this package stays free of terminal code; a
// binary without it keeps requests open for the holder to read.
var Deliver func(window, session, text string) error

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

// Serve runs the renewal loop until ctx ends or the worker is idle.
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
	if o.Deliver == nil {
		o.Deliver = Deliver
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
	w := &loop{o: o, clients: map[string]*client.Client{}}
	self, err := newHandover(o)
	if err != nil {
		o.Logger.Warn("the executable of this worker cannot be identified; handover to a replaced binary is off", "error", err)
	}
	o.Logger.Info("worker started", "version", version.String(), "pid", os.Getpid(), "executable", self.path())
	idleSince := o.Now()
	ticker := time.NewTicker(o.Tick)
	defer ticker.Stop()
	for {
		live, err := w.pass(ctx)
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

type loop struct {
	o       Options
	clients map[string]*client.Client
	// disconnected holds, per execution, the first and the latest renewal
	// attempt that reached no decision of the server since the last confirmed
	// renewal: retries are paced by the renewal interval and logged.
	disconnected map[string]outage
}

type outage struct{ since, last time.Time }

func (w *loop) client(c remote.Credential) (*client.Client, error) {
	key := c.ServerURL + "\x00" + c.ClientID
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

// pass renews every due credential once and returns the number still live.
func (w *loop) pass(ctx context.Context) (int, error) {
	credentials, err := remote.Credentials(w.o.Home)
	if err != nil {
		return 0, err
	}
	live := 0
	for _, c := range credentials {
		if c.ResourceKind == "group" {
			if err := remote.RefreshExecutionContext(w.o.Home, &c); err != nil {
				w.o.Logger.Error("group execution context could not be read", "group", c.TaskID, "error", err)
			} else if err := remote.WriteCredential(w.o.Home, c); err != nil {
				w.o.Logger.Error("group execution context could not be saved", "group", c.TaskID, "error", err)
			}
		}

		if c.Lost {
			w.stopLost(ctx, w.o.Logger, c)
			continue
		}
		if w.step(ctx, c) {
			live++
		}
	}
	return live, nil
}

// step handles one credential and reports whether it is still live.
func (w *loop) step(ctx context.Context, c remote.Credential) bool {
	now := w.o.Now()
	interval := c.RenewInterval.Std()
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if now.Sub(c.RenewedAt) < interval {
		return true
	}
	if o, known := w.disconnected[c.ExecutionID]; known && now.Sub(o.last) < interval {
		return true
	}
	log := w.o.Logger.With("project", c.ProjectID, "task", c.TaskID, "generation", c.Generation, "execution", c.ExecutionID)
	cl, err := w.client(c)
	if err != nil {
		w.retry(log, c, now, err)
		return true
	}
	callCtx, cancel := context.WithTimeout(ctx, min(interval, 10*time.Second))
	defer cancel()
	project := cl.Project(c.ProjectID)
	if c.ResourceKind == "integration" {
		_, err = project.RenewIntegration(callCtx, c.TaskID, *c.Claim())
	} else if c.ResourceKind == "group" {
		_, err = project.RenewGroup(callCtx, c.TaskID, *c.Claim())
	} else {
		_, err = project.Renew(callCtx, c.TaskID, *c.Claim())
	}
	switch {
	case err == nil:
		w.reconnected(log, c, now)
		c.RenewedAt = now
		if c.ResourceKind == "integration" {
			return w.integrationRenewed(ctx, log, c)
		}
		observeCtx, stopObservation := context.WithTimeout(ctx, 2*time.Second)
		observation := w.observe(observeCtx, c)
		stopObservation()
		if observation.Status != "unknown" {
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
			// The observation and its report run on the clock of callCtx;
			// delivery gets a fresh budget so a slow terminal cannot starve it.
			deliverCtx, stopDeliver := context.WithTimeout(ctx, min(interval, 10*time.Second))
			w.deliver(deliverCtx, log, project, &c)
			stopDeliver()
		}
		if err := remote.WriteCredential(w.o.Home, c); err != nil {
			log.Error("credential write failed", "error", err)
		}
		return true
	case api.IsCode(err, api.CodeTakenOver):
		// A newer generation took the lease over: this execution may never
		// write again.
		w.lose(log, c, err.Error())
		return false
	case api.IsCode(err, api.CodeLeaseInvalid) || api.IsCode(err, api.CodeHeldByOther) || api.IsCode(err, api.CodeNotFound) || c.ResourceKind == "integration" && api.IsCode(err, api.CodeRevisionConflict):
		// A released claim is a normal end; a lost or replaced one ends this
		// execution. Only the server's own view decides: a lease refusal while
		// the view still shows this generation active or lapsed proves
		// nothing. An integration revision conflict is decisive by itself: the
		// members its claim froze changed under an unchanged lease.
		var claim *api.ClaimView
		var lookupErr error
		if c.ResourceKind == "integration" {
			var value api.Integration
			if value, lookupErr = project.Integration(callCtx, c.TaskID); lookupErr == nil {
				claim = &api.ClaimView{Generation: value.Generation, Status: value.Status}
			}
		} else if c.ResourceKind == "group" {
			var value api.GroupView
			if value, lookupErr = project.Group(callCtx, c.TaskID); lookupErr == nil {
				claim = value.Claim
			}
		} else {
			var value api.Task
			if value, lookupErr = project.Show(callCtx, c.TaskID); lookupErr == nil {
				claim = value.Claim
			}
		}
		switch {
		case lookupErr != nil && !api.IsCode(lookupErr, api.CodeNotFound):
			// The refusal cannot be read against the server's view yet.
			w.retry(log, c, now, errors.Join(err, lookupErr))
			return true
		case claim != nil && claim.Generation == c.Generation && claim.Status == api.ClaimReleased:
			log.Info("claim released; credential removed")
			if err := remote.RemoveCredential(w.o.Home, c); err != nil {
				log.Error("credential removal failed", "error", err)
			}
			return false
		case claim != nil && claim.Generation == c.Generation && api.ClaimStands(claim.Status) && !api.IsCode(err, api.CodeRevisionConflict):
			log.Error("renewal refused while the server still shows this generation; retrying", "error", err, "status", claim.Status)
			return true
		}
		w.lose(log, c, err.Error())
		return false
	default:
		// The server is unreachable or failing. Only the server decides the
		// lease: a lapsed lease is renewed when the server is back, and only a
		// takeover ends this execution, so the agent keeps working.
		w.retry(log, c, now, err)
		return true
	}
}

// retry records a renewal that did not reach a decision of the server; the
// next attempt waits one renewal interval. It never marks the credential lost
// or stops the agent.
func (w *loop) retry(log *slog.Logger, c remote.Credential, now time.Time, err error) {
	if w.disconnected == nil {
		w.disconnected = map[string]outage{}
	}
	o, known := w.disconnected[c.ExecutionID]
	if !known {
		o.since = now
	}
	o.last = now
	w.disconnected[c.ExecutionID] = o
	log.Warn("renewal failed; retrying", "error", err, "disconnected_since", o.since)
}

// reconnected logs the first confirmed renewal after failed ones.
func (w *loop) reconnected(log *slog.Logger, c remote.Credential, now time.Time) {
	if o, known := w.disconnected[c.ExecutionID]; known {
		delete(w.disconnected, c.ExecutionID)
		log.Info("renewal confirmed again", "disconnected_for", now.Sub(o.since))
	}
}

// lose marks the credential lost and stops the managed agent. The worktree,
// branch, and uncommitted changes are left untouched for manual handling.
func (w *loop) lose(log *slog.Logger, c remote.Credential, reason string) {
	c.Lost, c.LostReason = true, reason
	log.Warn("lease lost", "reason", reason)
	if err := remote.WriteCredential(w.o.Home, c); err != nil {
		log.Error("credential write failed; agent stop is deferred", "error", err)
		return
	}
	w.stopLost(context.Background(), log, c)
}

// deliver hands the open requests of this generation to the agent once each.
// A durable acceptance reserves delivery before the terminal effect. An
// uncertain attempt is inspected by the holder instead of being typed again.
func (w *loop) deliver(ctx context.Context, log *slog.Logger, project *client.Project, c *remote.Credential) {
	if w.o.Deliver == nil {
		return
	}
	requests, err := project.Requests(ctx, c.TaskID)
	if err != nil {
		log.Warn("reading requests failed", "error", err)
		return
	}
	var task *api.Task
	for _, r := range requests {
		if r.Status != api.RequestOpen || r.TargetGeneration != c.Generation || slices.Contains(c.Delivered, r.ID) {
			continue
		}
		if task == nil {
			t, err := project.Show(ctx, c.TaskID)
			if err != nil {
				log.Warn("reading the task failed", "error", err)
				return
			}
			task = &t
		}
		if r.Dispatch != nil && !r.Dispatch.ConfirmBy.IsZero() && !task.ObservedAt.Before(r.Dispatch.ConfirmBy) {
			_, err := project.AnswerRequest(ctx, "expired-"+c.ExecutionID+"-"+r.ID, c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestRejected, ExpectedRevision: r.Revision, Note: "dispatch proposal acceptance window elapsed; a new inspected proposal needs a new ID"}, *c.Claim())
			if err != nil {
				log.Warn("rejecting expired dispatch proposal failed", "request", r.ID, "error", err)
			}
			continue
		}
		if r.Dispatch != nil && r.CardRevision != task.Revision {
			_, err := project.AnswerRequest(ctx, "stale-"+c.ExecutionID+"-"+r.ID, c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestRejected, ExpectedRevision: r.Revision, Note: "dispatch proposal targets an earlier card version; inspect the current task before creating a new proposal"}, *c.Claim())
			if err != nil {
				log.Warn("rejecting stale dispatch proposal failed", "request", r.ID, "error", err)
			}
			continue
		}
		window, session := board.MetadataFrom(task.Text, board.FieldWindow), board.MetadataFrom(task.Text, board.FieldSession)
		if window == "" || session == "" {
			continue
		}
		if c.Window != "" && c.Window != window || c.Session != "" && c.Session != session {
			log.Warn("request routing differs from this execution's local facts", "request", r.ID)
			continue
		}
		accepted, err := project.AnswerRequest(ctx, "delivery-"+c.ExecutionID+"-"+r.ID, c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: r.Revision, Note: "automatic delivery reserved; outcome is uncertain until observed"}, *c.Claim())
		if err != nil {
			log.Warn("reserving request delivery failed", "request", r.ID, "error", err)
			continue
		}
		c.Delivered = append(c.Delivered, r.ID)
		if err := remote.WriteCredential(w.o.Home, *c); err != nil {
			log.Error("request delivery intent could not be saved", "request", r.ID, "error", err)
			continue
		}
		line := requestLine(c.TaskID, r)
		if err := w.o.Deliver(window, session, line); err != nil {
			log.Warn("request delivery outcome unresolved; automatic retry suppressed", "request", r.ID, "error", err)
			continue
		}
		if _, err := project.AnswerRequest(ctx, client.NewKey(), c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: accepted.Revision, Note: "delivered to the agent"}, *c.Claim()); err != nil {
			log.Warn("request delivery receipt failed", "request", r.ID, "error", err)
		}
		log.Info("request delivered", "request", r.ID, "kind", r.Kind)
	}
}

// requestLine renders a request as one terminal line; control characters of
// the remote message never reach the pane.
func requestLine(task string, r api.CollabRequest) string {
	message := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, r.Message)
	message += fmt.Sprintf("; inspect the full request and its proposed material: kander request show %s %s --json; do not act on superseded requests or another execution generation", task, r.ID)
	return fmt.Sprintf("# kander-request %s %s for %s from %s: %s (answer: kander request answer %s %s done|rejected [note])",
		r.ID, r.Kind, task, r.FromClient, message, task, r.ID)
}
