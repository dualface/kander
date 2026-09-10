package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
)

// handoffPhase tracks the modal path from reading the imported card to editing
// its contract, attesting the creation self-review, confirming the start, and
// reporting the result.
type handoffPhase int

const (
	handoffLoading handoffPhase = iota
	handoffEdit
	handoffReview
	handoffSaving
	handoffConfirm
	handoffRunning
	handoffFinished
)

// handoffFieldID addresses one editable contract part. handoffConclusion and
// handoffAttest are the self-review rows and never live in the card draft.
type handoffFieldID int

const (
	handoffType handoffFieldID = iota
	handoffSize
	handoffGoal
	handoffUserDecisions
	handoffExpectedOutcome
	handoffAcceptanceCriteria
	handoffThreatModel
	handoffOutOfScope
	handoffDiscussion
	handoffFieldCount
	handoffConclusion
	handoffAttest
)

type handoffFieldSpec struct {
	LabelKey string
	Section  string
	Field    string
	Multi    bool
}

// handoffFieldSpecs keeps the editor order: the two inferred selectors first,
// then the contract sections in the order a creator reads them.
var handoffFieldSpecs = [handoffFieldCount]handoffFieldSpec{
	{LabelKey: "tui.handoff_type", Field: board.FieldType},
	{LabelKey: "tui.handoff_size", Field: board.FieldSize},
	{LabelKey: "tui.handoff_goal", Section: board.SectionGoal, Multi: true},
	{LabelKey: "tui.handoff_user_decisions", Section: board.SectionUserDecisions, Multi: true},
	{LabelKey: "tui.handoff_expected_outcome", Section: board.SectionExpectedOutcome, Multi: true},
	{LabelKey: "tui.handoff_acceptance_criteria", Section: board.SectionAcceptanceCriteria, Multi: true},
	{LabelKey: "tui.handoff_threat_model", Section: board.SectionThreatModel, Multi: true},
	{LabelKey: "tui.handoff_out_of_scope", Section: board.SectionOutOfScope, Multi: true},
	{LabelKey: "tui.handoff_discussion", Section: board.SectionDiscussion, Multi: true},
}

func (spec handoffFieldSpec) selectValue() bool {
	return spec.Field == board.FieldType || spec.Field == board.FieldSize
}

func handoffFieldLabel(id handoffFieldID) string {
	if id == handoffConclusion {
		return t("tui.handoff_conclusion")
	}
	if id == handoffAttest {
		return t("tui.handoff_attest")
	}
	if id < 0 || id >= handoffFieldCount {
		return ""
	}
	return t(handoffFieldSpecs[id].LabelKey)
}

// handoffCard is one committed backlog card read for the contract editor.
type handoffCard struct {
	TaskID   string
	State    string
	Size     string
	Language string
	Revision uint64
	Text     string
	Warnings []string
}

// handoffSaveRequest is one controlled spec.md update from the editor.
type handoffSaveRequest struct {
	taskID   string
	revision uint64
	text     string
}

// prepareHandoffCard reads one backlog card for the editor. It is the default
// PrepareHandoff binding: the board snapshot owns the revision the later
// controlled update has to match.
func prepareHandoffCard(root, taskID string) (handoffCard, error) {
	var warnings board.WarningLog
	snapshot, err := board.ReadSnapshotWithWarnings(root, taskID, &warnings)
	if err != nil {
		return handoffCard{}, err
	}
	size := snapshot.Entry.Kind
	if size == "" {
		size = board.MetadataFrom(snapshot.Text, board.FieldSize)
	}
	return handoffCard{
		TaskID:   snapshot.Entry.TaskID,
		State:    snapshot.Entry.State,
		Size:     size,
		Language: board.MetadataFrom(snapshot.Text, board.FieldLanguage),
		Revision: snapshot.Revision,
		Text:     snapshot.Text,
		Warnings: warnings.Messages(),
	}, nil
}

