package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

func runRemoteResume(cfg *config.Config, agent *string, launcher, task, message, messageFile string, messageSet bool, timeout float64, options DispatchOptions) int {
	if err := ValidateTimeout(timeout, "resume"); err != nil {
		return fail(err)
	}
	text, err := ReadMessage(message, messageSet, messageFile, "resume")
	if err != nil {
		return fail(err)
	}
	if agent != nil || options.Decision != "" {
		// An explicit takeover starts an agent unless it replays a completed launch. Refuse it
		// before the session creates the client identity or registers the project, when no
		// receipt of this project under the keys the takeover can have is one to replay.
		// launchRemote decides from the receipt otherwise. An implicit resume may only deliver
		// the message to a live executor, so it decides for itself.
		var keys []any
		for _, newRound := range takeoverRounds(options) {
			keys = append(keys, launchReceiptInput{task, takeoverInput(task, launcher, text, agent, options, newRound)})
		}
		if err := refuseBeforeSession(cfg, launcher, "task-launch", agent != nil, keys...); err != nil {
			return fail(err)
		}
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

// takeoverInput is the launch input of a takeover. runRemoteResumeLaunch and the check before the
// session build it here, so both name the same receipt. options.ID must be set when newRound.
func takeoverInput(task, launcher, text string, agent *string, options DispatchOptions, newRound bool) remoteLaunchInput {
	input := remoteLaunchInput{Launcher: launcher, TaskID: task, Message: text, Decision: options.Decision, Base: options.Base, Takeover: true, Native: agent == nil}
	if agent != nil {
		input.Agent = *agent
	}
	if newRound {
		digest := sha256.Sum256([]byte(task + "\x00" + options.ID))
		input.PendingRequest = api.CollaborationRequestID("dispatch-proposal-" + hex.EncodeToString(digest[:]))
	}
	return input
}

// takeoverRounds lists the ways a takeover can be keyed before the card is read: as a plain
// takeover, and, when the options name a dispatch, as the round of that dispatch. A takeover
// that starts a new round without a dispatch ID gets a fresh one, so it never repeats a receipt.
func takeoverRounds(options DispatchOptions) []bool {
	if options.ID != "" {
		return []bool{false, true}
	}
	return []bool{false}
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
	if newRound && options.ID == "" {
		options.ID, err = board.NewDispatchID()
		if err != nil {
			return fail(err)
		}
	}
	input := takeoverInput(card.TaskID, launcher, text, agent, options, newRound)
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

// executeRemoteResume runs after the session of the host was opened, so the launch skips the
// check that precedes the session.
func executeRemoteResume(input remoteLaunchInput, options DispatchOptions, newRound bool, timeout float64, journal *remote.ActionJournal) int {
	input.SessionChecked = true
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
