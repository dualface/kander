package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

// renewConcurrency is the most renewals in flight at once. The server
// serializes the renewals of one project, so a higher cap would only turn
// server queueing into client timeouts.
const renewConcurrency = 8

// credentialKey is the identity of one credential. An execution that
// coordinates its own group holds a task credential and a group credential
// under one execution ID, so the execution ID alone is not an identity.
type credentialKey struct{ ServerURL, ProjectID, ResourceKind, TaskID, ExecutionID string }

func keyOf(c remote.Credential) credentialKey {
	return credentialKey{c.ServerURL, c.ProjectID, c.ResourceKind, c.TaskID, c.ExecutionID}
}

// renewInterval is the interval at which the server expects the credential to
// be renewed.
func renewInterval(c remote.Credential) time.Duration {
	if interval := c.RenewInterval.Std(); interval > 0 {
		return interval
	}
	return 15 * time.Second
}

func (w *loop) credentialLog(c remote.Credential) *slog.Logger {
	return w.o.Logger.With("project", c.ProjectID, "task", c.TaskID, "generation", c.Generation, "execution", c.ExecutionID)
}

// renewals is the running renewal loop.
type renewals struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// startRenewals starts the renewal loop; it does nothing while the loop runs.
// The first batch runs at once, the next ones every tick.
func (w *loop) startRenewals(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.renewing != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &renewals{cancel: cancel, done: make(chan struct{})}
	w.renewing = r
	tick := w.o.Tick
	if tick <= 0 {
		tick = time.Second
	}
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			w.renewRound(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// stopRenewals cancels the renewal loop and waits until it has exited. A
// renewal in flight is cancelled with it and is not counted as an outage.
func (w *loop) stopRenewals() {
	w.mu.Lock()
	r := w.renewing
	w.renewing = nil
	w.mu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// renewRound renews every due credential, at most renewConcurrency at a time,
// and returns when all of them are done.
func (w *loop) renewRound(ctx context.Context) {
	started := w.o.Now()
	credentials, err := w.credentials()
	if err != nil {
		if ctx.Err() == nil {
			w.o.Logger.Error("renewal round failed", "error", err)
		}
		return
	}
	var due []remote.Credential
	for _, c := range credentials {
		if !c.Lost && w.renewDue(c, started) {
			due = append(due, c)
		}
	}
	renewBounded(ctx, due, w.renewOne)
	if ctx.Err() == nil {
		w.warnSlow("renewal round exceeded the renewal interval", started, credentials)
	}
}

// renewBounded calls renew for every credential, at most renewConcurrency at a
// time, and returns when the calls are done. Once ctx ends it starts no more.
func renewBounded(ctx context.Context, due []remote.Credential, renew func(context.Context, remote.Credential)) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, renewConcurrency)
	for _, c := range due {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			renew(ctx, c)
		}()
	}
	wg.Wait()
}

