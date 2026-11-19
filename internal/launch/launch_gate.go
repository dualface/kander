package launch

import (
	"encoding/json"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// The launcher gate. With --json a launcher that takes the terminal is refused, but only by a
// call that would start an agent: a replay of a completed launch and a message to a live
// executor start nothing, and a refusal must leave every receipt, directory and registration as
// it found them, so that the same command can be repeated without --json.
//
// launchWouldStart is the one decision of whether a call starts an agent. It reads nothing and
// writes nothing: its inputs are the receipt as it stands (nil when there is none) and the facts
// the caller already holds. Every entry that can start an agent asks it:
//
//   - start and an explicit takeover (launchRemote): before the session is opened, through
//     mayLaunchAnew, and again once the receipt of the launch has been read;
//   - an implicit resume (runImplicitNativeResume): before its command receipt is written;
//   - orchestrate (StartOrchestrator and startOrchestrator): before the session and before the
//     receipt of the session;
//   - issue triage and issue result --action start (issue.gateLauncher): before anything, because
//     triage has no receipt and the receipt key of result needs the inspection.
//
// Every write that follows is sequenced behind it: the directories and the registration of the
// session (remote.Open), and the receipts (BeginAction and RetainAction). The receipt writers
// call the same decision again under the lock of the receipt, with the state it has then, so a
// retry that read an older state cannot rewrite a receipt before it is refused.
func launchWouldStart(existing *remote.LocalAction, retryFailed bool) bool {
	if existing == nil {
		return true
	}
	switch existing.Phase {
	case "prepared", "session-ready":
		return true
	case "failed":
		// BeginAction starts a new attempt over a failed receipt; RetainAction keeps it and
		// replays the failure.
		return retryFailed
	}
	// complete replays; every other phase is reconciled by the recovery of the launch.
	return false
}

// nativeResumeWouldStart is launchWouldStart for the command receipt of an implicit resume.
// child is the receipt of the launch the command wraps, canContinue is whether this host holds a
// stopped executor to continue, and execution is the execution ID of the current credential.
func nativeResumeWouldStart(existing, child *remote.LocalAction, canContinue bool, execution string) bool {
	if existing == nil {
		return canContinue
	}
	var saved nativeResumeIntent
	if len(existing.Result) != 0 {
		_ = json.Unmarshal(existing.Result, &saved)
	}
	switch existing.Phase {
	case "pending":
		return launchWouldStart(child, !saved.Input.Native)
	case "complete":
		// The receipt of an older execution is replayed; a later confirmed stop continues anew.
		return canContinue && saved.CompletedExecution != "" && saved.CompletedExecution == execution
	}
	return canContinue
}

// gateLaunch refuses launcher for cfg when the call starts an agent.
func gateLaunch(cfg *config.Config, launcher string, starts bool) error {
	if !starts {
		return nil
	}
	return refuseConfiguredLauncher(cfg, launcher)
}

// actionGate is the gate the receipt writers call under the lock of the receipt.
func actionGate(cfg *config.Config, launcher string, retryFailed bool) remote.ActionGate {
	return func(existing *remote.LocalAction) error {
		return gateLaunch(cfg, launcher, launchWouldStart(existing, retryFailed))
	}
}

// mayLaunchAnew reports, before any session exists, whether a call may be a replay. A host with
// no receipt of the kind for the task has nothing to replay, so a call that is refused for its
// launcher is refused before the session creates the client identity or registers the project.
func mayLaunchAnew(kind, task string) bool {
	maybe, err := remote.MayReplayAction(kind, task)
	return err != nil || maybe
}
