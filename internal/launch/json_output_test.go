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

// Every launch form (start, resume, orchestrate, issue triage and result, chat) prepares its
// launcher through prepareLaunch, so a launcher that takes the terminal is refused there with
// --json, before a container or an agent exists.
func TestPrepareLaunchRefusesTerminalLaunchersWithJSON(t *testing.T) {
	for _, launcher := range []string{"foreground", "console"} {
		status, stdout, stderr := runJSON(func() int {
			plan, err := prepareLaunch(launcher, t.TempDir(), "start")
			if err == nil {
				t.Errorf("%s: no refusal, plan=%+v", launcher, plan)
				return 0
			}
			return command.FailStatus(2, err)
		})
		if status != 2 || stdout != "" {
			t.Fatalf("%s: status=%d stdout=%q stderr=%q", launcher, status, stdout, stderr)
		}
		var object struct {
			Error command.Notice `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &object); err != nil || object.Error.Code != command.CodeInteractive || !strings.Contains(object.Error.Message, launcher) {
			t.Fatalf("%s: stderr=%q %v", launcher, stderr, err)
		}
	}
}

// A confirmed release prints the claim object with --json and carries the closing remark as the
// notice coordinator_release_final; standard error stays empty.
func TestReleaseResultWithJSON(t *testing.T) {
	f := newReleaseFixture(t)
	status, stdout, stderr := commandtest.Capture(func() int { return RunCoordinatorRemote([]string{"--json", "release", "--end-execution", f.path}) })
	if status != 0 || stderr != "" {
		t.Fatalf("status %d\nstdout=%s\nstderr=%s", status, stdout, stderr)
	}
	var object struct {
		Claim   map[string]any   `json:"claim"`
		Notices []command.Notice `json:"notices"`
	}
	if err := json.Unmarshal([]byte(stdout), &object); err != nil || object.Claim["status"] != "released" {
		t.Fatalf("result %s: %v", stdout, err)
	}
	if len(object.Notices) != 1 || object.Notices[0].Code != "coordinator_release_final" || !strings.Contains(object.Notices[0].Message, f.group) {
		t.Fatalf("notices %+v", object.Notices)
	}
}

// An unconfirmed or misplaced --end-execution is a usage error object before any request.
func TestReleaseArgumentErrorsWithJSON(t *testing.T) {
	f := newReleaseFixture(t)
	f.requests.Store(0)
	for _, args := range [][]string{{"release", f.path}, {"release", f.path, "--end-execution"}, {"claim", "--end-execution", f.path}} {
		status, stdout, stderr := commandtest.Capture(func() int { return RunCoordinatorRemote(append([]string{"--json"}, args...)) })
		if status != 2 || stdout != "" || !strings.Contains(stderr, `"code":"usage"`) {
			t.Fatalf("%v: status %d\nstdout=%s\nstderr=%s", args, status, stdout, stderr)
		}
	}
	if n := f.requests.Load(); n != 0 {
		t.Fatalf("the commands sent %d requests", n)
	}
}
