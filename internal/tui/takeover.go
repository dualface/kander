package tui

import (
	"context"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"

	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
)

type takeoverPhase int

const (
	takeoverLoading takeoverPhase = iota
	takeoverReady
	takeoverRunning
	takeoverFinished
)

// takeoverState is the confirmation dialog of the issues overlay `s` key. It
// replaces the removed handoff form: the TUI starts the same takeover session
// the `kander issue triage` command starts, and everything about the card
// contract happens inside that session with the user.
type takeoverState struct {
	sequence   uint64
	phase      takeoverPhase
	repository issue.Repository
	number     int
	// cardID is the backlog card that already carries the issue; empty means
	// the session imports a new card after the user agrees.
	cardID   string
	agent    string
	launcher string
	// blockedReason explains why the contract-start exit cannot start from the
	// TUI (failed preview or a launcher that needs the caller's terminal). The
	// dialog and its jump exit stay usable; the dialog shows the reason, and the
	// start exit reports it again as its result when it is used.
	blockedReason string
	failed        bool
	message       string
	bodyView      viewport.Model
}

// takeoverPreviewResult carries the resolved agent and launcher back to the
// dialog. The request is bound to the dialog sequence, so a result of a closed
// or replaced dialog is dropped.
type takeoverPreviewResult struct {
	sequence uint64
	preview  launch.TriagePreview
	err      error
}

// takeoverResult carries one finished start attempt. Only the dialog that
// started it may consume the result.
type takeoverResult struct {
	sequence uint64
	outcome  issue.TriageOutcome
	err      error
}

// takeoverIdentity renders the confirmed identity of one issue. It is built
// from the validated repository fields and the issue number only, never from
// remote text.
func takeoverIdentity(repository issue.Repository, number int) string {
	return repository.Owner + "/" + repository.Name + "#" + strconv.Itoa(number)
}

// issuesTakeover is the `s` key of the overlay. A bound card jumps (backlog
// opens the confirmation dialog instead, because that card can also complete
// its contract); an unbound issue opens the dialog that starts a takeover.
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
	if local, ok := a.issuesLocalCard(number); ok {
		if local.State == "backlog" {
			a.openTakeover(*repository, number, local.TaskID)
			return
		}
		if a.issuesFocusLocalCard(local.TaskID) {
			a.showFocusNotice(t("tui.issues_bound_state", local.TaskID, a.Context.stateLabel(local.State)))
			return
		}
		a.issuesSetNotice(a.Context.IssuesImportNoCard + ": " + local.TaskID)
		return
	}
	a.openTakeover(*repository, number, "")
}

// openTakeover shows the dialog and resolves the configured agent and launcher
// in the background. The dialog appears immediately, so a slow configuration
// read never blocks the key press.
func (a *App) openTakeover(repository issue.Repository, number int, cardID string) {
	a.takeoverSeq++
	dialog := &takeoverState{
		sequence:   a.takeoverSeq,
		phase:      takeoverLoading,
		repository: repository,
		number:     number,
		cardID:     cardID,
	}
	a.Takeover = dialog
	prepare := a.PrepareTriage
	if prepare == nil {
		// Tests may build an App without the binding; the start still uses the
		// configured defaults, the dialog just cannot show them in advance.
		dialog.phase = takeoverReady
		return
	}
	sequence := dialog.sequence
	a.pendingWork = func() any {
		preview, err := prepare()
		return takeoverPreviewResult{sequence: sequence, preview: preview, err: err}
	}
}

