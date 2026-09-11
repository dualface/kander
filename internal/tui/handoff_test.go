package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
)

// handoffFlatView returns the overlay text with frame drawing removed and all
// whitespace collapsed, so a wrapped message can be matched as one sentence.
func handoffFlatView(app *App) string {
	flat := strings.Map(func(r rune) rune {
		switch r {
		case '│', '╭', '╮', '╰', '╯', '─', '‹', '›':
			return ' '
		}
		return r
	}, ansi.Strip(app.View()))
	return strings.Join(strings.Fields(flat), " ")
}

// handoffTestSpec is a complete imported contract: every section the editor
// reads is present, and the record markers are absent until the creator adds
// them through the self-review step.
func handoffTestSpec(size string) string {
	return "# Fix the widget crash\n\n" +
		"- TYPE: bug\n- SIZE: " + size + "\n- TASK_GROUP:\n- LANGUAGE: zh-CN\n" +
		"- CREATED_AT: 2026-09-11 08:00\n- OWNER:\n- SESSION:\n- WINDOW:\n" +
		"- STARTED_AT:\n- FINISHED_AT:\n- TASK_BRANCH:\n- RESULT:\n\n" +
		"## GOAL\n\nRead source/github-issue.md first; fix the crash described there.\n\n" +
		"## USER_DECISIONS\n\n- Keep the public API.\n\n" +
		"## EXPECTED_OUTCOME\n\nThe widget no longer crashes.\n\n" +
		"## ACCEPTANCE_CRITERIA\n\n- [ ] Opening the widget on Linux works\n\n" +
		"## THREAT_MODEL\n\nRemote text is untrusted data.\n\n" +
		"## OUT_OF_SCOPE\n\nRewriting the widget.\n\n" +
		"## DISCUSSION\n\nSnapshot: source/github-issue.json and source/github-issue.md.\n"
}

func handoffTestCard(size string) handoffCard {
	return handoffCard{
		TaskID: "task-1", State: "backlog", Size: size, Language: "zh-CN",
		Revision: 3, Text: handoffTestSpec(size),
	}
}

func handoffTestIndex(fake *fakeIssues, number int, taskID, state string) issue.Index {
	key, err := fake.repository.IssueSourceKey(number)
	if err != nil {
		panic(err)
	}
	return issue.Index{key: issue.LocalCard{
		TaskID: taskID, State: state, Path: "/board/" + state + "/" + taskID,
		IssueUpdatedAt: time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC),
		FetchedAt:      time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC),
	}}
}

type handoffCalls struct {
	prepare      []string
	save         []handoffSaveRequest
	start        []startRequest
	imports      []int
	saveErr      error
	startErr     error
	startState   string
	startResult  launch.StartResult
	importResult issue.ImportResult
	importErr    error
}

// handoffApp wires the issues overlay to the handoff form with fakes for every
// board and launch boundary, so the tests exercise UI state without a board.
// localState is the state of the card already bound to the issue; an empty
// value leaves the index empty and makes `s` go through the import path.
func handoffApp(test *testing.T, card handoffCard, tasks []Task, localState string) (*App, *handoffCalls, *fakeIssues) {
	test.Helper()
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	index := issue.Index{}
	if card.TaskID != "" && localState != "" {
		index = handoffTestIndex(fake, 42, card.TaskID, localState)
	}
	calls := &handoffCalls{}
	importer := func(_ context.Context, _ issue.Repository, number int, _ issue.ImportOptions) (issue.ImportResult, error) {
		calls.imports = append(calls.imports, number)
		return calls.importResult, calls.importErr
	}
	app, _ := issuesImportApp(test, fake, tasks, index, importer)
	app.PrepareHandoff = func(_ issue.Repository, _ int, taskID string) (handoffCard, error) {
		calls.prepare = append(calls.prepare, taskID)
		return card, nil
	}
	app.SaveHandoff = func(request handoffSaveRequest) (uint64, error) {
		calls.save = append(calls.save, request)
		if calls.saveErr != nil {
			return 0, calls.saveErr
		}
		return request.revision + 1, nil
	}
	app.StartHandoff = func(request startRequest) (launch.StartResult, string, error) {
		calls.start = append(calls.start, request)
		return calls.startResult, calls.startState, calls.startErr
	}
	app.PrepareStart = func(id string) (startRequest, error) {
		return startRequest{
			StartPreview: launch.StartPreview{TaskID: id, State: "backlog", Size: card.Size, Agent: "claude", Launcher: "herdr"},
			root:         "/board",
		}, nil
	}
	app.HandleKey("g")
	runPendingWork(test, app)
	return app, calls, fake
}

