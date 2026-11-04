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
	"path/filepath"
	"syscall"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/remote"
)

// idleExit is how long a worker without live claims stays before exiting.
const idleExit = 2 * time.Minute

// Options configure one worker loop; tests shorten the timings.
type Options struct {
	Home     string
	Tick     time.Duration
	IdleExit time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
	// Stop ends a managed agent process; the default signals the PID.
	Stop func(pid int) error
}

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
	if o.Stop == nil {
		o.Stop = stopProcess
	}
	if err := os.MkdirAll(o.Home, 0o700); err != nil {
		return err
	}
	file, err := fs.OpenLockFile(o.Home, filepath.Join(o.Home, remote.WorkerLockName))
	if err != nil {
		return err
	}
	defer file.Close()
	lockCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	lock, err := fs.LockExclusiveContext(lockCtx, file)
	cancel()
	if err != nil {
		return ErrRunning
	}
	defer lock.Unlock()
	w := &loop{o: o, clients: map[string]*client.Client{}}
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
}

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
		if c.Lost {
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
	interval, ttl := c.RenewInterval.Std(), c.TTL.Std()
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if ttl <= 0 {
		ttl = 90 * time.Second
	}
	if now.Sub(c.RenewedAt) < interval {
		return true
	}
	log := w.o.Logger.With("project", c.ProjectID, "task", c.TaskID, "generation", c.Generation, "execution", c.ExecutionID)
	cl, err := w.client(c)
	if err != nil {
		w.lose(log, c, err.Error())
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, min(interval, 10*time.Second))
	defer cancel()
	project := cl.Project(c.ProjectID)
	_, err = project.Renew(callCtx, c.TaskID, *c.Claim())
	switch {
	case err == nil:
		c.RenewedAt = now
		if err := remote.WriteCredential(w.o.Home, c); err != nil {
			log.Error("credential write failed", "error", err)
		}
		return true
	case api.IsCode(err, api.CodeLeaseInvalid) || api.IsCode(err, api.CodeHeldByOther) || api.IsCode(err, api.CodeNotFound):
		// A released claim is a normal end; anything else lost the lease.
		if t, serr := project.Show(callCtx, c.TaskID); serr == nil && t.Claim != nil && t.Claim.Generation == c.Generation && t.Claim.Status == api.ClaimReleased {
			log.Info("claim released; credential removed")
			if err := remote.RemoveCredential(w.o.Home, c); err != nil {
				log.Error("credential removal failed", "error", err)
			}
			return false
		}
		w.lose(log, c, err.Error())
		return false
	default:
		// The server is unreachable or failing. The lease is decided by the
		// server clock; once a full TTL passed since the last confirmed
		// renewal this host must assume the lease is gone.
		if now.Sub(c.RenewedAt) >= ttl {
			w.lose(log, c, "no confirmed renewal within the lease TTL: "+err.Error())
			return false
		}
		log.Warn("renewal failed; retrying", "error", err)
		return true
	}
}

// lose marks the credential lost and stops the managed agent. The worktree,
// branch, and uncommitted changes are left untouched for manual handling.
func (w *loop) lose(log *slog.Logger, c remote.Credential, reason string) {
	c.Lost, c.LostReason = true, reason
	if c.AgentPID > 0 {
		if err := w.o.Stop(c.AgentPID); err != nil {
			log.Error("stopping the managed agent failed; its later submissions are refused by the server", "pid", c.AgentPID, "error", err)
			c.LostReason += "; agent stop failed: " + err.Error()
		} else {
			log.Info("managed agent stopped", "pid", c.AgentPID)
		}
	}
	log.Warn("lease lost", "reason", reason)
	if err := remote.WriteCredential(w.o.Home, c); err != nil {
		log.Error("credential write failed", "error", err)
	}
}
