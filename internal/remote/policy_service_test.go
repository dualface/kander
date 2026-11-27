package remote_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

// serviceClient is one host against a real server: its own client home and scope configuration.
type serviceClient struct {
	home, config string
}

func newServiceClient(t *testing.T, serverURL string, adjust func(*config.Config)) serviceClient {
	t.Helper()
	root := t.TempDir()
	c := serviceClient{home: filepath.Join(root, "client"), config: filepath.Join(root, "config.json")}
	c.use(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	cfg.Server.URL = serverURL
	cfg.Project.Name = "golf"
	if adjust != nil {
		adjust(cfg)
	}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c serviceClient) use(t *testing.T) {
	t.Setenv(remote.EnvClientHome, c.home)
	t.Setenv(config.EnvConfig, c.config)
}

func runService(build func() *cobra.Command, args ...string) (int, string, string) {
	return commandtest.Capture(func() int { return commandtest.Run(build(), args...) })
}

func serviceList() *cobra.Command { return remote.NewListCommand(nil) }

// policyServer is a real server in this process on a fixed loopback address, so a restart
// keeps the URL and the cache key of the clients.
type policyServer struct {
	t      *testing.T
	data   string
	addr   string
	srv    *server.Server
	http   *httptest.Server
	wrap   func(http.Handler) http.Handler
	closed bool
}

func startPolicyServer(t *testing.T, wrap func(http.Handler) http.Handler) *policyServer {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &policyServer{t: t, data: t.TempDir(), addr: listener.Addr().String(), wrap: wrap}
	s.start(listener)
	t.Cleanup(s.stop)
	return s
}

func (s *policyServer) start(listener net.Listener) {
	s.t.Helper()
	srv, err := server.Open(server.Options{DataDir: s.data, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		s.t.Fatal(err)
	}
	var handler http.Handler = srv.Handler()
	if s.wrap != nil {
		handler = s.wrap(handler)
	}
	s.srv = srv
	s.http = &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	s.http.Start()
	s.closed = false
}

func (s *policyServer) stop() {
	if s.closed {
		return
	}
	s.http.Close()
	_ = s.srv.Close()
	s.closed = true
}

func (s *policyServer) restart() {
	s.t.Helper()
	s.stop()
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.t.Skip("the loopback port could not be bound again:", err)
	}
	s.start(listener)
}

func (s *policyServer) url() string { return "http://" + s.addr }

// loadedConfig reads the configuration the way a later command does, which starts without the
// policy answers of the commands before it.
func loadedConfig(t *testing.T) *config.Config {
	t.Helper()
	config.ForgetServerPolicies()
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func hasNotice(cfg *config.Config, code string) bool {
	for _, n := range cfg.Policy.Notices {
		if n.Code == code {
			return true
		}
	}
	return false
}

func openSession(t *testing.T) *remote.Session {
	t.Helper()
	cfg := loadedConfig(t)
	dir, _ := os.Getwd()
	session, err := remote.OpenWith(t.Context(), cfg, dir, remote.OpenOptions{QuietPolicy: true})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// The scenario of the card: two clients with different local review stages share one policy.
func TestPolicyIsSharedByTwoClients(t *testing.T) {
	srv := startPolicyServer(t, nil)
	a := newServiceClient(t, srv.url(), func(cfg *config.Config) { cfg.ReviewStages["large"]["PMQA"] = "required" })
	b := newServiceClient(t, srv.url(), func(cfg *config.Config) { cfg.ReviewStages["large"]["PMQA"] = "skip" })

	// 1. Unset: each client keeps its own values and is told so.
	for _, c := range []struct {
		client serviceClient
		mode   string
	}{{a, "required"}, {b, "skip"}} {
		c.client.use(t)
		code, _, errOut := runService(serviceList)
		if code != 0 || !strings.Contains(errOut, "kander policy init") {
			t.Fatalf("list: code=%d err=%q", code, errOut)
		}
		cfg := loadedConfig(t)
		if cfg.ReviewStages["large"]["PMQA"] != c.mode || cfg.Policy.Status().Source != config.PolicySourceLocal || !hasNotice(cfg, config.NoticePolicyUnset) {
			t.Fatalf("unset client: stages %v policy %+v", cfg.ReviewStages, cfg.Policy)
		}
	}

	// 2. A uploads its values; B takes them with its next command that opens the project.
	a.use(t)
	if code, out, errOut := runService(remote.NewPolicyCommand, "init", "--yes"); code != 0 || !strings.Contains(out, "revision 1") {
		t.Fatalf("init: code=%d out=%q err=%q", code, out, errOut)
	}
	b.use(t)
	if code, _, errOut := runService(serviceList); code != 0 || strings.Contains(errOut, "kander policy init") {
		t.Fatalf("list after init: code=%d err=%q", code, errOut)
	}
	cfg := loadedConfig(t)
	if cfg.ReviewStages["large"]["PMQA"] != "required" || cfg.Policy.Status().Revision != 1 || cfg.Policy.Status().Source != config.PolicySourceServerCache {
		t.Fatalf("B after A's init: stages %v policy %+v", cfg.ReviewStages, cfg.Policy.Status())
	}
	if !hasNotice(cfg, config.NoticePolicyOverride) {
		t.Fatalf("B's replaced local value is not listed: %+v", cfg.Policy.Notices)
	}
	data, err := os.ReadFile(b.config)
	if err != nil || !strings.Contains(string(data), `"skip"`) {
		t.Fatalf("B's config.json changed: %s %v", data, err)
	}

	// 3. Two writers that read the same revision: one wins, the other gets the conflict.
	writeFrom := func(s *remote.Session, read api.ProjectPolicy, mode string) error {
		var values map[string]any
		if err := json.Unmarshal(read.Values, &values); err != nil {
			t.Fatal(err)
		}
		values["git"] = map[string]any{"branch_mode": mode}
		raw, _ := json.Marshal(values)
		_, err := s.Project.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: read.Revision, Values: raw})
		return err
	}
	var sessions []*remote.Session
	var reads []api.ProjectPolicy
	for _, c := range []serviceClient{a, b} {
		c.use(t)
		s := openSession(t)
		read, err := s.Project.Policy(t.Context())
		if err != nil || read.Revision != 1 {
			t.Fatalf("read %+v %v", read, err)
		}
		sessions, reads = append(sessions, s), append(reads, read)
	}
	if err := writeFrom(sessions[0], reads[0], "main-only"); err != nil {
		t.Fatal(err)
	}
	if err := writeFrom(sessions[1], reads[1], "main-develop"); !api.IsCode(err, api.CodePolicyRevisionConflict) {
		t.Fatalf("the second writer got %v", err)
	}

	// 4. A restart keeps the policy.
	a.use(t)
	_, before, _ := runService(remote.NewPolicyCommand, "show", "--json")
	srv.restart()
	code, after, errOut := runService(remote.NewPolicyCommand, "show", "--json")
	if code != 0 || after != before || !strings.Contains(after, `"revision":2`) {
		t.Fatalf("show after restart: code=%d before=%q after=%q err=%q", code, before, after, errOut)
	}
}

func TestOpenAppliesTheRegistrationPolicyWithoutAnotherRequest(t *testing.T) {
	var policyRequests atomic.Int32
	srv := startPolicyServer(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/policy") && r.Method == http.MethodGet {
				policyRequests.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	newServiceClient(t, srv.url(), func(cfg *config.Config) { cfg.Git.BranchMode = "main-only" })
	if code, _, errOut := runService(remote.NewPolicyCommand, "init", "--yes"); code != 0 {
		t.Fatalf("init: %s", errOut)
	}
	newServiceClient(t, srv.url(), func(cfg *config.Config) { cfg.Git.BranchMode = "main-develop" })
	cfg := loadedConfig(t)
	if cfg.Git.BranchMode != "main-develop" {
		t.Fatalf("a client without a cache used %q", cfg.Git.BranchMode)
	}
	before := policyRequests.Load()
	dir, _ := os.Getwd()
	session, err := remote.Open(t.Context(), cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Git.BranchMode != "main-only" || cfg.Policy.Source != config.PolicySourceServer || cfg.Policy.Revision != 1 {
		t.Fatalf("Open did not apply the policy: %q %+v", cfg.Git.BranchMode, cfg.Policy)
	}
	if policyRequests.Load() != before {
		t.Fatal("Open sent a separate policy request")
	}
	cache := config.ReadPolicyCache(session.Home, session.ServerURL, session.Info.ID)
	if cache.Status != config.PolicyCacheSet || cache.Snapshot.Revision != 1 {
		t.Fatalf("cache %+v", cache)
	}
	// The worker reads the same cache through an execution credential's project ID.
	if got := remote.CredentialPolicy(session.Home, remote.Credential{ServerURL: session.ServerURL, ProjectID: session.Info.ID}); got.Status != config.PolicyCacheSet {
		t.Fatalf("credential cache %+v", got)
	}
}
