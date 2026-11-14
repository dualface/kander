package servicectl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// Controller owns bounded start/stop operations. Executable is injectable for
// subprocess tests. A server outlives the command that starts it.
type Controller struct {
	Executable string
	Timeout    time.Duration
}

type StartOptions struct {
	DataDir, Listen string
	TTL, Renew      time.Duration
}

func (c Controller) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 10 * time.Second
}
func peer(target string) (*client.Client, error) {
	p, err := client.New(target, "server-control")
	if p != nil {
		p.Retries = 0
	}
	return p, err
}
func Info(ctx context.Context, target string) (api.ServerInfo, error) {
	p, err := peer(target)
	if err != nil {
		return api.ServerInfo{}, err
	}
	info, err := p.ServerInfo(ctx)
	info.URL = target
	return info, err
}
func healthy(ctx context.Context, target string) bool {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	p, err := peer(target)
	if err != nil {
		return false
	}
	h, err := p.Health(ctx)
	return err == nil && h.Ready
}

func (c Controller) Start(ctx context.Context, o StartOptions) (api.ServerInfo, error) {
	dir, err := DataDir(o.DataDir)
	if err != nil {
		return api.ServerInfo{}, err
	}
	target, err := ProbeURL(o.Listen)
	if err != nil {
		return api.ServerInfo{}, err
	}
	if healthy(ctx, target) {
		return Info(ctx, target)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return api.ServerInfo{}, err
	}
	if err := fs.EnsurePrivateDirectory(filepath.Dir(dir), dir, true); err != nil {
		return api.ServerInfo{}, err
	}
	logPath := filepath.Join(dir, "server.log")
	log, err := fs.OpenAppendFile(dir, logPath)
	if err != nil {
		return api.ServerInfo{}, err
	}
	defer log.Close()
	exe := c.Executable
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return api.ServerInfo{}, err
		}
	}
	cmd := exec.Command(exe, "server", "start", "--foreground", "--data", dir, "--listen", o.Listen, "--ttl", o.TTL.String(), "--renew", o.Renew.String())
	cmd.Stdout, cmd.Stderr = log.File, log.File
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return api.ServerInfo{}, err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	wait, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	for {
		if healthy(wait, target) {
			return Info(wait, target)
		}
		select {
		case <-wait.Done():
			// Kill only our own child. Another concurrent starter may own the lock.
			_ = cmd.Process.Kill()
			<-exited
			if healthy(ctx, target) {
				return Info(ctx, target)
			}
			tail, readErr := fs.ReadRegularFile(dir, logPath)
			if readErr != nil {
				return api.ServerInfo{}, fmt.Errorf("%s: %w", config.Text("server.start_failed", logPath, ""), readErr)
			}
			lines := strings.Split(strings.TrimSuffix(string(tail), "\n"), "\n")
			return api.ServerInfo{}, errors.New(config.Text("server.start_failed", logPath, strings.Join(lines[max(0, len(lines)-20):], "\n")))
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (c Controller) Stop(ctx context.Context, target string) (bool, error) {
	if !Local(target) {
		return false, errors.New(config.Text("server.local_only"))
	}
	info, err := Info(ctx, target)
	if client.IsUnreachable(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	data, err := fs.ReadRegularFile(info.DataDir, filepath.Join(info.DataDir, api.AdminTokenFile))
	if err != nil {
		return true, err
	}
	var record struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &record); err != nil || len(record.Token) != 64 {
		return true, errors.New(config.Text("server.token_invalid"))
	}
	p, err := peer(target)
	if err != nil {
		return true, err
	}
	if err := p.Shutdown(ctx, record.Token); err != nil {
		return true, err
	}
	wait, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	for {
		// A response, including not-ready, still means the process is listening.
		probe, done := context.WithTimeout(wait, 300*time.Millisecond)
		_, err := p.Health(probe)
		done()
		if client.IsUnreachable(err) && wait.Err() == nil {
			return true, nil
		}
		select {
		case <-wait.Done():
			return true, errors.New(config.Text("server.stop_timeout"))
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (c Controller) Restart(ctx context.Context, target string) (api.ServerInfo, error) {
	if !Local(target) {
		return api.ServerInfo{}, errors.New(config.Text("server.local_only"))
	}
	info, err := Info(ctx, target)
	if err != nil {
		return api.ServerInfo{}, err
	}
	if _, err := c.Stop(ctx, target); err != nil {
		return api.ServerInfo{}, err
	}
	return c.Start(ctx, StartOptions{DataDir: info.DataDir, Listen: info.Listen, TTL: info.TTL.Std(), Renew: info.Renew.Std()})
}
