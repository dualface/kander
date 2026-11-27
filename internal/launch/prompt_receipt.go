package launch

import (
	"errors"

	"github.com/dualface/kander/v2/internal/remote"
)

func bindLaunchPromptReceipt(plan *LaunchPlan, journal *remote.ActionJournal) {
	plan.beforePrompt = func() error {
		journal.Value.Phase = "prompt-attempted"
		return journal.Save()
	}
	retryUnsent := plan.renameMovesPrompt()
	plan.judge = func() launchVerdict {
		if retryUnsent && journal.Value.Phase == "launching" {
			return verdictCleanup
		}
		return verdictKeep
	}
}

// recordLaunchPromptFailure keeps ambiguous actions fenced. Only restart a
// rename-driven pane launch when the prompt was never attempted and the pane
// this launch owned has already been closed.
func recordLaunchPromptFailure(plan LaunchPlan, journal *remote.ActionJournal, err error) (handedOff bool, result error) {
	f := asLaunchFailure(err)
	if plan.renameMovesPrompt() && journal.Value.Phase == "launching" && !f.DeliveryUnknown &&
		f.Verdict == verdictCleanup && f.Outcome.Pane != "" && f.CloseError == "" {
		journal.Value.Phase, journal.Value.TaskFile = "failed", ""
		if saveErr := journal.Save(); saveErr == nil {
			return false, err
		} else {
			err = errors.Join(err, saveErr)
		}
	}
	return true, errors.Join(err, launchError("remote.launch_reconciliation_required", journal.Path))
}
