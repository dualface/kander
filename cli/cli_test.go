package cli_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/cli"
	"github.com/dualface/kander/client"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/server"
)

type recordingTransport struct {
	handler http.Handler
	calls   []string
}

func (tr *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tr.calls = append(tr.calls, r.Method+" "+r.URL.Path)
	w := httptest.NewRecorder()
	tr.handler.ServeHTTP(w, r)
	return w.Result(), nil
}

func TestEmbeddedCLIAddsCommandAndRoutesNativeCallsThroughTransport(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Server.URL = "https://kander.invalid"
	cfg.Project.Name = "embedding"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	s, err := server.Open(server.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	transport := &recordingTransport{handler: s.Handler()}
	t.Cleanup(func() { client.SetDefaultTransport(nil) })
	var ran bool
	extra := &cobra.Command{Use: "host", Run: func(*cobra.Command, []string) { ran = true }}
	root, err := cli.NewRoot(cli.Options{Transport: transport, Commands: []*cobra.Command{extra}})
	if err != nil {
		t.Fatal(err)
	}
	if code := cli.Execute(root, []string{"my-kander", "host"}); code != 0 || !ran {
		t.Fatalf("added command ran = %t, exit = %d", ran, code)
	}

	root, err = cli.NewRoot(cli.Options{})
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := commandtest.Capture(func() int {
		return cli.Execute(root, []string{"my-kander", "list", "--json"})
	})
	if code != 0 {
		t.Fatalf("native list exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	want := []string{
		"GET /api/v1/version", "POST /api/v1/projects",
		"GET /api/v1/projects/" + api.ProjectID(cfg.Project.Name) + "/tasks",
	}
	if !slices.Equal(transport.calls, want) {
		t.Fatalf("injected transport calls = %v, want %v", transport.calls, want)
	}
}

func TestNewRootReportsCommandConflicts(t *testing.T) {
	for _, tc := range []struct{ use, alias, conflict string }{
		{"server", "", "server"},
		{"help", "", "help"},
		{"ls", "", "ls"},
		{"host", "start", "start"},
		{"host-extra", "", "host-extra"},
	} {
		t.Run(tc.conflict, func(t *testing.T) {
			first := &cobra.Command{Use: "host-extra"}
			second := &cobra.Command{Use: tc.use}
			if tc.alias != "" {
				second.Aliases = []string{tc.alias}
			}
			root, err := cli.NewRoot(cli.Options{Commands: []*cobra.Command{first, second}})
			if root != nil || err == nil || !strings.Contains(err.Error(), tc.conflict) ||
				first.HasParent() || second.HasParent() {
				t.Fatalf("root = %v, error = %v, extensions attached = %v / %v", root, err, first.HasParent(), second.HasParent())
			}
		})
	}
}
