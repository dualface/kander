// Package builtin registers the built-in terminal backends. Import it for its
// side effect wherever a backend is looked up by launcher name.
package builtin

import (
	"os"

	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/direct"
	"github.com/dualface/kander/internal/terminal/herdr"
	"github.com/dualface/kander/internal/terminal/tmux"
)

func init() {
	// Auto resolution follows registration order: herdr wins over tmux.
	terminal.Register(herdr.New(os.Getenv, nil))
	terminal.Register(tmux.New(tmux.Name, os.Getenv))
	terminal.Register(tmux.New(tmux.SessionName, os.Getenv))
	terminal.Register(direct.New(direct.Foreground))
	terminal.Register(direct.New(direct.Console))
}
