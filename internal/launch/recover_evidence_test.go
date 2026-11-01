package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"
)

func setCardField(t *testing.T, root, task, field, value string) {
	t.Helper()
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^- ` + field + `:.*$`)
	if err = writeCardFixture(root, s.Entry, re.ReplaceAllLiteralString(s.Text, "- "+field+": "+value)); err != nil {
		t.Fatal(err)
	}
}

func requireUntouched(t *testing.T, root, task string, request board.CoordinatorRecover, before board.Snapshot) {
	t.Helper()
	after, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.Entry.State != before.Entry.State {
		t.Fatalf("card changed: %+v", after.Entry)
	}
	c, err := board.ReadCoordinatorCheckpointContext(context.Background(), root, request.GroupID)
	if err != nil || len(c.Recoveries) != 0 {
		t.Fatalf("%+v %v", c.Recoveries, err)
	}
}

func requireRefused(t *testing.T, root, task string, request board.CoordinatorRecover, wants ...string) {
	t.Helper()
	before, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	code, errOut := runRecover(t, request)
	if code == 0 {
		t.Fatal("reset passed")
	}
	for _, want := range wants {
		if !strings.Contains(errOut, want) {
			t.Fatalf("no %q in %s", want, errOut)
		}
	}
	requireUntouched(t, root, task, request, before)
}

// codexRecoverRequest is a blocked Codex member with no reference whose recorded
// pane is gone.
func codexRecoverRequest(t *testing.T) (string, string, board.CoordinatorRecover) {
	t.Helper()
	root, task, request := blockedRecoverRequest(t, func(string) string { return "" })
	setCardField(t, root, task, "SESSION", "codex")
	return root, task, request
}

func TestRecoverResolvesACodexSessionAtAStaleAddress(t *testing.T) {
	t.Run("drifted", func(t *testing.T) {
		root, task, request := codexRecoverRequest(t)
		codexRolloutAt(t, task, "old", "codex-rollout-id", "", time.Now())
		t.Setenv("KANBAN_TMUX_LIST_PANES", "%5\t$9\tmoved\t@9\tcodex\t0\tcodex-rollout-id\t")
		requireRefused(t, root, task, request, liveness.Drifted, "tmux:$9:@9:%5")
	})
	t.Run("no executor", func(t *testing.T) {
		root, task, request := codexRecoverRequest(t)
		codexRolloutAt(t, task, "old", "codex-rollout-id", "", time.Now())
		t.Setenv("KANBAN_TMUX_LIST_PANES", "%5\t$9\tmoved\t@9\tcodex\t0\tanother-session\t")
		requireReset(t, root, request)
	})
	t.Run("resolution failed", func(t *testing.T) {
		root, task, request := codexRecoverRequest(t)
		s, err := board.ReadSnapshot(root, task)
		if err != nil {
			t.Fatal(err)
		}
		if report := recoverLivenessFn(context.Background(), s); report.Status != liveness.Unknown || report.Evidence != nil {
			t.Fatalf("report=%+v", report)
		}
		requireReset(t, root, request)
	})
}

// Resolving first could fail, or find a newer rollout, and change this result. No
// rollout exists here, so a resolution would end in unknown.
func TestRecoverKeepsALiveCodexPaneWithoutResolving(t *testing.T) {
	root, task, request := codexRecoverRequest(t)
	writeFakeTmux(t, filepath.Join(root, "fake-bin", "tmux"), filepath.Join(root, "tmux.log"))
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "codex")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "marker")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if report := recoverLivenessFn(context.Background(), s); report.Status != liveness.Alive {
		t.Fatalf("report=%+v", report)
	}
	requireRefused(t, root, task, request, liveness.Alive)
}

// A resolution that outlasts the budget ends with it and reads unknown, so the
// reset keeps its share of the command deadline.
func TestRecoverResolutionStaysInsideTheProbeBudget(t *testing.T) {
	root, _, request := codexRecoverRequest(t)
	savedBudget, savedResolve := recoverProbeBudget, resolveIdentityFn
	t.Cleanup(func() { recoverProbeBudget, resolveIdentityFn = savedBudget, savedResolve })
	recoverProbeBudget = 200 * time.Millisecond
	waited := false
	resolveIdentityFn = func(ctx context.Context, taskID, text string) (AgentSession, error) {
		<-ctx.Done()
		waited = true
		return AgentSession{}, ctx.Err()
	}
	started := time.Now()
	requireReset(t, root, request)
	if elapsed := time.Since(started); !waited || elapsed > 10*time.Second {
		t.Fatalf("waited=%v elapsed=%s", waited, elapsed)
	}
}

func TestCodexSessionScanStopsWithItsContext(t *testing.T) {
	_, task, _ := codexRecoverRequest(t)
	codexRolloutAt(t, task, "old", "codex-rollout-id", "", time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := codexSessionsForTask(ctx, task); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRecoverRefusesSeveralMatchesThenPassesOnceTheyStop(t *testing.T) {
	root, task, request := blockedRecoverRequest(t, func(reference string) string { return paneRow("%8", reference) + "\n" + paneRow("%9", reference) })
	requireRefused(t, root, task, request, "2 个 executor", "kander coordinator recover")
	// The refusal changed nothing, so the same request works after the panes stop.
	t.Setenv("KANBAN_TMUX_LIST_PANES", paneRow("%9", "another-session"))
	requireReset(t, root, request)
}

func TestRecoverRefusesATmuxPaneWithoutAMarker(t *testing.T) {
	root, task, request := blockedRecoverRequest(t, func(string) string { return "" })
	writeFakeTmux(t, filepath.Join(root, "fake-bin", "tmux"), filepath.Join(root, "tmux.log"))
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "claude")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "")
	if err := os.Remove(filepath.Join(root, "tmux.log.pane-session")); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	report := recoverLivenessFn(context.Background(), s)
	if report.Status != liveness.Unknown || report.Evidence == nil {
		t.Fatalf("report=%+v", report)
	}
	requireRefused(t, root, task, request, report.Detail, "tmux:$42:@9:%9")
}

// herdrRecoverRequest moves a blocked member to herdr. KANBAN_RECORDED_PANE and
// KANBAN_FOUND_PANE answer pane get for the recorded and the found pane; an empty
// recorded answer means the pane is gone.
func herdrRecoverRequest(t *testing.T) (string, string, board.CoordinatorRecover) {
	t.Helper()
	root, task, request := blockedRecoverRequest(t, func(string) string { return "" })
	setCardField(t, root, task, "SESSION", "pi wanted")
	setCardField(t, root, task, "WINDOW", "herdr:w1:t1:w1:p1")
	writeRecoverHerdr(t, root)
	return root, task, request
}

func writeRecoverHerdr(t *testing.T, root string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$2" = "list" ]; then
  printf '%s\n' "$KANBAN_HERDR_LIST_JSON"
  exit 0
fi
if [ "$3" = "w1:p1" ] && [ -z "$KANBAN_RECORDED_PANE" ]; then
  printf '%s\n' '{"error":{"code":"pane_not_found","message":"gone"}}' >&2
  exit 1
fi
if [ "$3" = "w1:p1" ]; then
  printf '{"result":{"pane":%s}}\n' "$KANBAN_RECORDED_PANE"
  exit 0
fi
printf '{"result":{"pane":%s}}\n' "$KANBAN_FOUND_PANE"
`
	if err := os.WriteFile(filepath.Join(root, "fake-bin", "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func herdrPane(pane, tab, status, session string) string {
	return `{"pane_id":"` + pane + `","tab_id":"` + tab + `","agent":"pi","agent_status":"` + status + `","agent_session":` + session + `}`
}

// herdrFoundList is the pane list of an executor that moved to w9:p9.
const herdrFoundList = `{"result":{"panes":[{"pane_id":"w9:p9","tab_id":"w9:t9","agent":"pi","agent_status":"idle","agent_session":{"value":"wanted"}}]}}`

func TestRecoverRefusesAHerdrPaneItCouldNotProve(t *testing.T) {
	const (
		id       = `{"value":"wanted"}`
		unknown  = `{"kind":"path","value":"/no/such/session"}`
		recorded = "herdr:w1:t1:w1:p1"
		moved    = "herdr:w9:t9:w9:p9"
	)
	for name, tc := range map[string]struct{ recorded, found, want string }{
		"recorded identity uncertain": {recorded: herdrPane("w1:p1", "w1:t1", "idle", unknown), want: recorded},
		"recorded status unknown":     {recorded: herdrPane("w1:p1", "w1:t1", "strange", id), want: recorded},
		"found identity uncertain":    {found: herdrPane("w9:p9", "w9:t9", "idle", unknown), want: moved},
		"found status unknown":        {found: herdrPane("w9:p9", "w9:t9", "strange", id), want: moved},
	} {
		t.Run(name, func(t *testing.T) {
			root, task, request := herdrRecoverRequest(t)
			t.Setenv("KANBAN_RECORDED_PANE", tc.recorded)
			t.Setenv("KANBAN_FOUND_PANE", tc.found)
			t.Setenv("KANBAN_HERDR_LIST_JSON", herdrFoundList)
			requireRefused(t, root, task, request, liveness.Unknown, tc.want, "kander coordinator recover")
		})
	}
}

func TestRecoverRefusalNamesAStepThatWorks(t *testing.T) {
	card := func(state string) board.Snapshot {
		return board.Snapshot{Entry: board.Entry{TaskID: "task", State: state}, Text: "- WINDOW: tmux:$1:@1:%1\n"}
	}
	for name, tc := range map[string]struct {
		state  string
		report liveness.Report
		wants  []string
		absent string
	}{
		"working alive":   {"working", liveness.Report{Status: liveness.Alive, Detail: "seen"}, []string{"seen", "tmux:$1:@1:%1", "resume --agent", "coordinator recover"}, ""},
		"working drifted": {"working", liveness.Report{Status: liveness.Drifted, NewWindow: "tmux:$9:@9:%9"}, []string{"tmux:$9:@9:%9", "resume --agent"}, "%1"},
		"review alive":    {"review", liveness.Report{Status: liveness.AliveOrphaned}, []string{"tmux:$1:@1:%1", "coordinator recover"}, "resume --agent"},
		"review drifted":  {"review", liveness.Report{Status: liveness.Drifted, NewWindow: "tmux:$9:@9:%9"}, []string{"tmux:$9:@9:%9", "coordinator recover"}, "resume --agent"},
		"working evidence": {"working", liveness.Report{Status: liveness.Unknown, Evidence: &liveness.ExecutorEvidence{Window: "tmux:$9:@9:%9"}},
			[]string{"tmux:$9:@9:%9", "coordinator recover"}, "resume --agent"},
		"several matches": {"working", liveness.Report{Status: liveness.Unknown, Evidence: &liveness.ExecutorEvidence{Matches: 3}}, []string{"3", "session", "coordinator recover"}, "resume --agent"},
	} {
		t.Run(name, func(t *testing.T) {
			err := recoverRefusal(card(tc.state), tc.report)
			if err == nil {
				t.Fatal("no refusal")
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("no %q in %s", want, err)
				}
			}
			if tc.absent != "" && strings.Contains(err.Error(), tc.absent) {
				t.Fatalf("%q in %s", tc.absent, err)
			}
		})
	}
	for _, report := range []liveness.Report{{Status: liveness.Unknown}, {Status: liveness.Stopped}, {Status: liveness.IdentityMismatch}, {Status: liveness.Unprobeable}} {
		if err := recoverRefusal(card("working"), report); err != nil {
			t.Fatalf("%s: %v", report.Status, err)
		}
	}
}

func TestRecoverRefusesAPendingStartAndNamesItsWaysOut(t *testing.T) {
	root, task, request := blockedRecoverRequest(t, func(string) string { return "" })
	takeoverFixture(t, root, task)
	requireRefused(t, root, task, request, "--adopt --decision", "--agent <agent> --decision")
	requireRecord(t, root, task, board.ExecutionPending)
}

// A takeover refuses a binding, so the error must end it first for its last step to work.
func TestRecoverOfABoundPendingStartNamesAWayOutThatWorks(t *testing.T) {
	root, task, in, c := launchCoordinatorFixture(t)
	d, err := PrepareBoundDispatch(root, in)
	if err != nil {
		t.Fatal(err)
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	r := board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: {Revision: s.Revision, DispatchID: "stale-round", Epoch: 1}}}
	blocked, _ := ReconcileCoordinator(context.Background(), root, r)
	if blocked.Members[task].Blocked == nil {
		t.Fatal("no block")
	}
	takeoverFixture(t, root, task)
	request := board.CoordinatorRecover{GroupID: blocked.GroupID, Member: task, ExpectedRevision: blocked.Revision, ExpectedEpoch: blocked.Authority.Epoch, Decision: "user-decision", Reason: "reset"}
	requireRefused(t, root, task, request, "--adopt --decision", "dispatch fail", d.Input.ID, "--agent <agent> --decision")
	code := 0
	_, errOut, _ := capture(t, func() error {
		code = RunDispatch([]string{"cancel", task, d.Input.ID, strconv.FormatUint(d.Revision, 10), "starter gone", "--decision", "user"})
		return nil
	})
	if code != 0 {
		t.Fatalf("code=%d %s", code, errOut)
	}
	if code, errOut := resumeReview(t, "--agent", "claude", "--decision", "user", "--launcher", "tmux", "--message", "take over", task); code != 0 {
		t.Fatalf("code=%d %s", code, errOut)
	}
	requireRecord(t, root, task, board.ExecutionRunning)
}

