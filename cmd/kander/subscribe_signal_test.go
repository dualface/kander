//go:build !windows

package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// A termination signal ends `kander subscribe` with status 0 and the lines already written, with
// and without --follow: the default single-event mode must not change what the signal does.
func TestSubscribeSignalExitsZeroInBothModes(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	cfg := config.DefaultConfig()
	cfg.Language, cfg.AgentLanguage = "en", "en"
	session, _ := commandServiceWithServer(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "subscribe-signal", Title: "Subscribe signal", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	// The test process keeps a handler of its own, so a signal that arrives before the command
	// installed its handler does not end the test binary.
	sink := make(chan os.Signal, 64)
	signal.Notify(sink, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(sink) })
	run := func(args ...string) ([]string, commandResult) {
		finished := make(chan struct{})
		go func() {
			// Long enough for the first snapshot on a loaded machine; the signal repeats until the
			// command has ended, since the command installs its handler after the test starts it.
			time.Sleep(3 * time.Second)
			for {
				_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
				select {
				case <-finished:
					return
				case <-time.After(500 * time.Millisecond):
				}
			}
		}()
		result := runKander(t, append([]string{"subscribe", "--json", "--refresh", "1"}, append(args, "--watch", task.TaskID)...)...)
		close(finished)
		if result.code != 0 || result.stderr != "" {
			t.Fatalf("%v: exit=%d\nstdout=%s\nstderr=%s", args, result.code, result.stdout, result.stderr)
		}
		var names []string
		for _, line := range jsonStreamLines(t, result.stdout) {
			names = append(names, line["event"].(string))
		}
		return names, result
	}
	followed, _ := run("--follow")
	single, _ := run()
	if len(single) == 0 || single[0] != "snapshot" || !reflect.DeepEqual(followed, single) {
		t.Fatalf("follow=%v single=%v", followed, single)
	}
}
