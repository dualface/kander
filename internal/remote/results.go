package remote

import (
	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/command"
)

// taskRef is the result object of a command that creates or moves one card.
type taskRef struct {
	TaskID   string `json:"task_id"`
	State    string `json:"state"`
	Location string `json:"location"`
	Revision uint64 `json:"revision"`
}

func newTaskRef(s *Session, t api.Task) taskRef {
	return taskRef{TaskID: t.TaskID, State: t.State, Location: location(s, t.TaskID), Revision: t.Revision}
}

// printTask ends a command that created or moved one card: the card location line, or with
// --json the taskRef.
func printTask(s *Session, t api.Task) error {
	if command.JSON() {
		return command.Exit(command.Result(newTaskRef(s, t)))
	}
	_, err := stdout.Write([]byte(location(s, t.TaskID) + "\n"))
	return err
}

// updateResult is the result object of kander update.
type updateResult struct {
	TaskID   string `json:"task_id"`
	Document string `json:"document"`
	Revision uint64 `json:"revision"`
}

// moveResult is the result object of a plain kander move.
type moveResult struct {
	taskRef
	Replayed         bool   `json:"replayed"`
	CompletionReport string `json:"completion_report,omitempty"`
}

// boundMoveResult is what a move bound to a dispatch prints, with and without --json: the move
// receipt and, with --json, the completion report that the text form prints on stderr.
type boundMoveResult struct {
	*api.MoveReceipt
	CompletionReport string `json:"completion_report,omitempty"`
}

// claimResult is the result object of a claiming move.
type claimResult struct {
	taskRef
	Generation  uint64 `json:"generation"`
	ExecutionID string `json:"execution_id"`
}

// bindResult is the result object of kander init.
type bindResult struct {
	ProjectName string `json:"project_name"`
	ProjectID   string `json:"project_id"`
	ServerURL   string `json:"server_url"`
	BoardPath   string `json:"board_path,omitempty"`
}

// guardResult is the result object of kander guard-write.
type guardResult struct {
	Path    string `json:"path"`
	Allowed bool   `json:"allowed"`
}

// requestList is the result object of kander request list.
type requestList struct {
	Requests []api.CollabRequest `json:"requests"`
}
