package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/servicectl"
)

// forbiddenInput fails the test when anything reads a question answer.
type forbiddenInput struct{ t *testing.T }

func (r forbiddenInput) Read([]byte) (int, error) {
	r.t.Helper()
	r.t.Error("the self-start read an answer")
	return 0, errors.New("no input")
}

// countStarts counts the calls of the server starter the fixture installed.
func countStarts() *atomic.Int32 {
	var calls atomic.Int32
	next := startLocalServer
	startLocalServer = func(ctx context.Context, o servicectl.StartOptions) (api.ServerInfo, error) {
		calls.Add(1)
		return next(ctx, o)
	}
	return &calls
}

// createBoard makes the project board directory of the default data
// directory, the sign that the default server is the project's server.
func createBoard(t *testing.T, cfg *config.Config) string {
	t.Helper()
	dir, err := servicectl.DataDir("")
	if err != nil {
		t.Fatal(err)
	}
	board := filepath.Join(dir, "projects", api.ProjectDirName(cfg.Project.Name), "board")
	if err := os.MkdirAll(board, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSelfStartsLocalServerWithoutAsking(t *testing.T) {
	for _, mode := range []string{"pipe", EnvExecutionID, EnvCoordinatorExecutionID, "suspended"} {
		t.Run(mode, func(t *testing.T) {
			cfg, available, _, out := serverGateFixture(t)
			createBoard(t, cfg)
			starts := countStarts()
			serverPromptInput = forbiddenInput{t}
			switch mode {
			case "pipe":
				serverTerminal = func() bool { return false }
			case "suspended":
				defer SuspendServerQuestions()()
			default:
				t.Setenv(mode, "x")
			}
			prompted, err := prepareServer(t.Context(), gateClient(t, cfg), cfg)
			if err != nil || !prompted || starts.Load() != 1 || !available.Load() {
				t.Fatalf("prompted=%v err=%v starts=%d", prompted, err, starts.Load())
			}
			if strings.Contains(out.String(), "[Y/n]") || !strings.Contains(out.String(), "Started the local server "+cfg.Server.URL) {
				t.Fatalf("notice=%q", out.String())
			}
		})
	}
}

func TestSelfStartInJSONModeReportsANotice(t *testing.T) {
	cfg, available, _, out := serverGateFixture(t)
	createBoard(t, cfg)
	starts := countStarts()
	serverPromptInput = forbiddenInput{t}
	var runErr error
	probe := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		_, runErr = prepareServer(t.Context(), gateClient(t, cfg), cfg)
		return runErr
	}}
	code, stdout, _ := commandtest.Capture(func() int { return commandtest.Run(probe, "--json") })
	if code != 0 || runErr != nil || starts.Load() != 1 || !available.Load() {
		t.Fatalf("code=%d err=%v starts=%d", code, runErr, starts.Load())
	}
	if !strings.Contains(stdout, `"server_self_started"`) || out.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout, out.String())
	}
}

func TestSelfStartNeedsTheProjectBoard(t *testing.T) {
	t.Run("interactive still asks", func(t *testing.T) {
		cfg, available, _, out := serverGateFixture(t)
		starts := countStarts()
		serverPromptInput = strings.NewReader("n\n")
		_, err := prepareServer(t.Context(), gateClient(t, cfg), cfg)
		if err == nil || starts.Load() != 0 || available.Load() || !strings.Contains(out.String(), "[Y/n]") {
			t.Fatalf("err=%v starts=%d out=%q", err, starts.Load(), out.String())
		}
	})
	t.Run("noninteractive fails", func(t *testing.T) {
		cfg, available, _, out := serverGateFixture(t)
		starts := countStarts()
		serverTerminal = func() bool { return false }
		_, err := prepareServer(t.Context(), gateClient(t, cfg), cfg)
		dir, _ := servicectl.DataDir("")
		if err == nil || !strings.Contains(err.Error(), "kander server start") || !strings.Contains(err.Error(), dir) || starts.Load() != 0 || available.Load() || out.Len() != 0 {
			t.Fatalf("err=%v starts=%d out=%q", err, starts.Load(), out.String())
		}
	})
}

