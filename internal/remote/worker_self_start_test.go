package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/servicectl"
)

// renewable is a live credential of execution id.
func renewable(id string) Credential {
	return Credential{
		ServerURL: "http://127.0.0.1:1", ClientID: "client", ProjectID: "project", TaskID: "task",
		ExecutionID: id, Generation: 1, Token: "token-" + id, RenewedAt: time.Now().UTC(),
	}
}

// writeCredential stores c in home, creating the private home first.
func writeCredential(t *testing.T, home string, c Credential) {
	t.Helper()
	if err := ensureHome(home); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredential(home, c); err != nil {
		t.Fatal(err)
	}
}

// workerStartFixture replaces the worker starter with a counter and gives the
// test an empty client home. next is the error the counted start returns.
func workerStartFixture(t *testing.T) (home string, starts *atomic.Int32, next *error) {
	t.Helper()
	home = filepath.Join(t.TempDir(), "client")
	var calls atomic.Int32
	var failure error
	old := startCredentialWorker
	startCredentialWorker = func(h, executable string) (*WorkerProcess, error) {
		if h != home || executable == "" {
			t.Errorf("start(%q, %q)", h, executable)
		}
		calls.Add(1)
		return nil, failure
	}
	workerStarterInjected.Store(true)
	t.Cleanup(func() {
		startCredentialWorker = old
		workerStarterInjected.Store(false)
	})
	return home, &calls, &failure
}

func lockPath(home string) string { return filepath.Join(home, WorkerLockName) }

func TestWorkerStartsForARenewableCredential(t *testing.T) {
	home, starts, _ := workerStartFixture(t)
	writeCredential(t, home, renewable("e1"))
	if err := ensureCredentialWorker(home); err != nil || starts.Load() != 1 {
		t.Fatalf("err=%v starts=%d", err, starts.Load())
	}
	if _, err := os.Stat(lockPath(home)); !os.IsNotExist(err) {
		t.Fatalf("the probe created worker.lock: %v", err)
	}
	// The probe takes and releases the lock; it never leaves it held.
	if held, err := lockHeld(home, lockPath(home)); held || err != nil {
		t.Fatalf("held=%v err=%v", held, err)
	}
}

