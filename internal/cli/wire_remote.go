package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/worker"
)

// The client binary reaches the board only through the server, so these
// entrances are the service ones; storage helpers remain available to the
// server and package tests only.
func init() {
	Commands["worker"] = worker.Run
	Commands["request"] = remote.RunRequest
	Commands["init"] = remote.RunInit
	Commands["list"] = remote.RunList
	Commands["show"] = remote.RunShow
	Commands["new"] = remote.RunNew
	Commands["update"] = remote.RunUpdate
	Commands["move"] = remote.RunMove
	Commands["pick"] = remote.RunPick
	Commands["guard-write"] = remote.RunGuardWrite
}

// RequireService prevents a registered lifecycle runner from entering its
// storage compatibility helpers when the client has no project binding.
func RequireService(runner Runner) Runner {
	return func(args []string) int {
		cfg, err := config.Load(false)
		if err == nil && cfg.Project.Name == "" {
			err = remote.ErrNotConfigured
		}
		if err != nil {
			if errors.Is(err, remote.ErrNotConfigured) {
				fmt.Fprintln(os.Stderr, config.Text("remote.not_configured"))
			} else {
				fmt.Fprintf(os.Stderr, "kander: %s\n", err)
			}
			return 1
		}
		return runner(args)
	}
}