// unresolved keeps its way through the probe: its starter has ended.
func TestRecoverOfAnUnresolvedStartFollowsTheProbe(t *testing.T) {
	saved := recoverLivenessFn
	t.Cleanup(func() { recoverLivenessFn = saved })
	root, task, request := blockedRecoverRequest(t, func(string) string { return "" })
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	if _, err = board.BeginTakeover(root, s.Entry, board.TakeoverInput{Owner: "claude", Session: "claude taken", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if _, err = board.UnresolveExecution(root, s.Entry, "", "late failure"); err != nil {
		t.Fatal(err)
	}
	requireRecord(t, root, task, board.ExecutionUnresolved)
	recoverLivenessFn = func(context.Context, board.Snapshot) liveness.Report { return liveness.Report{Status: liveness.Alive} }
	if _, err = RecoverCoordinator(context.Background(), root, request); err == nil {
		t.Fatal("alive passed")
	}
	recoverLivenessFn = func(context.Context, board.Snapshot) liveness.Report {
		return liveness.Report{Status: liveness.Stopped}
	}
	if _, err = RecoverCoordinator(context.Background(), root, request); err != nil {
		t.Fatal(err)
	}
}

// reviewMember is a group member in review with no dispatch binding.
func reviewMember(t *testing.T) (string, string) {
	t.Helper()
	root, task, _, _ := launchCoordinatorFixture(t)
	if s, err := board.ReadSnapshot(root, task); err != nil || s.Entry.State != "review" {
		t.Fatalf("%+v %v", s.Entry, err)
	}
	return root, task
}

func resumeReview(t *testing.T, args ...string) (int, string) {
	t.Helper()
	code := 0
	_, errOut, _ := capture(t, func() error {
		code = RunResume(args)
		return nil
	})
	return code, errOut
}

func TestReviewMemberTakesATakeoverOnlyWhileItsStartIsUnfinished(t *testing.T) {
	root, task := reviewMember(t)
	args := []string{"--agent", "claude", "--decision", "user", "--launcher", "tmux", "--message", "take over", task}
	if code, errOut := resumeReview(t, args...); code == 0 || !strings.Contains(errOut, "review") {
		t.Fatalf("running record: code=%d %s", code, errOut)
	}
	requireRecord(t, root, task, board.ExecutionRunning)
	takeoverFixture(t, root, task)
	if code, errOut := resumeReview(t, args...); code != 0 {
		t.Fatalf("pending record: code=%d %s", code, errOut)
	}
	if record := requireRecord(t, root, task, board.ExecutionRunning); record.Kind != board.ExecutionTakeover {
		t.Fatalf("record %+v", record)
	}
	if s, err := board.ReadSnapshot(root, task); err != nil || s.Entry.State != "review" {
		t.Fatalf("%+v %v", s.Entry, err)
	}
}
