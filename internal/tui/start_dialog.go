package tui

type startDialog struct {
	confirmDialog
	startRequest
}

func startDialogTitle(dialog *startDialog) string {
	if title := confirmSharedTitle(dialog.phase, dialog.failed); title != "" {
		return title
	}
	return t("tui.start_confirm")
}

func (a *App) renderStartConfirmation() (popupBox, string) {
	dialog := a.StartConfirmation
	paragraphs := []string{dialog.TaskID + "\n" + t("tui.start_state", dialog.State)}
	hint := confirmHint(dialog.phase)
	switch dialog.phase {
	case confirmLoading:
		paragraphs = append(paragraphs, confirmSettings("", ""))
	case confirmFinished:
		paragraphs = []string{dialog.message}
	default:
		paragraphs = append(paragraphs, confirmSettings(dialog.Agent, dialog.Launcher))
		if dialog.State == "backlog" {
			paragraphs = append(paragraphs, t("tui.start_backlog"))
		}
		paragraphs = append(paragraphs, dialog.Warnings...)
	}
	return a.renderConfirm(paragraphs, hint, startDialogTitle(dialog), &dialog.bodyView)
}

func (a *App) handleStartMouse(x, y, buttons int) {
	if a.StartConfirmation.handleWheel(x, y, buttons, a.handleBoardMouse, a.startTargetValid) {
		a.StartConfirmation = nil
	}
}