// renewDue reports whether the credential is due: its last renewal is one
// renewal interval old, and a failed attempt was not made within that interval.
func (w *loop) renewDue(c remote.Credential, now time.Time) bool {
	interval := renewInterval(c)
	if now.Sub(c.RenewedAt) < interval {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	o, known := w.disconnected[keyOf(c)]
	return !known || now.Sub(o.last) >= interval
}

// renewOne renews one credential and acts on the answer of the server. Only
// the answer of the server ends an execution.
func (w *loop) renewOne(ctx context.Context, c remote.Credential) {
	if ctx.Err() != nil {
		return
	}
	now := w.o.Now()
	interval := renewInterval(c)
	log := w.credentialLog(c)
	cl, err := w.client(c)
	if err != nil {
		w.retry(log, c, now, err)
		return
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
	if err != nil && ctx.Err() != nil {
		// The renewal loop was stopped: the call was cancelled, not refused.
		return
	}
	switch {
	case err == nil:
		w.reconnected(log, c, now)
		c.RenewedAt = now
		if err := remote.WriteCredential(w.o.Home, c); err != nil && !errors.Is(err, remote.ErrCredentialReleased) {
			log.Error("credential write failed", "error", err)
		}
		w.mu.Lock()
		w.owed[keyOf(c)] = struct{}{}
		w.mu.Unlock()
	case api.IsCode(err, api.CodeTakenOver):
		// A newer generation took the lease over: this execution may never
		// write again.
		w.markLost(log, c, err.Error())
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
		if ctx.Err() != nil {
			return
		}
		switch {
		case lookupErr != nil && !api.IsCode(lookupErr, api.CodeNotFound):
			// The refusal cannot be read against the server's view yet.
			w.retry(log, c, now, errors.Join(err, lookupErr))
		case claim != nil && claim.Generation == c.Generation && claim.Status == api.ClaimReleased:
			// The owed work goes first: a log call can block, and the work loop
			// must not start a step for a claim the server released.
			w.withdrawWork(c)
			log.Info("claim released; credential removed")
			if err := remote.RemoveCredential(w.o.Home, c); err != nil {
				log.Error("credential removal failed", "error", err)
			}
		case claim != nil && claim.Generation == c.Generation && api.ClaimStands(claim.Status) && !api.IsCode(err, api.CodeRevisionConflict):
			w.withdrawWork(c)
			log.Error("renewal refused while the server still shows this generation; retrying", "error", err, "status", claim.Status)
		default:
			w.markLost(log, c, err.Error())
		}
	default:
		// The server is unreachable or failing. Only the server decides the
		// lease: a lapsed lease is renewed when the server is back, and only a
		// takeover ends this execution, so the agent keeps working.
		w.retry(log, c, now, err)
	}
}

// withdrawWork cancels the work step a credential is owed. A renewal that the
// server did not confirm, or the release of the claim, supersedes the earlier
// confirmation: a work step still waiting behind a slow one must not run for it.
func (w *loop) withdrawWork(c remote.Credential) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.owed, keyOf(c))
}

// retry records a renewal that did not reach a decision of the server; the
// next attempt waits one renewal interval, and the work owed from an earlier
// confirmation is withdrawn. It never marks the credential lost or stops the
// agent.
func (w *loop) retry(log *slog.Logger, c remote.Credential, now time.Time, err error) {
	w.mu.Lock()
	key := keyOf(c)
	delete(w.owed, key)
	o, known := w.disconnected[key]
	if !known {
		o.since = now
	}
	o.last = now
	w.disconnected[key] = o
	w.mu.Unlock()
	log.Warn("renewal failed; retrying", "error", err, "disconnected_since", o.since)
}

// reconnected logs the first confirmed renewal after failed ones.
func (w *loop) reconnected(log *slog.Logger, c remote.Credential, now time.Time) {
	w.mu.Lock()
	key := keyOf(c)
	o, known := w.disconnected[key]
	delete(w.disconnected, key)
	w.mu.Unlock()
	if known {
		log.Info("renewal confirmed again", "disconnected_for", now.Sub(o.since))
	}
}

// markLost records on the credential that its lease is lost and leaves the
// agent to the work loop. It reports whether the mark was saved: when the write
// fails the credential stays unmarked, and the next renewal gets the same
// answer from the server.
func (w *loop) markLost(log *slog.Logger, c remote.Credential, reason string) bool {
	c.Lost, c.LostReason = true, reason
	log.Warn("lease lost", "reason", reason)
	if err := remote.WriteCredential(w.o.Home, c); err != nil {
		log.Error("credential write failed; agent stop is deferred", "error", err)
		return false
	}
	// One lock covers both maps: a work step that finds the owed entry gone
	// is sure the mark is already there.
	w.mu.Lock()
	defer w.mu.Unlock()
	key := keyOf(c)
	delete(w.owed, key)
	w.lostMarks[key] = struct{}{}
	return true
}
