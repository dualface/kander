package tui

import (
	"context"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
)

// takeoverState confirms investigation or completed-result reconciliation.
// cardID selects only the result protocol; the original completed card stays read-only.
type takeoverState struct {
	confirmDialog
	cardID     string
	repository issue.Repository
	number     int
	agent      string
	launcher   string
}

// takeoverIdentity renders the confirmed identity of one issue. It is built
// from the validated repository fields and the issue number only, never from
// remote text.
func takeoverIdentity(repository issue.Repository, number int) string {
	return repository.Owner + "/" + repository.Name + "#" + strconv.Itoa(number)
}

// issuesTakeover opens investigation for unbound issues or result sync for done cards.
func (a *App) issuesTakeover() {
	st := a.Issues
	if st == nil || st.importing || a.Takeover != nil {
		return
	}
	number := a.issuesSelectedNumber()
	repository := a.issuesRepository()
	if number <= 0 || repository == nil {
		a.issuesSetNotice(a.Context.IssuesNoTarget)
		return
	}
	if a.offerBoardInit(boardInitTakeover) {
		return
	}
	if card, ok := a.issuesLocalCard(number); ok {
		if card.State != "done" {
			return
		}
		a.openTakeover(*repository, number)
		a.Takeover.cardID = card.TaskID
		return
	}
	a.openTakeover(*repository, number)
}

// openTakeover shows the dialog and resolves the configured agent and launcher
// in the background. The dialog appears immediately, so a slow configuration
// read never blocks the key press.
func (a *App) openTakeover(repository issue.Repository, number int) {
	a.takeoverSeq++
	dialog := &takeoverState{
		confirmDialog: confirmDialog{sequence: a.takeoverSeq, phase: confirmLoading},
		repository:    repository,
		number:        number,
	}
	a.Takeover = dialog
	prepare := a.PrepareTriage
	if prepare == nil {
		// Tests may build an App without the binding; the start still uses the
		// configured defaults, the dialog just cannot show them in advance.
		dialog.phase = confirmReady
		return
	}
	sequence := dialog.sequence
	a.pendingWork = func() any {
		preview, err := prepare()
		return confirmWork{kind: workTakeoverPreview, sequence: sequence, payload: preview, err: err}
	}
}

// applyTakeoverPreview fills the resolved settings in. An unbound issue has no
// jump exit, so a failed preview or a launcher that needs the caller's terminal
// is refused here exactly like the board start dialog and points at the CLI.
func (a *App) applyTakeoverPreview(work confirmWork) {
	dialog := a.Takeover
	if dialog == nil || !dialog.matches(work.sequence, confirmLoading) {
		return
	}
	if !a.takeoverTargetCurrent(dialog) {
		a.Takeover = nil
		a.issuesSetNotice(t("tui.issues_result_stale"))
		return
	}
	if work.err != nil {
		a.Takeover = nil
		a.issuesSetNotice(t("tui.issues_takeover_preview_failed", issue.TriageError(work.err)))
		return
	}
	preview, _ := work.payload.(launch.TriagePreview)
	dialog.agent, dialog.launcher = preview.Agent, preview.Launcher
	if !backgroundStartLauncher(preview.Launcher) {
		a.Takeover = nil
		a.issuesSetNotice(t("tui.start_use_cli", preview.Launcher))
		return
	}
	dialog.phase = confirmReady
}

func (a *App) handleTakeoverKey(key string) {
	dialog := a.Takeover
	if dialog == nil {
		return
	}
	switch dialog.handleKey(key) {
	case confirmWait:
		a.issuesSetNotice(t("dialog.loading_keys"))
	case confirmCancel, confirmClose:
		a.Takeover = nil
	case confirmAccept:
		a.startTakeover(dialog)
	}
}