// applyTakeoverPreview fills the resolved settings in. A bound backlog card
// keeps its dialog even when the preview fails or the launcher needs the
// terminal of the caller: its default jump exit never depends on the preview,
// and only the contract-start exit is disabled with the reason recorded. An
// unbound issue has no jump exit, so it is refused here exactly like the board
// start dialog and points at the CLI instead.
func (a *App) applyTakeoverPreview(result takeoverPreviewResult) {
	dialog := a.Takeover
	if dialog == nil || dialog.sequence != result.sequence || dialog.phase != takeoverLoading {
		return
	}
	bound := dialog.cardID != ""
	if result.err != nil {
		reason := t("tui.issues_takeover_preview_failed", issue.TriageError(result.err))
		if bound {
			dialog.blockedReason = reason
			dialog.phase = takeoverReady
			return
		}
		a.Takeover = nil
		a.issuesSetNotice(reason)
		return
	}
	dialog.agent, dialog.launcher = result.preview.Agent, result.preview.Launcher
	if !backgroundStartLauncher(result.preview.Launcher) {
		reason := t("tui.start_use_cli", result.preview.Launcher)
		if bound {
			dialog.blockedReason = reason
			dialog.phase = takeoverReady
			return
		}
		a.Takeover = nil
		a.issuesSetNotice(reason)
		return
	}
	dialog.phase = takeoverReady
}

func (a *App) handleTakeoverKey(key string) {
	dialog := a.Takeover
	if dialog == nil {
		return
	}
	switch dialog.phase {
	case takeoverRunning:
		// The request is already in flight; the dialog owns the input until its
		// result lands, so a stray key cannot orphan the session it started.
		return
	case takeoverFinished:
		a.Takeover = nil
		return
	case takeoverLoading:
		if key == "esc" || key == "n" || key == "N" {
			a.Takeover = nil
		}
		return
	}
	switch key {
	case "esc", "n", "N":
		a.Takeover = nil
	case "y", "enter":
		// On a bound backlog card the default exit jumps to the card; the
		// contract session is the explicit `s` alternative.
		if dialog.cardID != "" {
			a.jumpToBoundCard(dialog)
			return
		}
		a.startTakeover(dialog)
	case "s", "S":
		if dialog.cardID != "" {
			a.startTakeover(dialog)
		}
	}
}

// jumpToBoundCard is the default exit of the bound-card dialog: focus the card
// on the board and report its state, so the user sees where the work is.
func (a *App) jumpToBoundCard(dialog *takeoverState) {
	card := dialog.cardID
	state := ""
	if local, ok := a.issuesLocalCard(dialog.number); ok {
		state = a.Context.stateLabel(local.State)
	}
	a.Takeover = nil
	if a.issuesFocusLocalCard(card) {
		a.showFocusNotice(t("tui.issues_bound_state", card, state))
		return
	}
	a.issuesSetNotice(a.Context.IssuesImportNoCard + ": " + card)
}

// startTakeover runs the shared StartTriage path in the background; the TUI
// never touches the board and never blocks on the network or the container.
func (a *App) startTakeover(dialog *takeoverState) {
	if dialog.blockedReason != "" {
		// A bound card disabled its start exit instead of closing the dialog;
		// the start attempt ends with that reason as its result.
		dialog.phase = takeoverFinished
		dialog.failed = true
		dialog.message = dialog.blockedReason
		return
	}
	runner := a.TriageIssue
	if runner == nil {
		dialog.phase = takeoverFinished
		dialog.failed = true
		dialog.message = t("tui.issues_takeover_unavailable")
		return
	}
	// The agent and launcher the dialog showed and validated are passed on, so a
	// configuration change after the preview cannot start a different pair, and
	// never a launcher that needs the caller's terminal.
	options := issue.TriageOptions{CardID: dialog.cardID, Agent: dialog.agent, Launcher: dialog.launcher}
	sequence, repository, number := dialog.sequence, dialog.repository, dialog.number
	dialog.phase = takeoverRunning
	dialog.failed = false
	dialog.message = ""
	a.pendingWork = func() any {
		ctx, cancel := context.WithTimeout(context.Background(), issuesTriageTimeout)
		defer cancel()
		outcome, err := runner(ctx, repository, number, options)
		return takeoverResult{sequence: sequence, outcome: outcome, err: err}
	}
}

