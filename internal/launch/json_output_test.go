package launch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/command/commandtest"
)

// runJSON runs run under a command that was given --json and returns its exit status and what
// the command layer wrote to stdout and stderr.
func runJSON(run func() int) (int, string, string) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return command.Exit(run()) }}
	return commandtest.Capture(func() int { return commandtest.Run(c, "--json") })
}

// The outcome of a reconcile is the result object with --json, with the exit status that the
// text form has; a request that was refused is the error object and prints no checkpoint.
func TestReconcileOutcomeWithJSON(t *testing.T) {
	blocked := board.CoordinatorCheckpoint{Members: map[string]board.CoordinatorMember{"a": {Blocked: &board.CoordinatorBlock{Reason: "x"}}, "b": {}}}
	for _, tc := range []struct {
		name   string
		c      board.CoordinatorCheckpoint
		err    error
		status int
		result string
		notice string
	}{
		{"complete", board.CoordinatorCheckpoint{Members: map[string]board.CoordinatorMember{"a": {}}}, nil, 0, "complete", ""},
		{"partial", blocked, nil, 3, "partial", ""},
		{"failed", blocked, &board.CoordinatorBlockedError{Group: "g", Reason: "x"}, 1, "failed", "reconcile_blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, stdout, stderr := runJSON(func() int { return finishReconcile(tc.c, tc.err) })
			if status != tc.status || stderr != "" {
				t.Fatalf("status=%d stderr=%q", status, stderr)
			}
			var decoded struct {
				Result  string           `json:"result"`
				Notices []command.Notice `json:"notices"`
			}
			if e := json.Unmarshal([]byte(stdout), &decoded); e != nil || decoded.Result != tc.result {
				t.Fatalf("%s %v", stdout, e)
			}
			if (tc.notice == "") != (len(decoded.Notices) == 0) || len(decoded.Notices) > 0 && decoded.Notices[0].Code != tc.notice {
				t.Fatalf("notices=%+v", decoded.Notices)
			}
		})
	}
	status, stdout, stderr := runJSON(func() int { return finishReconcile(board.CoordinatorCheckpoint{}, errors.New("checkpoint CAS")) })
	if status != 1 || stdout != "" || !strings.Contains(stderr, `"error":{"code":"failed","message":"checkpoint CAS"}`) {
		t.Fatalf("refused: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}
