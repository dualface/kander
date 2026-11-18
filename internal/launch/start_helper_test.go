package launch

import (
	"fmt"
	"os"

	"github.com/dualface/kander/internal/board"
)

// commandStart starts one card of a local board and reports the result the
// way a start command does: warnings on stderr, then the launch report. An
// empty taskID selects the only todo card. It drives Start, which the terminal
// board calls with a local board root.
func commandStart(root, agentOverride, launcherOverride, taskID string) error {
	if taskID == "" {
		loaded, err := loadBoardFn(root)
		if err != nil {
			return err
		}
		entry, err := selectTodo(loaded, "")
		if err != nil {
			return err
		}
		taskID = entry.TaskID
	}
	result, err := Start(root, agentOverride, launcherOverride, taskID)
	for _, warning := range result.Warnings {
		fmt.Fprint(os.Stderr, warning)
	}
	if err != nil {
		return err
	}
	if result.Superseded {
		stepAside(result.TaskID, result.Plan, result.Outcome)
		return nil
	}
	return reportLaunch(t("launch.started"), board.Entry{TaskID: result.TaskID, Kind: result.Size}, result.Agent, result.Plan, result.Outcome)
}

// startOrchestratorLocal runs the orchestrator start against the cards of a
// local board root, without a server session.
func startOrchestratorLocal(request OrchestrateRequest) (OrchestrateResult, error) {
	var warnings board.WarningLog
	cfg, err := loadEffective()
	if err != nil {
		return OrchestrateResult{}, err
	}
	loaded, err := board.LoadBoardWithWarnings(request.Root, &warnings)
	if err != nil {
		return OrchestrateResult{}, err
	}
	result, err := startOrchestrator(request, cfg, loaded, nil)
	result.Warnings = append(warnings.Messages(), result.Warnings...)
	return result, err
}