func TestSelfStartSkipsAHeldDataLock(t *testing.T) {
	cfg, available, _, _ := serverGateFixture(t)
	dir := createBoard(t, cfg)
	starts := countStarts()
	lockPath := filepath.Join(dir, servicectl.DataLockName)
	file, err := fs.OpenLockFile(dir, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	_, err = prepareServer(t.Context(), gateClient(t, cfg), cfg)
	if time.Since(began) > time.Second || starts.Load() != 0 || available.Load() {
		t.Fatalf("held lock took %v, starts=%d", time.Since(began), starts.Load())
	}
	var failure *command.Failure
	if err == nil || !errors.As(err, &failure) || failure.Code != CodeUnreachable || !strings.Contains(err.Error(), lockPath) || !strings.Contains(err.Error(), "kander server start") {
		t.Fatalf("err=%v", err)
	}
	// The lock being free again lets the next call start the server.
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareServer(t.Context(), gateClient(t, cfg), cfg); err != nil || starts.Load() != 1 {
		t.Fatalf("after release err=%v starts=%d", err, starts.Load())
	}
}

func TestSelfStartLeavesAFreeLockFileAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, servicectl.DataLockName)
	if held, err := lockHeld(dir, path); held || err != nil {
		t.Fatalf("missing lock file: %v, %v", held, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the probe created the lock file: %v", err)
	}
	holder, err := fs.OpenLockFile(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	lock, err := fs.LockExclusive(holder)
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	held, err := lockHeld(dir, path)
	if !held || err != nil || time.Since(began) > 30*time.Millisecond {
		t.Fatalf("held lock: %v, %v after %v", held, err, time.Since(began))
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if held, err := lockHeld(dir, path); held || err != nil {
		t.Fatalf("released lock: %v, %v", held, err)
	}
}

func TestRemoteURLNeverSelfStarts(t *testing.T) {
	cfg, _, _, out := serverGateFixture(t)
	createBoard(t, cfg)
	starts := countStarts()
	cfg.Server.URL = "http://example.invalid"
	c, _ := client.New(cfg.Server.URL, "remote-test")
	c.Retries = 0
	if _, err := prepareServer(t.Context(), c, cfg); err == nil || starts.Load() != 0 || out.Len() != 0 {
		t.Fatalf("remote err=%v starts=%d out=%q", err, starts.Load(), out.String())
	}
}

// A test binary started on its own would run every test again, so without a
// starter the test injected nothing is started, whatever the conditions say.
func TestNoSelfStartFromATestBinaryWithoutInjection(t *testing.T) {
	cfg, available, _, _ := serverGateFixture(t)
	dir := createBoard(t, cfg)
	serverStarterInjected.Store(false)
	startLocalServer = (servicectl.Controller{}).Start
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(EnvClientHome, home)
	writeCredential(t, home, renewable("e1"))
	if _, err := Open(t.Context(), cfg, t.TempDir()); err == nil || available.Load() {
		t.Fatalf("Open started a server: %v", err)
	}
	// Both starters open their log before they run a command, so an absent log
	// means no start was tried.
	for _, log := range []string{filepath.Join(dir, "server.log"), filepath.Join(home, "worker.log")} {
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Fatalf("a process start was tried: %s: %v", log, err)
		}
	}
	// Without a server the worker is no concern, so check it with a server up.
	available.Store(true)
	if _, err := Open(t.Context(), cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "worker.log")); !os.IsNotExist(err) {
		t.Fatalf("the worker start was tried: %v", err)
	}
}

// A server a concurrent client is starting holds the data lock before it
// listens: the lock alone must not fail this client.
func TestSelfStartWaitsForAServerThatHoldsTheLockWhileStarting(t *testing.T) {
	cfg, available, _, out := serverGateFixture(t)
	dir := createBoard(t, cfg)
	starts := countStarts()
	file, err := fs.OpenLockFile(dir, filepath.Join(dir, servicectl.DataLockName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	time.AfterFunc(200*time.Millisecond, func() { available.Store(true) })
	prompted, err := prepareServer(t.Context(), gateClient(t, cfg), cfg)
	if err != nil || prompted || starts.Load() != 0 || out.Len() != 0 {
		t.Fatalf("prompted=%v err=%v starts=%d out=%q", prompted, err, starts.Load(), out.String())
	}
}

func TestACanceledSessionNeverSelfStarts(t *testing.T) {
	cfg, available, _, out := serverGateFixture(t)
	createBoard(t, cfg)
	starts := countStarts()
	serverPromptInput = forbiddenInput{t}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepareServer(ctx, gateClient(t, cfg), cfg); err == nil || starts.Load() != 0 || available.Load() || out.Len() != 0 {
		t.Fatalf("err=%v starts=%d out=%q", err, starts.Load(), out.String())
	}
}
