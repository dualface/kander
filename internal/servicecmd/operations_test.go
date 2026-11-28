package servicecmd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/servicectl"
	"github.com/dualface/kander/v2/internal/svcinstall"
)

// workerHost is a client home with a fake systemd: starting the worker service takes
// worker.lock and records a managed worker, as the real service would.
type workerHost struct {
	t       *testing.T
	home    string
	unitDir string
	mu      sync.Mutex
	calls   []string
	lock    *fs.ExclusiveLock
	file    *os.File
	linger  string
	// down makes every systemctl command fail, as on a host without a user instance.
	down bool
}

func newWorkerHost(t *testing.T) *workerHost {
	h := &workerHost{t: t, home: filepath.Join(t.TempDir(), "client"), unitDir: t.TempDir(), linger: "no"}
	t.Setenv("KANDER_CLIENT_HOME", h.home)
	t.Setenv("KANDER_MANAGED", "")
	svcinstall.UseForTest(t, svcinstall.TestPlatform{GOOS: "linux", Dir: h.unitDir, Run: h.run, User: "alice"})
	entry := filepath.Join(t.TempDir(), "kander")
	if err := os.WriteFile(entry, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := []any{globalEntry, packaged, workerStopPoll, workerStopTimeout, readyTimeout, readyPoll}
	globalEntry = func() (string, error) { return entry, nil }
	packaged = func() bool { return false }
	workerStopPoll, workerStopTimeout, readyTimeout, readyPoll = 5*time.Millisecond, 5*time.Second, 5*time.Second, 5*time.Millisecond
	t.Cleanup(func() {
		globalEntry = saved[0].(func() (string, error))
		packaged = saved[1].(func() bool)
		workerStopPoll, workerStopTimeout = saved[2].(time.Duration), saved[3].(time.Duration)
		readyTimeout, readyPoll = saved[4].(time.Duration), saved[5].(time.Duration)
		h.release()
	})
	return h
}

func (h *workerHost) run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	h.mu.Lock()
	h.calls = append(h.calls, line)
	down := h.down
	h.mu.Unlock()
	if down && name == "systemctl" {
		return []byte("Failed to connect to bus"), errors.New("exit status 1")
	}
	switch line {
	case "systemctl --user start kander-worker.service", "systemctl --user restart kander-worker.service":
		h.release()
		h.hold(true)
	case "systemctl --user stop kander-worker.service", "systemctl --user disable --now kander-worker.service":
		h.release()
	case "loginctl show-user alice --property=Linger":
		return []byte("Linger=" + h.linger + "\n"), nil
	}
	return nil, nil
}

// hold takes worker.lock and writes the record of a worker that is managed or not.
func (h *workerHost) hold(managed bool) {
	h.t.Helper()
	if err := os.MkdirAll(h.home, 0o700); err != nil {
		h.t.Fatal(err)
	}
	file, err := fs.OpenLockFile(h.home, filepath.Join(h.home, remote.WorkerLockName))
	if err != nil {
		h.t.Fatal(err)
	}
	lock, taken, err := fs.TryLockExclusive(file)
	if err != nil || !taken {
		h.t.Fatalf("worker.lock: %v %v", taken, err)
	}
	h.file, h.lock = file, lock
	if err := remote.WriteWorkerRecord(h.home, remote.WorkerRecord{Version: "v", PID: 4242, Managed: managed, StartedAt: time.Now()}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *workerHost) release() {
	if h.file != nil {
		_ = h.lock.Unlock()
		_ = h.file.Close()
		h.file, h.lock = nil, nil
	}
}

func (h *workerHost) commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := slices.Clone(h.calls)
	h.calls = nil
	return out
}