// startTakeover runs the shared StartTriage path in the background; the TUI
// never touches the board and never blocks on the network or the container.
func (a *App) startTakeover(dialog *takeoverState) {
	if !a.takeoverTargetCurrent(dialog) {
		a.Takeover = nil
		a.issuesSetNotice(t("tui.issues_result_stale"))
		return
	}
	runner := a.TriageIssue
	if dialog.cardID != "" {
		runner = a.ResultIssue
	}
	if runner == nil {
		dialog.finish(t("tui.issues_takeover_unavailable"), true)
		return
	}
	// The agent and launcher the dialog showed and validated are passed on, so a
	// configuration change after the preview cannot start a different pair, and
	// never a launcher that needs the caller's terminal.
	options := issue.TriageOptions{Agent: dialog.agent, Launcher: dialog.launcher, CardID: dialog.cardID}
	sequence, repository, number := dialog.sequence, dialog.repository, dialog.number
	dialog.run()
	a.pendingWork = func() any {
		ctx, cancel := context.WithTimeout(context.Background(), issuesTriageTimeout)
		defer cancel()
		outcome, err := runner(ctx, repository, number, options)
		return confirmWork{kind: workTakeoverResult, sequence: sequence, payload: outcome, err: err}
	}
}

func (a *App) applyTakeoverResult(work confirmWork) {
	dialog := a.Takeover
	if dialog == nil || dialog.sequence != work.sequence {
		return
	}
	if work.err != nil {
		dialog.finish(t("tui.issues_takeover_failed", issue.TriageError(work.err)), true)
		return
	}
	outcome, _ := work.payload.(issue.TriageOutcome)
	dialog.agent, dialog.launcher = outcome.Agent, outcome.Launcher
	address := strings.TrimSpace(outcome.Address)
	if address == "" {
		address = orDash(address)
	}
	message := t("tui.issues_takeover_started", outcome.Agent, outcome.Launcher, address)
	if len(outcome.Warnings) > 0 {
		message += "\n" + strings.Join(outcome.Warnings, "\n")
	}
	dialog.finish(message, false)
}

func takeoverTitle(dialog *takeoverState) string {
	if title := confirmSharedTitle(dialog.phase, dialog.failed); title != "" {
		return title
	}
	if dialog.cardID != "" {
		return t("tui.issues_result_title", itoa(dialog.number))
	}
	return t("tui.issues_takeover_title", itoa(dialog.number))
}

func (a *App) renderTakeover() (popupBox, string) {
	dialog := a.Takeover
	identity := takeoverIdentity(dialog.repository, dialog.number)
	paragraphs := []string{}
	hint := confirmHint(dialog.phase)
	switch dialog.phase {
	case confirmLoading:
		paragraphs = append(paragraphs, t("tui.issues_takeover_loading"), confirmSettings("", ""))
	case confirmRunning:
		paragraphs = append(paragraphs, confirmSettings(dialog.agent, dialog.launcher))
	case confirmFinished:
		paragraphs = []string{dialog.message}
	default:
		body := t("tui.issues_takeover_body", identity)
		if dialog.cardID != "" {
			body = t("tui.issues_result_body", identity, dialog.cardID)
		}
		paragraphs = append(paragraphs, body)
		if dialog.agent != "" || dialog.launcher != "" {
			paragraphs = append(paragraphs, confirmSettings(dialog.agent, dialog.launcher))
		}
	}
	return a.renderConfirm(paragraphs, hint, takeoverTitle(dialog), &dialog.bodyView)
}

// handleTakeoverMouse scrolls the dialog body. While the settings are still
// loading the wheel keeps operating the overlay underneath, and changing the
// selected issue closes the dialog, matching the board start dialog.
func (a *App) handleTakeoverMouse(x, y, buttons int) {
	if a.Takeover.handleWheel(x, y, buttons, a.handleIssuesMouse, func() bool {
		return a.Takeover != nil && a.Takeover.number == a.issuesSelectedNumber()
	}) {
		a.Takeover = nil
	}
}

func (a *App) takeoverTargetCurrent(dialog *takeoverState) bool {
	repository := a.issuesRepository()
	if repository == nil || *repository != dialog.repository || a.issuesSelectedNumber() != dialog.number {
		return false
	}
	card, bound := a.issuesLocalCard(dialog.number)
	if dialog.cardID == "" {
		return !bound
	}
	return bound && card.State == "done" && card.TaskID == dialog.cardID
}
