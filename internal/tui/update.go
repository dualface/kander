package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/install"
)

type updateDialog struct {
	confirmDialog
	info install.UpdateInfo
}

func (a *App) receiveUpdateCheck(info *install.UpdateInfo, err error) {
	if err != nil {
		a.showFocusNotice(t("update.check_failed", err.Error()))
		return
	}
	if info == nil {
		return
	}
	a.pendingUpdate = info
	a.activatePendingUpdate()
}

func (a *App) updateBlocked() bool {
	return a.UpdateDialog != nil || a.TaskActions != nil || a.Chat != nil || a.Options != nil || a.Help ||
		a.StartConfirmation != nil || a.BoardInit != nil || a.Takeover != nil || a.Issues != nil || a.shouldShowWelcome()
}

func (a *App) activatePendingUpdate() {
	if a.pendingUpdate == nil || a.updateBlocked() {
		return
	}
	info := *a.pendingUpdate
	a.pendingUpdate = nil
	a.UpdateDialog = &updateDialog{confirmDialog: confirmDialog{phase: confirmReady}, info: info}
}

func (a *App) handleUpdateKey(key string) tea.Cmd {
	dialog := a.UpdateDialog
	switch dialog.handleKey(key) {
	case confirmCancel:
		a.UpdateDialog = nil
	case confirmAccept:
		dialog.run()
		ctx, cancel := context.WithCancel(context.Background())
		a.updateApplyCancel = cancel
		apply, info := a.updateApply, dialog.info
		return func() tea.Msg {
			result, err := apply(ctx, info)
			return updateApplyMsg{result: result, err: err}
		}
	case confirmClose:
		if !dialog.failed && a.RestartPath != "" {
			a.Running = false
		} else {
			a.UpdateDialog = nil
		}
	}
	return nil
}

func (a *App) receiveUpdateApply(result install.UpdateResult, err error) {
	dialog := a.UpdateDialog
	if dialog == nil || dialog.phase != confirmRunning {
		return
	}
	if err != nil {
		message := t("update.failed", err.Error())
		if diagnostic := strings.TrimSpace(result.Diagnostic); diagnostic != "" {
			message += "\n\n" + diagnostic
		}
		dialog.finish(message, true)
		return
	}
	a.RestartPath = result.Path
	dialog.finish(t("update.success", result.Version), false)
}

func (a *App) renderUpdate() (popupBox, string) {
	dialog := a.UpdateDialog
	paragraphs := []string{t("update.available", dialog.info.Current, dialog.info.Version)}
	hint := confirmHint(dialog.phase)
	switch dialog.phase {
	case confirmRunning:
		paragraphs = []string{t("update.running", dialog.info.Version)}
	case confirmFinished:
		paragraphs = []string{dialog.message}
		if !dialog.failed {
			hint = t("update.restart_keys")
		}
	}
	return a.renderConfirm(paragraphs, hint, t("update.title"), &dialog.bodyView)
}
