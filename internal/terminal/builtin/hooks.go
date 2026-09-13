package builtin

import (
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/herdr"
)

type builtinHook struct {
	name string
	hook terminal.Hook
}

// builtinHooks is the single inventory of shipped terminal hooks. A fresh
// slice keeps the inventory from becoming mutable registration state.
func builtinHooks() []builtinHook {
	return []builtinHook{
		{"herdr-socket-session", herdr.ReportSession},
		{"herdr-socket-focus", herdr.FocusPane},
	}
}

func registerHooks() {
	for _, entry := range builtinHooks() {
		terminal.RegisterHook(entry.name, entry.hook)
	}
}
