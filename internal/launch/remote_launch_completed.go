package launch

import (
	"encoding/json"

	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/terminal"
)

// taskLaunchReceipt contains display facts only. Replaying it neither restores
// an ended bearer credential nor claims that a historical agent is alive.
type taskLaunchReceipt struct {
	TaskID, Size, Agent, Launcher, Container, Pane string
	Target                                         terminal.Target
	SessionMarked                                  bool
}

func completedRemoteLaunch(journal *remote.ActionJournal) (StartResult, error) {
	var saved taskLaunchReceipt
	if err := json.Unmarshal(journal.Value.Result, &saved); err != nil {
		return StartResult{}, err
	}
	if saved.TaskID != journal.Value.TaskID || saved.Agent == "" || saved.Launcher == "" {
		return StartResult{}, launchError("remote.launch_reconciliation_required", journal.Path)
	}
	return StartResult{executionID: journal.Value.ExecutionID, TaskID: saved.TaskID, Size: saved.Size, Agent: saved.Agent, Plan: LaunchPlan{Launcher: saved.Launcher, Target: saved.Target}, Outcome: LaunchOutcome{Container: saved.Container, Pane: saved.Pane, SessionMarked: saved.SessionMarked, Replayed: true}}, nil
}
