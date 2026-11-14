package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/worker"
)

func init() {
	Commands["worker"] = worker.Run
	Commands["request"] = remote.RunRequest
}

// RemoteCommands are the mandatory service entrances of the client binary.
// Storage helpers remain available to the server and package tests only.
var RemoteCommands = map[string]Runner{
	"init":        remote.RunInit,
	"list":        remote.RunList,
	"show":        remote.RunShow,
	"new":         remote.RunNew,
	"update":      remote.RunUpdate,
	"move":        remote.RunMove,
	"pick":        remote.RunPick,
	"guard-write": remote.RunGuardWrite,
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
