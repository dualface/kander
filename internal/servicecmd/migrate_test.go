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
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/servicectl"
	"github.com/dualface/kander/v2/internal/svcinstall"
)

// freeListen is a loopback address nothing listens at.
func freeListen(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// fakeServer is a server of data at listen. The authenticated stop closes it.
type fakeServer struct {
	hs      *httptest.Server
	listen  string
	stopped atomic.Bool
}

func serveAt(t *testing.T, listen, data string, pid int, managed bool) *fakeServer {
	t.Helper()
	l, err := net.Listen("tcp", listen)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeServer{listen: listen}
	f.hs = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/v1/server":
			_ = json.NewEncoder(w).Encode(api.ServerInfo{DataDir: data, PID: pid})
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(api.Health{Ready: true})
		case "/api/v1/server/shutdown":
			f.stopped.Store(true)
			w.WriteHeader(http.StatusAccepted)
			go f.hs.Close()
		}
	}))
	f.hs.Listener.Close()
	f.hs.Listener = l
	f.hs.Start()
	t.Cleanup(f.hs.Close)
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(data, api.AdminTokenFile)
	if _, err := os.Stat(token); os.IsNotExist(err) {
		if err := fs.WriteTextAtomic(data, token, `{"token":"`+strings.Repeat("a", 64)+`"}`, false); err != nil {
			t.Fatal(err)
		}
	}
	record, _ := json.Marshal(servicectl.ListenRecord{Listen: listen, PID: pid, Managed: managed})
	if err := os.WriteFile(filepath.Join(data, servicectl.ListenRecordName), record, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// serverHost is a host whose service manager runs a fake server service. As with systemd, a
// start of an active service does nothing, and a restart runs the current definition again;
// listen is the address the current definition names.
type serverHost struct {
	*workerHost
	data    string
	mu      sync.Mutex
	listen  string
	active  *fakeServer
	nextPID int
}

func newServerHost(t *testing.T, goos string) *serverHost {
	h := &serverHost{workerHost: newWorkerHost(t), data: filepath.Join(t.TempDir(), "data"), nextPID: 100}
	var registered []byte
	svcinstall.UseForTest(t, svcinstall.TestPlatform{GOOS: goos, Dir: h.unitDir, User: "alice", Temp: t.TempDir(),
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			switch {
			case line == "systemctl --user start kander-server.service", line == "schtasks /Run /TN kander-server":
				h.serve(false)
			case line == "systemctl --user restart kander-server.service":
				h.serve(true)
			case strings.HasPrefix(line, "schtasks /Create"):
				data, err := os.ReadFile(args[4])
				registered = data
				return nil, err
			case strings.HasPrefix(line, "schtasks /Query /TN kander-server /XML"):
				if registered == nil {
					return nil, errors.New("exit status 1")
				}
				return registered, nil
			}
			return h.run(ctx, name, args...)
		}})
	if goos == "windows" {
		globalEntry = func() (string, error) { return `C:\kander.exe`, nil }
	}
	return h
}

// serve starts the service server at the address of the current definition: always on a
// restart, and on a start only when the service is not active.
func (h *serverHost) serve(restart bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active != nil && !h.active.stopped.Load() {
		if !restart {
			return
		}
		h.active.hs.Close()
	}
	h.nextPID++
	h.active = serveAt(h.t, h.listen, h.data, h.nextPID, true)
}

func (h *serverHost) install(t *testing.T, listen string) (InstallResult, error) {
	h.mu.Lock()
	h.listen = listen
	h.mu.Unlock()
	return Install(t.Context(), svcinstall.KindServer, ServerOptions{Listen: listen, DataDir: h.data, TTL: time.Minute, Renew: 10 * time.Second})
}

func answers(t *testing.T, listen string) bool {
	t.Helper()
	target, _ := servicectl.ProbeURL(listen)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := servicectl.Info(ctx, target)
	return err == nil
}

// A changed listen address restarts the service: the running server at the old address is
// the one of the service, although nothing answers at the new one yet.
func TestInstallMovesARunningServiceServerToANewListenAddress(t *testing.T) {
	h := newServerHost(t, "linux")
	first, second := freeListen(t), freeListen(t)
	if _, err := h.install(t, first); err != nil {
		t.Fatal(err)
	}
	old := h.active
	h.commands()
	result, err := h.install(t, second)
	if err != nil || !result.Changed || !result.Restarted || result.Stopped != "" {
		t.Fatalf("install at a new address: %+v %v", result, err)
	}
	if got := h.commands(); !slices.Contains(got, "systemctl --user restart kander-server.service") {
		t.Fatalf("commands %q", got)
	}
	if answers(t, first) || !answers(t, second) || old.stopped.Load() {
		t.Fatalf("old address answers %v, new address answers %v, authenticated stop %v", answers(t, first), answers(t, second), old.stopped.Load())
	}
}