func TestInstallWorkerIsIdempotentAndRestartsOnAChange(t *testing.T) {
	h := newWorkerHost(t)
	ctx := t.Context()
	result, err := Install(ctx, svcinstall.KindWorker, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Restarted || result.Stopped != "" || result.Linger != "no" || result.ClientHome != h.home {
		t.Fatalf("first install: %+v", result)
	}
	want := []string{"systemctl --user show-environment", "systemctl --user daemon-reload", "systemctl --user enable kander-worker.service",
		"systemctl --user start kander-worker.service", "loginctl show-user alice --property=Linger"}
	if got := h.commands(); !slices.Equal(got, want) {
		t.Fatalf("commands\n%q\nwant\n%q", got, want)
	}
	unit, err := os.ReadFile(filepath.Join(h.unitDir, "kander-worker.service"))
	if err != nil || !strings.Contains(string(unit), `Environment="KANDER_CLIENT_HOME=`+h.home+`"`) || !strings.Contains(string(unit), "KillMode=process") {
		t.Fatalf("unit: %v\n%s", err, unit)
	}
	info, _ := os.Stat(filepath.Join(h.home, "worker.log"))
	if info == nil {
		t.Fatal("the log was not created before the service manager writes it")
	}

	// Unchanged: the file stays and the running service is left alone.
	before, _ := os.Stat(filepath.Join(h.unitDir, "kander-worker.service"))
	result, err = Install(ctx, svcinstall.KindWorker, ServerOptions{})
	if err != nil || result.Changed || result.Restarted {
		t.Fatalf("second install: %+v %v", result, err)
	}
	after, _ := os.Stat(filepath.Join(h.unitDir, "kander-worker.service"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged unit was rewritten")
	}
	for _, c := range h.commands() {
		if strings.Contains(c, "start") || strings.Contains(c, "daemon-reload") {
			t.Fatalf("an unchanged install ran %q", c)
		}
	}

	// A changed definition restarts the running service.
	entry := filepath.Join(t.TempDir(), "kander")
	if err := os.WriteFile(entry, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	globalEntry = func() (string, error) { return entry, nil }
	result, err = Install(ctx, svcinstall.KindWorker, ServerOptions{})
	if err != nil || !result.Changed || !result.Restarted {
		t.Fatalf("changed install: %+v %v", result, err)
	}
	if got := h.commands(); !slices.Contains(got, "systemctl --user restart kander-worker.service") {
		t.Fatalf("commands %q", got)
	}
}

// A worker that kander had started in the background is asked to stop through worker.stop and
// the service manager starts its own; nothing in the client home is lost.
func TestInstallStopsADetachedWorkerFirst(t *testing.T) {
	h := newWorkerHost(t)
	h.hold(false)
	credential := filepath.Join(h.home, "credential-kept")
	if err := os.WriteFile(credential, []byte("lease"), 0o600); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			if _, found, _ := remote.ReadWorkerStopRequest(h.home); found {
				h.release()
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	result, err := Install(t.Context(), svcinstall.KindWorker, ServerOptions{})
	<-stopped
	if err != nil || !strings.Contains(result.Stopped, "4242") {
		t.Fatalf("install: %+v %v", result, err)
	}
	if data, err := os.ReadFile(credential); err != nil || string(data) != "lease" {
		t.Fatalf("the client home lost a file: %v", err)
	}
	if status, _ := remote.InspectWorker(h.home); status.Run != remote.WorkerRunning || !status.Record.Managed {
		t.Fatalf("the service worker does not run: %+v", status)
	}
}

func TestInstallRefusals(t *testing.T) {
	t.Run("msix", func(t *testing.T) {
		h := newWorkerHost(t)
		packaged = func() bool { return true }
		if _, err := Install(t.Context(), svcinstall.KindWorker, ServerOptions{}); err == nil || !strings.Contains(err.Error(), "Microsoft Store") {
			t.Fatalf("an MSIX install must be refused: %v", err)
		}
		if _, err := os.Stat(filepath.Join(h.unitDir, "kander-worker.service")); !os.IsNotExist(err) {
			t.Fatal("a refused install wrote a unit")
		}
	})
	t.Run("no systemd user instance", func(t *testing.T) {
		h := newWorkerHost(t)
		h.down = true
		_, err := Install(t.Context(), svcinstall.KindWorker, ServerOptions{})
		if err == nil || !strings.Contains(err.Error(), "automatic start") {
			t.Fatalf("a host without a user instance must be refused and keep the self-start: %v", err)
		}
	})
	t.Run("foreign unit", func(t *testing.T) {
		h := newWorkerHost(t)
		path := filepath.Join(h.unitDir, "kander-worker.service")
		if err := os.WriteFile(path, []byte("[Service]\nExecStart=/bin/true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var foreign *svcinstall.ForeignError
		if _, err := Install(t.Context(), svcinstall.KindWorker, ServerOptions{}); !errors.As(err, &foreign) || foreign.Location != path {
			t.Fatalf("install over a hand-written unit: %v", err)
		}
		if _, _, err := Uninstall(t.Context(), svcinstall.KindWorker); !errors.As(err, &foreign) {
			t.Fatalf("uninstall of a hand-written unit: %v", err)
		}
		if data, _ := os.ReadFile(path); string(data) != "[Service]\nExecStart=/bin/true\n" {
			t.Fatal("the hand-written unit changed")
		}
	})
}

func TestStatusUninstallAndKanderUninstall(t *testing.T) {
	h := newWorkerHost(t)
	ctx := t.Context()
	if _, err := Install(ctx, svcinstall.KindWorker, ServerOptions{}); err != nil {
		t.Fatal(err)
	}
	statuses := Inspect(ctx, svcinstall.Kinds)
	server, worker := statuses[0], statuses[1]
	if server.Installed || server.Foreign {
		t.Fatalf("server: %+v", server)
	}
	if !worker.Installed || !worker.Running || !worker.Managed || worker.PID != 4242 || worker.ClientHome != h.home || len(worker.Warnings) != 0 {
		t.Fatalf("worker: %+v", worker)
	}
	entry, _ := globalEntry()
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if st := Inspect(ctx, []svcinstall.Kind{svcinstall.KindWorker})[0]; !st.MissingExecutable || len(st.Warnings) == 0 {
		t.Fatalf("a missing executable is a problem: %+v", st)
	}
	services, err := inspectUninstallServices(ctx)
	if err != nil || len(services) != 1 || services[0].Kind != "worker" {
		t.Fatalf("kander uninstall services: %+v %v", services, err)
	}
	h.commands()
	if err := services[0].Unregister(ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.commands(); !slices.Equal(got, []string{"systemctl --user disable --now kander-worker.service", "systemctl --user daemon-reload"}) {
		t.Fatalf("commands %q", got)
	}
	if _, found, err := Uninstall(ctx, svcinstall.KindWorker); found || err != nil {
		t.Fatalf("a second uninstall finds nothing: %v %v", found, err)
	}
}

// The server service records the options of kander server start, with an absolute data
// directory, and the service manager starts it.
func TestInstallServerRecordsTheServerOptions(t *testing.T) {
	h := newWorkerHost(t)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := probe.Addr().String()
	probe.Close()
	base := t.TempDir()
	t.Chdir(base)
	data := filepath.Join(base, "data")
	var server *httptest.Server
	t.Cleanup(func() {
		if server != nil {
			server.Close()
		}
	})
	run := h.run
	svcinstall.UseForTest(t, svcinstall.TestPlatform{GOOS: "linux", Dir: h.unitDir, User: "alice", Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "--user start kander-server.service" {
			l, err := net.Listen("tcp", listen)
			if err != nil {
				return nil, err
			}
			server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/api/v1/server":
					_ = json.NewEncoder(w).Encode(api.ServerInfo{DataDir: data, PID: 99})
				case "/api/v1/health":
					_ = json.NewEncoder(w).Encode(api.Health{Ready: true})
				}
			}))
			server.Listener.Close()
			server.Listener = l
			server.Start()
			record, _ := json.Marshal(servicectl.ListenRecord{Listen: listen, PID: 99, Managed: true})
			if err := os.WriteFile(filepath.Join(data, servicectl.ListenRecordName), record, 0o600); err != nil {
				return nil, err
			}
		}
		return run(ctx, name, args...)
	}})
	result, err := Install(t.Context(), svcinstall.KindServer, ServerOptions{Listen: listen, DataDir: "data", TTL: 2 * time.Minute, Renew: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"server", "start", "--foreground", "--data", data, "--listen", listen, "--ttl", "2m0s", "--renew", "20s"}
	if !slices.Equal(result.Args, want) || !result.Changed {
		t.Fatalf("args %q, want %q (%+v)", result.Args, want, result)
	}
	unit, _ := os.ReadFile(filepath.Join(h.unitDir, "kander-server.service"))
	if !strings.Contains(string(unit), `"--data" "`+data+`"`) || strings.Contains(string(unit), "KillMode") {
		t.Fatalf("unit:\n%s", unit)
	}
	st := Inspect(t.Context(), []svcinstall.Kind{svcinstall.KindServer})[0]
	if !st.Running || !st.Managed || st.PID != 99 {
		t.Fatalf("status %+v", st)
	}
}
