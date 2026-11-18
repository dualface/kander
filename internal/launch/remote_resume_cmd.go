package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func runRemoteResume(cfg *config.Config, agent *string, launcher, task, message, messageFile string, messageSet bool, timeout float64, options DispatchOptions) int {
	if err := ValidateTimeout(timeout, "resume"); err != nil {
		return fail(err)
	}
	text, err := ReadMessage(message, messageSet, messageFile, "resume")
	if err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	card, err := session.Project.Show(ctx, task)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(card.Text)); err != nil {
		return fail(err)
	}
	if agent == nil && options.Decision == "" {
		return runImplicitNativeResume(ctx, session, card, launcher, text, timeout, options)
	}
	return runRemoteResumeLaunch(ctx, session, card, agent, launcher, text, timeout, options, nil)
}

func runRemoteResumeLaunch(ctx context.Context, session *remote.Session, card api.Task, agent *string, launcher, text string, timeout float64, options DispatchOptions, journal *remote.ActionJournal) int {
	var err error
	if strings.TrimSpace(options.Decision) == "" {
		return fail(launchError("remote.takeover_decision_required"))
	}
	newRound := options.ID != "" || options.Kind != "" || options.Base != "" || options.EvidenceFile != ""
	bound := board.MetadataFrom(card.Text, "DISPATCH_ID")
	if bound != "" && newRound {
		dispatch, err := session.Project.Dispatch(ctx, card.TaskID, bound)
		if err != nil {
			return fail(err)
		}
		if options.ID == bound {
			if text != dispatch.Input.Message || options.Kind != "" && options.Kind != dispatch.Input.Kind || options.Base != "" && options.Base != dispatch.Input.Base {
				return fail(launchError("remote.dispatch_proposal_changed"))
			}
			newRound = false
		} else if dispatch.State != board.DispatchCompleted {
			return fail(launchError("remote.finish_dispatch_before_restart"))
		}
	}
	if card.State == "review" && board.TaskGroupFrom(card.Text) != "" && !newRound && bound == "" {
		record, err := session.Project.ExecutionRecord(ctx, card.TaskID)
		if err != nil {
			return fail(err)
		}
		if !record.StartUnfinished() {
			return fail(launchError("launch.review_dispatch_required", card.TaskID))
		}
	}
	input := remoteLaunchInput{Launcher: launcher, TaskID: card.TaskID, Message: text, Decision: options.Decision, Base: options.Base, Takeover: true, Native: agent == nil}
	if agent != nil {
		input.Agent = *agent
	}
	if newRound {
		if options.ID == "" {
			options.ID, err = board.NewDispatchID()
			if err != nil {
				return fail(err)
			}
		}
		digest := sha256.Sum256([]byte(card.TaskID + "\x00" + options.ID))
		input.PendingRequest = api.CollaborationRequestID("dispatch-proposal-" + hex.EncodeToString(digest[:]))
	}
	if journal != nil {
		state := nativeResumeIntent{Input: input, Options: options, NewRound: newRound}
		data, err := json.Marshal(state)
		if err != nil {
			return fail(err)
		}
		journal.Value.Result, journal.Value.Phase = data, "pending"
		if err := journal.Save(); err != nil {
			return fail(err)
		}
	}
	return executeRemoteResume(input, options, newRound, timeout, journal)
}

func executeRemoteResume(input remoteLaunchInput, options DispatchOptions, newRound bool, timeout float64, journal *remote.ActionJournal) int {
	result, err := launchRemote(input)
	for _, warning := range result.Warnings {
		command.Notify("launch_warning", warning)
	}
	if err != nil {
		return exitLaunch(err)
	}
	if result.Superseded {
		return supersededExit("resume", result.TaskID, result.Plan, result.Outcome)
	}
	var queued *api.CollabRequest
	if newRound {
		request, err := notifyRemoteForExecution(input.TaskID, input.Message, "", "", true, timeout, options, result.executionID)
		if err != nil {
			return exitLaunch(&UnresolvedError{TaskID: input.TaskID, Window: locationOf(result.Plan, result.Outcome), Reason: err.Error(), Command: "kander"})
		}
		queued = &request
	}
	if journal != nil {
		var state nativeResumeIntent
		if err := json.Unmarshal(journal.Value.Result, &state); err != nil {
			return fail(err)
		}
		state.CompletedExecution = result.executionID
		data, err := json.Marshal(state)
		if err != nil {
			return fail(err)
		}
		journal.Value.Result, journal.Value.Phase = data, "complete"
		if err := journal.Save(); err != nil {
			return fail(err)
		}
	}
	return startedExit("resume", board.Entry{TaskID: result.TaskID, Kind: result.Size}, result.Agent, result.Plan, result.Outcome, queued)
}
