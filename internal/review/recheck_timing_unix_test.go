//go:build unix

package review

import (
	"context"
	"errors"
	"testing"
	"time"
)

const hangs = time.Duration(-1)

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

type simRead struct {
	start, budget time.Duration
	completed     bool
}

// recheckSim is a controlled clock with process snapshots that take simulated time. A snapshot samples the
// process table when its read starts. It serves one member, which is alive until exitAt, and it is canceled
// when its budget ends first. Nothing here depends on the real clock.
type recheckSim struct {
	at        time.Duration
	snapshot  func(start time.Duration, call int) time.Duration
	exitAt    time.Duration
	overshoot time.Duration
	failAt    map[int]error
	readDelay time.Duration // scheduling delay before a read creates its context
	reads     []simRead
}

func constSnapshot(d time.Duration) func(time.Duration, int) time.Duration {
	return func(time.Duration, int) time.Duration { return d }
}

func newSim(snapshot time.Duration, exitAt time.Duration) *recheckSim {
	return &recheckSim{snapshot: constSnapshot(snapshot), exitAt: exitAt}
}

var simBase = time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)

func (s *recheckSim) clock() recheckClock {
	return recheckClock{
		now: func() time.Time { return simBase.Add(s.at) },
		sleep: func(d time.Duration) {
			s.at += d + s.overshoot
		},
		table: s.table,
	}
}

func (s *recheckSim) table(limit time.Duration, hard time.Time) (map[int]procInfo, error) {
	call := len(s.reads)
	s.at += s.readDelay
	start := s.at
	if !hard.IsZero() {
		limit = min(limit, hard.Sub(simBase.Add(start)))
	}
	if err := s.failAt[call]; err != nil {
		s.reads = append(s.reads, simRead{start: start, budget: limit})
		return nil, err
	}
	took := s.snapshot(start, call)
	if limit <= 0 || took == hangs || took >= limit {
		s.reads = append(s.reads, simRead{start: start, budget: limit})
		s.at += max(limit, 0)
		return nil, context.DeadlineExceeded
	}
	s.reads = append(s.reads, simRead{start: start, budget: limit, completed: true})
	s.at += took
	if start >= s.exitAt {
		return map[int]procInfo{}, nil
	}
	return map[int]procInfo{100: member("a")}, nil
}

func (s *recheckSim) completed() int {
	n := 0
	for _, r := range s.reads {
		if r.completed {
			n++
		}
	}
	return n
}

func (s *recheckSim) positiveReads() []simRead {
	var reads []simRead
	for _, r := range s.reads {
		if r.budget > 0 {
			reads = append(reads, r)
		}
	}
	return reads
}

// baselineRecheck is the schedule of v1.1.0 to v1.5.2, kept as the comparison for the extended schedule. It
// returns true when the result would be rejected.
func baselineRecheck(c recheckClock) bool {
	deadline := c.now().Add(reviewerRecheckLimit)
	for {
		table, err := c.table(min(recheckSnapshotLimit, deadline.Sub(c.now())), time.Time{})
		if err != nil {
			return true
		}
		if len(table) == 0 {
			return false
		}
		if deadline.Sub(c.now()) < recheckPollInterval+recheckSnapshotLimit {
			return true
		}
		c.sleep(recheckPollInterval)
	}
}

// runRecheck runs the recheck of the observer against the simulation.
func runRecheck(s *recheckSim) (collection processCollection, empty bool) {
	return newGroupObserver(fakeGroup(), s.clock()).collect(true)
}

func baselineRejects(s *recheckSim) bool { return baselineRecheck(s.clock()) }

