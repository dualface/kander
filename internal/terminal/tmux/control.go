package tmux

import (
	"context"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// ReadOutput captures the visible pane text.
func (b *Backend) ReadOutput(ctx context.Context, conn terminal.Conn, pane string) (string, error) {
	res, err := conn.Run(ctx, conn.Program, []string{"capture-pane", "-p", "-t", pane})
	if err != nil {
		return "", commandError(err.Error(), res, err)
	}
	if res.Code != 0 {
		return "", commandError(config.Text("launch.tmux_failed", "capture-pane", orExit(trimNL(res.Stderr), res.Code)), res, nil)
	}
	return res.Stdout, nil
}

// WaitOutput is not native to tmux: callers poll ReadOutput.
func (b *Backend) WaitOutput(context.Context, terminal.Conn, string, string, int) error {
	return terminal.ErrUnsupported
}

// DeliverText types the text literally and submits it with a separate Enter.
func (b *Backend) DeliverText(ctx context.Context, conn terminal.Conn, pane, text string) error {
	for _, args := range [][]string{
		{"send-keys", "-t", pane, "-l", text},
		{"send-keys", "-t", pane, "Enter"},
	} {
		res, err := conn.Run(ctx, conn.Program, args)
		if err != nil {
			return commandError(config.Text("launch.tmux_send_keys_failed", err.Error()), res, err)
		}
		if res.Code != 0 {
			return commandError(config.Text("launch.tmux_send_keys_failed", orExit(strings.TrimSpace(res.Stderr), res.Code)), res, nil)
		}
	}
	return nil
}

// Focus selects the window and pane and switches the current client to them.
func (b *Backend) Focus(ctx context.Context, conn terminal.Conn, address terminal.Address) terminal.FocusResult {
	if b.getenv("TMUX") == "" {
		return terminal.FocusResult{ID: "focus.outside_tmux"}
	}
	facts, err := b.PaneFacts(ctx, conn, address.Pane)
	if err != nil {
		return terminal.FocusResult{ID: "focus.probe_failed", Args: []any{probe.FailureDetail(err)}}
	}
	if facts.Gone || facts.Dead == "1" {
		return terminal.FocusResult{ID: "focus.closed"}
	}
	for _, args := range [][]string{
		{"select-window", "-t", address.Session + ":" + address.Container},
		{"select-pane", "-t", address.Pane},
		{"switch-client", "-t", address.Session},
	} {
		if err := terminal.RunStep(ctx, conn, args); err != nil {
			return terminal.FocusResult{ID: "focus.switch_failed", Args: []any{err.Error()}}
		}
	}
	return terminal.FocusResult{Success: true, ID: "focus.success"}
}

// CloseContainer kills the window; an empty window id closes nothing.
func (b *Backend) CloseContainer(ctx context.Context, conn terminal.Conn, address terminal.Address) error {
	if address.Container == "" {
		return nil
	}
	res, err := conn.Run(ctx, conn.Program, []string{"kill-window", "-t", address.Container})
	if err != nil {
		return commandError(config.Text("launch.failed_to_close_tmux_window", err.Error()), res, err)
	}
	if res.Code == 0 {
		return nil
	}
	return commandError(config.Text("launch.failed_to_close_tmux_window", orExit(strings.TrimSpace(res.Stderr), res.Code)), res, nil)
}
