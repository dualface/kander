// Package focus switches the local terminal to a card's recorded WINDOW address.
package focus

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

// Result distinguishes a completed switch from a failure, with a localized notice.
type Result struct {
	Success bool
	Message string
}

type target struct{ launcher, session, window, pane string }

type executor struct {
	lookup    func(string) (string, error)
	getenv    func(string) string
	run       func(context.Context, string, []string) (probe.Result, error)
	herdr     func(context.Context, string, string) (probe.HerdrPaneProbe, error)
	tmux      func(context.Context, string, string) (probe.TmuxPaneProbe, error)
	paneFocus func(context.Context, string, string) error
}

// Window probes the recorded pane before switching, using one bounded budget.
// It never discovers another address or changes card metadata.
func Window(ctx context.Context, address string) Result {
	return (executor{exec.LookPath, os.Getenv, probe.CaptureContext,
		probe.ProbeHerdrPaneContext, probe.ProbeTmuxPaneContext, focusHerdrPane}).focus(ctx, address)
}

func notice(success bool, id string, args ...any) Result {
	return Result{Success: success, Message: config.Text(id, args...)}
}

func parse(address string) (target, string) {
	if strings.TrimSpace(address) == "" {
		return target{}, "focus.no_window"
	}
	parts := strings.Split(address, ":")
	launcher := parts[0]
	if launcher != "herdr" && launcher != "tmux" && launcher != "tmux-session" {
		return target{}, "focus.unsupported"
	}
	for _, part := range parts {
		if part == "" || strings.ContainsFunc(part, unicode.IsControl) {
			return target{}, "focus.invalid_window"
		}
	}
	switch {
	case launcher == "herdr" && len(parts) == 3:
		return target{launcher: launcher, window: parts[1], pane: parts[2]}, ""
	case launcher == "herdr" && len(parts) == 5:
		// Public herdr IDs include their workspace prefix, for example w1:t2 and w1:p3.
		return target{launcher: launcher, window: strings.Join(parts[1:3], ":"), pane: strings.Join(parts[3:5], ":")}, ""
	case launcher != "herdr" && len(parts) == 4:
		return target{launcher, parts[1], parts[2], parts[3]}, ""
	default:
		return target{}, "focus.invalid_window"
	}
}

func (e executor) focus(parent context.Context, address string) Result {
	dest, reason := parse(address)
	if reason != "" {
		return notice(false, reason)
	}
	command := dest.launcher
	if command == "tmux-session" {
		command = "tmux"
	}
	program, err := e.lookup(command)
	if err != nil {
		return notice(false, "focus.command_missing", command)
	}
	if command == "tmux" && e.getenv("TMUX") == "" {
		return notice(false, "focus.outside_tmux")
	}
	ctx, cancel := probe.WithDefaultTimeout(parent)
	defer cancel()
	if command == "herdr" {
		facts, err := e.herdr(ctx, program, dest.pane)
		if err != nil {
			return notice(false, "focus.probe_failed", probe.FailureDetail(err))
		}
		if facts.Pane == nil {
			return notice(false, "focus.closed")
		}
		if err := e.execute(ctx, program, []string{"tab", "focus", dest.window}); err != nil {
			return notice(false, "focus.switch_failed", err.Error())
		}
		if err := e.paneFocus(ctx, e.getenv("HERDR_SOCKET_PATH"), dest.pane); err != nil {
			return notice(true, "focus.tab_only", probe.FailureDetail(err))
		}
		return notice(true, "focus.success")
	}
	facts, err := e.tmux(ctx, program, dest.pane)
	if err != nil {
		return notice(false, "focus.probe_failed", probe.FailureDetail(err))
	}
	if facts.Facts == nil || facts.Facts.Dead == "1" {
		return notice(false, "focus.closed")
	}
	for _, args := range [][]string{
		{"select-window", "-t", dest.session + ":" + dest.window},
		{"select-pane", "-t", dest.pane},
		{"switch-client", "-t", dest.session},
	} {
		if err := e.execute(ctx, program, args); err != nil {
			return notice(false, "focus.switch_failed", err.Error())
		}
	}
	return notice(true, "focus.success")
}

func (e executor) execute(ctx context.Context, program string, args []string) error {
	result, err := e.run(ctx, program, args)
	if err != nil {
		return fmt.Errorf("%s: %s", args[0], probe.FailureDetail(err))
	}
	if result.Code != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail == "" {
			detail = fmt.Sprintf("exit %d", result.Code)
		}
		return fmt.Errorf("%s: %s", args[0], detail)
	}
	return nil
}
