package worker

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/remote"
)

// handoverRig sets up a worker that is about to hand over: the executable is
// replaced, the probe passes, and the start of the successor fails.
type handoverRig struct {
	*rig
	w      *loop
	lock   *workerLock
	self   *handover
	opened *renewals
	clock  *heldClock
	onStop atomic.Pointer[func(pid int)]
}

// heldClock is the clock of the worker in a handoverRig. It follows the clock
// of the harness, unless it is held: then the worker sees no time pass while a
// test moves the clock of the server, so it cannot renew a lease the test is
// about to take over.
type heldClock struct {
	mu   sync.Mutex
	h    *harness
	held bool
	at   time.Time
}

func (c *heldClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.held {
		return c.at
	}
	return c.h.now()
}

func (c *heldClock) hold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held, c.at = true, c.h.now()
}

func (c *heldClock) resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held = false
}

func newHandoverRig(t *testing.T, h *harness) *handoverRig {
	t.Helper()
	r := newRigOn(t, h)
	o := r.options()
	o.Tick = 10 * time.Millisecond
	clock := &heldClock{h: h}
	o.Now = clock.now
	hr := &handoverRig{rig: r, clock: clock}
	record := h.workerOptions().Stop
	o.Stop = func(pid int) error {
		if hook := hr.onStop.Load(); hook != nil {
			(*hook)(pid)
		}
		return record(pid)
	}
	w := newLoop(o)
	lock := &workerLock{home: r.home}
	if err := lock.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lock.release)
	self, err := newHandover(o)
	if err != nil {
		t.Fatal(err)
	}
	w.startRenewals(t.Context())
	t.Cleanup(w.stopRenewals)
	r.writeExecutable("v2")
	hr.w, hr.lock, hr.self, hr.opened = w, lock, self, w.renewing
	return hr
}