// On Windows the scheduled task does not stop the server: the server of the service stops at
// its old address through the authenticated stop, and the task starts again.
func TestInstallMovesTheWindowsServiceServerThroughTheProtocol(t *testing.T) {
	h := newServerHost(t, "windows")
	first, second := freeListen(t), freeListen(t)
	if _, err := h.install(t, first); err != nil {
		t.Fatal(err)
	}
	old := h.active
	h.commands()
	result, err := h.install(t, second)
	if err != nil || !result.Restarted {
		t.Fatalf("install at a new address: %+v %v", result, err)
	}
	got := h.commands()
	if !old.stopped.Load() || !slices.Contains(got, "schtasks /Run /TN kander-server") || !answers(t, second) {
		t.Fatalf("authenticated stop %v, commands %q", old.stopped.Load(), got)
	}
	for _, c := range got {
		if strings.Contains(c, "/End") {
			t.Fatalf("the task was ended: %q", got)
		}
	}
}

// A detached server of the same data directory that listens at another address is stopped
// before the service manager starts its own, as one at the same address is.
func TestInstallStopsADetachedServerOfTheDataDirectoryAtAnotherAddress(t *testing.T) {
	h := newServerHost(t, "linux")
	elsewhere, listen := freeListen(t), freeListen(t)
	detached := serveAt(t, elsewhere, h.data, 77, false)
	result, err := h.install(t, listen)
	if err != nil || !strings.Contains(result.Stopped, "77") || result.Restarted {
		t.Fatalf("install: %+v %v", result, err)
	}
	if !detached.stopped.Load() || !answers(t, listen) {
		t.Fatalf("authenticated stop %v, service answers %v", detached.stopped.Load(), answers(t, listen))
	}
}

// A server of another data directory at an address the data directory once recorded is not
// the one of the service and stays.
func TestInstallLeavesAServerOfAnotherDataDirectoryAtTheRecordedAddress(t *testing.T) {
	h := newServerHost(t, "linux")
	elsewhere, listen := freeListen(t), freeListen(t)
	other := serveAt(t, elsewhere, filepath.Join(t.TempDir(), "other"), 55, false)
	if err := os.MkdirAll(h.data, 0o700); err != nil {
		t.Fatal(err)
	}
	record, _ := json.Marshal(servicectl.ListenRecord{Listen: elsewhere, PID: 54})
	if err := os.WriteFile(filepath.Join(h.data, servicectl.ListenRecordName), record, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := h.install(t, listen)
	if err != nil || result.Stopped != "" || other.stopped.Load() || !answers(t, elsewhere) {
		t.Fatalf("install: %+v %v, other stopped %v", result, err, other.stopped.Load())
	}
}

// A worker service installed again for another client home restarts, so the worker of the old
// home does not keep running in its place.
func TestInstallMovesTheWorkerServiceToAnotherClientHome(t *testing.T) {
	h := newWorkerHost(t)
	if _, err := Install(t.Context(), svcinstall.KindWorker, ServerOptions{}); err != nil {
		t.Fatal(err)
	}
	h.commands()
	h.home = filepath.Join(t.TempDir(), "other-client")
	t.Setenv("KANDER_CLIENT_HOME", h.home)
	result, err := Install(t.Context(), svcinstall.KindWorker, ServerOptions{})
	if err != nil || !result.Changed || !result.Restarted || result.ClientHome != h.home {
		t.Fatalf("install for another client home: %+v %v", result, err)
	}
	if got := h.commands(); !slices.Contains(got, "systemctl --user restart kander-worker.service") || slices.Contains(got, "systemctl --user start kander-worker.service") {
		t.Fatalf("commands %q", got)
	}
}

// Another HTTP application at the address of a stopped service, which answers 404, is not a
// Kander server: the install moves the service to its new address all the same.
func TestInstallMovesAStoppedServiceAwayFromAnAddressAnotherApplicationTook(t *testing.T) {
	h := newServerHost(t, "linux")
	first, second := freeListen(t), freeListen(t)
	if _, err := h.install(t, first); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.active.hs.Close()
	h.active.stopped.Store(true)
	h.mu.Unlock()
	l, err := net.Listen("tcp", first)
	if err != nil {
		t.Fatal(err)
	}
	other := httptest.NewUnstartedServer(http.NotFoundHandler())
	other.Listener.Close()
	other.Listener = l
	other.Start()
	t.Cleanup(other.Close)
	h.commands()
	result, err := h.install(t, second)
	if err != nil || !result.Changed || !result.Restarted || result.Stopped != "" {
		t.Fatalf("install at a new address: %+v %v", result, err)
	}
	if got := h.commands(); !slices.Contains(got, "systemctl --user restart kander-server.service") || !answers(t, second) {
		t.Fatalf("commands %q, new address answers %v", got, answers(t, second))
	}
	if resp, err := http.Get("http://" + first + "/api/v1/server"); err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the other application changed: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}
}
