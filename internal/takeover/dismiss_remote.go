package takeover

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/terminal"
)

func commandRemoteDismiss(cfg *config.Config, task string, timeout float64) error {
	if err := launch.ValidateTimeout(timeout, "dismiss"); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	service, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return err
	}
	operationCtx, finishOperation := service.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	card, err := service.Project.Show(ctx, task)
	if err != nil {
		return err
	}
	if card.State != "done" && card.State != "archived" {
		return takeoverError("takeover.only_tasks_in_done_or_archived_can_be_dismissed", card.TaskID, card.State)
	}
	if err := cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(card.Text)); err != nil {
		return err
	}
	local, err := service.RetainedForDismiss(card)
	if err != nil {
		return err
	}
	cardSession, cardWindow := board.MetadataFrom(card.Text, board.FieldSession), board.MetadataFrom(card.Text, board.FieldWindow)
	// recordedSession is what the card and the execution record must hold:
	// the retained session, or only its agent for an execution started before
	// remote starts confirmed the discovered session to the server.
	recordedSession := local.Session
	if local.Window == cardWindow && agentOnlySession(cardSession, local.Session) {
		recordedSession = cardSession
	}
	if recordedSession != cardSession || local.Window != cardWindow {
		return remote.LocalExecutionRefused(remote.RefusalSceneMismatch, remote.RefusalValue(local.Session), remote.RefusalValue(local.Window), remote.RefusalValue(cardSession), remote.RefusalValue(cardWindow))
	}
	record, err := service.Project.ExecutionRecord(ctx, card.TaskID)
	if err != nil {
		return err
	}
	if record.SelfClaimed() {
		return remote.LocalExecutionRefused(remote.RefusalSelfClaimed, card.TaskID, record.Kind)
	}
	if mismatch := recordMismatch(record, board.MetadataFrom(card.Text, board.FieldOwner), recordedSession); mismatch != "" {
		return remote.LocalExecutionRefused(remote.RefusalRecordMismatch, mismatch)
	}
	backend, address, parsed := terminal.ParseWindow(local.Window)
	if !parsed || !backend.Capabilities().Container {
		return takeoverError("takeover.task_has_no_dismissible_terminal_container", local.Window)
	}
	agent, reference, _ := strings.Cut(local.Session, " ")
	if agent == "" || reference == "" {
		return remote.LocalExecutionRefused(remote.RefusalSessionInvalid, local.Session)
	}
	session := launch.AgentSession{Agent: agent, Reference: reference}
	journal, fresh, err := service.BeginAction("dismiss", local)
	if err != nil {
		return err
	}
	defer journal.Close()
	if !fresh && journal.Value.Phase == "complete" {
		reportDismiss(t("remote.execution_dismissed", card.TaskID), card.TaskID, "dismissed")
		return nil
	}
	program, err := lookPath(backend.Executable())
	if err != nil {
		return err
	}
	pane, err := backend.PaneFacts(ctx, probeConn(program), address.Pane)
	if err != nil {
		return err
	}
	if pane.Gone {
		journal.Value.Phase = "complete"
		if err := journal.Save(); err != nil {
			return err
		}
		reportDismiss(t("remote.execution_target_gone", card.TaskID, local.Window), card.TaskID, "target_gone")
		return nil
	}
	if !fresh && journal.Value.Phase != "prepared" {
		return takeoverError("remote.launch_reconciliation_required", journal.Path)
	}
	if err := sameWorkerPane(ctx, backend, pane, session); err != nil {
		return err
	}
	exitCommand, err := AgentExitCommand(agent)
	if err != nil {
		return err
	}
	current, err := service.Project.Show(ctx, card.TaskID)
	if err != nil {
		return err
	}
	if changed := taskChanges(card, current, local); changed != "" {
		return remote.LocalExecutionRefused(remote.RefusalTaskChanged, card.TaskID, changed)
	}
	journal.Value.Phase = "exiting"
	if err := journal.Save(); err != nil {
		return err
	}
	if backend.Capabilities().AgentIdentity {
		_, _, err = dismissAgentPaneAt(backend, address, true, toLive(session), session, exitCommand, timeout, false)
	} else {
		_, _, err = dismissProcessPaneAt(backend, address, toLive(session), session, exitCommand, timeout, false)
	}
	if err != nil {
		return err
	}
	journal.Value.Phase = "complete"
	if err := journal.Save(); err != nil {
		return err
	}
	reportDismiss(t("remote.execution_dismissed", card.TaskID), card.TaskID, "dismissed")
	return nil
}

// agentOnlySession reports whether the card session is only the agent of the
// retained session, which carries a reference. A remote start recorded that
// pair for a session it discovered without confirming it to the server; the
// retained session then identifies the executor, and the pane must still prove
// it before anything is sent.
func agentOnlySession(card, retained string) bool {
	agent, reference, _ := strings.Cut(retained, " ")
	return card != "" && card == agent && reference != ""
}

// recordMismatch names each field in which the execution record differs from
// the card or the retained execution, with both values; empty means they agree.
func recordMismatch(record board.ExecutionRecord, owner, session string) string {
	var fields []string
	if record.Owner != owner {
		fields = append(fields, remote.RefusalText("record_owner", fmt.Sprintf("%q", record.Owner), fmt.Sprintf("%q", owner)))
	}
	if record.Session != session {
		fields = append(fields, remote.RefusalText("record_session", fmt.Sprintf("%q", record.Session), fmt.Sprintf("%q", session)))
	}
	return strings.Join(fields, "; ")
}

// taskChanges names what changed on the server between the first read of the
// card and the read before the exit, with the values before and after; empty
// means nothing changed.
func taskChanges(before, after api.Task, local remote.RetainedExecution) string {
	var changes []string
	if after.Revision != before.Revision {
		changes = append(changes, remote.RefusalText("change_revision", before.Revision, after.Revision))
	}
	if after.State != before.State {
		changes = append(changes, remote.RefusalText("change_state", before.State, after.State))
	}
	if after.Claim == nil {
		return strings.Join(append(changes, remote.RefusalText("change_claim_removed")), "; ")
	}
	if after.Claim.ExecutionID != local.ExecutionID {
		changes = append(changes, remote.RefusalText("change_claim_execution", local.ExecutionID, after.Claim.ExecutionID))
	}
	if after.Claim.Generation != local.Generation {
		changes = append(changes, remote.RefusalText("change_claim_generation", local.Generation, after.Claim.Generation))
	}
	return strings.Join(changes, "; ")
}
