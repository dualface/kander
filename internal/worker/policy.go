package worker

import (
	"context"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// policyRefreshTimeout bounds one read of a project policy in the work loop.
const policyRefreshTimeout = 10 * time.Second

// policyProject is the key of one policy cache: a project of one server.
type policyProject struct{ ServerURL, ProjectID string }

// policyHint is the policy version the last confirmed renewal of a project
// answered, and the client identity the worker reads the policy with.
type policyHint struct {
	clientID string
	epoch    string
	revision uint64
}

// notePolicy records the policy version a confirmed renewal answered and wakes
// the policy loop. The renewal loop only records it, so a slow policy read
// never delays a renewal.
func (w *loop) notePolicy(c remote.Credential, renewal api.Renewal) {
	w.mu.Lock()
	w.policyHints[policyProject{c.ServerURL, c.ProjectID}] = policyHint{clientID: c.ClientID, epoch: renewal.PolicyEpoch, revision: renewal.PolicyRevision}
	w.mu.Unlock()
	select {
	case w.policyWake <- struct{}{}:
	default:
	}
}

// startPolicyRefresh starts the policy loop: a goroutine of its own, beside the
// renewal loop and the work loop, so neither a renewal nor a slow terminal
// step of the work loop delays a policy read. It does nothing while the loop
// runs.
func (w *loop) startPolicyRefresh(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.policies != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &renewals{cancel: cancel, done: make(chan struct{})}
	w.policies = r
	go func() {
		defer close(r.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.policyWake:
				w.refreshPolicies(ctx)
			}
		}
	}()
}

// stopPolicyRefresh cancels the policy loop and waits until it has exited.
func (w *loop) stopPolicyRefresh() {
	w.mu.Lock()
	r := w.policies
	w.policies = nil
	w.mu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// refreshPolicies reads the policy of every project whose last renewal named a
// newer policy version than the policy cache of this client home holds, so
// the executing agents read the new values at their next kander config. The
// hints are taken, so a project is read at most once per renewal: a failed
// read is tried again when the next renewal still names a newer version. Only
// the policy loop calls it, one read at a time, so one project is never
// refreshed twice at once by this worker.
func (w *loop) refreshPolicies(ctx context.Context) {
	w.mu.Lock()
	hints := w.policyHints
	w.policyHints = map[policyProject]policyHint{}
	w.mu.Unlock()
	for project, hint := range hints {
		if ctx.Err() != nil {
			return
		}
		if config.ReadPolicyCache(w.o.Home, project.ServerURL, project.ProjectID).Behind(hint.epoch, hint.revision) {
			w.refreshPolicy(ctx, project, hint)
		}
	}
}

// refreshPolicy reads one project policy with the policy refresh function,
// which stores it in the policy cache. A failure is only a warning: the
// leases, the work loop, and the credentials do not depend on it.
func (w *loop) refreshPolicy(ctx context.Context, project policyProject, hint policyHint) {
	log := w.o.Logger.With("project", project.ProjectID, "policy_revision", hint.revision)
	cl, err := w.client(remote.Credential{ServerURL: project.ServerURL, ClientID: hint.clientID})
	if err != nil {
		log.Warn("project policy refresh failed; retried at the next renewal", "error", err)
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, policyRefreshTimeout)
	defer cancel()
	service := cl.Project(project.ProjectID)
	cacheErr, err := remote.RefreshPolicy(callCtx, w.o.Home, project.ServerURL, project.ProjectID, func() (*api.ProjectPolicy, error) {
		policy, err := service.Policy(callCtx)
		return &policy, err
	})
	switch {
	case ctx.Err() != nil:
		// The worker is stopping: the read was cancelled, not refused.
	case err != nil:
		log.Warn("project policy refresh failed; retried at the next renewal", "error", err)
	case cacheErr != nil:
		log.Warn("project policy cache could not be written; retried at the next renewal", "error", cacheErr)
	default:
		log.Info("project policy refreshed")
	}
}
