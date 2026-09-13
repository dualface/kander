package herdr

import (
	"context"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// ReadOutput reads the pane text.
func (b *Backend) ReadOutput(ctx context.Context, conn terminal.Conn, pane string) (string, error) {
	res, err := run(ctx, conn, []string{"pane", "read", pane})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", commandError(terminal.KindExit, config.Text("launch.herdr_failed", "pane read", failureDetail(res)), res, nil)
	}
	return res.Stdout, nil
}

// WaitOutput blocks until recent pane output contains match, a literal
// substring or a "regex:"-prefixed expression, or herdr's timeout elapses.
func (b *Backend) WaitOutput(ctx context.Context, conn terminal.Conn, pane, match string, timeoutMS int) error {
	args := []string{"pane", "wait-output", pane}
	if strings.HasPrefix(match, "regex:") {
		args = append(args, "--regex", strings.TrimPrefix(match, "regex:"), "--source", "recent", "--timeout", strconv.Itoa(timeoutMS))
	} else {
		args = append(args, "--match", match, "--source", "recent", "--timeout", strconv.Itoa(timeoutMS))
	}
	res, err := run(ctx, conn, args)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return commandError(terminal.KindExit, config.Text("launch.herdr_pane_is_not_ready", failureDetail(res)), res, nil)
	}
	return nil
}

// DeliverText hands the text to the agent TUI already running in the pane.
// herdr sends it per the pane's bracketed-paste mode, then an Enter, and
// rejects a pane stopped at an approval or question UI.
func (b *Backend) DeliverText(ctx context.Context, conn terminal.Conn, pane, text string) error {
	res, err := run(ctx, conn, []string{"agent", "prompt", pane, text})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return commandError(terminal.KindExit, config.Text("launch.herdr_agent_prompt_failed", failureDetail(res)), res, nil)
	}
	return nil
}

// Focus switches to the tab, then best-effort focuses the pane through the
// herdr socket.
func (b *Backend) Focus(ctx context.Context, conn terminal.Conn, address terminal.Address) terminal.FocusResult {
	facts, err := b.PaneFacts(ctx, conn, address.Pane)
	if err != nil {
		return terminal.FocusResult{ID: "focus.probe_failed", Args: []any{probe.FailureDetail(err)}}
	}
	if facts.Gone {
		return terminal.FocusResult{ID: "focus.closed"}
	}
	if err := terminal.RunStep(ctx, conn, []string{"tab", "focus", address.Container}); err != nil {
		return terminal.FocusResult{ID: "focus.switch_failed", Args: []any{err.Error()}}
	}
	if err := b.paneFocus(ctx, b.getenv("HERDR_SOCKET_PATH"), address.Pane); err != nil {
		return terminal.FocusResult{Success: true, ID: "focus.tab_only", Args: []any{probe.FailureDetail(err)}}
	}
	return terminal.FocusResult{Success: true, ID: "focus.success"}
}

// CloseContainer closes the tab.
func (b *Backend) CloseContainer(ctx context.Context, conn terminal.Conn, address terminal.Address) error {
	res, err := run(ctx, conn, []string{"tab", "close", address.Container})
	if err != nil {
		commandErr, _ := terminal.AsCommandError(err)
		return commandError(terminal.KindExec, config.Text("launch.failed_to_close_tab", address.Container, err.Error()), res, commandErr.Cause)
	}
	if res.Code == 0 {
		return nil
	}
	return commandError(terminal.KindExit, config.Text("launch.failed_to_close_tab", address.Container, failureDetail(res)), res, nil)
}