func TestRecheckUsesTheFullWindow(t *testing.T) {
	for _, tc := range []struct {
		name         string
		sim          func() *recheckSim
		baselineFail bool
		completed    int
	}{
		{"fast snapshot, exit at 700", func() *recheckSim { return newSim(ms(5), ms(700)) }, true, 0},
		{"snapshot 201, exit at 400", func() *recheckSim { return newSim(ms(201), ms(400)) }, true, 0},
		{"slow first snapshot 465, then fast", func() *recheckSim {
			s := newSim(ms(5), ms(100))
			s.snapshot = func(_ time.Duration, call int) time.Duration {
				if call == 0 {
					return ms(465)
				}
				return ms(5)
			}
			return s
		}, true, 2},
		{"snapshot 480 each", func() *recheckSim { return newSim(ms(480), ms(100)) }, true, 2},
		{"snapshot 400, exit at 430", func() *recheckSim { return newSim(ms(400), ms(430)) }, false, 0},
		{"first snapshot 449.5 with 1 ms overshoot", func() *recheckSim {
			s := newSim(ms(5), ms(100))
			s.overshoot = ms(1)
			s.snapshot = func(_ time.Duration, call int) time.Duration {
				if call == 0 {
					return ms(449.5)
				}
				return ms(460)
			}
			return s
		}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := baselineRejects(tc.sim()); got != tc.baselineFail {
				t.Fatalf("baseline rejects = %v, want %v", got, tc.baselineFail)
			}
			s := tc.sim()
			collection, empty := runRecheck(s)
			if collection.lingering || !empty || collection.evidence.Observation.Failed {
				t.Fatalf("%+v reads=%+v", collection, s.reads)
			}
			if tc.completed != 0 && s.completed() != tc.completed {
				t.Fatalf("completed observations = %d, want %d: %+v", s.completed(), tc.completed, s.reads)
			}
		})
	}
}

func TestRecheckKeepsAnAliveMemberRejected(t *testing.T) {
	s := newSim(ms(5), time.Hour)
	collection, empty := runRecheck(s)
	got := outcomes(collection.evidence)
	if !collection.lingering || empty || collection.evidence.Observation.Failed || got["100/a"] != memberLeftover {
		t.Fatalf("%+v %v", collection, got)
	}
	if s.at > reviewerRecheckLimit || s.at < reviewerRecheckLimit-recheckSnapshotLimit {
		t.Fatalf("recheck ended at %v", s.at)
	}
}

func TestRecheckWithSlowSnapshotsReadsTwice(t *testing.T) {
	s := newSim(ms(400), time.Hour)
	collection, _ := runRecheck(s)
	if !collection.lingering || len(s.reads) != 2 || s.at != ms(850) {
		t.Fatalf("reads=%+v end=%v", s.reads, s.at)
	}
}

func TestTruncatedObservationJudgesTheLastCompletedOne(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sim   func() *recheckSim
		reads int
	}{
		{"single completed observation", func() *recheckSim {
			s := newSim(ms(5), time.Hour)
			s.overshoot = ms(40)
			s.snapshot = func(_ time.Duration, call int) time.Duration {
				if call == 0 {
					return ms(440)
				}
				return hangs
			}
			return s
		}, 2},
		{"several completed observations", func() *recheckSim {
			s := newSim(ms(5), time.Hour)
			s.snapshot = func(start time.Duration, _ int) time.Duration {
				if start > ms(600) {
					return hangs
				}
				return ms(5)
			}
			return s
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.sim()
			collection, empty := runRecheck(s)
			got := outcomes(collection.evidence)
			if !collection.lingering || empty || collection.evidence.Observation.Failed || got["100/a"] != memberLeftover {
				t.Fatalf("%+v %v", collection, got)
			}
			last := s.reads[len(s.reads)-1]
			if last.completed || last.budget >= recheckSnapshotLimit || last.start+last.budget != reviewerRecheckLimit {
				t.Fatalf("the last read must be canceled at the limit: %+v", s.reads)
			}
			if tc.reads != 0 && (len(s.reads) != tc.reads || s.completed() != 1) {
				t.Fatalf("%+v", s.reads)
			}
		})
	}
}

func TestOvershootPastTheLimitReadsNothingMore(t *testing.T) {
	s := newSim(ms(440), time.Hour)
	s.overshoot = ms(600)
	collection, _ := runRecheck(s)
	got := outcomes(collection.evidence)
	if len(s.reads) != 1 || !collection.lingering || collection.evidence.Observation.Failed || got["100/a"] != memberLeftover {
		t.Fatalf("%+v %+v", s.reads, collection)
	}
}