// runHandoffStart is the default StartHandoff binding: the confirmed card goes
// through the same backlog -> todo move and launch as the board's `s` key, and
// the state is read back afterwards so a failure reports where the card is.
func runHandoffStart(root string, request startRequest) (launch.StartResult, string, error) {
	result, err := runTaskStart(request)
	state := ""
	if snapshot, readErr := board.ReadSnapshot(root, request.TaskID); readErr == nil {
		state = snapshot.Entry.State
	}
	return result, state, err
}

// handoffStartRunner is one confirmed backlog -> todo -> start attempt. It
// returns the card state observed after the attempt, so a failure can report
// where the card really is instead of assuming a rollback happened.
type handoffStartRunner func(request startRequest) (launch.StartResult, string, error)

// handoffState is the whole state of the handoff overlay. The issues overlay
// stays open behind it, so closing the form returns to the same selection.
type handoffState struct {
	sequence uint64
	phase    handoffPhase

	repository issue.Repository
	number     int
	issueTitle string
	taskID     string
	revision   uint64
	size       string
	language   string
	source     string

	values map[handoffFieldID]string

	focus   handoffFieldID
	editing bool
	caret   int
	scroll  int

	attest     bool
	conclusion string

	request      startRequest
	previewReady bool
	gateErr      error
	actualState  string
	notice       string
	message      string
	failed       bool
}

func (state *handoffState) value(id handoffFieldID) string {
	if id == handoffConclusion {
		return state.conclusion
	}
	return state.values[id]
}

func (state *handoffState) setValue(id handoffFieldID, value string) {
	if id == handoffConclusion {
		state.conclusion = value
		return
	}
	if state.values == nil {
		state.values = map[handoffFieldID]string{}
	}
	state.values[id] = value
}

// Every background result carries the request sequence and the issue and card
// identity; a result that no longer matches the open handoff is dropped.
type handoffLoadResult struct {
	sequence   uint64
	repository issue.Repository
	number     int
	taskID     string
	card       handoffCard
	err        error
}

type handoffImportResult struct {
	sequence   uint64
	repository issue.Repository
	number     int
	result     issue.ImportResult
	index      issue.Index
	err        error
}

type handoffSaveResult struct {
	sequence   uint64
	repository issue.Repository
	number     int
	taskID     string
	text       string
	revision   uint64
	err        error
}

type handoffPreviewResult struct {
	sequence   uint64
	repository issue.Repository
	number     int
	taskID     string
	request    startRequest
	err        error
}

type handoffStartResult struct {
	sequence uint64
	taskID   string
	result   launch.StartResult
	state    string
	err      error
}

func (a *App) issuesTitle(number int) string {
	st := a.Issues
	if st == nil || number <= 0 {
		return ""
	}
	if st.detail != nil && st.detailNumber == number {
		return st.detail.Title
	}
	for _, item := range st.items {
		if item.Number == number {
			return item.Title
		}
	}
	return ""
}

// issuesHandoff is the overlay's `s` key: it opens the contract editor of the
// card already bound to the issue, imports the issue first when no card exists,
// and refuses to start an issue whose card left backlog.
func (a *App) issuesHandoff() {
	st := a.Issues
	if st == nil || st.importing || a.Handoff != nil {
		return
	}
	number := a.issuesSelectedNumber()
	repository := a.issuesRepository()
	if number <= 0 || repository == nil {
		a.issuesSetNotice(a.Context.IssuesNoTarget)
		return
	}
	title := a.issuesTitle(number)
	if local, ok := a.issuesLocalCard(number); ok {
		if local.State != "backlog" {
			if a.issuesFocusLocalCard(local.TaskID) {
				a.showFocusNotice(t("tui.handoff_duplicate", local.TaskID, a.Context.stateLabel(local.State)))
				return
			}
			a.issuesSetNotice(a.Context.IssuesImportNoCard + ": " + local.TaskID)
			return
		}
		a.openHandoff(*repository, number, title, local.TaskID)
		return
	}
	a.handoffImport(*repository, number, title)
}

