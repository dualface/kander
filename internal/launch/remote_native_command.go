package launch

import (
	"context"
	"encoding/json"
	"os"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
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
func runImplicitNativeResume(ctx context.Context, session *remote.Session, card api.Task, launcher, text string, timeout float64, options DispatchOptions) int {
	journal, _, err := session.RetainAction("native-resume-command", struct {
		Task, Launcher, Message string
		Options                 DispatchOptions
	}{card.TaskID, launcher, text, options})
	if err != nil {
		return fail(err)
	}
	defer journal.Close()
	credential, found, err := session.Credential(card.TaskID)
	if err != nil {
		return fail(err)
	}
	var saved nativeResumeIntent
	if len(journal.Value.Result) != 0 {
		if err := json.Unmarshal(journal.Value.Result, &saved); err != nil {
			return fail(err)
		}
	}
	if journal.Value.Phase == "pending" {
		return executeRemoteResume(saved.Input, saved.Options, saved.NewRound, timeout, journal)
	}
	ownedStopped := found && !credential.Lost && card.Claim != nil && card.Claim.Status == api.ClaimActive && card.Claim.ClientID == session.Client.ClientID() && card.Claim.ExecutionID == credential.ExecutionID && observeLocalExecution(ctx, credential).Status == "stopped"
	if journal.Value.Phase == "complete" {
		if !ownedStopped || saved.CompletedExecution == "" || saved.CompletedExecution != credential.ExecutionID {
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
	if ownedStopped {
		record, err := session.Project.ExecutionRecord(ctx, card.TaskID)
		if err != nil {
			return fail(err)
		}
		ownedStopped = !record.StartUnfinished()
	}
	if !ownedStopped {
		if launcher != "" {
			return fail(launchError("remote.launcher_requires_takeover"))
		}
		request, err := NotifyRemote(card.TaskID, text, "", "", true, timeout, options)
		if err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(request); err != nil {
			return fail(err)
		}
		return 0
	}
	options.Decision = "native-resume:" + journal.Value.ExecutionID
	return runRemoteResumeLaunch(ctx, session, card, nil, launcher, text, timeout, options, journal)
}

func reportNativeReceipt(task string) int {
	if err := reportLaunch(t("launch.started"), board.Entry{TaskID: task}, "", LaunchPlan{}, LaunchOutcome{Replayed: true}); err != nil {
		return exitLaunch(err)
	}
	return 0
}
