package liveness

import (
	"context"
	"errors"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

func (s *subscription) readRemote(ctx context.Context) (subscriptionFacts, error) {
	facts := subscriptionFacts{observedAt: nowFn().UTC()}
	query := api.SnapshotQuery{}
	if !s.hasGroups {
		query.Tasks = append(append([]string{}, s.opts.Members...), s.opts.Watch...)
		if !uniqueStrings(query.Tasks) {
			return facts, errors.New(t("liveness.watched_target_duplicates_a_member_task", "--watch"))
		}
	}
	for _, reference := range s.opts.Watch {
		if taskGroupRe.MatchString(reference) {
			query.Groups = append(query.Groups, reference)
		}
	}
	if s.opts.Group != "" {
		query.Groups = append(query.Groups, s.opts.Group)
	}
	ctx, cancel := context.WithTimeout(ctx, subscriptionReadTimeout)
	defer cancel()
	snapshot, err := s.service.Project.Snapshot(ctx, query)
	if err != nil {
		return facts, err
	}
	facts.scanned, err = board.RestoreView(snapshot.View)
	if err != nil {
		return facts, err
	}
	facts.observedAt = nowFn().UTC()
	facts.coordinators = snapshot.Coordinators
	facts.serverNow = snapshot.ObservedAt
	facts.claims = snapshot.Claims
	facts.cursor = snapshot.Cursor
	facts, err = s.expandFacts(facts)
	if err != nil {
		return facts, err
	}
	facts.blocked = snapshot.Blocked
	s.cursor = snapshot.Cursor
	return facts, nil
}
func remoteObservation(id string, facts subscriptionFacts) Report {
	text, _ := facts.scanned.Document(id)
	entry := facts.scanned.Entries[id]
	result := Report{TaskID: id, Agent: board.MetadataFrom(text, board.FieldOwner), Status: Unknown, Channel: "server", ObservedAt: facts.observedAt, Identity: identityFrom(entry, text), RuntimeState: "unknown", Detail: t("liveness.subscription_observation_pending")}
	agent := remote.ClaimAgent(facts.claims[id], facts.serverNow, facts.observedAt)
	result.Status, result.RuntimeState, result.Detail, result.ObservationValid = agent.Status, agent.RuntimeState, agent.Reason, agent.Valid
	result.ObservedAt, _ = time.Parse(time.RFC3339Nano, agent.ObservedAt)
	return result
}
func changedClaims(previous, current subscriptionFacts) []string {
	var changed []string
	for _, id := range current.monitored {
		a, b := previous.claims[id], current.claims[id]
		if a == nil && b == nil {
			continue
		}
		if a == nil || b == nil || a.Generation != b.Generation || a.ExecutionID != b.ExecutionID || a.Status != b.Status {
			changed = append(changed, id)
			continue
		}
		old, new := "", ""
		if a.Observation != nil {
			old = a.Observation.Status
		}
		if b.Observation != nil {
			new = b.Observation.Status
		}
		if old != new {
			changed = append(changed, id)
		}
	}
	return changed
}

func changedCoordinators(previous, current subscriptionFacts) (changed, takenOver bool) {
	if len(previous.coordinators) != len(current.coordinators) {
		changed = true
	}
	for id, next := range current.coordinators {
		old, found := previous.coordinators[id]
		if !found || old.Revision != next.Revision || old.ContentHash != next.ContentHash || old.Epoch != next.Epoch {
			changed = true
		}
		if found && old.Epoch != next.Epoch {
			takenOver = true
		}
		a, b := old.Claim, next.Claim
		if a == nil && b == nil {
			continue
		}
		if a == nil || b == nil || a.Generation != b.Generation || a.Status != b.Status || a.ExecutionID != b.ExecutionID {
			changed = true
		}
		if a != nil && b != nil && a.Generation != b.Generation {
			takenOver = true
		}
	}
	return
}