// openHandoff reads one backlog card through pendingWork, then opens the editor.
func (a *App) openHandoff(repository issue.Repository, number int, title, taskID string) {
	prepare := a.PrepareHandoff
	if prepare == nil {
		a.issuesSetNotice(t("tui.handoff_load_failed", t("tui.handoff_unavailable")))
		return
	}
	a.handoffSeq++
	sequence := a.handoffSeq
	a.Handoff = &handoffState{
		sequence: sequence, phase: handoffLoading,
		repository: repository, number: number, issueTitle: title, taskID: taskID,
		values: map[handoffFieldID]string{},
	}
	a.pendingWork = func() any {
		card, err := prepare(repository, number, taskID)
		return handoffLoadResult{sequence: sequence, repository: repository, number: number, taskID: taskID, card: card, err: err}
	}
}

// handoffImport imports the selected issue and continues into the editor. The
// import is bound to the request sequence and the issue identity; a stale
// result never opens another card.
func (a *App) handoffImport(repository issue.Repository, number int, title string) {
	st := a.Issues
	importer := a.ImportIssue
	if st == nil || st.importing {
		return
	}
	if importer == nil {
		a.issuesSetNotice(a.Context.IssuesImportFailed)
		return
	}
	a.handoffSeq++
	sequence := a.handoffSeq
	a.handoffImportSeq = sequence
	st.importing = true
	a.issuesSetNotice(a.Context.IssuesImporting)
	a.Handoff = &handoffState{
		sequence: sequence, phase: handoffLoading,
		repository: repository, number: number, issueTitle: title,
		values: map[handoffFieldID]string{},
	}
	loader := a.ImportIndex
	a.pendingWork = func() any {
		ctx, cancel := context.WithTimeout(context.Background(), issuesRequestTimeout)
		defer cancel()
		result, err := importer(ctx, repository, number, issue.ImportOptions{})
		out := handoffImportResult{sequence: sequence, repository: repository, number: number, result: result, err: err}
		if err == nil && loader != nil {
			out.index, _ = loader()
		}
		return out
	}
}

func (a *App) closeHandoff() {
	a.Handoff = nil
	a.handoffSeq++
	if a.Issues != nil {
		a.Issues.importing = false
	}
}

// reloadHandoff re-reads the card after a write conflict so the creator can
// reconcile the draft with what changed instead of retrying blindly.
func (a *App) reloadHandoff(state *handoffState) {
	prepare := a.PrepareHandoff
	if prepare == nil {
		state.phase = handoffFinished
		state.failed = true
		state.message = t("tui.handoff_load_failed", t("tui.handoff_unavailable"))
		return
	}
	a.handoffSeq++
	sequence := a.handoffSeq
	state.sequence = sequence
	state.phase = handoffLoading
	repository, number, taskID := state.repository, state.number, state.taskID
	a.pendingWork = func() any {
		card, err := prepare(repository, number, taskID)
		return handoffLoadResult{sequence: sequence, repository: repository, number: number, taskID: taskID, card: card, err: err}
	}
}

// handoffTargetMoved reports whether the issues overlay no longer selects the
// issue a background result belongs to, which makes that result stale.
func (a *App) handoffTargetMoved(repository issue.Repository, number int) bool {
	current := a.issuesRepository()
	if current == nil || current.Host != repository.Host || current.Owner != repository.Owner || current.Name != repository.Name {
		return true
	}
	return a.issuesSelectedNumber() != number
}

func (a *App) applyHandoffImport(result handoffImportResult) {
	st := a.Issues
	if result.sequence != a.handoffImportSeq {
		return
	}
	if st != nil {
		st.importing = false
	}
	state := a.Handoff
	if state == nil || result.sequence != state.sequence {
		return
	}
	if a.handoffTargetMoved(result.repository, result.number) {
		a.closeHandoff()
		return
	}
	if result.index != nil && st != nil {
		st.index = result.index
		a.LastRefresh = time.Time{}
	}
	if result.err != nil {
		a.Handoff = nil
		a.issuesSetNotice(a.Context.IssuesImportFailed + ": " + issue.Message(result.err))
		return
	}
	if result.result.State != "backlog" {
		a.Handoff = nil
		if a.issuesFocusLocalCard(result.result.TaskID) {
			a.showFocusNotice(t("tui.handoff_duplicate", result.result.TaskID, a.Context.stateLabel(result.result.State)))
			return
		}
		a.issuesSetNotice(a.Context.IssuesImportNoCard + ": " + result.result.TaskID)
		return
	}
	a.openHandoff(state.repository, state.number, state.issueTitle, result.result.TaskID)
}

