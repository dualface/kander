package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
)

// uninstallEnvironment isolates the client home, the configuration, and the server data
// directory, and points the configured server URL at serverURL.
func uninstallEnvironment(t *testing.T, serverURL string) (home, dataDir string) {
	t.Helper()
	root := t.TempDir()
	home, dataDir = filepath.Join(root, "client"), filepath.Join(root, "data", "kander-server")
	t.Setenv(EnvClientHome, home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv(config.EnvConfig, filepath.Join(root, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	cfg.Server.URL = serverURL
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return home, dataDir
}

// infoServer answers the server information route of a server that reports dataDir.
func infoServer(t *testing.T, dataDir string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/server" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: r.URL.Path})
			return
		}
		_ = json.NewEncoder(w).Encode(api.ServerInfo{PID: 4242, DataDir: dataDir})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestInspectUninstallBackgroundFindsServerWorkerAndLeases(t *testing.T) {
	// The URL is set once the server exists; the data directory it reports is the isolated one.
	root := t.TempDir()
	served := filepath.Join(root, "served")
	server := infoServer(t, served)
	home, dataDir := uninstallEnvironment(t, server.URL)
	holdWorkerLock(t, home)
	if err := WriteWorkerRecord(home, WorkerRecord{Version: "2.0.0", PID: 777, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	live := Credential{ServerURL: server.URL, ClientID: "c-host", ProjectID: "p-1", TaskID: "20261127-live-task", ExecutionID: "x-live", Generation: 3, Token: "t", AgentPID: 55}
	lost := Credential{ServerURL: server.URL, ClientID: "c-host", ProjectID: "p-1", TaskID: "20261127-lost-task", ExecutionID: "x-lost", Generation: 1, Token: "t", Lost: true}
	for _, c := range []Credential{live, lost} {
		if err := WriteCredential(home, c); err != nil {
			t.Fatal(err)
		}
	}

	found := InspectUninstallBackground(t.Context())
	if found.Err != nil {
		t.Fatal(found.Err)
	}
	if found.ClientHome != home || found.ServerDataDir != dataDir {
		t.Fatalf("home=%q data=%q", found.ClientHome, found.ServerDataDir)
	}
	if len(found.Processes) != 2 {
		t.Fatalf("processes=%+v", found.Processes)
	}
	srv, worker := found.Processes[0], found.Processes[1]
	// The served directory holds no administration key, so the server is reported, not stopped.
	if srv.Kind != install.UninstallServer || srv.PID != 4242 || srv.Where != server.URL || srv.Stop != nil || srv.Reason == "" {
		t.Fatalf("server=%+v", srv)
	}
	if worker.Kind != install.UninstallWorker || worker.PID != 777 || worker.Where != home || worker.Stop == nil {
		t.Fatalf("worker=%+v", worker)
	}
	want := install.UninstallLease{ProjectID: "p-1", TaskID: live.TaskID, Kind: "task", Generation: 3, AgentPID: 55}
	if len(found.Leases) != 1 || found.Leases[0] != want {
		t.Fatalf("leases=%+v", found.Leases)
	}

	// With the key readable the server can be stopped.
	key, _ := json.Marshal(map[string]string{"token": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	if err := os.MkdirAll(served, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(served, api.AdminTokenFile), key, 0o600); err != nil {
		t.Fatal(err)
	}
	if found := InspectUninstallBackground(t.Context()); found.Processes[0].Stop == nil {
		t.Fatalf("server with a readable key cannot be stopped: %+v", found.Processes[0])
	}

	// A worker that keeps its lock past the wait is a failure that names the lock file.
	workerStopTimeout = 50 * time.Millisecond
	t.Cleanup(func() { workerStopTimeout = 30 * time.Second })
	err := worker.Stop(t.Context())
	if err == nil || !strings.Contains(err.Error(), filepath.Join(home, WorkerLockName)) {
		t.Fatalf("timeout error=%v", err)
	}
}

func TestInspectUninstallBackgroundWithNothingRunning(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	uninstallEnvironment(t, url)
	found := InspectUninstallBackground(t.Context())
	if found.Err != nil || len(found.Processes) != 0 || len(found.Leases) != 0 {
		t.Fatalf("found=%+v", found)
	}
}
