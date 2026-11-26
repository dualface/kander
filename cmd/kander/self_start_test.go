package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/servicectl"
)

// serverProcesses counts the live processes whose command line names dir as
// the data directory of a server. It is Linux-only: /proc is the only portable
// process list the test can read without a helper.
func serverProcesses(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(string(data), "\x00")
		for i, arg := range args {
			if arg == "--data" && i+1 < len(args) && args[i+1] == dir && i > 0 && args[1] == "server" {
				count++
			}
		}
	}
	return count
}

func TestConcurrentSelfStart(t *testing.T) {
	ctl, address, target := serverCommandEnv(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(remote.EnvExecutionID, "")
	_ = os.Unsetenv(remote.EnvExecutionID)
	dir, err := servicectl.DataDir("")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Server.URL = target
	cfg.Project.Name = "selfstart"
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ctl.Stop(ctx, target)
	}
	t.Cleanup(stop)

	// Bind the project once, so its board exists in the default data directory.
	restore := remote.UseServerStarterForTest(ctl.Start)
	defer restore()
	if _, err := ctl.Start(t.Context(), servicectl.StartOptions{DataDir: dir, Listen: address, TTL: api.DefaultTTL, Renew: api.DefaultRenewInterval}); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Open(t.Context(), cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	stop()

	// Two clients now open a session against the stopped server at once.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, errs[i] = remote.Open(ctx, cfg, t.TempDir())
		})
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent self-start: %v %v", errs[0], errs[1])
	}
	info, err := servicectl.Info(t.Context(), target)
	if err != nil || info.DataDir != dir {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if runtime.GOOS != "linux" {
		return
	}
	// The losing starter kills and reaps its own child before it returns.
	deadline := time.Now().Add(5 * time.Second)
	for serverProcesses(t, dir) != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("%d server processes serve %s, want 1", serverProcesses(t, dir), dir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
