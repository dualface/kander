//go:build unix

package review

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
)

// A recheck separates a member that is already exiting from one that stays. The limit includes every process
// snapshot, so a slow ps cannot extend it, and it stays short because rejected reviewer children keep running
// until SIGKILL.
const (
	reviewerRecheckLimit = time.Second
	recheckSnapshotLimit = 500 * time.Millisecond
	recheckPollInterval  = 50 * time.Millisecond
)

// memberKey is a process incarnation: one PID with two start times is two members.
type memberKey struct {
	pid       int
	startTime string
}

// groupObserver records every incarnation seen in the reviewer process group. A tracker that never completed
// an observation attributes nothing.
type groupObserver struct {
	leader  int
	tracker *reviewerProcessTree
	members map[memberKey]*processMember
}

func newGroupObserver(lp *launchedProcess) *groupObserver {
	o := &groupObserver{leader: lp.cmd.Process.Pid, members: map[memberKey]*processMember{}}
	if lp.tracker != nil && lp.tracker.observed {
		o.tracker = lp.tracker
	}
	return o
}

func (o *groupObserver) attributed(key memberKey) bool {
	if o.tracker == nil {
		return false
	}
	startTime, ok := o.tracker.ownTree[key.pid]
	return ok && startTime == key.startTime
}

// observe records one bounded snapshot and returns the live members it holds. Group membership decides alone:
// a reused PID outside the group is a stranger, and a new incarnation inside it is a member.
func (o *groupObserver) observe(limit time.Duration) (map[memberKey]bool, error) {
	table, err := boundedProcessTable(limit)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	live := map[memberKey]bool{}
	for pid, info := range table {
		if info.pgid != o.leader || info.zombie {
			continue
		}
		key := memberKey{pid: pid, startTime: info.startTime}
		live[key] = true
		o.members[key] = &processMember{PID: pid, PPID: info.ppid, PGID: info.pgid, StartTime: info.startTime, Executable: info.executable, ObservedAt: at}
	}
	return live, nil
}

// recheck polls until no unattributed member is left or the limit ends. It returns the last observation.
func (o *groupObserver) recheck() (map[memberKey]bool, error) {
	deadline := time.Now().Add(reviewerRecheckLimit)
	for {
		live, err := o.observe(min(recheckSnapshotLimit, time.Until(deadline)))
		if err != nil {
			return nil, err
		}
		if !o.hasUnattributed(live) || time.Until(deadline) < recheckPollInterval+recheckSnapshotLimit {
			return live, nil
		}
		time.Sleep(recheckPollInterval)
	}
}

func (o *groupObserver) hasUnattributed(live map[memberKey]bool) bool {
	for key := range live {
		if !o.attributed(key) {
			return true
		}
	}
	return false
}

func (o *groupObserver) outcome(key memberKey, live map[memberKey]bool, recheck bool, err error) string {
	switch {
	case !recheck:
		return memberNotRechecked
	case err != nil:
		return memberUnknown
	case o.attributed(key):
		return memberAttributed
	case live[key]:
		return memberLeftover
	}
	return memberExited
}

// classifiedMembers returns the capped records, leftovers first, and the number left out.
func (o *groupObserver) classifiedMembers(live map[memberKey]bool, recheck bool, err error) ([]processMember, int) {
	members := make([]processMember, 0, len(o.members))
	for key, member := range o.members {
		record := *member
		record.Outcome = o.outcome(key, live, recheck, err)
		members = append(members, record)
	}
	rank := func(m processMember) int {
		if m.Outcome == memberLeftover {
			return 0
		}
		return 1
	}
	slices.SortFunc(members, func(a, b processMember) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.PID, b.PID), strings.Compare(a.StartTime, b.StartTime))
	})
	if len(members) > processEvidenceMaxMembers {
		return members[:processEvidenceMaxMembers], len(members) - processEvidenceMaxMembers
	}
	return members, 0
}

// observeReviewerGroup runs after the group was found alive. Only a clean leader exit earns a recheck; a
// timeout, an interrupt, or a failed exit has no result to protect, so the members are recorded once. The
// second result is true when the last observation found the group empty.
func observeReviewerGroup(lp *launchedProcess, recheck bool) (processCollection, bool) {
	o := newGroupObserver(lp)
	started := time.Now()
	var live map[memberKey]bool
	var err error
	if recheck {
		live, err = o.recheck()
	} else {
		live, err = o.observe(recheckSnapshotLimit)
	}
	evidence := &processEvidence{Schema: 1, LeaderPID: o.leader, Cleanup: processCleanup{Sigkill: sigkillPending}}
	if recheck {
		evidence.Recheck = processRecheck{Performed: true, DurationMS: time.Since(started).Milliseconds()}
	}
	if err != nil {
		evidence.Observation = processObservation{Failed: true, Reason: err.Error()}
	}
	evidence.Members, evidence.OmittedMembers = o.classifiedMembers(live, recheck, err)
	lingering := !recheck || err != nil || o.hasUnattributed(live)
	return processCollection{lingering: lingering, evidence: evidence}, err == nil && len(live) == 0
}

// boundedProcessTable keeps a snapshot inside its budget even when the read ignores cancellation, which the
// /proc walk does.
func boundedProcessTable(limit time.Duration) (map[int]procInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	type result struct {
		table map[int]procInfo
		err   error
	}
	done := make(chan result, 1)
	read := readProcessTable
	go func() {
		table, err := read(ctx)
		done <- result{table, err}
	}()
	select {
	case r := <-done:
		return r.table, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
