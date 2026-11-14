package tui

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/version"
)

func newTUIService(t *testing.T, source string) string {
	t.Helper()
	srv, err := server.Open(server.Options{Version: version.String(), DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if source != "" {
		if _, err := srv.Import("tui-fixture", "", source); err != nil {
			t.Fatal(err)
		}
	}
	connection := httptest.NewServer(srv.Handler())
	t.Cleanup(connection.Close)
	return connection.URL
}
func bindStartupService(t *testing.T) {
	t.Helper()
	// This fixture supplies the explicit service binding independently of the
	// scope config, so first-launch doctor and language checks remain exercised.
	payload, err := json.Marshal(map[string]any{"server": map[string]string{"url": newTUIService(t, "")}, "project": map[string]string{"name": "tui-fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, config.OverlayFilename), payload, 0600); err != nil {
		t.Fatal(err)
	}
}