func TestFailedObservationIsNotATruncation(t *testing.T) {
	t.Run("hung read with a full budget", func(t *testing.T) {
		s := newSim(ms(5), time.Hour)
		s.snapshot = func(_ time.Duration, call int) time.Duration {
			if call == 0 {
				return ms(5)
			}
			return hangs
		}
		collection, _ := runRecheck(s)
		if !collection.lingering || !collection.evidence.Observation.Failed || s.reads[1].budget != recheckSnapshotLimit {
			t.Fatalf("%+v %+v", collection, s.reads)
		}
	})
	t.Run("another error after two completed observations", func(t *testing.T) {
		s := newSim(ms(205), time.Hour)
		s.failAt = map[int]error{2: errors.New("snapshot refused")}
		collection, _ := runRecheck(s)
		if s.completed() != 2 || s.reads[2].budget >= recheckSnapshotLimit {
			t.Fatalf("%+v", s.reads)
		}
		if !collection.lingering || !collection.evidence.Observation.Failed {
			t.Fatalf("%+v", collection)
		}
	})
}

// TestExtendedScheduleKeepsTheBaselinePrefix runs the baseline and the recheck over the same grid. A result
// the baseline passed stays passed, and every read the baseline made with a positive budget starts at the same
// time with the same budget.
func TestExtendedScheduleKeepsTheBaselinePrefix(t *testing.T) {
	var snapshots, exits []time.Duration
	for v := 1; v <= 500; v += 5 {
		snapshots = append(snapshots, ms(float64(v)))
	}
	snapshots = append(snapshots, ms(500))
	for v := 1; v <= 1100; v += 5 {
		exits = append(exits, ms(float64(v)))
	}
	exits = append(exits, ms(1100))
	for _, overshoot := range []time.Duration{0, ms(1), ms(10), ms(25)} {
		for _, snapshot := range snapshots {
			for _, exit := range exits {
				base := &recheckSim{snapshot: constSnapshot(snapshot), exitAt: exit, overshoot: overshoot}
				baselineRejected := baselineRejects(base)
				s := &recheckSim{snapshot: constSnapshot(snapshot), exitAt: exit, overshoot: overshoot}
				o := newGroupObserver(fakeGroup(), s.clock())
				live, err := o.recheck()
				rejected := err != nil || o.hasUnattributed(live)
				if !baselineRejected && rejected {
					t.Fatalf("snapshot %v exit %v overshoot %v: baseline passed, recheck rejected", snapshot, exit, overshoot)
				}
				want, got := base.positiveReads(), s.positiveReads()
				if len(got) < len(want) {
					t.Fatalf("snapshot %v exit %v overshoot %v: reads %+v, baseline %+v", snapshot, exit, overshoot, got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("snapshot %v exit %v overshoot %v: read %d is %+v, baseline %+v", snapshot, exit, overshoot, i, got[i], want[i])
					}
				}
				if end := s.at; end > reviewerRecheckLimit+overshoot {
					t.Fatalf("snapshot %v exit %v overshoot %v: ended at %v", snapshot, exit, overshoot, end)
				}
			}
		}
	}
}

// TestDelayBeforeAReadDoesNotMoveTheEndOfTheRecheck delays every read by 1 ms before it creates its context.
// The planned last read starts at 897 ms with 5 ms snapshots behind it, but this one takes 103.5 ms. A read
// samples the process table when it starts, so this one sees the member gone at 897 ms. Its completion is
// what falls late: counted from the delayed start, its budget would end at 1001 ms and it would finish at
// 1000.5 ms, after the 1 second limit. The absolute end of the recheck cancels it, and the last completed
// observation still decides. The simulated read applies the end itself, so the production read is checked
// apart in TestReadEndsAtTheEndOfTheRecheck.
func TestDelayBeforeAReadDoesNotMoveTheEndOfTheRecheck(t *testing.T) {
	s := newSim(ms(5), ms(890))
	s.readDelay = ms(1)
	s.snapshot = func(start time.Duration, _ int) time.Duration {
		if start >= ms(890) {
			return ms(103.5)
		}
		return ms(5)
	}
	collection, empty := runRecheck(s)
	got := outcomes(collection.evidence)
	last := s.reads[len(s.reads)-1]
	if !collection.lingering || empty || collection.evidence.Observation.Failed || got["100/a"] != memberLeftover {
		t.Fatalf("%+v %v reads=%+v", collection, got, s.reads)
	}
	if last.completed || last.start < ms(890) || last.start+last.budget != reviewerRecheckLimit {
		t.Fatalf("the last read must start late and be canceled at the limit: %+v", s.reads)
	}
}