func (a *App) applyTakeoverResult(result takeoverResult) {
	dialog := a.Takeover
	if dialog == nil || dialog.sequence != result.sequence {
		return
	}
	dialog.phase = takeoverFinished
	if result.err != nil {
		dialog.failed = true
		dialog.message = t("tui.issues_takeover_failed", issue.TriageError(result.err))
		return
	}
	dialog.failed = false
	dialog.agent, dialog.launcher = result.outcome.Agent, result.outcome.Launcher
	address := strings.TrimSpace(result.outcome.Address)
	if address == "" {
		address = orDash(address)
	}
	message := t("tui.issues_takeover_started", result.outcome.Agent, result.outcome.Launcher, address)
	if len(result.outcome.Warnings) > 0 {
		message += "\n" + strings.Join(result.outcome.Warnings, "\n")
	}
	dialog.message = message
}

func takeoverTitle(dialog *takeoverState) string {
	switch dialog.phase {
	case takeoverLoading:
		return t("tui.start_loading")
	case takeoverRunning:
		return t("tui.start_title_starting")
	case takeoverFinished:
		if dialog.failed {
			return t("tui.start_title_failed")
		}
		return t("tui.start_title_started")
	}
	if dialog.cardID != "" {
		return t("tui.issues_contract_title", itoa(dialog.number), dialog.cardID)
	}
	return t("tui.issues_takeover_title", itoa(dialog.number))
}

func (a *App) renderTakeover() (popupBox, string) {
	dialog := a.Takeover
	identity := takeoverIdentity(dialog.repository, dialog.number)
	paragraphs := []string{}
	hint := t("tui.issues_takeover_keys")
	switch dialog.phase {
	case takeoverLoading:
		placeholder := t("tui.start_loading")
		paragraphs = append(paragraphs, t("tui.issues_takeover_loading"), t("tui.start_settings", placeholder, placeholder))
		hint = t("tui.issues_takeover_loading_keys")
	case takeoverRunning:
		paragraphs = append(paragraphs, t("tui.start_settings", dialog.agent, dialog.launcher))
		hint = t("tui.issues_takeover_starting", identity)
	case takeoverFinished:
		paragraphs = []string{dialog.message}
		hint = t("tui.start_result_keys")
	default:
		if dialog.cardID != "" {
			state := ""
			if local, ok := a.issuesLocalCard(dialog.number); ok {
				state = a.Context.stateLabel(local.State)
			}
			paragraphs = append(paragraphs, t("tui.issues_contract_body", dialog.cardID, state))
			hint = t("tui.issues_contract_keys",
				t("tui.issues_contract_jump"),
				t("tui.issues_contract_start"),
				t("tui.issues_contract_cancel"),
			)
		} else {
			paragraphs = append(paragraphs, t("tui.issues_takeover_body", identity))
		}
		// A failed preview leaves no settings to show; an empty settings line
		// would read as an empty configuration, so only the reason is shown.
		if dialog.agent != "" || dialog.launcher != "" {
			paragraphs = append(paragraphs, t("tui.start_settings", dialog.agent, dialog.launcher))
		}
		if dialog.blockedReason != "" {
			paragraphs = append(paragraphs, dialog.blockedReason)
		}
	}
	return a.renderStartDialog(paragraphs, hint, takeoverTitle(dialog), &dialog.bodyView)
}

// handleTakeoverMouse scrolls the dialog body. While the settings are still
// loading the wheel keeps operating the overlay underneath, and changing the
// selected issue closes the dialog, matching the board start dialog.
func (a *App) handleTakeoverMouse(x, y, buttons int) {
	dialog := a.Takeover
	delta := mouseWheelDelta(buttons)
	if delta == 0 {
		return
	}
	if dialog.phase == takeoverLoading {
		a.handleIssuesMouse(x, y, buttons)
		if dialog.number != a.issuesSelectedNumber() {
			a.Takeover = nil
		}
		return
	}
	if delta > 0 {
		dialog.bodyView.ScrollDown(delta)
	} else {
		dialog.bodyView.ScrollUp(-delta)
	}
}
