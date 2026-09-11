package tui

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
)

// Regression tests for the review findings that the handoff form had to close:
// the published size deciding the gate, the managed records around the
// self-review, the conflict reload, and the in-flight import flag.

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

// A CARD_REVIEW record an independent producer writes while the form is open
// survives the conflict reload and the next publication.
func TestHandoffConflictKeepsTheConcurrentReviewRecord(test *testing.T) {
	card := handoffTestCard("large")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	updated := handoffCardWithRecord("large")
	updated.Revision = card.Revision + 1
	app.PrepareHandoff = func(issue.Repository, int, string) (handoffCard, error) { return updated, nil }
	calls.saveErr = &board.Error{Code: "board.transaction_conflict", Message: "stale revision"}
	state.attest = true
	state.conclusion = "self-ok"
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	runPendingWork(test, app)
	runPendingWork(test, app)
	if state.phase != handoffEdit {
		test.Fatalf("reload did not return to the form: phase %d notice %q", state.phase, state.notice)
	}
	if !strings.Contains(state.values[handoffDiscussion], "- CARD_REVIEW: PASS") {
		test.Fatalf("the concurrent record was not folded into the draft: %q", state.values[handoffDiscussion])
	}
	calls.saveErr = nil
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	runPendingWork(test, app)
	if len(calls.save) != 2 {
		test.Fatalf("republishing was refused: %q", state.notice)
	}
	if len(calls.save) == 2 && !strings.Contains(calls.save[1].text, "- CARD_REVIEW: PASS") {
		test.Fatalf("republishing dropped the independent record:\n%s", calls.save[1].text)
	}
	if state.gateErr != nil {
		test.Fatalf("gate blocked although the record is on the card: %v", state.gateErr)
	}
}

// A record the producer rewrote in between replaces the stale draft line
// instead of being duplicated or reported as an injected record.
func TestHandoffConflictReplacesAStaleRecordLine(test *testing.T) {
	card := handoffCardWithRecord("large")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	updated := handoffCardWithRecord("large")
	updated.Text = strings.Replace(updated.Text, "- CARD_REVIEW: PASS", "- CARD_REVIEW: the independent agent checked the final contract", 1)
	updated.Revision = card.Revision + 1
	app.PrepareHandoff = func(issue.Repository, int, string) (handoffCard, error) { return updated, nil }
	calls.saveErr = &board.Error{Code: "board.transaction_conflict", Message: "stale revision"}
	state.attest = true
	state.conclusion = "self-ok"
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	runPendingWork(test, app)
	runPendingWork(test, app)
	if !strings.Contains(state.values[handoffDiscussion], "- CARD_REVIEW: the independent agent checked the final contract") {
		test.Fatalf("the rewritten record is missing from the draft: %q", state.values[handoffDiscussion])
	}
	if strings.Contains(state.values[handoffDiscussion], "- CARD_REVIEW: PASS") {
		test.Fatalf("the stale record line was kept: %q", state.values[handoffDiscussion])
	}
	calls.saveErr = nil
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	runPendingWork(test, app)
	if len(calls.save) != 2 {
		test.Fatalf("republishing was refused: %q", state.notice)
	}
	if text := calls.save[1].text; strings.Count(text, "- CARD_REVIEW:") != 1 {
		test.Fatalf("republishing duplicated or dropped the record:\n%s", text)
	}
}

// The form refuses a draft that would delete a record the card holds, and the
// SELF_REVIEW line it owns is rewritten instead of being duplicated.
func TestHandoffRefusesDroppingAStoredRecord(test *testing.T) {
	card := handoffCardWithRecord("large")
	app, calls, _ := handoffApp(test, card, []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app)
	state := app.Handoff
	state.values[handoffDiscussion] = strings.Replace(state.values[handoffDiscussion], "- CARD_REVIEW: PASS\n", "", 1)
	state.attest = true
	state.conclusion = "checked"
	app.handoffValidateAndReview()
	app.HandleKey("ctrl-s")
	if state.phase != handoffReview || len(calls.save) != 0 {
		test.Fatalf("the dropped record was accepted: phase %d", state.phase)
	}
	if state.notice != t("tui.handoff_record_dropped", "CARD_REVIEW: PASS") {
		test.Fatalf("notice %q", state.notice)
	}

	// The SELF_REVIEW line is the record this form owns: a second save with a
	// new conclusion rewrites it instead of tripping the drop check.
	app2, calls2, _ := handoffApp(test, handoffTestCard("small"), []Task{{TaskID: "task-1", Title: "Task", State: "backlog"}}, "backlog")
	openHandoffForm(test, app2)
	submitSelfReview(test, app2, "first conclusion")
	state2 := app2.Handoff
	state2.phase = handoffReview
	state2.attest = true
	state2.conclusion = "second conclusion"
	app2.HandleKey("ctrl-s")
	runPendingWork(test, app2)
	if len(calls2.save) != 2 {
		test.Fatalf("rewriting the SELF_REVIEW record was refused: %q", state2.notice)
	}
	if text := calls2.save[1].text; !strings.Contains(text, "- SELF_REVIEW: second conclusion") || strings.Contains(text, "first conclusion") {
		test.Fatalf("the self-review record was not rewritten:\n%s", text)
	}
}

// Publishing a card that already carries a bulleted SELF_REVIEW line replaces
// that record instead of adding one more line per save.
func TestHandoffBuildReplacesABulletedSelfReviewRecord(test *testing.T) {
	source := handoffTestSpec("small") + "\n- CARD_REVIEW: independent reviewer concluded ok\n\n- SELF_REVIEW: old conclusion\n"
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
}