func (a *App) applyHandoffLoad(result handoffLoadResult) {
	state := a.Handoff
	if state == nil || result.sequence != state.sequence || result.taskID != state.taskID {
		return
	}
	if a.handoffTargetMoved(result.repository, result.number) {
		a.closeHandoff()
		return
	}
	if result.err != nil {
		state.phase = handoffFinished
		state.failed = true
		state.message = t("tui.handoff_load_failed", issue.Message(result.err))
		return
	}
	if result.card.State != "backlog" {
		a.Handoff = nil
		if a.issuesFocusLocalCard(result.card.TaskID) {
			a.showFocusNotice(t("tui.handoff_duplicate", result.card.TaskID, a.Context.stateLabel(result.card.State)))
			return
		}
		a.issuesSetNotice(a.Context.IssuesImportNoCard + ": " + result.card.TaskID)
		return
	}
	values, err := parseHandoffValues(result.card.Text)
	if err != nil {
		state.phase = handoffFinished
		state.failed = true
		state.message = t("tui.handoff_load_failed", err.Error())
		return
	}
	state.taskID = result.card.TaskID
	state.revision = result.card.Revision
	state.size = values[handoffSize]
	state.language = result.card.Language
	state.source = result.card.Text
	state.values = values
	state.phase = handoffEdit
	state.focus = handoffType
	state.editing = false
	state.scroll = 0
	state.gateErr = nil
	state.previewReady = false
	if warnings := strings.Join(result.card.Warnings, " "); warnings != "" {
		state.notice = strings.TrimSpace(state.notice + " " + warnings)
	}
}

// handoffValidateAndReview moves from the contract draft to the self-review
// attestation. Only a complete draft passes; the validation is mechanical and
// says nothing about whether the creator's statements are true.
func (a *App) handoffValidateAndReview() {
	state := a.Handoff
	if state == nil || (state.phase != handoffEdit && state.phase != handoffReview) {
		return
	}
	if err := validateHandoffDraft(state.values); err != nil {
		state.phase = handoffEdit
		state.editing = false
		state.focus = handoffFieldForError(err)
		state.notice = err.Error()
		return
	}
	state.phase = handoffReview
	state.focus = handoffAttest
	state.editing = false
	state.scroll = 0
	state.notice = ""
}

// handoffSubmitReview queues the one controlled update that carries the edited
// contract and the creator's explicitly typed conclusion.
func (a *App) handoffSubmitReview() {
	state := a.Handoff
	if state == nil || state.phase != handoffReview {
		return
	}
	if !state.attest {
		state.notice = t("tui.handoff_attest_required")
		state.focus = handoffAttest
		return
	}
	if strings.TrimSpace(state.conclusion) == "" {
		state.notice = t("tui.handoff_conclusion_required")
		state.focus = handoffConclusion
		state.editing = true
		state.caret = runeCount(state.conclusion)
		return
	}
	save := a.SaveHandoff
	if save == nil {
		state.notice = t("tui.handoff_save_failed", t("tui.handoff_unavailable"))
		return
	}
	text, err := buildHandoffText(state)
	if err != nil {
		state.notice = err.Error()
		return
	}
	sequence, taskID, revision := state.sequence, state.taskID, state.revision
	repository, number := state.repository, state.number
	state.phase = handoffSaving
	state.editing = false
	state.notice = ""
	a.pendingWork = func() any {
		revision, err := save(handoffSaveRequest{taskID: taskID, revision: revision, text: text})
		return handoffSaveResult{sequence: sequence, repository: repository, number: number, taskID: taskID, text: text, revision: revision, err: err}
	}
}