func TestWorkerStartsOnlyForRenewableCredentials(t *testing.T) {
	cases := map[string]func(t *testing.T, home string){
		"no credentials": func(*testing.T, string) {},
		"released": func(t *testing.T, home string) {
			c := renewable("e1")
			writeCredential(t, home, c)
			tombstone := filepath.Join(credentialDir(home, c.ServerURL, c.ProjectID, c.TaskID), c.ExecutionID+".released")
			if err := os.WriteFile(tombstone, []byte("released\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"lost": func(t *testing.T, home string) {
			c := renewable("e1")
			c.Lost = true
			writeCredential(t, home, c)
		},
		"damaged": func(t *testing.T, home string) {
			c := renewable("e1")
			writeCredential(t, home, c)
			path := filepath.Join(credentialDir(home, c.ServerURL, c.ProjectID, c.TaskID), c.ExecutionID+".json")
			if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			home, starts, _ := workerStartFixture(t)
			prepare(t, home)
			if err := ensureCredentialWorker(home); err != nil || starts.Load() != 0 {
				t.Fatalf("err=%v starts=%d", err, starts.Load())
			}
			if _, err := os.Stat(lockPath(home)); !os.IsNotExist(err) {
				t.Fatalf("worker.lock was created: %v", err)
			}
		})
	}
}

func TestWorkerStartsOnceARenewableCredentialFollowsALostOne(t *testing.T) {
	home, starts, _ := workerStartFixture(t)
	lost := renewable("e1")
	lost.Lost = true
	writeCredential(t, home, lost)
	writeCredential(t, home, renewable("e2"))
	if err := ensureCredentialWorker(home); err != nil || starts.Load() != 1 {
		t.Fatalf("err=%v starts=%d", err, starts.Load())
	}
}

func TestWorkerIsNotStartedWhileOneHoldsTheLock(t *testing.T) {
	home, starts, _ := workerStartFixture(t)
	writeCredential(t, home, renewable("e1"))
	file, err := fs.OpenLockFile(home, lockPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	began := time.Now()
	if err := ensureCredentialWorker(home); err != nil || starts.Load() != 0 {
		t.Fatalf("err=%v starts=%d", err, starts.Load())
	}
	// WorkerHeld would wait 50 ms on a held lock; the probe never waits.
	if elapsed := time.Since(began); elapsed > 30*time.Millisecond {
		t.Fatalf("a held worker.lock took %v", elapsed)
	}
	// The probe alone answers within 10 ms. The best of a few tries keeps a
	// scheduling stall of a loaded machine from failing the bound.
	best := time.Hour
	for range 5 {
		began := time.Now()
		held, err := lockHeld(home, lockPath(home))
		if !held || err != nil {
			t.Fatalf("held=%v err=%v", held, err)
		}
		best = min(best, time.Since(began))
	}
	if best > 10*time.Millisecond {
		t.Fatalf("the held worker.lock probe took %v", best)
	}
}

func TestOpenSurvivesAWorkerStartFailure(t *testing.T) {
	cfg, available, _, out := serverGateFixture(t)
	available.Store(true)
	home, starts, next := workerStartFixture(t)
	t.Setenv(EnvClientHome, home)
	writeCredential(t, home, renewable("e1"))
	*next = errors.New("no executable")
	if s, err := Open(t.Context(), cfg, t.TempDir()); err != nil || s == nil {
		t.Fatalf("Open failed: %v", err)
	}
	if starts.Load() != 1 || !strings.Contains(out.String(), "the background worker did not start") || !strings.Contains(out.String(), "no executable") {
		t.Fatalf("starts=%d notice=%q", starts.Load(), out.String())
	}
}

func TestOpenStartsNoWorkerWhenTheServerIsUnreachable(t *testing.T) {
	cfg, _, _, _ := serverGateFixture(t)
	home, starts, _ := workerStartFixture(t)
	t.Setenv(EnvClientHome, home)
	writeCredential(t, home, renewable("e1"))
	// Nothing listens and there is no board to start a server for.
	cfg.Server.URL = "http://127.0.0.1:1"
	serverTerminal = func() bool { return false }
	if _, err := Open(t.Context(), cfg, t.TempDir()); err == nil || starts.Load() != 0 {
		t.Fatalf("a failed session started the worker: err=%v starts=%d", err, starts.Load())
	}
}

func TestOpenStartsTheWorkerOnceTheServerIsUp(t *testing.T) {
	cfg, available, _, _ := serverGateFixture(t)
	available.Store(true)
	home, starts, _ := workerStartFixture(t)
	t.Setenv(EnvClientHome, home)
	writeCredential(t, home, renewable("e1"))
	if _, err := Open(t.Context(), cfg, t.TempDir()); err != nil || starts.Load() != 1 {
		t.Fatalf("err=%v starts=%d", err, starts.Load())
	}
}

func TestDetachedChildrenGetNeitherTheExecutionVariablesNorTheCallerDirectory(t *testing.T) {
	t.Setenv(EnvExecutionID, "exec-1")
	t.Setenv(EnvCoordinatorExecutionID, "exec-2")
	t.Setenv("KANDER_KEEP_ME", "yes")
	home := filepath.Join(t.TempDir(), "client")
	cmd := workerCommand(home, "/bin/kander")
	if cmd.Dir != home {
		t.Fatalf("worker directory %q, want %q", cmd.Dir, home)
	}
	if !slices.Contains(cmd.Env, EnvClientHome+"="+home) || !slices.Contains(cmd.Env, "KANDER_KEEP_ME=yes") {
		t.Fatalf("worker env lost a variable: %v", cmd.Env)
	}
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, EnvExecutionID+"=") || strings.HasPrefix(entry, EnvCoordinatorExecutionID+"=") {
			t.Fatalf("worker env keeps %q", entry)
		}
	}
}

func TestOpenStartsNothingForACommandCanceledWhileTheServerStarts(t *testing.T) {
	cfg, available, _, _ := serverGateFixture(t)
	createBoard(t, cfg)
	home, starts, _ := workerStartFixture(t)
	t.Setenv(EnvClientHome, home)
	writeCredential(t, home, renewable("e1"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	next := startLocalServer
	startLocalServer = func(c context.Context, o servicectl.StartOptions) (api.ServerInfo, error) {
		cancel()
		return next(c, o)
	}
	if _, err := Open(ctx, cfg, t.TempDir()); !errors.Is(err, context.Canceled) || starts.Load() != 0 || !available.Load() {
		t.Fatalf("err=%v worker starts=%d", err, starts.Load())
	}
}
