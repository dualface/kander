//go:build unix

package review

import (
	"cmp"
	"context"
	"errors"
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
	// recheckSettleMargin is kept at the end of reviewerRecheckLimit when the extension plans one more
	// observation. It decides only whether to start one and never shortens its budget.
	recheckSettleMargin = 50 * time.Millisecond
)

// recheckClock is the time and snapshot source of the group observation. It is a variable so tests can drive
// the timing of a recheck, sleep overshoot included.
type recheckClock struct {
	now   func() time.Time
	sleep func(time.Duration)
	table func(limit time.Duration, hard time.Time) (map[int]procInfo, error)
}

var clock = recheckClock{now: time.Now, sleep: time.Sleep, table: boundedProcessTable}

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
//
// The read ends after limit, or at hard when that comes first; a zero hard sets no absolute end. The recheck
// passes its own end as hard, because a delay before the read starts would otherwise move the end of a short
// budget past the end of the recheck.
func (o *groupObserver) observe(limit time.Duration, hard time.Time) (map[memberKey]bool, error) {
	table, err := clock.table(limit, hard)
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

// recheck polls until no unattributed member is left or the limit ends. It returns the last completed
// observation, which holds an unattributed member whenever the error is nil and the recheck stopped on time.
//
// The schedule has two parts. The original schedule observes, waits recheckPollInterval, and stops once the
// next observation could no longer have the full recheckSnapshotLimit. It is the unchanged prefix: it reads at
// the same moments with the same budgets as before, so everything it let through is still let through. The
// extension continues where the original stopped. It reserves the measured duration of the last observation
// and recheckSettleMargin at the end of reviewerRecheckLimit, and starts another observation only when that
// fits, so a slow ps is not followed by a read that the limit would cut off. Two exceptions keep it useful
// with slow snapshots. The poll wait shrinks, down to none, so the next observation starts as early as the
// cadence of the last one allows, and the second observation is attempted even when it does not fit, because
// one completed observation is a weak basis. After two completed observations a read that does not fit is not
// started. The budget of an observation is never shortened for that: it is the smaller of
// recheckSnapshotLimit and the time left, in both parts.
func (o *groupObserver) recheck() (map[memberKey]bool, error) {
	deadline := clock.now().Add(reviewerRecheckLimit)
	planUntil := deadline.Add(-recheckSettleMargin)
	var last map[memberKey]bool
	completed := 0
	for {
		began := clock.now()
		budget := min(recheckSnapshotLimit, deadline.Sub(began))
		if completed >= 1 && budget <= 0 {
			// A sleep overshot the limit. The last completed observation decides.
			return last, nil
		}
		live, err := o.observe(budget, deadline)
		if err != nil {
			// A read cut off by the limit before it got its full budget is not a failure when an earlier
			// observation completed: that one decides, and its unattributed member still rejects the result.
			if completed >= 1 && budget < recheckSnapshotLimit && errors.Is(err, context.DeadlineExceeded) {
				return last, nil
			}
			return nil, err
		}
		completed++
		last = live
		if !o.hasUnattributed(live) {
			return live, nil
		}
		now := clock.now()
		next := now.Add(recheckPollInterval)
		if deadline.Sub(now) < recheckPollInterval+recheckSnapshotLimit {
			// The original schedule stops here. Another observation takes as long as the last one did.
			took := now.Sub(began)
			if next.Add(took).After(planUntil) {
				next = laterOf(now, began.Add(recheckPollInterval))
			}
			if !next.Before(planUntil) || (completed >= 2 && next.Add(took).After(planUntil)) {
				return last, nil
			}
		}
		if next.After(now) {
			clock.sleep(next.Sub(now))
		}
	}
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
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
		live, err = o.observe(recheckSnapshotLimit, time.Time{})
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
func boundedProcessTable(limit time.Duration, hard time.Time) (map[int]procInfo, error) {
	end := time.Now().Add(limit)
	if !hard.IsZero() && hard.Before(end) {
		end = hard
	}
	ctx, cancel := context.WithDeadline(context.Background(), end)
	defer cancel()
	type result struct {
		table    map[int]procInfo
		err      error
		finished time.Time
	}
	done := make(chan result, 1)
	read := readProcessTable
	go func() {
		table, err := read(ctx)
		done <- result{table, err, time.Now()}
	}()
	select {
	case r := <-done:
		return settleTableRead(ctx, r.finished, r.table, r.err)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// settleTableRead decides what a finished read means once its budget may have ended. The caller waits on the
// context and the read at once, so either can arrive first. A read that finished after the deadline is the
// deadline, whatever it returned: its table was taken after the limit, and a late success must not replace
// the conclusion of the last observation inside it. A read that failed because the cancellation killed it is
// the deadline too. A read that finished before the deadline keeps its own result, also when it is received
// late.
func settleTableRead(ctx context.Context, finished time.Time, table map[int]procInfo, err error) (map[int]procInfo, error) {
	if deadline, ok := ctx.Deadline(); ok && finished.After(deadline) {
		return nil, context.DeadlineExceeded
	}
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return table, err
}
