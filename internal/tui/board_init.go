package tui

import (
	"github.com/dualface/kander/internal/board"
)

// boardInitAction is the operation that needed a board and should continue
// after a successful kander init.
type boardInitAction int

const (
	boardInitChat boardInitAction = iota + 1
	boardInitImport
	boardInitImportComments
	boardInitTakeover
)

// boardInitState confirms creating kanban/ through the same path as kander init.
type boardInitState struct {
	confirmDialog
	next boardInitAction
	path string
}

func (a *App) needsBoardInit() bool {
	return a != nil && a.missingBoard
}

// offerBoardInit opens the init confirmation when the board is missing.
// It returns true when the caller must wait for the user to confirm or cancel.
func (a *App) offerBoardInit(next boardInitAction) bool {
	if !a.needsBoardInit() {
		return false
	}
	a.openBoardInit(next)
	return true
}

func (a *App) openBoardInit(next boardInitAction) {
	if a.BoardInit != nil {
		return
	}
	a.boardInitSeq++
	dialog := &boardInitState{confirmDialog: confirmDialog{sequence: a.boardInitSeq, phase: confirmLoading}, next: next}
	a.BoardInit = dialog
	preview := a.PreviewBoardInit
	if preview == nil {
		preview = func() (string, error) { return board.PlannedInitRoot("") }
	}
	sequence := dialog.sequence
	a.pendingWork = func() any {
		path, err := preview()
		return confirmWork{kind: workBoardInitPreview, sequence: sequence, payload: path, err: err}
	}
}

func (a *App) applyBoardInitPreview(work confirmWork) {
	dialog := a.BoardInit
	if dialog == nil || !dialog.matches(work.sequence, confirmLoading) {
		return
	}
	if work.err != nil {
		a.BoardInit = nil
		a.showFocusNotice(t("tui.board_init_preview_failed", work.err.Error()))
		return
	}
	path, _ := work.payload.(string)
	dialog.path, dialog.phase = path, confirmReady
}

func (a *App) handleBoardInitKey(key string) {
	dialog := a.BoardInit
	if dialog == nil {
		return
	}
	switch dialog.handleKey(key) {
	case confirmWait:
		a.showFocusNotice(t("dialog.loading_keys"))
	case confirmCancel, confirmClose:
		a.BoardInit = nil
	case confirmAccept:
		a.startBoardInit(dialog)
	}
}

func (a *App) startBoardInit(dialog *boardInitState) {
	run := a.InitBoard
	if run == nil {
		run = func() (string, error) {
			root, _, _, err := board.InitBoard("")
			return root, err
		}
	}
	sequence := dialog.sequence
	dialog.run()
	a.pendingWork = func() any {
		root, err := run()
		return confirmWork{kind: workBoardInitResult, sequence: sequence, payload: root, err: err}
	}
}

func (a *App) applyBoardInitResult(work confirmWork) {
	dialog := a.BoardInit
	if dialog == nil || !dialog.matches(work.sequence, confirmRunning) {
		return
	}
	if work.err != nil {
		dialog.finish(t("tui.board_init_failed", work.err.Error()), true)
		return
	}
	root, _ := work.payload.(string)
	next := dialog.next
	a.BoardInit = nil
	if a.AttachBoard != nil {
		a.AttachBoard(root)
	} else {
		a.boardRoot = root
		a.missingBoard = false
	}
	a.refreshBoard()
	a.continueBoardInit(next)
}

func (a *App) continueBoardInit(next boardInitAction) {
	switch next {
	case boardInitChat:
		a.openChat()
	case boardInitImport:
		a.issuesImport(false)
	case boardInitImportComments:
		a.issuesImport(true)
	case boardInitTakeover:
		a.issuesTakeover()
	}
}

func boardInitTitle(dialog *boardInitState) string {
	switch dialog.phase {
	case confirmRunning:
		return t("tui.board_init_running")
	case confirmLoading, confirmFinished:
		return confirmSharedTitle(dialog.phase, dialog.failed)
	default:
		return t("tui.board_init_title")
	}
}

func (a *App) renderBoardInit() (popupBox, string) {
	dialog := a.BoardInit
	paragraphs := []string{}
	hint := confirmHint(dialog.phase)
	switch dialog.phase {
	case confirmLoading:
		paragraphs = append(paragraphs, t("tui.board_init_body", t("dialog.loading")))
	case confirmRunning:
		paragraphs = append(paragraphs, t("tui.board_init_body", dialog.path), t("tui.board_init_running"))
	case confirmFinished:
		paragraphs = []string{dialog.message}
	default:
		paragraphs = append(paragraphs, t("tui.board_init_body", dialog.path))
	}
	return a.renderConfirm(paragraphs, hint, boardInitTitle(dialog), &dialog.bodyView)
}

func (a *App) handleBoardInitMouse(x, y, buttons int) {
	a.BoardInit.handleWheel(x, y, buttons, nil, func() bool { return true })
}
