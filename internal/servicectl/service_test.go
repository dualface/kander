package servicectl

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/dualface/kander/v2/internal/svcinstall"
)

// serviceRig is a server service whose service manager is a fake: starting it makes the test
// server ready, stopping it closes the test server.
type serviceRig struct {
	hs       *httptest.Server
	dataDir  string
	ready    atomic.Bool
	shutdown atomic.Bool
	mu       sync.Mutex
	calls    []string
}

func (r *serviceRig) run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	r.mu.Lock()
	r.calls = append(r.calls, line)
	r.mu.Unlock()
	switch line {
	case "systemctl --user start kander-server.service", "systemctl --user restart kander-server.service":
		r.ready.Store(true)
	case "systemctl --user stop kander-server.service":
		r.hs.Close()
	}
	return nil, nil
}

func (r *serviceRig) commands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// newServiceRig installs a server service for the client home of this test whose listen
// address is the test server.
func newServiceRig(t *testing.T, goos string, run func(*serviceRig) func(context.Context, string, ...string) ([]byte, error)) *serviceRig {
	r := &serviceRig{dataDir: t.TempDir()}
	if err := fs.WriteTextAtomic(r.dataDir, filepath.Join(r.dataDir, api.AdminTokenFile), `{"token":"`+strings.Repeat("a", 64)+`"}`, false); err != nil {
		t.Fatal(err)
	}
	r.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/v1/server":
			_ = json.NewEncoder(w).Encode(api.ServerInfo{DataDir: r.dataDir, PID: 42})
		case "/api/v1/server/shutdown":
			r.shutdown.Store(true)
			w.WriteHeader(http.StatusAccepted)
			go r.hs.Close()
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(api.Health{Ready: r.ready.Load()})
		}
	}))
	t.Cleanup(r.hs.Close)
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv("KANDER_CLIENT_HOME", home)
	runner := r.run
	if run != nil {
		runner = run(r)
	}
	svcinstall.UseForTest(t, svcinstall.TestPlatform{GOOS: goos, Dir: t.TempDir(), Run: runner, User: "alice", Temp: t.TempDir()})
	spec := svcinstall.Spec{Kind: svcinstall.KindServer, Executable: "/usr/local/bin/kander",
		Args:       ServerStartArguments(r.dataDir, StartOptions{Listen: strings.TrimPrefix(r.hs.URL, "http://"), TTL: 90 * time.Second, Renew: 15 * time.Second}),
		ClientHome: home, WorkDir: r.dataDir, LogPath: filepath.Join(r.dataDir, "server.log")}
	if goos == "windows" {
		spec.Executable = `C:\kander.exe`
	}
	prepared, err := svcinstall.Prepare(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Write(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.calls = nil
	r.mu.Unlock()
	return r
}

// The executable of a detached start does not exist: a test that goes the detached way fails.
var noDetachedStart = Controller{Executable: "/nonexistent/kander", Timeout: 2 * time.Second}

func TestStartGoesThroughTheServiceManager(t *testing.T) {
	r := newServiceRig(t, "linux", nil)
	info, err := noDetachedStart.Start(t.Context(), StartOptions{DataDir: r.dataDir, Listen: "127.0.0.1:1"})
	if err != nil || info.PID != 42 {
		t.Fatalf("start: %+v %v", info, err)
	}
	if want := []string{"systemctl --user start kander-server.service"}; !slices.Equal(r.commands(), want) {
		t.Fatalf("commands %q", r.commands())
	}
	// A running service server is reused.
	if _, err := noDetachedStart.Start(t.Context(), StartOptions{}); err != nil || len(r.commands()) != 1 {
		t.Fatalf("a running server started again: %v %q", err, r.commands())
	}
}

func TestStartRefusesFlagsThatDifferFromTheService(t *testing.T) {
	r := newServiceRig(t, "linux", nil)
	for _, o := range []StartOptions{
		{DataDir: t.TempDir(), ExplicitData: true},
		{Listen: "127.0.0.1:1", ExplicitListen: true},
	} {
		_, err := noDetachedStart.Start(t.Context(), o)
		if err == nil || !strings.Contains(err.Error(), "kander service install server") {
			t.Fatalf("%+v: %v", o, err)
		}
	}
	if len(r.commands()) != 0 {
		t.Fatalf("a refused start ran %q", r.commands())
	}
	if _, err := noDetachedStart.Start(t.Context(), StartOptions{DataDir: r.dataDir, ExplicitData: true}); err != nil {
		t.Fatalf("the data directory of the service is accepted: %v", err)
	}
}

func TestStopAndRestartGoThroughTheServiceManager(t *testing.T) {
	r := newServiceRig(t, "linux", nil)
	r.ready.Store(true)
	info, err := noDetachedStart.Restart(t.Context(), "http://127.0.0.1:1")
	if err != nil || info.PID != 42 {
		t.Fatalf("restart: %+v %v", info, err)
	}
	running, err := noDetachedStart.Stop(t.Context(), "http://127.0.0.1:1")
	if err != nil || !running {
		t.Fatalf("stop: %v %v", running, err)
	}
	want := []string{"systemctl --user restart kander-server.service", "systemctl --user stop kander-server.service"}
	if !slices.Equal(r.commands(), want) || r.shutdown.Load() {
		t.Fatalf("commands %q, authenticated shutdown %v", r.commands(), r.shutdown.Load())
	}
}

func TestAnotherClientHomeOrAManagedProcessKeepsTheDirectPath(t *testing.T) {
	for _, env := range [][2]string{{"KANDER_CLIENT_HOME", "/another/client"}, {"KANDER_MANAGED", "1"}} {
		t.Run(env[0], func(t *testing.T) {
			r := newServiceRig(t, "linux", nil)
			t.Setenv(env[0], env[1])
			_, err := noDetachedStart.Start(t.Context(), StartOptions{DataDir: filepath.Join(t.TempDir(), "data"), Listen: "127.0.0.1:1", TTL: time.Minute, Renew: time.Second})
			if err == nil || len(r.commands()) != 0 {
				t.Fatalf("the detached start must be used: %v %q", err, r.commands())
			}
		})
	}
}

// On Windows the scheduled task does not stop the server: the authenticated stop does, and a
// restart starts the task again.
func TestWindowsStopsTheServiceServerThroughTheProtocol(t *testing.T) {
	var registered []byte
	r := newServiceRig(t, "windows", func(r *serviceRig) func(context.Context, string, ...string) ([]byte, error) {
		return func(ctx context.Context, name string, args ...string) ([]byte, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			switch {
			case strings.HasPrefix(line, "schtasks /Create"):
				data, err := os.ReadFile(args[4])
				registered = data
				return nil, err
			case strings.HasPrefix(line, "schtasks /Query /TN kander-server /XML"):
				if registered == nil {
					return nil, errors.New("exit status 1")
				}
				return registered, nil
			case line == "schtasks /Run /TN kander-server":
				r.ready.Store(true)
			}
			return r.run(ctx, name, args...)
		}
	})
	r.ready.Store(true)
	running, err := noDetachedStart.Stop(t.Context(), "http://127.0.0.1:1")
	if err != nil || !running || !r.shutdown.Load() {
		t.Fatalf("stop: %v %v shutdown=%v", running, err, r.shutdown.Load())
	}
	for _, c := range r.commands() {
		if strings.Contains(c, "/End") {
			t.Fatalf("the task was ended: %q", r.commands())
		}
	}
	if err := svcinstall.Stop(t.Context(), svcinstall.KindServer); !errors.Is(err, svcinstall.ErrProtocolStop) {
		t.Fatal(err)
	}
}
