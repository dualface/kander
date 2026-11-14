package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/servicectl"
	"github.com/dualface/kander/internal/version"
)

func serverGateFixture(t *testing.T) (*config.Config, *atomic.Bool, *api.VersionInfo, *bytes.Buffer) {
	t.Helper()
	t.Setenv(EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(EnvExecutionID, "")
	_ = os.Unsetenv(EnvExecutionID)
	t.Setenv(EnvCoordinatorExecutionID, "")
	_ = os.Unsetenv(EnvCoordinatorExecutionID)
	oldIn, oldTerm, oldOut := serverPromptInput, serverTerminal, stderr
	oldStart, oldRestart := startLocalServer, restartLocalServer
	serverVersionChecks.Clear()
	t.Cleanup(func() {
		serverPromptInput = oldIn
		serverTerminal = oldTerm
		stderr = oldOut
		startLocalServer = oldStart
		restartLocalServer = oldRestart
		serverVersionChecks.Clear()
	})
	serverTerminal = func() bool { return true }
	var out bytes.Buffer
	stderr = &out
	var available atomic.Bool
	v := api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: version.String()}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		switch r.URL.Path {
		case "/api/v1/version":
			_ = json.NewEncoder(w).Encode(v)
		case "/api/v1/projects":
			_ = json.NewEncoder(w).Encode(api.Project{ID: api.ProjectID("gate"), Name: "gate"})
		default:
			if strings.HasSuffix(r.URL.Path, "/tasks") {
				_ = json.NewEncoder(w).Encode(api.TaskList{})
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hs.Close)
	cfg := config.DefaultConfig()
	cfg.Server.URL = hs.URL
	cfg.Project.Name = "gate"
	startLocalServer = func(ctx context.Context, o servicectl.StartOptions) (api.ServerInfo, error) {
		if ctx.Err() != nil {
			t.Fatal("startup inherited expired context")
		}
		expected, _ := servicectl.DataDir("")
		if o.DataDir != expected || o.Listen != servicectl.ListenAddress(hs.URL) {
			t.Fatalf("startup=%+v", o)
		}
		available.Store(true)
		return api.ServerInfo{}, nil
	}
	restartLocalServer = func(ctx context.Context, target string) (api.ServerInfo, error) {
		if ctx.Err() != nil || target != hs.URL {
			t.Fatal("invalid restart context or target")
		}
		v.ServerVersion = version.String()
		v.Protocol = api.ProtocolVersion
		v.MinProtocol = api.MinProtocolVersion
		return api.ServerInfo{}, nil
	}
	return cfg, &available, &v, &out
}

func gateClient(t *testing.T, cfg *config.Config) *client.Client {
	t.Helper()
	c, err := client.New(config.ServerURL(cfg), "gate-test")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	return c
}

func TestLocalServerStartPromptDecisions(t *testing.T) {
	for _, input := range []string{"\n", "y\n", "Y\n", "n\n", "N\n", "", "y"} {
		t.Run(strings.ReplaceAll(input, "\n", "ENTER"), func(t *testing.T) {
			cfg, available, _, out := serverGateFixture(t)
			serverPromptInput = strings.NewReader(input)
			_, err := prepareServer(t.Context(), gateClient(t, cfg), cfg)
			want := input == "\n" || input == "y\n" || input == "Y\n"
			if available.Load() != want || (err == nil) != want {
				t.Fatalf("input=%q available=%v err=%v", input, available.Load(), err)
			}
			dir, _ := servicectl.DataDir("")
			for _, part := range []string{cfg.Server.URL, dir, filepath.Join(dir, "projects", api.ProjectDirName("gate"), "board"), filepath.Join(dir, "server.log"), "[Y/n]"} {
				if !strings.Contains(out.String(), part) {
					t.Fatalf("prompt missing %q: %s", part, out.String())
				}
			}
		})
	}
}

func TestNoninteractiveNeverStartsLocalServer(t *testing.T) {
	for _, mode := range []string{"pipe", EnvExecutionID, EnvCoordinatorExecutionID} {
		t.Run(mode, func(t *testing.T) {
			cfg, available, _, out := serverGateFixture(t)
			if mode == "pipe" {
				serverTerminal = func() bool { return false }
			} else {
				t.Setenv(mode, "")
			}
			_, err := prepareServer(t.Context(), gateClient(t, cfg), cfg)
			dir, _ := servicectl.DataDir("")
			if err == nil || !strings.Contains(err.Error(), "kander server start") || !strings.Contains(err.Error(), dir) || available.Load() || out.Len() != 0 {
				t.Fatalf("noninteractive=%v %q", err, out.String())
			}
		})
	}
}

func TestNonloopbackUnreachableDoesNotPrompt(t *testing.T) {
	cfg, _, _, out := serverGateFixture(t)
	cfg.Server.URL = "http://example.invalid"
	c, _ := client.New(cfg.Server.URL, "remote-test")
	c.Retries = 0
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := prepareServer(ctx, c, cfg)
	if err == nil || out.Len() != 0 {
		t.Fatalf("remote=%v %s", err, out.String())
	}
}

func TestLocalServerVersionHandling(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		for _, incompatible := range []bool{false, true} {
			for _, input := range []string{"y\n", "n\n"} {
				t.Run(strings.Join([]string{map[bool]string{true: "tty", false: "pipe"}[interactive], map[bool]string{true: "incompatible", false: "compatible"}[incompatible], input}, "-"), func(t *testing.T) {
					cfg, available, v, out := serverGateFixture(t)
					available.Store(true)
					v.ServerVersion = "old-version"
					if incompatible {
						v.MinProtocol = api.ProtocolVersion + 1
						v.Protocol = api.ProtocolVersion + 1
					}
					serverTerminal = func() bool { return interactive }
					serverPromptInput = strings.NewReader(input)
					_, err := prepareServer(t.Context(), gateClient(t, cfg), cfg)
					restarted := interactive && input == "y\n"
					if (err != nil) != (incompatible && !restarted) {
						t.Fatalf("version=%v", err)
					}
					if (v.ServerVersion == version.String()) != restarted {
						t.Fatalf("restart=%+v", v)
					}
					text := out.String()
					if err != nil {
						text += err.Error()
					}
					if !strings.Contains(text, "old-version") || !strings.Contains(text, version.String()) {
						t.Fatalf("missing versions: %q", text)
					}
					if !interactive && incompatible && !strings.Contains(text, "kander server restart") {
						t.Fatal(text)
					}
					if !interactive && !incompatible {
						out.Reset()
						_, _ = prepareServer(t.Context(), gateClient(t, cfg), cfg)
						if out.Len() != 0 {
							t.Fatal("repeated warning")
						}
					}
				})
			}
		}
	}
}

