package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/servicectl"
)

// Re-enter the real command implementation in a child process. The child is
// detached by Controller just like the distributed executable.
func TestMain(m *testing.M) {
	if os.Getenv("KANDER_TEST_SERVER_CHILD") == "1" && len(os.Args) > 2 && os.Args[1] == "server" {
		os.Exit(runServer(os.Args[2:]))
	}
	if os.Getenv("KANDER_TEST_SERVER_CHILD") == "hang" && len(os.Args) > 2 && os.Args[1] == "server" {
		f := flag.NewFlagSet("child", flag.ExitOnError)
		dir := f.String("data", "", "")
		f.Bool("foreground", false, "")
		f.String("listen", "", "")
		f.String("ttl", "", "")
		f.String("renew", "", "")
		_ = f.Parse(os.Args[3:])
		_ = os.WriteFile(filepath.Join(*dir, "child.pid"), []byte(fmt.Sprint(os.Getpid())), 0600)
		fmt.Fprintln(os.Stdout, "stdout-marker")
		for i := range 25 {
			fmt.Fprintf(os.Stderr, "timeout-line-%02d\n", i)
		}
		time.Sleep(time.Hour)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func serverCommandEnv(t *testing.T) (servicectl.Controller, string, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("KANDER_TEST_SERVER_CHILD", "1")
	cfg := config.DefaultConfig()
	cfg.Server.URL = "http://127.0.0.1:1"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return servicectl.Controller{Executable: exe}, address, "http://" + address
}

func TestServerBackgroundLifecycle(t *testing.T) {
	ctl, address, target := serverCommandEnv(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "server.log")
	if err := os.WriteFile(logPath, []byte("existing-log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	o := servicectl.StartOptions{DataDir: dir, Listen: address, TTL: 123 * time.Second, Renew: 17 * time.Second}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ctl.Stop(ctx, target)
	})
	info, err := ctl.Start(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if info.DataDir != dir || info.LogPath != logPath || info.PID == os.Getpid() || info.Listen != address || info.TTL.Std() != o.TTL || info.Renew.Std() != o.Renew {
		t.Fatalf("info=%+v", info)
	}
	again, err := ctl.Start(t.Context(), o)
	if err != nil || again.PID != info.PID {
		t.Fatalf("second start=%+v %v", again, err)
	}
	cfg := config.DefaultConfig()
	cfg.Server.URL = target
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	withServerOutput(t, &output, func() {
		if code := runServer([]string{"status", "--json"}); code != 0 {
			t.Fatalf("status=%d", code)
		}
	})
	var status api.ServerInfo
	if err := json.Unmarshal(output.Bytes(), &status); err != nil || status.PID != info.PID || status.DataDir != dir || status.URL != target {
		t.Fatalf("status=%s err=%v", output.String(), err)
	}
	restarted, err := ctl.Restart(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.PID == info.PID || restarted.DataDir != dir || restarted.Listen != address || restarted.TTL != info.TTL || restarted.Renew != info.Renew {
		t.Fatalf("restart=%+v", restarted)
	}
	if running, err := ctl.Stop(t.Context(), target); err != nil || !running {
		t.Fatalf("stop=%v %v", running, err)
	}
	if running, err := ctl.Stop(t.Context(), target); err != nil || running {
		t.Fatalf("second stop=%v %v", running, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil || !bytes.HasPrefix(log, []byte("existing-log\n")) || !bytes.Contains(log, []byte(`"msg":"listening"`)) {
		t.Fatalf("log=%s err=%v", log, err)
	}
	if code := runServer([]string{"status"}); code == 0 {
		t.Fatal("stopped server reported running")
	}
}

func withServerOutput(t *testing.T, b *bytes.Buffer, run func()) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old; r.Close() }()
	run()
	w.Close()
	_, _ = b.ReadFrom(r)
}

func TestConcurrentServerStart(t *testing.T) {
	ctl, address, target := serverCommandEnv(t)
	dir := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ctl.Stop(ctx, target)
	})
	var wg sync.WaitGroup
	infos := make([]api.ServerInfo, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			infos[i], errs[i] = ctl.Start(t.Context(), servicectl.StartOptions{DataDir: dir, Listen: address, TTL: api.DefaultTTL, Renew: api.DefaultRenewInterval})
		})
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || infos[0].PID != infos[1].PID {
		t.Fatalf("concurrent=%+v %v", infos, errs)
	}
}

func TestServerStartTimeoutShowsLast20Lines(t *testing.T) {
	ctl, address, _ := serverCommandEnv(t)
	t.Setenv("KANDER_TEST_SERVER_CHILD", "hang")
	ctl.Timeout = 2 * time.Second
	dir := t.TempDir()
	_, err := ctl.Start(t.Context(), servicectl.StartOptions{DataDir: dir, Listen: address})
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "server.log")) || !strings.Contains(err.Error(), "timeout-line-24") || strings.Contains(err.Error(), "timeout-line-04") {
		t.Fatalf("timeout=%v", err)
	}
	log, readErr := os.ReadFile(filepath.Join(dir, "server.log"))
	if readErr != nil || !bytes.Contains(log, []byte("stdout-marker")) || !bytes.Contains(log, []byte("timeout-line-24")) {
		t.Fatalf("stdout/stderr log=%s %v", log, readErr)
	}
	// Start waits for its killed child, so the helper cannot append after return.
	if _, err := os.ReadFile(filepath.Join(dir, "child.pid")); err != nil {
		t.Fatal(err)
	}
}

