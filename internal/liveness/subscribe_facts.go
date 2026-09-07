package liveness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"sort"
	"time"

	"github.com/dualface/kander/internal/board"
)

const subscriptionReadTimeout = 2 * time.Second

type subscriptionFacts struct {
	scanned    board.Board
	states     map[string]string
	revisions  map[string]uint64
	dispatches map[string]dispatchJSON
	watched    []string
	monitored  []string
	groups     map[string][]string
	versions   map[string]string
	observedAt time.Time
}

type subscription struct {
	opts                 subscribeOptions
	id                   string
	seq                  uint64
	reconciliation       bool
	last                 subscriptionFacts
	hasGroups            bool
	probes               *subscriptionProbes
	heartbeat            time.Duration
	attention            map[string]dispatchAttentionKey
	dispatchProbePending bool
}

func newSubscription(opts subscribeOptions) (*subscription, error) {
	if !taskGroupRe.MatchString(opts.Group) {
		return nil, fmt.Errorf("%s", t("liveness.invalid_task_group_id", opts.Group))
	}
	if len(opts.Members) == 0 {
		return nil, fmt.Errorf("%s", t("liveness.subscription_members_required"))
	}
	opts.Members = append([]string(nil), opts.Members...)
	opts.Watch = append([]string(nil), opts.Watch...)
	for i, value := range opts.Members {
		id, err := board.NormalizeTaskID(value)
		if err != nil {
			return nil, err
		}
		opts.Members[i] = id
	}
	if !uniqueStrings(opts.Members) {
		return nil, fmt.Errorf("%s", t("liveness.member_task_ids_must_not_be_repeated"))
	}
	session := &subscription{opts: opts, attention: map[string]dispatchAttentionKey{}}
	for i, value := range opts.Watch {
		if taskGroupRe.MatchString(value) {
			session.hasGroups = true
			continue
		}
		id, err := board.NormalizeTaskID(value)
		if err != nil {
			return nil, err
		}
		session.opts.Watch[i] = id
	}
	if !uniqueStrings(session.opts.Watch) {
		return nil, fmt.Errorf("%s", t("liveness.watched_task_ids_must_not_be_repeated", "--watch"))
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	session.id = hex.EncodeToString(id[:])
	return session, nil
}

func (s *subscription) readContext(ctx context.Context, root string) (facts subscriptionFacts, err error) {
	facts.observedAt = nowFn().UTC()
	ctx, cancel := context.WithTimeout(ctx, subscriptionReadTimeout)
	defer cancel()
	if s.hasGroups {
		facts.scanned, err = board.ScanDispatchesContext(ctx, root, nil)
	} else {
		ids := append(append([]string{}, s.opts.Members...), s.opts.Watch...)
		if !uniqueStrings(ids) {
			return facts, fmt.Errorf("%s", t("liveness.watched_target_duplicates_a_member_task", "--watch"))
		}
		facts.scanned, err = board.ScanDispatchesContext(ctx, root, ids)
	}
	facts.observedAt = nowFn().UTC()
	if err != nil {
		return facts, err
	}
	facts.states = map[string]string{}
	facts.revisions = map[string]uint64{}
	facts.dispatches = map[string]dispatchJSON{}
	facts.groups = map[string][]string{}
	facts.versions = map[string]string{}
	var membership board.Membership
	if s.hasGroups {
		membership = facts.scanned.GroupMembership()
		// Unknown ownership could conceal a member of any requested group.
		if err = membership.Err(); err != nil {
			return facts, err
		}
	} else if len(facts.scanned.Problems) > 0 {
		return facts, fmt.Errorf("%s", facts.scanned.Problems[0].Message)
	}
	memberSet := map[string]bool{}
	for _, id := range s.opts.Members {
		memberSet[id] = true
	}
	watchedSet := map[string]bool{}
	for _, reference := range s.opts.Watch {
		expanded := []string{reference}
		if taskGroupRe.MatchString(reference) {
			expanded = membership.Groups[reference]
			// An emptied group remains observable after a valid initial snapshot.
			if len(expanded) == 0 && s.seq == 0 {
				return facts, fmt.Errorf("%s", t("liveness.watched_task_group_has_no_members", reference))
			}
			facts.groups[reference] = append([]string{}, expanded...)
			facts.versions[reference] = membership.Versions[reference]
			if len(expanded) == 0 {
				facts.versions[reference] = "empty"
			}
		}
		for _, id := range expanded {
			if memberSet[id] {
				return facts, fmt.Errorf("%s", t("liveness.watched_target_duplicates_a_member_task", id))
			}
			if watchedSet[id] {
				return facts, fmt.Errorf("%s", t("liveness.watched_task_ids_must_not_be_repeated", id))
			}
			watchedSet[id] = true
			facts.watched = append(facts.watched, id)
		}
	}
	sort.Strings(facts.watched)
	facts.monitored = append(append([]string{}, s.opts.Members...), facts.watched...)
	sort.Strings(facts.monitored)
	for _, id := range facts.monitored {
		text, e := facts.scanned.Document(id)
		if e != nil {
			return facts, e
		}
		if memberSet[id] {
			actual := board.TaskGroupFrom(text)
			if actual != s.opts.Group {
				if actual == "" {
					actual = "N/A"
				}
				return facts, fmt.Errorf("%s", t("liveness.task_does_not_belong_to_the_specified_group_actual", id, actual))
			}
		}
		facts.states[id] = facts.scanned.Entries[id].State
		revision, e := facts.scanned.Revision(id)
		if e != nil {
			return facts, e
		}
		facts.revisions[id] = revision
		dispatch, e := facts.scanned.CurrentDispatch(id)
		if e != nil {
			return facts, e
		}
		if dispatch != nil {
			facts.dispatches[id] = summarizeDispatch(*dispatch, facts.observedAt)
		}
	}
	return facts, nil
}

func membershipChanged(previous, current subscriptionFacts) bool {
	return !reflect.DeepEqual(previous.versions, current.versions)
}

func removedMembers(previous, current subscriptionFacts) []string {
	removedSet := map[string]bool{}
	for group, ids := range previous.groups {
		present := map[string]bool{}
		for _, id := range current.groups[group] {
			present[id] = true
		}
		for _, id := range ids {
			if !present[id] {
				removedSet[id] = true
			}
		}
	}
	var removed []string
	for id := range removedSet {
		removed = append(removed, id)
	}
	sort.Strings(removed)
	return removed
}

func (s *subscription) payload(event string, facts subscriptionFacts) groupEvent {
	s.seq++
	return groupEvent{
		SchemaVersion: 1, SubscriptionID: s.id, Seq: s.seq, ObservedAt: facts.observedAt,
		Dispatches: facts.dispatches,
		Event:      event, GroupID: s.opts.Group, Tasks: facts.states, TaskRevisions: facts.revisions,
		Watched: facts.watched, WatchReferences: s.opts.Watch, Memberships: facts.groups,
		MembershipVersions: facts.versions, MembershipComplete: true,
		ReconciliationRequired: s.reconciliation, ReadStatus: "committed",
	}
}

func (s *subscription) emit(w io.Writer, event string, facts subscriptionFacts, changed []changeEvent, updated, removed []string) error {
	payload := s.payload(event, facts)
	payload.Changed = changed
	payload.Updated = updated
	payload.Removed = removed
	if event == "heartbeat" {
		payload.Liveness = s.probes.liveness(facts, s.heartbeat)
	}
	if err := emitEvent(w, payload); err != nil {
		return err
	}
	s.last = facts
	return nil
}

func (s *subscription) unavailable(w io.Writer, facts subscriptionFacts, cause error) error {
	// Last committed values are diagnostic only; the explicit flags forbid using
	// this terminal event as proof that dependencies became satisfied.
	observed := facts.observedAt
	facts = s.last
	if facts.states == nil {
		facts.states = map[string]string{}
		facts.revisions = map[string]uint64{}
	}
	facts.observedAt = observed
	event := "read-error"
	status := board.SnapshotReadStatus(cause)
	if status == "invalid" {
		event = "membership-unknown"
	}
	payload := s.payload(event, facts)
	payload.ReadStatus = status
	payload.MembershipComplete = false
	payload.ReconciliationRequired = true
	payload.Detail = t("liveness.subscription_facts_unavailable", cause.Error())
	if err := emitEvent(w, payload); err != nil {
		return err
	}
	return cause
}
