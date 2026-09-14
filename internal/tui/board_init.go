package tui

import (
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/dualface/kander/internal/board"
)

type boardInitPhase int

const (
	boardInitLoading boardInitPhase = iota
	boardInitReady
	boardInitRunning
	boardInitFinished
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
	sequence uint64
	phase    boardInitPhase
	next     boardInitAction
	path     string
	failed   bool
	message  string
	bodyView viewport.Model
}

type boardInitPreviewResult struct {
	sequence uint64
	path     string
	err      error
}

type boardInitResult struct {
	sequence uint64
	root     string
	err      error
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
	dialog := &boardInitState{sequence: a.boardInitSeq, phase: boardInitLoading, next: next}
	a.BoardInit = dialog
	preview := a.PreviewBoardInit
	if preview == nil {
		preview = func() (string, error) { return board.PlannedInitRoot("") }
	}
	sequence := dialog.sequence
	a.pendingWork = func() any {
		path, err := preview()
		return boardInitPreviewResult{sequence: sequence, path: path, err: err}
	}
}

func (a *App) applyBoardInitPreview(result boardInitPreviewResult) {
	dialog := a.BoardInit
	if dialog == nil || dialog.sequence != result.sequence || dialog.phase != boardInitLoading {
		return
	}
	if result.err != nil {
		a.BoardInit = nil
		a.showFocusNotice(t("tui.board_init_preview_failed", result.err.Error()))
		return
	}
	dialog.path, dialog.phase = result.path, boardInitReady
}

func (a *App) handleBoardInitKey(key string) {
	dialog := a.BoardInit
	if dialog == nil {
		return
	}
	if dialog.phase == boardInitRunning {
		return
	}
	if dialog.phase == boardInitFinished {
		a.BoardInit = nil
		return
	}
	if key != "y" && key != "enter" {
		a.BoardInit = nil
		return
	}
	if dialog.phase == boardInitLoading {
		a.showFocusNotice(t("tui.start_loading_keys"))
		return
	}
	a.startBoardInit(dialog)
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
	dialog.phase = boardInitRunning
	dialog.failed = false
	dialog.message = ""
	a.pendingWork = func() any {
		root, err := run()
		return boardInitResult{sequence: sequence, root: root, err: err}
	}
}

func (a *App) applyBoardInitResult(result boardInitResult) {
	dialog := a.BoardInit
	if dialog == nil || dialog.sequence != result.sequence || dialog.phase != boardInitRunning {
		return
	}
	if result.err != nil {
		dialog.phase = boardInitFinished
		dialog.failed = true
		dialog.message = t("tui.board_init_failed", result.err.Error())
		dialog.bodyView.GotoTop()
		return
	}
	next := dialog.next
	a.BoardInit = nil
	if a.AttachBoard != nil {
		a.AttachBoard(result.root)
	} else {
		a.boardRoot = result.root
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
	case boardInitLoading:
		return t("tui.start_loading")
	case boardInitRunning:
		return t("tui.board_init_running")
	case boardInitFinished:
		return t("tui.start_title_failed")
	default:
		return t("tui.board_init_title")
	}
}

func (a *App) renderBoardInit() (popupBox, string) {
	dialog := a.BoardInit
	paragraphs := []string{}
	hint := t("tui.board_init_keys")
	switch dialog.phase {
	case boardInitLoading:
		placeholder := t("tui.start_loading")
		paragraphs = append(paragraphs, t("tui.board_init_body", placeholder))
		hint = t("tui.start_loading_keys")
	case boardInitRunning:
		paragraphs = append(paragraphs, t("tui.board_init_body", dialog.path), t("tui.board_init_running"))
		hint = t("tui.board_init_running")
	case boardInitFinished:
		paragraphs = []string{dialog.message}
		hint = t("tui.start_result_keys")
	default:
		paragraphs = append(paragraphs, t("tui.board_init_body", dialog.path))
	}
	return a.renderStartDialog(paragraphs, hint, boardInitTitle(dialog), &dialog.bodyView)
}

func (a *App) handleBoardInitMouse(x, y, buttons int) {
	dialog := a.BoardInit
	if dialog == nil {
		return
	}
	delta := mouseWheelDelta(buttons)
	if delta == 0 || dialog.phase == boardInitLoading {
		return
	}
	if delta > 0 {
		dialog.bodyView.ScrollDown(delta)
	} else {
		dialog.bodyView.ScrollUp(-delta)
	}
}
