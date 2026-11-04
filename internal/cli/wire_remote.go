package cli

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// RemoteCommands are the server-mode runners. When the effective config sets
// server.url they replace the v1 runners of the same name, so these commands
// read and write the board only through kander-server.
var RemoteCommands = map[string]Runner{
	"list":   remote.RunList,
	"show":   remote.RunShow,
	"new":    remote.RunNew,
	"update": remote.RunUpdate,
	"move":   remote.RunMove,
	"pick":   remote.RunPick,
}

// serverMode reports whether the effective config selects a server. An
// unreadable config keeps the v1 runner, which reports the config error itself.
func serverMode() bool {
	cfg, err := config.Load(true)
	return err == nil && remote.Enabled(cfg)
}
