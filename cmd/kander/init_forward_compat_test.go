package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

// kander init writes server.url and project.name into the project overlay; keys of a newer
// version that the overlay already holds, at the top level and inside server and project,
// must still be there afterwards.
func TestInitKeepsUnknownOverlayKeys(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	if _, err := config.Save(config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	dir := t.TempDir()
	t.Chdir(dir)
	overlayPath := filepath.Join(dir, config.OverlayFilename)
	seed := `{"future_top":{"x":1},"server":{"token":"t"},"project":{"extra":true}}`
	if err := os.WriteFile(overlayPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	runKander(t, "init", "--server", httpServer.URL, "--project", "keep-unknown").require(t, "bind with unknown overlay keys")
	raw, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	var overlay map[string]any
	if err := json.Unmarshal(raw, &overlay); err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{{"future_top", "x"}, {"server", "token"}, {"project", "extra"}} {
		if _, ok := config.OverlayGet(overlay, path...); !ok {
			t.Fatalf("init dropped %v from the overlay: %s", path, raw)
		}
	}
	if url, _ := config.OverlayGet(overlay, "server", "url"); url != httpServer.URL {
		t.Fatalf("server.url not written: %s", raw)
	}
	if name, _ := config.OverlayGet(overlay, "project", "name"); name != "keep-unknown" {
		t.Fatalf("project.name not written: %s", raw)
	}
}
