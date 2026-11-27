package server

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/servicectl"
)

// silentListener accepts connections and never answers them, like a process
// that took the address but does not speak HTTP. It returns its URL.
func silentListener(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	return "http://" + l.Addr().String()
}

// discoveryLimits sets the probe limit and the discovery patience for one test.
func discoveryLimits(t *testing.T, probe, patience time.Duration) {
	t.Helper()
	previousProbe, previousPatience := gcProbeTimeout, gcDiscoverPatience
	t.Cleanup(func() { gcProbeTimeout, gcDiscoverPatience = previousProbe, previousPatience })
	gcProbeTimeout, gcDiscoverPatience = probe, patience
}

var gcDiscoverRequest = api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20}

// A configured address that never answers does not hold up the discovery: the
// server at the recorded address is found within the same window.
func TestServerGCDiscoveryProbesTheRecordedAddressPastASilentOne(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	gcCommandEnv(t, silentListener(t), "")
	discoveryLimits(t, 200*time.Millisecond, 2*time.Second)
	if err := writeJSON(f.s.data, servicectl.ListenRecordName, servicectl.ListenRecord{Listen: f.http.Listener.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	execute, closeTarget, err := gcExecutorFor(t.Context(), f.s.data, gcDiscoverRequest)
	elapsed := time.Since(started)
	if err != nil || elapsed >= time.Second {
		t.Fatalf("discovery: %v after %v", err, elapsed)
	}
	defer closeTarget()
	// The running server holds the data directory lock, so only it can answer.
	plan, err := execute(t.Context(), true)
	if err != nil || len(plan.Projects) != 1 || plan.Projects[0].Name != "demo" {
		t.Fatalf("the executor must be the running server: %+v %v", plan, err)
	}
}

// Addresses that never answer end the discovery at its deadline, reported as
// a timeout rather than as no server.
func TestServerGCDiscoveryEndsAtItsDeadlineWhenNoAddressAnswers(t *testing.T) {
	f := newGCFixture(t)
	gcCommandEnv(t, silentListener(t), "")
	discoveryLimits(t, 200*time.Millisecond, time.Second)
	recorded := silentListener(t)
	if err := writeJSON(f.s.data, servicectl.ListenRecordName, servicectl.ListenRecord{Listen: recorded[len("http://"):]}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, _, err := gcExecutorFor(t.Context(), f.s.data, gcDiscoverRequest)
	elapsed := time.Since(started)
	var failure *command.Failure
	if !errors.As(err, &failure) || failure.Code != "gc_timeout" || elapsed >= 1500*time.Millisecond {
		t.Fatalf("want a gc_timeout failure within 1.5s: %v after %v", err, elapsed)
	}
}

// With the data directory free, a configured address that never answers costs
// one probe limit before the gc runs offline.
func TestServerGCDiscoveryGoesOfflineAfterOneProbeLimit(t *testing.T) {
	dir, _ := offlineData(t)
	gcCommandEnv(t, silentListener(t), "")
	discoveryLimits(t, 200*time.Millisecond, 30*time.Second)
	started := time.Now()
	_, closeTarget, err := gcExecutorFor(t.Context(), dir, gcDiscoverRequest)
	elapsed := time.Since(started)
	if err != nil || elapsed >= 1200*time.Millisecond {
		t.Fatalf("discovery: %v after %v", err, elapsed)
	}
	defer closeTarget()
	// The offline executor holds the data directory lock.
	if second, err := OpenOfflineGC(dir, "test"); !errors.Is(err, ErrDataDirInUse) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("the executor must be offline and hold the lock: %v", err)
	}
}
