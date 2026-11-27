package liveness

import (
	"context"
	"io"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command"
)

// policyRefreshTimeout bounds one read of the project policy by a subscription.
const policyRefreshTimeout = 15 * time.Second

// policyRefreshFailed is the code of the remark a failed policy read makes.
const policyRefreshFailed = "project_policy_refresh_failed"

// policyAnnounced returns the policy revision page announces when it is newer
// than the policy the subscription knows: the version of the page itself, or
// the highest revision of its policy change events. The page version also
// covers a change event the subscription skipped when it continued from the
// cursor of a later board snapshot. A repeated or late event at or below the
// known revision asks for nothing.
func (s *subscription) policyAnnounced(page api.EventPage) (uint64, bool) {
	announced := uint64(0)
	if s.service.PolicyAnnounced(page.PolicyEpoch, page.PolicyRevision) {
		announced = page.PolicyRevision
	}
	known := s.service.PolicyRevision()
	for _, e := range page.Events {
		if e.Kind == api.EventProjectPolicyChanged && e.Revision > known && e.Revision > announced {
			announced = e.Revision
		}
	}
	return announced, announced > 0
}

// refreshPolicy reads the project policy again, which updates the policy cache
// and the policy of the session, and then writes a policy-change line when the
// policy version changed, so the orchestrator reads kander config --json
// again. announced is the revision of the change event that asked for the read,
// 0 for a reset. When the read fails, the change of an event is still written
// with the announced revision: the orchestrator learns that the policy changed
// and its next kander config reads it. It reports whether it wrote the line.
func (s *subscription) refreshPolicy(ctx context.Context, w io.Writer, facts subscriptionFacts, announced uint64) (bool, error) {
	before := s.service.CurrentPolicy()
	readCtx, cancel := context.WithTimeout(ctx, policyRefreshTimeout)
	err := s.service.RefreshPolicy(readCtx)
	cancel()
	revision := announced
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		command.Notify(policyRefreshFailed, t("liveness.policy_refresh_failed", err.Error()))
		if announced == 0 {
			return false, nil
		}
	} else {
		after := s.service.CurrentPolicy()
		if after.Epoch == before.Epoch && after.Revision == before.Revision {
			return false, nil
		}
		revision = after.Revision
	}
	payload := s.payload("policy-change", facts)
	payload.PolicyRevision = &revision
	if err := emitEvent(w, payload); err != nil {
		return false, err
	}
	s.last = facts
	return true, nil
}
