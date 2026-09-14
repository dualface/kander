// Package herdr contains only the socket protocols used by the herdr definition.
package herdr

import (
	"context"
	"errors"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// ReportSession performs the socket handshake and verifies the reported identity.
func ReportSession(_ context.Context, call terminal.HookCall) terminal.HookResult {
	if call.Report == nil {
		return terminal.HookResult{Status: terminal.HookFailed, Err: errors.New("herdr session hook requires a report")}
	}
	err := reportSession(call)
	if errors.Is(err, terminal.ErrNoReportChannel) {
		return terminal.HookResult{Status: terminal.HookDegraded, Note: err.Error()}
	}
	if err != nil {
		return terminal.HookResult{Status: terminal.HookFailed, Err: err}
	}
	return terminal.HookResult{Status: terminal.HookOK}
}

// FocusPane preserves tab-only success when the socket cannot focus the pane.
func FocusPane(ctx context.Context, call terminal.HookCall) terminal.HookResult {
	if err := focusPane(ctx, call.Getenv("HERDR_SOCKET_PATH"), call.Values["pane"]); err != nil {
		return terminal.HookResult{Status: terminal.HookDegraded, Note: probe.FailureDetail(err)}
	}
	return terminal.HookResult{Status: terminal.HookOK}
}

func textError(id string, args ...any) *probe.Error {
	return &probe.Error{Message: config.Text(id, args...)}
}
func orNA(value string) string {
	if value == "" {
		return "N/A"
	}
	return value
}
