package worker

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

// The start of an order runs with the worker's environment, without the
// execution variables, with the scanned PATH and agent configuration
// directories, the worker's client home, and the order.
func TestStartEnvironmentOfAnOrder(t *testing.T) {
	t.Setenv(process.EnvExecutionID, "exec-1")
	t.Setenv(process.EnvCoordinatorExecutionID, "exec-2")
	t.Setenv(process.EnvManaged, "1")
	t.Setenv("PATH", "/worker/bin")
	t.Setenv("KANDER_KEEP", "kept")
	record := remote.HostRecord{Scan: &remote.HostScan{Path: "/scanned/bin"}}
	scan := remote.ProjectScan{Agents: []remote.AgentScan{{Name: "claude", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/accounts/two"}}}}
	env := startEnvironment("/client/home", "so-1", record, scan)
	for _, want := range []string{"PATH=/scanned/bin", "CLAUDE_CONFIG_DIR=/accounts/two", remote.EnvClientHome + "=/client/home", process.EnvStartOrder + "=so-1", "KANDER_KEEP=kept"} {
		if !slices.Contains(env, want) {
			t.Fatalf("%s missing from %v", want, env)
		}
	}
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if name == process.EnvExecutionID || name == process.EnvCoordinatorExecutionID || name == process.EnvManaged || entry == "PATH=/worker/bin" {
			t.Fatalf("%s reached the start", entry)
		}
	}
	if got := startArguments(api.StartOrder{TaskID: "20261128-x-task", Agent: "codex", Launcher: "tmux-session"}); !slices.Equal(got, []string{"start", "--json", "--agent", "codex", "--launcher", "tmux-session", "20261128-x-task"}) {
		t.Fatalf("arguments = %v", got)
	}
}

func TestChildFailureTakesTheJSONError(t *testing.T) {
	exit := exec.Command("false").Run()
	code, message := childFailure([]byte("note\n{\"error\":{\"code\":\"held_by_other\",\"message\":\"task is already claimed\"}}\n"), exit)
	if code != "held_by_other" || message != "task is already claimed" {
		t.Fatalf("code=%q message=%q", code, message)
	}
	if code, message := childFailure([]byte("panic: boom\n"), exit); code != "start_failed" || message != "panic: boom" {
		t.Fatalf("plain failure code=%q message=%q", code, message)
	}
	if code, _ := childFailure(nil, errors.New("killed")); code != "start_not_finished" {
		t.Fatalf("unfinished code=%q", code)
	}
	if code, _ := childFailure(nil, nil); code != "" {
		t.Fatalf("a successful start reports %q", code)
	}
}

// A registration is reported only while it still is the checkout of the
// project: a Git checkout bound to the server and project, whose origin is the
// remote the server registered.
func TestCheckRegistration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	h := httptest.NewServer(srv.Handler())
	defer h.Close()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	if _, err := config.Save(config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	c, err := client.New(h.URL, "c-0123456789ab-host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Register(t.Context(), "app", "https://example.invalid/app.git"); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "app")
	for _, args := range [][]string{{"init", "-q", repo}, {"-C", repo, "remote", "add", "origin", "https://example.invalid/app.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	overlay := `{"server":{"url":"` + h.URL + `"},"project":{"name":"app"}}`
	if err := os.WriteFile(filepath.Join(repo, config.OverlayFilename), []byte(overlay), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := remote.ProjectRegistration{ServerURL: h.URL, Project: "app", Root: repo, RegisteredAt: time.Now()}
	if err := checkRegistration(t.Context(), c, reg); err != nil {
		t.Fatalf("a valid registration: %v", err)
	}
	for name, change := range map[string]func(remote.ProjectRegistration) remote.ProjectRegistration{
		"missing directory": func(r remote.ProjectRegistration) remote.ProjectRegistration { r.Root += "-gone"; return r },
		"other project":     func(r remote.ProjectRegistration) remote.ProjectRegistration { r.Project = "other"; return r },
		"not a checkout":    func(r remote.ProjectRegistration) remote.ProjectRegistration { r.Root = t.TempDir(); return r },
	} {
		if err := checkRegistration(t.Context(), c, change(reg)); err == nil {
			t.Fatalf("%s passed the check", name)
		}
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "set-url", "origin", "https://example.invalid/fork.git").CombinedOutput(); err != nil {
		t.Fatalf("git: %v\n%s", err, out)
	}
	var mismatch errRegistration
	if err := checkRegistration(t.Context(), c, reg); !errors.As(err, &mismatch) {
		t.Fatalf("another origin: %v", err)
	}
}