func (r *handoverRig) handover(t *testing.T) handoverResult {
	t.Helper()
	result, err := r.w.tryHandover(t.Context(), r.self, r.lock)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (r *handoverRig) exited() bool {
	select {
	case <-r.opened.done:
		return true
	default:
		return false
	}
}

func TestHandoverStopsRenewalsBeforeTheLockIsReleased(t *testing.T) {
	r := newHandoverRig(t, newHarness(t))
	var exitedAtSpawn, freeAtSpawn bool
	r.spawn = func(home, _ string) (Successor, error) {
		held, err := remote.WorkerHeld(home)
		exitedAtSpawn, freeAtSpawn = r.exited(), err == nil && !held
		return nil, errors.New("no successor in this test")
	}
	r.handover(t)
	if !exitedAtSpawn || !freeAtSpawn {
		t.Fatalf("at the start of the successor the renewal loop must have exited and the lock be free: exited=%v free=%v", exitedAtSpawn, freeAtSpawn)
	}
}

func TestFailedHandoverResumesRenewals(t *testing.T) {
	r := newHandoverRig(t, newHarness(t))
	_, c := r.claimed("resumes")
	if got := r.handover(t); got != handoverNone {
		t.Fatalf("a failed handover keeps serving: %v", got)
	}
	if !r.exited() {
		t.Fatal("the renewal loop of the failed handover must have been replaced")
	}
	r.advance(16 * time.Second)
	waitFor(t, "a renewal after the failed handover", func() bool { return r.credentialOf(c).RenewedAt.Equal(r.now()) })
}

// The probe of a replaced binary takes up to ten seconds; the leases are not
// left unrenewed meanwhile.
func TestRenewalsContinueDuringTheHandoverProbe(t *testing.T) {
	events, wrap := renewalEvents()
	h := newHarnessWrapped(t, 0, wrap)
	r := newHandoverRig(t, h)
	_, c := r.claimed("probe-renews")
	var renewedDuringProbe bool
	r.probe = func() error {
		r.advance(16 * time.Second)
		select {
		case resource := <-events:
			renewedDuringProbe = resource == c.TaskID
		case <-time.After(waitCap):
		}
		return nil
	}
	r.spawn = func(string, string) (Successor, error) { return nil, errors.New("no successor in this test") }
	r.handover(t)
	if !renewedDuringProbe {
		t.Fatal("no renewal reached the server while the probe ran")
	}
}

// takeOverWhileHeld lets the server lapse and hands the tasks to host-b while
// the worker still sees the old time, then lets the worker see the new time.
// The worker finds each lease lost at its next renewal.
func (r *handoverRig) takeOverWhileHeld(cs ...remote.Credential) {
	r.clock.hold()
	r.advance(100 * time.Second)
	for _, c := range cs {
		r.takeOver(c)
	}
	r.clock.resume()
}

// markedLost reports whether the renewal loop has marked a lease lost that the
// work loop has not stopped yet.
func (r *handoverRig) markedLost() bool {
	r.w.mu.Lock()
	defer r.w.mu.Unlock()
	return len(r.w.lostMarks) > 0
}

// A lease lost while the probe of a replaced binary runs is stopped before the
// renewals pause for the handover.
func TestLostMarkDuringTheProbeIsStoppedBeforeTheHandover(t *testing.T) {
	h := newHarness(t)
	r := newHandoverRig(t, h)
	_, c := r.claimed("probe-lost")
	r.probe = func() error {
		r.takeOverWhileHeld(c)
		waitFor(t, "the renewal loop to mark the lease lost", r.markedLost)
		return nil
	}
	var stoppedAtSpawn int
	r.spawn = func(string, string) (Successor, error) {
		stoppedAtSpawn = len(r.stoppedPIDs())
		return nil, errors.New("no successor in this test")
	}
	r.handover(t)
	if stoppedAtSpawn != 1 {
		t.Fatalf("the agent must be stopped before the successor starts: %d stopped", stoppedAtSpawn)
	}
}

// A lease marked lost while the handover stops another agent is stopped before
// the lock is released, not left for the new worker or the next round. The
// renewals run until no mark is left, so a third lease is still renewed while
// the stops go on.
func TestLostMarkDuringAStopIsStoppedBeforeTheLockIsReleased(t *testing.T) {
	var holdFirstB sync.Once
	gateB := newHookGate(t)
	h := newHarnessWrapped(t, 0, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if resource, ok := isRenewal(r); ok && strings.Contains(resource, "b-second") {
				holdFirstB.Do(gateB.hold)
			}
			next.ServeHTTP(w, r)
		})
	})
	r := newHandoverRig(t, h)
	_, a := r.claimedAs("a-first", "exec-a")
	_, b := r.claimedAs("b-second", "exec-b")
	_, c := r.claimedAs("c-third", "exec-c")
	stopA, stopB := newHookGate(t), newHookGate(t)
	var stops atomic.Int32
	hook := func(int) {
		// The first stop is the one of a: b is marked while it is in progress.
		if stops.Add(1) == 1 {
			stopA.hold()
		} else {
			stopB.hold()
		}
	}
	r.onStop.Store(&hook)
	r.probe = func() error {
		r.takeOverWhileHeld(a, b)
		waitFor(t, "the renewal loop to mark a lost", r.markedLost)
		return nil
	}
	var stoppedAtSpawn int
	r.spawn = func(string, string) (Successor, error) {
		stoppedAtSpawn = len(r.stoppedPIDs())
		return nil, errors.New("no successor in this test")
	}
	wait := background(t, func() { r.handover(t) })
	stopA.waitEntered(t)
	// The renewal of b has been held at the server since the probe; it gets the
	// answer now, while the stop of a is in progress.
	gateB.let()
	waitFor(t, "the renewal loop to mark b lost", func() bool {
		r.w.mu.Lock()
		defer r.w.mu.Unlock()
		_, marked := r.w.lostMarks[keyOf(b)]
		return marked
	})
	stopA.let()
	stopB.waitEntered(t)
	// The stop of b is in progress and the renewals have not been paused: the
	// third lease is renewed meanwhile.
	r.advance(16 * time.Second)
	waitFor(t, "the renewal of the third lease during the stop of b", func() bool { return r.credentialOf(c).RenewedAt.Equal(r.now()) })
	stopB.let()
	wait()
	if stoppedAtSpawn != 2 {
		t.Fatalf("both agents must be stopped before the successor starts: %d stopped", stoppedAtSpawn)
	}
}