// TestCutOffByTheEndOfTheRecheckIsNotAFailure plans a full 500 ms budget for the second read, which starts
// at 499.5 ms. The 1 ms delay before it creates its context leaves 499.5 ms to the end of the recheck, so the
// read is canceled there with a planned budget of 500 ms. It is still a cut-off, not an observation failure.
func TestCutOffByTheEndOfTheRecheckIsNotAFailure(t *testing.T) {
	s := newSim(ms(5), time.Hour)
	s.readDelay = ms(1)
	s.snapshot = func(_ time.Duration, call int) time.Duration {
		if call == 0 {
			return ms(448.5)
		}
		return hangs
	}
	collection, empty := runRecheck(s)
	got := outcomes(collection.evidence)
	if len(s.reads) != 2 || s.reads[1].budget >= recheckSnapshotLimit {
		t.Fatalf("the second read must be clamped by the end of the recheck: %+v", s.reads)
	}
	if !collection.lingering || empty || collection.evidence.Observation.Failed || got["100/a"] != memberLeftover || s.at != reviewerRecheckLimit {
		t.Fatalf("%+v %v end=%v reads=%+v", collection, got, s.at, s.reads)
	}
}

// TestReadEndsAtTheEndOfTheRecheck runs the production read with a stand-in table reader that records the
// deadline of its context. The end of the recheck wins when it comes before the budget, and the budget wins
// otherwise.
func TestReadEndsAtTheEndOfTheRecheck(t *testing.T) {
	deadlines := make(chan time.Time, 3)
	injectRead(t, func(ctx context.Context) (map[int]procInfo, error) {
		deadline, _ := ctx.Deadline()
		deadlines <- deadline
		return map[int]procInfo{}, nil
	})
	hard := time.Now().Add(250 * time.Millisecond)
	if _, err := boundedProcessTable(recheckSnapshotLimit, hard); err != nil {
		t.Fatal(err)
	}
	if got := <-deadlines; !got.Equal(hard) {
		t.Fatalf("the read deadline is %v, want %v", got, hard)
	}
	for _, hard := range []time.Time{time.Now().Add(time.Hour), {}} {
		before := time.Now()
		if _, err := boundedProcessTable(recheckSnapshotLimit, hard); err != nil {
			t.Fatal(err)
		}
		if got := <-deadlines; got.Before(before.Add(recheckSnapshotLimit)) || got.After(time.Now().Add(recheckSnapshotLimit)) {
			t.Fatalf("the read deadline for limit %v is %v, want a deadline within the snapshot budget", hard, got)
		}
	}
}

func TestSnapshotReadErrorAfterTheDeadlineIsTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	deadline, _ := ctx.Deadline()
	before := deadline.Add(-time.Millisecond)
	_, err := settleTableRead(ctx, before, nil, errors.New("signal: killed"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	failure := errors.New("snapshot refused")
	if _, err = settleTableRead(t.Context(), before, nil, failure); !errors.Is(err, failure) {
		t.Fatalf("%v", err)
	}
	table := map[int]procInfo{1: member("a")}
	if got, err := settleTableRead(ctx, before, table, nil); err != nil || len(got) != 1 {
		t.Fatalf("a read that finished inside its budget stays completed when it is received late: %v %v", got, err)
	}
	if got, err := settleTableRead(ctx, deadline.Add(time.Millisecond), map[int]procInfo{}, nil); !errors.Is(err, context.DeadlineExceeded) || got != nil {
		t.Fatalf("a read that finished after the deadline is the deadline: %v %v", got, err)
	}
}