func (a *App) applyHandoffSave(result handoffSaveResult) {
	state := a.Handoff
	if state == nil || result.sequence != state.sequence || result.taskID != state.taskID {
		return
	}
	if result.err != nil {
		if isHandoffConflict(result.err) {
			state.notice = t("tui.handoff_conflict")
			a.reloadHandoff(state)
			return
		}
		state.phase = handoffReview
		state.notice = t("tui.handoff_save_failed", issue.Message(result.err))
		return
	}
	state.source = result.text
	if result.revision > 0 {
		state.revision = result.revision
	}
	state.gateErr = board.ValidateTodoContract(state.size, result.text)
	state.phase = handoffConfirm
	state.notice = ""
	state.previewReady = false
	a.queueHandoffPreview(state)
}

func isHandoffConflict(err error) bool {
	var boardErr *board.Error
	return errors.As(err, &boardErr) && boardErr.Code == "board.transaction_conflict"
}

// queueHandoffPreview resolves the agent and launcher through the same
// read-only preview the board's `s` key uses, so both paths agree on defaults.
func (a *App) queueHandoffPreview(state *handoffState) {
	prepare := a.PrepareStart
	sequence, taskID := state.sequence, state.taskID
	repository, number := state.repository, state.number
	if prepare == nil {
		state.notice = t("tui.handoff_preview_failed", t("tui.handoff_unavailable"))
		return
	}
	a.pendingWork = func() any {
		request, err := prepare(taskID)
		return handoffPreviewResult{sequence: sequence, repository: repository, number: number, taskID: taskID, request: request, err: err}
	}
}

func (a *App) applyHandoffPreview(result handoffPreviewResult) {
	state := a.Handoff
	if state == nil || result.sequence != state.sequence || result.taskID != state.taskID {
		return
	}
	if result.err != nil {
		state.notice = t("tui.handoff_preview_failed", issue.Message(result.err))
		return
	}
	state.request = result.request
	state.previewReady = true
	state.notice = ""
}

// handoffStartConfirmed runs the confirmed backlog -> todo -> launch attempt.
// The card is never moved to working first; launch.Start owns that claim after
// its own preflight.
func (a *App) handoffStartConfirmed() {
	state := a.Handoff
	if state == nil || state.phase != handoffConfirm || !state.previewReady {
		return
	}
	if state.gateErr != nil {
		return
	}
	if !backgroundStartLauncher(state.request.Launcher) {
		state.notice = t("tui.start_use_cli", state.request.Launcher)
		return
	}
	run := a.StartHandoff
	if run == nil {
		state.notice = t("tui.handoff_unavailable")
		return
	}
	sequence, request := state.sequence, state.request
	state.phase = handoffRunning
	state.notice = ""
	a.pendingWork = func() any {
		result, actual, err := run(request)
		return handoffStartResult{sequence: sequence, taskID: request.TaskID, result: result, state: actual, err: err}
	}
}

func (a *App) applyHandoffStart(result handoffStartResult) {
	state := a.Handoff
	if state == nil || result.sequence != state.sequence || result.taskID != state.taskID {
		return
	}
	a.refreshBoard()
	state.phase = handoffFinished
	state.actualState = result.state
	if result.err != nil {
		state.failed = true
		actual := a.Context.stateLabel(result.state)
		if actual == "" {
			actual = t("tui.handoff_state_unknown")
		}
		state.message = t("tui.handoff_start_failed", issue.Message(result.err), actual)
		return
	}
	r := result.result
	address := r.Outcome.Tab + ":" + r.Outcome.Pane
	if r.Plan.Launcher != "herdr" {
		address = r.Plan.Session + ":" + r.Outcome.Window + ":" + r.Outcome.Pane
	}
	state.failed = false
	state.message = t("tui.start_success", r.TaskID, r.Agent, r.Plan.Launcher, address)
	for _, warning := range r.Warnings {
		state.message += " " + warning
	}
}