// openHandoffForm drives the overlay keys up to the editable contract.
func openHandoffForm(test *testing.T, app *App) {
	test.Helper()
	app.HandleKey("s")
	runPendingWork(test, app)
	if app.Handoff == nil || app.Handoff.phase != handoffEdit {
		test.Fatalf("handoff form did not open: %+v", app.Handoff)
	}
}

// submitSelfReview drives the form through validation, attestation and the
// controlled save, stopping after the preview result lands.
func submitSelfReview(test *testing.T, app *App, conclusion string) {
	test.Helper()
	state := app.Handoff
	if state == nil {
		test.Fatal("no handoff state")
	}
	state.attest = true
	state.conclusion = conclusion
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	if state.phase != handoffSaving {
		test.Fatalf("save was not queued: phase %d notice %q", state.phase, state.notice)
	}
	runPendingWork(test, app)
	if state.phase != handoffConfirm {
		test.Fatalf("save result did not reach the confirmation: phase %d notice %q", state.phase, state.notice)
	}
	runPendingWork(test, app)
	if !state.previewReady {
		test.Fatalf("preview did not resolve: notice %q", state.notice)
	}
}

func TestHandoffOpensTheEditableContract(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	if len(calls.prepare) != 1 || calls.prepare[0] != "task-1" {
		test.Fatalf("prepare calls %+v", calls.prepare)
	}
	state := app.Handoff
	if state.taskID != "task-1" || state.revision != 3 || state.language != "zh-CN" || state.size != "small" {
		test.Fatalf("loaded identity %+v", state)
	}
	view := ansi.Strip(app.View())
	for _, want := range []string{"GOAL", "ACCEPTANCE_CRITERIA", "DISCUSSION", t("tui.handoff_undecided"), "LANGUAGE: zh-CN"} {
		if !strings.Contains(view, want) {
			test.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestHandoffFormValidationBlocksIncompleteDraft(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	state.values[handoffGoal] = ""
	app.HandleKey("ctrl-s")
	if state.phase != handoffEdit || state.focus != handoffGoal {
		test.Fatalf("empty goal accepted: phase %d focus %d", state.phase, state.focus)
	}
	if state.notice != t("tui.handoff_empty_field", "GOAL") {
		test.Fatalf("notice %q", state.notice)
	}
	state.values[handoffGoal] = "Fix the crash."
	app.HandleKey("ctrl-s")
	if state.phase != handoffEdit || state.focus != handoffGoal || state.notice != t("tui.handoff_source_required") {
		test.Fatalf("dropped source requirement accepted: %q", state.notice)
	}
	state.values[handoffGoal] = "Read source/github-issue.md and fix the crash."
	state.values[handoffAcceptanceCriteria] = "the widget works"
	app.HandleKey("ctrl-s")
	if state.phase != handoffEdit || state.focus != handoffAcceptanceCriteria ||
		state.notice != t("tui.handoff_acceptance_items") {
		test.Fatalf("acceptance without items accepted: %q", state.notice)
	}
	state.values[handoffAcceptanceCriteria] = "- [ ] the widget works"
	app.HandleKey("ctrl-s")
	if state.phase != handoffReview || state.focus != handoffAttest {
		test.Fatalf("valid draft did not reach the self-review: phase %d", state.phase)
	}
	if len(calls.save) != 0 {
		test.Fatal("validation wrote the card")
	}
}

func TestHandoffSelfReviewRecordsTheTypedConclusion(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	if state.notice != t("tui.handoff_attest_required") {
		test.Fatalf("missing attestation accepted: %q", state.notice)
	}
	app.HandleKey(" ")
	if !state.attest {
		test.Fatal("space did not attest")
	}
	app.HandleKey("ctrl-s")
	if !state.editing || state.focus != handoffConclusion || state.notice != t("tui.handoff_conclusion_required") {
		test.Fatalf("empty conclusion accepted: %+v", state)
	}
	for _, key := range []string{"s", "e", "l", "f", "-", "o", "k"} {
		app.HandleKey(key)
	}
	app.HandleKey("ctrl-s")
	if state.phase != handoffSaving {
		test.Fatalf("save was not queued: phase %d notice %q", state.phase, state.notice)
	}
	runPendingWork(test, app)
	if len(calls.save) != 1 {
		test.Fatalf("save calls %+v", calls.save)
	}
	saved := calls.save[0]
	if saved.taskID != "task-1" || saved.revision != 3 {
		test.Fatalf("save request %+v", saved)
	}
	if !strings.Contains(saved.text, "- SELF_REVIEW: self-ok") {
		test.Fatalf("self-review conclusion missing:\n%s", saved.text)
	}
	if strings.Contains(saved.text, "CARD_REVIEW") {
		test.Fatalf("the tool generated a CARD_REVIEW record:\n%s", saved.text)
	}
	if state.gateErr != nil {
		test.Fatalf("small card gate failed: %v", state.gateErr)
	}
	runPendingWork(test, app)
	if !state.previewReady || state.request.Agent != "claude" || state.request.Launcher != "herdr" {
		test.Fatalf("preview %+v", state.request)
	}
	view := ansi.Strip(app.View())
	for _, want := range []string{"task-1", "claude", "herdr", t("tui.handoff_gate_ready")} {
		if !strings.Contains(view, want) {
			test.Fatalf("confirmation missing %q:\n%s", want, view)
		}
	}
}

func TestHandoffCancelKeepsTheCardAndReopens(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	app.HandleKey("esc")
	if app.Handoff != nil || len(calls.save) != 0 {
		test.Fatalf("cancel wrote the card or kept the form: %+v", app.Handoff)
	}
	if app.Issues == nil {
		test.Fatal("cancel closed the issues overlay")
	}
	openHandoffForm(test, app)
	if len(calls.prepare) != 2 {
		test.Fatalf("reopen did not read the card again: %+v", calls.prepare)
	}
}

func TestHandoffWriteConflictReloadsTheCard(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	state.attest = true
	state.conclusion = "reviewed"
	app.handoffValidateAndReview()
	calls.saveErr = &board.Error{Code: "board.transaction_conflict", Message: "card changed"}
	app.HandleKey("ctrl-s")
	runPendingWork(test, app)
	if state.phase != handoffLoading || state.notice != t("tui.handoff_conflict") {
		test.Fatalf("conflict did not reload: phase %d notice %q", state.phase, state.notice)
	}
	runPendingWork(test, app)
	if state.phase != handoffEdit || state.notice != t("tui.handoff_conflict") {
		test.Fatalf("reload lost the notice: phase %d notice %q", state.phase, state.notice)
	}
	if len(calls.prepare) != 2 {
		test.Fatalf("reload did not read the card: %+v", calls.prepare)
	}
	if calls.prepare[1] != "task-1" {
		test.Fatalf("reload read another card: %+v", calls.prepare)
	}
}

func TestHandoffRefusesCardsThatLeftBacklog(test *testing.T) {
	card := handoffTestCard("small")
	tasks := []Task{{TaskID: "task-1", Title: "Task", State: "working"}}
	app, calls, _ := handoffApp(test, card, tasks, "working")
	app.HandleKey("s")
	if app.Handoff != nil || len(calls.prepare) != 0 || app.pendingWork != nil {
		test.Fatal("non-backlog card opened the form")
	}
	if app.Issues != nil {
		test.Fatal("refusal did not jump to the card")
	}
	if !strings.Contains(app.CopyNotice, t("tui.handoff_duplicate", "task-1", t("tui.working"))) {
		test.Fatalf("notice %q", app.CopyNotice)
	}
	if selected := app.Model.SelectedTask(); selected == nil || selected.TaskID != "task-1" {
		test.Fatalf("selection %+v", selected)
	}
}

func TestHandoffLargeCardNeedsAnIndependentReviewRecord(test *testing.T) {
	card := handoffTestCard("large")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	submitSelfReview(test, app, "creator checked")
	state := app.Handoff
	if state.gateErr == nil {
		test.Fatal("large card without CARD_REVIEW passed the todo gate")
	}
	// Compare the rendered hint with the catalog text ignoring whitespace: the
	// popup wrap may break a sentence inside a word, and another test in the
	// package may already have switched the active language.
	compact := func(text string) string { return strings.Join(strings.Fields(text), "") }
	flat := handoffFlatView(app)
	if !strings.Contains(compact(flat), compact(t("tui.handoff_gate_review_hint"))) {
		test.Fatalf("gate hint missing:\n%s", flat)
	}
	if !strings.Contains(flat, "CARD_REVIEW") {
		test.Fatalf("gate hint does not name the review record:\n%s", flat)
	}
	app.HandleKey("y")
	if len(calls.start) != 0 || app.pendingWork != nil {
		test.Fatal("blocked card was started")
	}
	// An independent reviewer added the record; the reopened form passes.
	card.Text = strings.Replace(card.Text, "## DISCUSSION\n\n", "## DISCUSSION\n\n- CARD_REVIEW: independent agent checked the contract\n\n", 1)
	app.PrepareHandoff = func(_ issue.Repository, _ int, taskID string) (handoffCard, error) {
		calls.prepare = append(calls.prepare, taskID)
		return card, nil
	}
	app.HandleKey("esc")
	openHandoffForm(test, app)
	submitSelfReview(test, app, "creator re-checked")
	if app.Handoff.gateErr != nil {
		test.Fatalf("recorded review still blocked: %v", app.Handoff.gateErr)
	}
	if !strings.Contains(app.Handoff.source, "- CARD_REVIEW: independent agent checked the contract") {
		test.Fatal("reload lost the review record")
	}
}

func TestHandoffStartSuccessAndFailureReportTheRealState(test *testing.T) {
	for _, tc := range []struct {
		name      string
		failed    bool
		state     string
		wantKey   string
		wantState string
	}{
		{name: "success", wantKey: "success"},
		{name: "failure", failed: true, state: "todo", wantKey: "failure", wantState: "todo"},
	} {
		test.Run(tc.name, func(test *testing.T) {
			card := handoffTestCard("small")
			app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
			openHandoffForm(test, app)
			submitSelfReview(test, app, "checked")
			calls.startResult = launch.StartResult{
				TaskID: "task-1", Agent: "claude",
				Plan:    launch.LaunchPlan{Launcher: "herdr", Session: "s"},
				Outcome: launch.LaunchOutcome{Tab: "t", Pane: "p"},
			}
			if tc.failed {
				calls.startErr = errors.New("launch failed")
				calls.startState = tc.state
			}
			app.HandleKey("y")
			if app.Handoff.phase != handoffRunning || len(calls.start) != 0 {
				test.Fatal("start ran on the UI thread")
			}
			runPendingWork(test, app)
			state := app.Handoff
			if state.phase != handoffFinished || state.failed != tc.failed {
				test.Fatalf("result %+v", state)
			}
			if len(calls.start) != 1 {
				test.Fatalf("start calls %+v", calls.start)
			}
			request := calls.start[0]
			if request.TaskID != "task-1" || request.State != "backlog" || request.Agent != "claude" || request.Launcher != "herdr" {
				test.Fatalf("start request %+v", request)
			}
			view := ansi.Strip(app.View())
			if tc.failed {
				want := t("tui.handoff_start_failed", "launch failed", t("tui.todo"))
				if !strings.Contains(view, want) || !strings.Contains(view, t("tui.handoff_retry_board")) {
					test.Fatalf("failure view missing %q:\n%s", want, view)
				}
			} else if want := t("tui.start_success", "task-1", "claude", "herdr", "t:p"); !strings.Contains(view, want) {
				test.Fatalf("success view missing %q:\n%s", want, view)
			}
			app.HandleKey("x")
			if app.Handoff != nil {
				test.Fatal("result was not closed by a key")
			}
		})
	}
}

func TestHandoffStaleResultsAreDropped(test *testing.T) {
	card := handoffTestCard("small")
	app, _, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	app.HandleKey("s")
	stale := app.pendingWork
	if stale == nil {
		test.Fatal("no cancelable load queued")
	}
	app.HandleKey("esc")
	// A late result from the cancelled form must not reopen it.
	app.applyWork(stale())
	if app.Handoff != nil || app.Issues.importing {
		test.Fatal("stale load landed on a closed form")
	}
	// A result whose issue is no longer selected is dropped as well.
	app.HandleKey("s")
	staleTarget := app.pendingWork
	if staleTarget == nil {
		test.Fatal("no load queued for the second target")
	}
	app.Issues.selected = 1
	app.applyWork(staleTarget())
	if app.Handoff != nil {
		test.Fatal("result for a previous selection stayed open")
	}

	// The same guard covers the import path.
	app2, calls2, _ := handoffApp(test, handoffTestCard("small"), nil, "")
	app2.HandleKey("s")
	staleImport := app2.pendingWork
	if staleImport == nil {
		test.Fatal("no import queued")
	}
	app2.HandleKey("esc")
	app2.applyWork(staleImport())
	if app2.Handoff != nil || app2.Issues.importing {
		test.Fatal("stale import landed on a closed form")
	}
	if len(calls2.imports) != 1 {
		test.Fatalf("import calls %+v", calls2.imports)
	}
}

func TestHandoffImportThenFormBindsTheCreatedCard(test *testing.T) {
	app, calls, _ := handoffApp(test, handoffTestCard("small"), nil, "")
	calls.importResult = issue.ImportResult{
		TaskID: "20260911-gh-dualface-kander-42-task", State: "backlog",
		Existing: false, SourceKey: "github://github.com/dualface/kander/issues/42",
	}
	app.HandleKey("s")
	if app.Handoff == nil || app.Handoff.phase != handoffLoading || !app.Issues.importing {
		test.Fatalf("import did not start: %+v", app.Handoff)
	}
	runPendingWork(test, app)
	if len(calls.imports) != 1 || calls.imports[0] != 42 {
		test.Fatalf("import calls %+v", calls.imports)
	}
	// The import result opens the form for the created task through pendingWork.
	if app.Handoff == nil || app.Handoff.taskID != "20260911-gh-dualface-kander-42-task" {
		test.Fatalf("created card not bound: %+v", app.Handoff)
	}
	if app.Issues.importing {
		test.Fatal("importing flag was not cleared")
	}
	runPendingWork(test, app)
	if app.Handoff.phase != handoffEdit {
		test.Fatalf("form did not open for the created card: phase %d", app.Handoff.phase)
	}
}

func TestHandoffImportFailureReportsAndAllowsRetry(test *testing.T) {
	app, calls, _ := handoffApp(test, handoffTestCard("small"), nil, "")
	calls.importErr = errors.New("gh: rate limited\x1b[2J")
	app.HandleKey("s")
	runPendingWork(test, app)
	if app.Handoff != nil {
		test.Fatal("failed import kept the form")
	}
	notice := app.Issues.notice
	if !strings.Contains(notice, t("tui.issues_import_failed")) || strings.ContainsAny(notice, "\x1b") {
		test.Fatalf("notice %q", notice)
	}
	app.HandleKey("s")
	if app.Handoff == nil {
		test.Fatal("retry did not start")
	}
	runPendingWork(test, app)
	if len(calls.imports) != 2 {
		test.Fatalf("retry calls %+v", calls.imports)
	}
}

func TestHandoffWritesNothingToTheTerminal(test *testing.T) {
	card := handoffTestCard("small")
	app, _, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	app.HandleKey("s")
	cmd := app.takePending()
	if cmd == nil {
		test.Fatal("no handoff work queued")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer
	message := cmd()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	writer.Close()
	written, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		test.Fatal(err)
	}
	if len(written) != 0 {
		test.Fatalf("background handoff wrote to the terminal: %q", written)
	}
	if _, ok := message.(workMsg); !ok {
		test.Fatalf("unexpected message %T", message)
	}
}

func TestHandoffBuildKeepsManagedRecords(test *testing.T) {
	source := handoffTestSpec("small") +
		"\n- CARD_REVIEW: independent reviewer concluded ok\n\nSELF_REVIEW: old conclusion\n"
	values, err := parseHandoffValues(source)
	if err != nil {
		test.Fatal(err)
	}
	state := &handoffState{source: source, values: values, conclusion: "new conclusion"}
	text, err := buildHandoffText(state)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(text, "old conclusion") {
		test.Fatal("the previous self-review line was kept")
	}
	if strings.Count(text, "SELF_REVIEW") != 1 || !strings.Contains(text, "- SELF_REVIEW: new conclusion") {
		test.Fatalf("self-review record wrong:\n%s", text)
	}
	if !strings.Contains(text, "- CARD_REVIEW: independent reviewer concluded ok") {
		test.Fatalf("CARD_REVIEW was rewritten:\n%s", text)
	}
	if !strings.Contains(text, "- LANGUAGE: zh-CN") || !strings.Contains(text, "- TYPE: bug") {
		test.Fatalf("managed fields changed:\n%s", text)
	}
	if !strings.Contains(text, "## USER_DECISIONS\n\n- Keep the public API.") {
		test.Fatalf("untouched section changed:\n%s", text)
	}
}

func TestHandoffFormLayoutFitsNarrowTerminalsAndNarrowWidths(test *testing.T) {
	card := handoffTestCard("small")
	app, _, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	for _, size := range [][2]int{{120, 30}, {60, 18}, {40, 12}, {30, 8}, {20, 6}} {
		app.Width, app.Height = size[0], size[1]
		_, popup := app.renderHandoff()
		for _, line := range strings.Split(ansi.Strip(popup), "\n") {
			if ansi.StringWidth(line) > size[0] {
				test.Fatalf("width %d overflow: %q", size[0], line)
			}
		}
	}
	// The focused field stays visible while focus walks the whole form.
	app.Width, app.Height = 80, 14
	for i := 0; i < int(handoffFieldCount); i++ {
		app.HandleKey("down")
		_, popup := app.renderHandoff()
		label := handoffFieldLabel(app.Handoff.focus)
		if !strings.Contains(ansi.Strip(popup), label) {
			test.Fatalf("focused field %q off screen:\n%s", label, ansi.Strip(popup))
		}
	}
}

func TestHandoffEditorKeepsTheCaretOnScreen(test *testing.T) {
	card := handoffTestCard("small")
	card.Text = strings.Replace(card.Text, "Read source/github-issue.md first; fix the crash described there.",
		strings.Repeat("line of the goal\n", 40)+"Read source/github-issue.md first.", 1)
	app, _, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	app.Handoff.focus = handoffGoal
	app.HandleKey("enter")
	if !app.Handoff.editing || app.Handoff.focus != handoffGoal {
		test.Fatal("field editor did not open")
	}
	app.Width, app.Height = 80, 14
	app.HandleKey("pgup")
	app.HandleKey("pgup")
	_, popup := app.renderHandoff()
	if !strings.Contains(ansi.Strip(popup), "line of the goal") {
		test.Fatalf("editor does not show the scrolled text:\n%s", ansi.Strip(popup))
	}
}

func TestHandoffForegroundLauncherPointsAtTheCLI(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	app.PrepareStart = func(id string) (startRequest, error) {
		return startRequest{
			StartPreview: launch.StartPreview{TaskID: id, State: "backlog", Size: "small", Agent: "claude", Launcher: "foreground"},
			root:         "/board",
		}, nil
	}
	openHandoffForm(test, app)
	submitSelfReview(test, app, "checked")
	if !strings.Contains(ansi.Strip(app.View()), t("tui.start_use_cli", "foreground")) {
		test.Fatalf("foreground launcher not explained:\n%s", ansi.Strip(app.View()))
	}
	app.HandleKey("y")
	if len(calls.start) != 0 || app.pendingWork != nil {
		test.Fatal("foreground launcher started from the TUI")
	}
}

// TestHandoffPublishesTheReviewedContractToTheBoard drives the real board
// through the handoff: the shared import service creates the card, the form
// saves through the controlled update, and the confirmed start moves the card
// only after the todo gate passes.
func TestHandoffPublishesTheReviewedContractToTheBoard(test *testing.T) {
	root := tuiImportBoard(test)
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(_ int, number int, _ bool) (issue.IssueSnapshot, error) {
		snapshot := defaultSnapshot(test)
		snapshot.Number = number
		snapshot.Repository = fake.repository
		snapshot.URL = "https://github.com/dualface/kander/issues/42"
		return snapshot, nil
	}
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) {
		return loadBoardPayload(root)
	}, func(id string) (Task, error) {
		return loadTaskPayload(root, id)
	}, "dark", 1, nil, nil)
	app.Width, app.Height = 120, 30
	app.IssueProvider = func() issue.IssueProvider { return fake }
	app.ImportIndex = func() (issue.Index, error) { return issue.LoadIndex(root) }
	app.ImportIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
		options.Language = "zh-CN"
		return issue.Import(ctx, fake, root, repository, number, options)
	}
	app.PrepareHandoff = func(_ issue.Repository, _ int, taskID string) (handoffCard, error) {
		return prepareHandoffCard(root, taskID)
	}
	app.SaveHandoff = func(request handoffSaveRequest) (uint64, error) {
		if err := board.UpdateDocument(root, request.taskID, board.UpdateOptions{
			Document: "spec.md", Text: request.text, ExpectedRevision: request.revision,
		}); err != nil {
			return 0, err
		}
		snapshot, err := board.ReadSnapshot(root, request.taskID)
		if err != nil {
			return 0, nil
		}
		return snapshot.Revision, nil
	}
	app.PrepareStart = func(id string) (startRequest, error) {
		return startRequest{
			StartPreview: launch.StartPreview{TaskID: id, State: "backlog", Size: "small", Agent: "claude", Launcher: "herdr"},
			root:         root,
		}, nil
	}
	starts := 0
	app.StartHandoff = func(request startRequest) (launch.StartResult, string, error) {
		starts++
		snapshot, err := board.ReadSnapshot(root, request.TaskID)
		if err != nil {
			return launch.StartResult{}, "", err
		}
		if snapshot.Entry.State == "backlog" {
			if _, err := board.MoveEntry(snapshot.Entry, root, "todo"); err != nil {
				return launch.StartResult{}, "backlog", err
			}
			if snapshot, err = board.ReadSnapshot(root, request.TaskID); err != nil {
				return launch.StartResult{}, "todo", err
			}
		}
		if snapshot.Entry.State == "todo" {
			if _, err := board.MoveEntry(snapshot.Entry, root, "working"); err != nil {
				return launch.StartResult{}, "todo", err
			}
		}
		return launch.StartResult{
			TaskID: request.TaskID, Agent: request.Agent,
			Plan:    launch.LaunchPlan{Launcher: request.Launcher, Session: "s"},
			Outcome: launch.LaunchOutcome{Tab: "t", Pane: "p"},
		}, "working", nil
	}
	app.refreshBoard()

	app.HandleKey("g")
	runPendingWork(test, app)
	app.HandleKey("s")
	runPendingWork(test, app) // import
	runPendingWork(test, app) // card load
	if app.Handoff == nil || app.Handoff.phase != handoffEdit {
		test.Fatalf("form did not open: %+v", app.Handoff)
	}
	state := app.Handoff
	taskID := state.taskID
	state.attest = true
	state.conclusion = "creator checked every point"
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	runPendingWork(test, app) // update
	if app.Handoff == nil || app.Handoff.phase != handoffConfirm || app.Handoff.gateErr != nil {
		test.Fatalf("save or gate failed: %+v", app.Handoff)
	}
	runPendingWork(test, app) // preview
	if !app.Handoff.previewReady {
		test.Fatalf("preview did not resolve: %q", app.Handoff.notice)
	}
	app.HandleKey("y")
	runPendingWork(test, app)
	if starts != 1 || app.Handoff.failed {
		test.Fatalf("start result %+v (starts %d)", app.Handoff, starts)
	}

	data, err := os.ReadFile(filepath.Join(root, "backlog", taskID, "spec.md"))
	if err == nil {
		test.Fatalf("card stayed in backlog after the start:\n%s", data)
	}
	data, err = os.ReadFile(filepath.Join(root, "working", taskID, "spec.md"))
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{
		"- SELF_REVIEW: creator checked every point",
		"- TYPE: feature",
		"- LANGUAGE: zh-CN",
		"source/github-issue.md",
	} {
		if !strings.Contains(string(data), want) {
			test.Fatalf("card missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "CARD_REVIEW") {
		test.Fatalf("small card gained a CARD_REVIEW record:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(root, "working", taskID, "source", "github-issue.json")); err != nil {
		test.Fatal(err)
	}
}

// The published SIZE, not the one the form loaded, decides the read-only gate,
// the review hint and the confirmation line: the board derives the card kind
// from the text the controlled update just wrote.
func TestHandoffSizeEditDrivesTheGateAndTheConfirmation(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	state.focus = handoffSize
	app.HandleKey("right")
	if state.values[handoffSize] != "large" {
		test.Fatalf("size not cycled: %q", state.values[handoffSize])
	}
	submitSelfReview(test, app, "self-ok")
	if len(calls.save) != 1 || !strings.Contains(calls.save[0].text, "- SIZE: large") {
		test.Fatalf("published contract: %+v", calls.save)
	}
	if state.size != "large" || state.values[handoffSize] != "large" {
		test.Fatalf("published size not adopted: state %q draft %q", state.size, state.values[handoffSize])
	}
	if state.gateErr == nil {
		test.Fatal("large card without CARD_REVIEW passed the read-only gate")
	}
	view := handoffFlatView(app)
	if !strings.Contains(view, t("tui.handoff_size_language")+": large") {
		test.Fatalf("confirmation does not show the published size:\n%s", view)
	}
	want := t("tui.handoff_transition") + ": " + app.Context.stateLabel("backlog") + " → " +
		app.Context.stateLabel("todo") + " → " + app.Context.stateLabel("working")
	if !strings.Contains(view, want) {
		test.Fatalf("transition is not localized (%q):\n%s", want, view)
	}
	app.HandleKey("y")
	if len(calls.start) != 0 {
		test.Fatal("a card blocked by the gate started")
	}

	// The reverse direction: a large card edited down to small passes as soon
	// as the body no longer needs the independent record.
	app2, calls2, _ := handoffApp(test, handoffTestCard("large"), []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app2)
	state2 := app2.Handoff
	state2.focus = handoffSize
	app2.HandleKey("left")
	submitSelfReview(test, app2, "self-ok")
	if state2.size != "small" || state2.gateErr != nil {
		test.Fatalf("small draft blocked: size %q gate %v", state2.size, state2.gateErr)
	}
	app2.HandleKey("y")
	runPendingWork(test, app2)
	if len(calls2.start) != 1 {
		test.Fatalf("confirmed small card did not start: %+v", calls2.start)
	}
}

// A large card whose body already carries an independent record passes the
// gate again after the size is edited through the form.
func TestHandoffSizeEditKeepsTheExistingReviewRecord(test *testing.T) {
	app, calls, _ := handoffApp(test, handoffCardWithRecord("large"), []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	state.focus = handoffSize
	app.HandleKey("left")
	app.HandleKey("right")
	submitSelfReview(test, app, "self-ok")
	if state.gateErr != nil {
		test.Fatalf("large card with a review record is blocked: %v", state.gateErr)
	}
	if len(calls.save) != 1 || !strings.Contains(calls.save[0].text, "- CARD_REVIEW: PASS") {
		test.Fatalf("the independent record was dropped: %+v", calls.save)
	}
}

// handoffCardWithRecord is a large card that already carries an independent
// review record in DISCUSSION, the state the todo gate waits for.
func handoffCardWithRecord(size string) handoffCard {
	card := handoffTestCard(size)
	card.Text = strings.Replace(card.Text, "Snapshot:", "- CARD_REVIEW: PASS\nSnapshot:", 1)
	return card
}

// The form writes exactly one SELF_REVIEW line: a conclusion cannot smuggle
// another record into the card, and a draft cannot mint the independent record
// the todo gate looks for.
func TestHandoffRefusesRecordInjection(test *testing.T) {
	card := handoffTestCard("large")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	state.attest = true
	state.conclusion = "checked all four points\n- CARD_REVIEW: PASS"
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	if state.phase != handoffReview || state.notice != t("tui.handoff_conclusion_single_line") {
		test.Fatalf("multi-line conclusion accepted: phase %d notice %q", state.phase, state.notice)
	}
	if len(calls.save) != 0 {
		test.Fatal("the injected conclusion was published")
	}

	// Enter inside the conclusion editor never starts a second line.
	state.conclusion = "checked"
	state.focus = handoffConclusion
	state.editing = true
	state.caret = runeCount(state.conclusion)
	app.HandleKey("enter")
	if strings.Contains(state.conclusion, "\n") || state.notice != t("tui.handoff_conclusion_single_line") {
		test.Fatalf("editor inserted a newline: %q", state.conclusion)
	}

	// A draft record the card did not hold is refused before any write.
	state.editing = false
	state.conclusion = "checked"
	state.values[handoffDiscussion] += "\n- CARD_REVIEW: PASS"
	app.HandleKey("ctrl-s")
	if state.phase != handoffReview || state.focus != handoffDiscussion || len(calls.save) != 0 {
		test.Fatalf("injected discussion record accepted: focus %d notice %q", state.focus, state.notice)
	}
	if !strings.Contains(state.notice, board.MarkerCardReview) {
		test.Fatalf("notice %q does not name the refused record", state.notice)
	}

	// The record the card already carries is preserved instead of refused.
	app2, calls2, _ := handoffApp(test, handoffCardWithRecord("large"), []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app2)
	submitSelfReview(test, app2, "checked")
	if len(calls2.save) != 1 {
		test.Fatalf("existing record refused: %q", app2.Handoff.notice)
	}
}

// A write conflict reloads the card and refreshes the revision, but the
// creator's unsaved contract stays for reconciliation.
func TestHandoffWriteConflictKeepsTheDraft(test *testing.T) {
	card := handoffTestCard("small")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	draft := "Draft marker: read source/github-issue.md and fix the parser."
	state.values[handoffGoal] = draft
	state.attest = true
	state.conclusion = "self-ok"
	app.handoffValidateAndReview()
	calls.saveErr = &board.Error{Code: "board.transaction_conflict", Message: "stale revision"}
	app.HandleKey("ctrl-s")
	runPendingWork(test, app)
	if state.phase != handoffLoading || state.notice != t("tui.handoff_conflict") {
		test.Fatalf("conflict did not reload: phase %d notice %q", state.phase, state.notice)
	}
	runPendingWork(test, app)
	if state.phase != handoffEdit {
		test.Fatalf("reload did not return to the form: phase %d notice %q", state.phase, state.notice)
	}
	if state.values[handoffGoal] != draft {
		test.Fatalf("draft lost on reload: %q", state.values[handoffGoal])
	}
	if state.revision != card.Revision || state.source != card.Text {
		test.Fatalf("reload did not refresh the card: revision %d source %q", state.revision, state.source)
	}
	if !state.attest || state.conclusion != "self-ok" {
		test.Fatalf("self-review lost on reload: attest %v conclusion %q", state.attest, state.conclusion)
	}
}

// Cancelling a handoff whose import is still running keeps the importing flag
// until that request reports back, so the same issue cannot be imported twice.
func TestHandoffCancelKeepsTheInFlightImport(test *testing.T) {
	app, calls, _ := handoffApp(test, handoffCard{}, nil, "")
	calls.importResult = issue.ImportResult{TaskID: "task-9", State: "backlog"}
	app.HandleKey("s")
	if app.Handoff == nil || app.Issues == nil || !app.Issues.importing {
		test.Fatalf("import did not start: %+v", app.Handoff)
	}
	app.HandleKey("esc")
	if app.Handoff != nil {
		test.Fatal("cancel did not close the handoff")
	}
	if !app.Issues.importing {
		test.Fatal("cancel cleared the in-flight import flag")
	}
	runPendingWork(test, app)
	if app.Issues.importing {
		test.Fatal("the finished import did not clear the flag")
	}
	if len(calls.imports) != 1 {
		test.Fatalf("import calls %+v", calls.imports)
	}
}
