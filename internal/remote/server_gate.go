package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/servicectl"
	"github.com/dualface/kander/internal/version"
	"golang.org/x/term"
)

var serverPromptInput io.Reader = os.Stdin
var serverTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())) }
var startLocalServer = (servicectl.Controller{}).Start
var restartLocalServer = (servicectl.Controller{}).Restart
var serverVersionChecks sync.Map
var serverQuestionsSuspended atomic.Bool

// SuspendServerQuestions keeps subsequent UI operations from reading terminal
// input behind the alt-screen. The caller restores the previous state on exit.
func SuspendServerQuestions() func() {
	previous := serverQuestionsSuspended.Swap(true)
	return func() { serverQuestionsSuspended.Store(previous) }
}

func serverInteractive() bool {
	_, execution := os.LookupEnv(EnvExecutionID)
	_, coordinator := os.LookupEnv(EnvCoordinatorExecutionID)
	return !serverQuestionsSuspended.Load() && !execution && !coordinator && serverTerminal()
}

// confirmServer defaults an actual empty line to yes. EOF never authorizes
// starting a process, even when the reader returned partial input.
func confirmServer(message string) bool {
	fmt.Fprint(stderr, message)
	line, err := bufio.NewReader(serverPromptInput).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.TrimSpace(line) {
	case "", "y", "Y":
		return true
	}
	return false
}

func prepareServer(ctx context.Context, c *client.Client, cfg *config.Config) (bool, error) {
	target := config.ServerURL(cfg)
	v, err := c.CheckCompatible(ctx)
	if !servicectl.Local(target) {
		return false, err
	}
	prompted := false
	if client.IsUnreachable(err) {
		dir, pathErr := servicectl.DataDir("")
		if pathErr != nil {
			return false, pathErr
		}
		unavailable := errors.New(config.Text("remote.server_not_running", target, dir))
		if !serverInteractive() {
			return false, unavailable
		}
		board := filepath.Join(dir, "projects", api.ProjectDirName(cfg.Project.Name), "board")
		if !confirmServer(config.Text("remote.server_start_prompt", target, dir, board, filepath.Join(dir, "server.log"))) {
			return true, unavailable
		}
		prompted = true
		fresh, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := startLocalServer(fresh, servicectl.StartOptions{DataDir: dir, Listen: servicectl.ListenAddress(target), TTL: api.DefaultTTL, Renew: api.DefaultRenewInterval}); err != nil {
			return true, err
		}
		v, err = c.CheckCompatible(fresh)
	}
	incompatible := api.IsCode(err, api.CodeProtocolIncompatible)
	if err != nil && !incompatible {
		return prompted, err
	}
	if v.ServerVersion == version.String() && !incompatible {
		return prompted, nil
	}
	// Every Open still checks the protocol. Only the version notification or
	// restart question is once per endpoint in this client process.
	if _, loaded := serverVersionChecks.LoadOrStore(target, struct{}{}); loaded {
		if incompatible {
			return prompted, errors.New(config.Text("remote.server_restart_required", v.ServerVersion, version.String()))
		}
		return prompted, nil
	}
	if !serverInteractive() {
		if incompatible {
			return prompted, errors.New(config.Text("remote.server_restart_required", v.ServerVersion, version.String()))
		}
		fmt.Fprintln(stderr, config.Text("remote.server_version_warning", v.ServerVersion, version.String()))
		return prompted, nil
	}
	prompted = true
	if !confirmServer(config.Text("remote.server_restart_prompt", v.ServerVersion, version.String())) {
		return true, err
	}
	fresh, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := restartLocalServer(fresh, target); err != nil {
		return true, err
	}
	_, err = c.CheckCompatible(fresh)
	return true, err
}
