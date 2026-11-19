package launch

import (
	"context"
	"encoding/json"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/remote"
)

type nativeResumeIntent struct {
	Input              remoteLaunchInput
	Options            DispatchOptions
	NewRound           bool
	CompletedExecution string
}

// The implicit decision must be durable before a handoff can change the current
// generation. A new invocation reads this command record before agent probing.
// nativeResumeReceipt is the logical input of the command receipt of an implicit resume.
type nativeResumeReceipt struct {
	Task, Launcher, Message string
	Options                 DispatchOptions
}

// The implicit decision must be durable before a handoff can change the current
// generation. A new invocation reads this command record before agent probing.
func runImplicitNativeResume(ctx context.Context, session *remote.Session, card api.Task, launcher, text string, timeout float64, options DispatchOptions) int {
	logical := nativeResumeReceipt{card.TaskID, launcher, text, options}
	credential, found, err := session.Credential(card.TaskID)
	if err != nil {
		return fail(err)
	}
	// One probe of the executor serves every decision below. A second probe could see a process
	// that exited in between and send the command down another branch.
	var probed, stopped bool
	ownedStopped := func() bool {
		if !probed {
			probed = true
			stopped = found && !credential.Lost && card.Claim != nil && api.ClaimStands(card.Claim.Status) && card.Claim.ClientID == session.Client.ClientID() && card.Claim.ExecutionID == credential.ExecutionID && observeLocalExecution(ctx, credential).Status == "stopped"
		}
		return stopped
	}
	var continuable, continuableKnown bool
	var continuableErr error
	canContinue := func() bool {
		if !continuableKnown {
			continuableKnown = true
			continuable = ownedStopped()
			if continuable {
				var record board.ExecutionRecord
				record, continuableErr = session.Project.ExecutionRecord(ctx, card.TaskID)
				continuable = continuableErr == nil && !record.StartUnfinished()
			}
		}
		return continuable
	}
	// With --json a launcher that takes the terminal is refused, but only where this command would
	// start an agent: a replay of a completed receipt, and a message to a live executor, start
	// nothing. The receipts are read without writing, so a refusal leaves none behind and the same
	// command can be repeated without --json.
	if command.JSON() {
		starts := canContinue()
		if action, exists, err := session.PeekAction("native-resume-command", logical); err == nil && exists {
			var saved nativeResumeIntent
			if len(action.Result) != 0 {
				_ = json.Unmarshal(action.Result, &saved)
			}
			switch action.Phase {
			case "pending":
				starts = childLaunchStarts(session, card.TaskID, saved.Input)
			case "complete":
				starts = starts && saved.CompletedExecution != "" && saved.CompletedExecution == credential.ExecutionID
			}
		}
		if starts {
			if err := RefuseTerminalLauncher(launcher); err != nil {
				return fail(err)
			}
		}
	}
	journal, _, err := session.RetainAction("native-resume-command", logical)
	if err != nil {
		return fail(err)
	}
	defer journal.Close()
	var saved nativeResumeIntent
	if len(journal.Value.Result) != 0 {
		if err := json.Unmarshal(journal.Value.Result, &saved); err != nil {
			return fail(err)
		}
	}
	if journal.Value.Phase == "pending" {
		return executeRemoteResume(saved.Input, saved.Options, saved.NewRound, timeout, journal)
	}
	if journal.Value.Phase == "complete" {
		if !ownedStopped() || saved.CompletedExecution == "" || saved.CompletedExecution != credential.ExecutionID {
			return reportNativeReceipt(card.TaskID)
		}
		// A later confirmed stop is a new intentional continuation. Keep the child
		// task-launch receipt; rotate only this command's next decision identity.
		journal.Value.ExecutionID = remote.NewExecutionID()
		journal.Value.Result = nil
		journal.Value.Phase = "prepared"
		if err := journal.Save(); err != nil {
			return fail(err)
		}
	}
	if !canContinue() {
		if continuableErr != nil {
			return fail(continuableErr)
		}
		if launcher != "" {
			return fail(launchError("remote.launcher_requires_takeover"))
		}
		request, err := NotifyRemote(card.TaskID, text, "", "", true, timeout, options)
		if err != nil {
			return fail(err)
		}
		if code := command.Object(request); code != 0 {
			return code
		}
		return 0
	}
	options.Decision = "native-resume:" + journal.Value.ExecutionID
	return runRemoteResumeLaunch(ctx, session, card, nil, launcher, text, timeout, options, journal)
}

func reportNativeReceipt(task string) int {
	return startedExit("resume", board.Entry{TaskID: task}, "", LaunchPlan{}, LaunchOutcome{Replayed: true}, nil)
}
