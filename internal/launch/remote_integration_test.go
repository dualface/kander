package launch

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestIntegrationCommandValidatesInputWithoutLocalBoard(t *testing.T) {
	setupBoard(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { hs.Close(); _ = srv.Close() })
	cfg := envConfig("claude", "tmux", nil)
	cfg.Server.URL, cfg.Project.Name = hs.URL, "integration-input"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
		code int
	}{
		{"invalid-utf8", []byte{0xff}, 2},
		{"oversized", make([]byte, 65537), 2},
		{"unknown-field", []byte(`{"unknown":true}`), 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(cwd, test.name+".json")
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			code, out := captureStdout(t, func() int { return RunCoordinator([]string{"integrate", path}) })
			if code != test.code || len(out) != 0 {
				t.Fatalf("code=%d output=%s", code, out)
			}
		})
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("integration command touched local board: %v", err)
	}
}
func TestIntegrationCommandRequiresConfiguredService(t *testing.T) {
	setupBoard(t)
	cfg := envConfig("claude", "tmux", nil)
	if code := runRemoteIntegration(context.Background(), cfg, []string{"integrate", filepath.Join(t.TempDir(), "input.json")}); code != 1 {
		t.Fatalf("unconfigured integration code=%d", code)
	}
}
