package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/worker"
)

func commandService(t *testing.T, cfg *config.Config) *remote.Session {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), TTL: 20 * time.Second, RenewInterval: 200 * time.Millisecond, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	cfg.Server.URL = httpServer.URL
	cfg.Project.Name = "command-fixture"
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the production worker lock before launch. This prevents the in-process
	// CLI fixture from spawning the test binary as a detached worker.
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- worker.Serve(ctx, worker.Options{Home: session.Home, Tick: 20 * time.Millisecond, Logger: logger})
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("worker: %v", err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		held, err := remote.WorkerHeld(session.Home)
		if err != nil {
			t.Fatal(err)
		}
		if held {
			return session
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker lock was not acquired")
	return nil
}
func currentCommandSession(t *testing.T) *remote.Session {
	t.Helper()
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	return session
}
func waitCommandFact(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("service/worker fact was not observed")
}

func runKanderInput(t *testing.T, input string, args ...string) commandResult {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "input-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	before := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = before }()
	return runKander(t, args...)
}