type delayedServerAnswer struct{ cancel context.CancelFunc }

func (r delayedServerAnswer) Read(p []byte) (int, error) { r.cancel(); copy(p, "y\n"); return 2, nil }

func TestServerPromptOutlivesCallerDeadline(t *testing.T) {
	cfg, _, _, _ := serverGateFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	serverPromptInput = delayedServerAnswer{cancel: cancel}
	// Cancellation during the question models spending more than 15 seconds
	// there; neither startup nor the subsequent handshake uses that deadline.
	prompted, err := prepareServer(ctx, gateClient(t, cfg), cfg)
	if err != nil || !prompted || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("prompt=%v %v", prompted, err)
	}
}

func TestDefaultServerURLDoesNotEnableOptionalStorage(t *testing.T) {
	cfg, available, _, _ := serverGateFixture(t)
	available.Store(true)
	target := cfg.Server.URL
	cfg.Server.URL = ""
	old := config.DefaultServerURL
	config.DefaultServerURL = target
	t.Cleanup(func() { config.DefaultServerURL = old })
	if Enabled(cfg) || !ServiceSelected(cfg, true) {
		t.Fatal("default URL changed optional storage selection")
	}
	if s, err := Open(t.Context(), cfg, t.TempDir()); err != nil || s.ServerURL != target {
		t.Fatalf("default session=%+v %v", s, err)
	}
	if err := RequireBinding(&config.Config{}); err == nil || !strings.Contains(err.Error(), "kander init --project <name>") {
		t.Fatalf("binding=%v", err)
	}
}

// deadlineServerAnswer consumes the real command deadline at its question.
type deadlineServerAnswer struct{ ctx context.Context }

func (r deadlineServerAnswer) Read(p []byte) (int, error) { <-r.ctx.Done(); return copy(p, "y\n"), nil }

func TestListAfterStartupOrRestartQuestionRenewsOperationBudget(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "startup", true: "restart"}[restart], func(t *testing.T) {
			cfg, available, v, _ := serverGateFixture(t)
			t.Chdir(t.TempDir())
			t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
			cfg.Language = "en"
			if _, err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			if restart {
				available.Store(true)
				v.ServerVersion = "old-version"
			}
			oldContext, oldOutput := commandContext, stdout
			var output bytes.Buffer
			stdout = &output
			t.Cleanup(func() { commandContext = oldContext; stdout = oldOutput })
			var original context.Context
			commandContext = func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				original = ctx
				serverPromptInput = deadlineServerAnswer{ctx: ctx}
				return ctx, cancel
			}
			if code := RunList([]string{"--json"}); code != 0 || output.Len() == 0 {
				t.Fatalf("list after question exit=%d output=%q", code, output.String())
			}
			if !errors.Is(original.Err(), context.DeadlineExceeded) {
				t.Fatal("question did not consume original budget")
			}
		})
	}
}

func TestRenewedOperationStillHonorsExplicitCancellation(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), time.Minute)
	s := &Session{afterQuestion: true, operationBudget: time.Minute}
	ctx, done := s.OperationContext(parent)
	defer done()
	cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal(ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("explicit cancellation was lost")
	}
}