func TestServerForegroundFlagsAndDefaults(t *testing.T) {
	_, _, _ = serverCommandEnv(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := config.DefaultConfig()
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	original := runForegroundServer
	defer func() { runForegroundServer = original }()
	var got server.Options
	var listen string
	runForegroundServer = func(_ context.Context, o server.Options, address string) error { got = o; listen = address; return nil }
	if code := runServer([]string{"start", "--foreground"}); code != 0 {
		t.Fatalf("default=%d", code)
	}
	if listen != server.DefaultListen || got.DataDir != filepath.Join(os.Getenv("XDG_DATA_HOME"), "kander-server") || got.TTL != server.DefaultTTL || got.RenewInterval != server.DefaultRenewInterval {
		t.Fatalf("defaults=%+v %s", got, listen)
	}
	dir := t.TempDir()
	if code := runServer([]string{"start", "--foreground", "--data", dir, "--listen", "127.0.0.1:9999", "--ttl", "120s", "--renew", "20s"}); code != 0 {
		t.Fatal(code)
	}
	if got.DataDir != dir || listen != "127.0.0.1:9999" || got.TTL != 120*time.Second || got.RenewInterval != 20*time.Second {
		t.Fatalf("flags=%+v %s", got, listen)
	}
}

func TestForegroundServerInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses authenticated HTTP shutdown; console signals are Unix-only")
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {

			ctl, address, target := serverCommandEnv(t)
			cmd := exec.Command(ctl.Executable, "server", "start", "--foreground", "--data", t.TempDir(), "--listen", address)
			var log bytes.Buffer
			cmd.Stderr = &log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			p, _ := client.New(target, "signal-test")
			p.Retries = 0
			deadline := time.Now().Add(10 * time.Second)
			for {
				ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
				_, err := p.Health(ctx)
				cancel()
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("server not ready")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			for line := range strings.SplitSeq(strings.TrimSpace(log.String()), "\n") {
				if !json.Valid([]byte(line)) {
					t.Fatalf("non-JSON log: %s", line)
				}
			}

		})
	}
}

func TestServerRestartWithActiveEventPoll(t *testing.T) {
	ctl, address, target := serverCommandEnv(t)
	dir := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ctl.Stop(ctx, target)
	})
	first, err := ctl.Start(t.Context(), servicectl.StartOptions{DataDir: dir, Listen: address, TTL: api.DefaultTTL, Renew: api.DefaultRenewInterval})
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.New(target, "restart-poll")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	info, err := c.Register(t.Context(), "restart-poll", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	page, err := p.Events(t.Context(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan struct{})
	var once sync.Once
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { once.Do(func() { close(sent) }) }})
	finished := make(chan error, 1)
	go func() { _, err := p.Events(ctx, page.Cursor, 25*time.Second); finished <- err }()
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("poll was not sent")
	}
	select {
	case err := <-finished:
		t.Fatalf("poll ended before restart: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	next, err := ctl.Restart(t.Context(), target)
	if err != nil || next.PID == first.PID {
		t.Fatalf("active-poll restart=%+v %v", next, err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("old poll did not drain")
	}
}
