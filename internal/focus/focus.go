// Package focus switches the local terminal to a card's recorded WINDOW address.
package focus

import (
	"context"
	"os/exec"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
	_ "github.com/dualface/kander/internal/terminal/builtin"
)

// Result distinguishes a completed switch from a failure, with a localized notice.
type Result struct {
	Success bool
	Message string
}

type executor struct {
	lookup  func(string) (string, error)
	backend func(string) (terminal.Backend, bool)
	run     terminal.Runner
}

// Window probes the recorded pane before switching, using one bounded budget.
// It never discovers another address or changes card metadata.
func Window(ctx context.Context, address string) Result {
	return (executor{exec.LookPath, terminal.Lookup, terminal.ProbeRunner}).focus(ctx, address)
}

func notice(success bool, id string, args ...any) Result {
	return Result{Success: success, Message: config.Text(id, args...)}
}

func (e executor) parse(address string) (terminal.Backend, terminal.Address, string) {
	if strings.TrimSpace(address) == "" {
		return nil, terminal.Address{}, "focus.no_window"
	}
	fields := strings.Split(address, ":")
	backend, ok := e.backend(fields[0])
	if !ok || !backend.Capabilities().Focus {
		return nil, terminal.Address{}, "focus.unsupported"
	}
	for _, field := range fields {
		if field == "" || strings.ContainsFunc(field, unicode.IsControl) {
			return nil, terminal.Address{}, "focus.invalid_window"
		}
	}
	target, ok := backend.ParseFocusAddress(fields)
	if !ok {
		return nil, terminal.Address{}, "focus.invalid_window"
	}
	return backend, target, ""
}

func (e executor) focus(parent context.Context, address string) Result {
	backend, target, reason := e.parse(address)
	if reason != "" {
		return notice(false, reason)
	}
	program, err := e.lookup(backend.Executable())
	if err != nil {
		return notice(false, "focus.command_missing", backend.Executable())
	}
	ctx, cancel := probe.WithDefaultTimeout(parent)
	defer cancel()
	result := backend.Focus(ctx, terminal.Conn{Program: program, Run: e.run}, target)
	return notice(result.Success, result.ID, result.Args...)
}
