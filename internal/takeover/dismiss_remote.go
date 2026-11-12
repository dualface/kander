package takeover

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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
	local, err := service.Retained(card)
	if err != nil {
		return err
	}
	if local.Session != board.MetadataFrom(card.Text, board.FieldSession) || local.Window != board.MetadataFrom(card.Text, board.FieldWindow) {
		return takeoverError("remote.local_execution_required")
	}
	record, err := service.Project.ExecutionRecord(ctx, card.TaskID)
	if err != nil {
		return err
	}
	if record.SelfClaimed() || record.Owner != board.MetadataFrom(card.Text, board.FieldOwner) || record.Session != local.Session {
		return takeoverError("remote.local_execution_required")
	}
	backend, address, parsed := terminal.ParseWindow(local.Window)
	if !parsed || !backend.Capabilities().Container {
		return takeoverError("takeover.task_has_no_dismissible_terminal_container", local.Window)
	}
	agent, reference, _ := strings.Cut(local.Session, " ")
	if agent == "" || reference == "" {
		return takeoverError("remote.local_execution_required")
	}
	session := launch.AgentSession{Agent: agent, Reference: reference}
	journal, fresh, err := service.BeginAction("dismiss", local)
	if err != nil {
		return err
	}
	defer journal.Close()
	if !fresh && journal.Value.Phase == "complete" {
		fmt.Println(t("remote.execution_dismissed", card.TaskID))
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
		return journal.Save()
	}
	if !fresh && journal.Value.Phase != "prepared" {
		return takeoverError("remote.launch_reconciliation_required", journal.Path)
	}
	if err := sameWorkerPane(ctx, backend, pane, session); err != nil {
		return err
	}
	command, err := AgentExitCommand(agent)
	if err != nil {
		return err
	}
	current, err := service.Project.Show(ctx, card.TaskID)
	if err != nil {
		return err
	}
	if current.Revision != card.Revision || current.State != card.State || current.Claim == nil || current.Claim.ExecutionID != local.ExecutionID || current.Claim.Generation != local.Generation {
		return takeoverError("remote.local_execution_required")
	}
	journal.Value.Phase = "exiting"
	if err := journal.Save(); err != nil {
		return err
	}
	if backend.Capabilities().AgentIdentity {
		_, _, err = dismissAgentPaneAt(backend, address, true, toLive(session), session, command, timeout, false)
	} else {
		_, _, err = dismissProcessPaneAt(backend, address, toLive(session), session, command, timeout, false)
	}
	if err != nil {
		return err
	}
	journal.Value.Phase = "complete"
	if err := journal.Save(); err != nil {
		return err
	}
	fmt.Println(t("remote.execution_dismissed", card.TaskID))
	return nil
}
