package remote

import (
	"context"
	"strconv"
	"sync"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
)

// sessionPolicyMu guards Info.Policy and the policy values of the
// configuration of every session once Open returned: RefreshPolicy may run in
// the background of a long-running process while other work reads them. One
// lock for all sessions keeps Session copyable; a process holds few of them.
var sessionPolicyMu sync.Mutex

// PolicyRevision is the project policy revision this session works with: the
// revision of the policy the server answered at registration or at the last
// refresh, 0 when the project has none or the server keeps no policies.
func (s *Session) PolicyRevision() uint64 {
	return s.CurrentPolicy().Revision
}

// CurrentPolicy is the project policy this session works with, revision 0
// when the project has none. A long-running process refreshes the policy in
// the background while other work of the same session reads it, so the
// policy fields are read and replaced under a lock.
func (s *Session) CurrentPolicy() api.ProjectPolicy {
	sessionPolicyMu.Lock()
	defer sessionPolicyMu.Unlock()
	if s.Info.Policy == nil {
		return api.ProjectPolicy{}
	}
	return *s.Info.Policy
}

// BranchMode is the git.branch_mode in effect for this session: the policy
// value when a policy applies, otherwise the local one. A session built
// without a configuration has no local value and reports the default, as a
// configuration without the key does.
func (s *Session) BranchMode() string {
	sessionPolicyMu.Lock()
	defer sessionPolicyMu.Unlock()
	if s.config == nil {
		return config.DefaultGit().BranchMode
	}
	return s.config.Git.BranchMode
}

// RefreshPolicy reads the project policy again with the policy refresh
// function, which writes the cache when the answer is newer, and applies it
// to the session and its configuration.
func (s *Session) RefreshPolicy(ctx context.Context) error {
	policy, cacheErr, err := s.FetchPolicy(ctx)
	if err != nil {
		return err
	}
	if cacheErr != nil {
		remark("project_policy_cache_failed", config.Text("policy.cache_write_failed", cacheErr.Error()))
	}
	sessionPolicyMu.Lock()
	defer sessionPolicyMu.Unlock()
	s.Info.Policy = &policy
	if s.config != nil {
		config.ApplyServerPolicy(s.config, PolicySnapshot(policy))
	}
	return nil
}

// WithCurrentPolicy sends a request that carries the policy revision and
// branch mode of the session. When the server refuses it as stale, the policy
// is read again and the request is sent once more with the new values,
// through the same idempotency key: a stale refusal writes no receipt. A
// second stale refusal is an error that names the server's revision.
// refreshed runs between the two attempts, after the refresh, so a caller
// can adapt what depends on the policy (a start branch, a journal).
func (s *Session) WithCurrentPolicy(ctx context.Context, send func(revision uint64, branchMode string) error, refreshed func() error) error {
	err := send(s.PolicyRevision(), s.BranchMode())
	if !api.IsCode(err, api.CodePolicyRevisionStale) {
		return err
	}
	if err := s.RefreshPolicy(ctx); err != nil {
		return err
	}
	if refreshed != nil {
		if err := refreshed(); err != nil {
			return err
		}
	}
	return s.StillStale(ctx, send(s.PolicyRevision(), s.BranchMode()))
}

// StillStale turns a second stale refusal, the one after a refresh, into an
// error that names the server's current revision and kander policy show. Any
// other result passes through.
func (s *Session) StillStale(ctx context.Context, err error) error {
	if !api.IsCode(err, api.CodePolicyRevisionStale) {
		return err
	}
	current := "?"
	if policy, readErr := s.Project.Policy(ctx); readErr == nil {
		current = strconv.FormatUint(policy.Revision, 10)
	}
	return &api.Error{Code: api.CodePolicyRevisionStale, Message: config.Text("remote.policy_still_stale", current, strconv.FormatUint(s.PolicyRevision(), 10))}
}
