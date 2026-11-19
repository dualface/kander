package launch

import (
	"context"
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
//   - start and an explicit takeover (launchRemote, runRemoteResume): before the session is opened,
//     through refuseBeforeSession, and again once the receipt of the launch has been read;
//   - an implicit resume (runImplicitNativeResume): before its command receipt is written;
//   - orchestrate (StartOrchestrator and startOrchestrator): before the session and before the
//     receipt of the session;
//   - issue triage and issue result --action start (issue.gateLauncher): before anything, because
//     triage has no receipt and the receipt key of result needs the inspection.
//
// Every write that follows is sequenced behind it: the directories and the registration of the
// session (remote.Open), and the receipts (BeginAction and BeginActionGated). The receipt writers
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
		// BeginAction starts a new attempt over a failed receipt; BeginActionGated with
		// retryFailed false keeps it and replays the failure.
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

// refuseBeforeSession is the check that comes before the session of the host is opened, which
// creates the client identity and registers the project. It reads only: the client identity if
// there is one, the project resolved by name without registering it, and the receipts of this
// server and project under the exact keys of the invocation (logicals, one per way the
// invocation can be keyed). When no receipt of this project can exist, or none of the receipts
// is one that a call would replay or reconcile, the call certainly starts an agent and is
// refused for its launcher now. When anything is uncertain (the service did not answer, a
// receipt may be replayed) it returns nil and the exact check after Open decides.
func refuseBeforeSession(cfg *config.Config, launcher, kind string, retryFailed bool, logicals ...any) error {
	refusal := gateLaunch(cfg, launcher, true)
	if refusal == nil {
		return nil // not a launcher that takes the terminal, or not --json
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCall)
	defer cancel()
	host, nothing, err := remote.LookupHost(ctx, cfg)
	if err != nil {
		return nil
	}
	if nothing {
		return refusal
	}
	for _, logical := range logicals {
		existing, found, err := host.PeekAction(kind, logical)
		if err != nil {
			return nil
		}
		if found && !launchWouldStart(&existing, retryFailed) {
			return nil
		}
	}
	return refusal
}
