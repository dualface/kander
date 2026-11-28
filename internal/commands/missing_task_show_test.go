package commands

import (
	"encoding/json"
	"testing"

	"github.com/dualface/kander/v2/internal/cli"
	"github.com/dualface/kander/v2/internal/command/commandtest"
)

// The service answers a missing task with not_found, and the client reports the
// board code that response carries instead of a generic internal error.
func TestShowMissingTaskThroughServiceIsNotFound(t *testing.T) {
	boundCardCLI(t)
	code, stdout, stderr := commandtest.Capture(func() int {
		return cli.Run([]string{"kander", "--lang", "en", "--json", "show", "20261128-missing-task"})
	})
	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if code != 1 || stdout != "" || json.Unmarshal([]byte(stderr), &out) != nil ||
		out.Error.Code != "board.task_does_not_exist" || out.Error.Message != "Task does not exist: 20261128-missing-task" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
