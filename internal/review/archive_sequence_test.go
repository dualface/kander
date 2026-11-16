//go:build unix

package review

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/statemachine/reviewrun"
)

// recordingStore records the calls of the execution driver that change a run:
// the phase updates and the finalization.
type recordingStore struct {
	reviewStore
	calls []string
}

func (s *recordingStore) Update(run board.ReviewRun) error {
	s.calls = append(s.calls, "update "+run.Phase+" "+run.LaunchStatus)
	return s.reviewStore.Update(run)
}

func (s *recordingStore) Finalize(run board.ReviewRun, report []byte) (board.ReviewRun, error) {
	s.calls = append(s.calls, "finalize "+run.LaunchStatus+" "+run.ExecutionStatus)
	return s.reviewStore.Finalize(run, report)
}

// archiveSequences is the set the Lean machine proves accepted
// (Proofs/ReviewRun.lean, archiveSequences): the sequences of calls the driver
// makes on one run, written as the persisted values.
var archiveSequences = [][]string{
	{"update launching unknown", "update running started", "finalize started ok"},
	{"update launching unknown", "update running started", "finalize started failed"},
	{"update launching unknown", "finalize not_started not_started"},
	{"finalize not_started not_started"},
	{"finalize not_started interrupted"},
	{"update launching unknown", "finalize unknown interrupted"},
	{"update launching unknown", "update running started", "finalize started interrupted"},
}

// kernelEvent maps one recorded call to the kernel event it is.
func kernelEvent(call string) reviewrun.Event {
	words := strings.Fields(call)
	launch := map[string]reviewrun.Launch{"unknown": reviewrun.LaunchUnknown, "started": reviewrun.LaunchStarted,
		"not_started": reviewrun.LaunchNotStarted}
	if words[0] == "update" {
		phase := map[string]reviewrun.Phase{"launching": reviewrun.PhaseLaunching, "running": reviewrun.PhaseRunning}
		return reviewrun.Update{ToPhase: phase[words[1]], Launch: launch[words[2]], Exec: reviewrun.ExecIncomplete,
			ExitZero: true, SameInput: true}
	}
	exec := map[string]reviewrun.Exec{"ok": reviewrun.ExecOk, "failed": reviewrun.ExecFailed,
		"not_started": reviewrun.ExecNotStarted, "interrupted": reviewrun.ExecInterrupted}
	ok := words[2] == "ok"
	return reviewrun.Finalize{Launch: launch[words[1]], Exec: exec[words[2]], ExitZero: ok, ReportOk: true, Reason: !ok,
		Complete: true, SameInput: true}
}

// archiveDrive runs the real execution path of one archived review on the
// harness's board with the store calls recorded. A run that exists is recovered,
// as runReview does; a new one goes through executeReview. prefix is the calls
// that made the run what it was before this invocation. before runs after the
// invocation has prepared the run and before it executes, to break the
// environment or stage a crash.
func archiveDrive(t *testing.T, args []string, prefix []string, before func(*reviewContext, *recordingStore)) []string {
	t.Helper()
	options, remaining, err := parseArchiveOptions(args)
	if err != nil {
		t.Fatal(err)
	}
	agent, rest, err := splitAgentArgs(remaining)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := validateContextMode(agent, rest, false, "")
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingStore{reviewStore: localReviewStore{root: os.Getenv(board.EnvBoardDir)}, calls: slices.Clone(prefix)}
	fresh, err := archiveInvocation(&ctx, options, rest, store.Root(), board.FindingsSchemaReceiver, store)
	if err != nil {
		t.Fatal(err)
	}
	if before != nil {
		before(&ctx, store)
	}
	if fresh {
		executeReview(ctx, make(chan os.Signal, 1))
	} else {
		ctx.archive.recover()
	}
	return store.calls
}

// stageCrash leaves a run unfinished as a driver killed after the given phase
// updates would, through the same calls the driver makes.
func stageCrash(t *testing.T, args []string, phases ...[2]string) []string {
	t.Helper()
	var calls []string
	archiveDrive(t, args, nil, func(ctx *reviewContext, store *recordingStore) {
		for _, p := range phases {
			if err := ctx.archive.phase(p[0], p[1]); err != nil {
				t.Fatal(err)
			}
		}
		calls = slices.Clone(store.calls)
		// The run stays unfinished: the invocation that follows replays it.
		ctx.archive = nil
	})
	return calls
}

func inProvedSet(calls []string) bool {
	for _, want := range archiveSequences {
		if slices.Equal(want, calls) {
			return true
		}
	}
	return false
}

// The calls the execution driver makes on a run are the sequences the Lean machine
// proves accepted. Each case runs the real path of the driver, with only the
// reviewer process and the environment faked: a reviewer that succeeds, one that
// fails, one that cannot start, a private runtime directory that cannot be
// created, and the replay of a run left unfinished after each phase. A run that
// loses or reorders a phase update records another sequence and fails here.
func TestArchiveCallSequencesAreTheProvedSet(t *testing.T) {
	crashAt := func(phases ...[2]string) func(t *testing.T, h *reviewHarness, args []string) []string {
		return func(t *testing.T, h *reviewHarness, args []string) []string {
			prefix := stageCrash(t, args, phases...)
			return archiveDrive(t, args, prefix, nil)
		}
	}
	cases := []struct {
		name string
		run  func(t *testing.T, h *reviewHarness, args []string) []string
	}{
		{"ok", func(t *testing.T, h *reviewHarness, args []string) []string {
			t.Setenv("FAKE_CODEX_REPORT", "PASS\n"+emptyStructuredReview)
			return archiveDrive(t, args, nil, nil)
		}},
		{"failed", func(t *testing.T, h *reviewHarness, args []string) []string {
			t.Setenv("FAKE_CODEX_FAIL", "1")
			return archiveDrive(t, args, nil, nil)
		}},
		{"launch failure", func(t *testing.T, h *reviewHarness, args []string) []string {
			writeFake(t, h.fake, "#!/nonexistent/kander-test-interpreter\n")
			return archiveDrive(t, args, nil, nil)
		}},
		{"runtime directory failure", func(t *testing.T, h *reviewHarness, args []string) []string {
			return archiveDrive(t, args, nil, func(ctx *reviewContext, _ *recordingStore) {
				ctx.tempRoot = filepath.Join(h.root, "missing-temp-root")
			})
		}},
		{"replay of a prepared run", crashAt()},
		{"replay of a launching run", crashAt([2]string{"launching", "unknown"})},
		{"replay of a running run", crashAt([2]string{"launching", "unknown"}, [2]string{"running", "started"})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, _, args := archiveHarness(t)
			calls := c.run(t, h, args)
			if !inProvedSet(calls) {
				t.Fatalf("calls %q are not a proved sequence", calls)
			}
		})
	}
}

// The kernel accepts every proved sequence from a prepared run, and the set is
// closed: a driver that skips the launching update before the running one, or
// updates a run after it finalized, makes a sequence the kernel refuses.
func TestArchiveProvedSequencesAreAcceptedByTheKernel(t *testing.T) {
	prepared := func() reviewrun.State {
		st, ok := reviewrun.Step(reviewrun.Initial(reviewrun.Params{}), reviewrun.Create{Ids: true, Cards: true, Batch: true, Role: true}, reviewrun.Params{})
		if !ok {
			t.Fatal("the kernel refuses a new run")
		}
		return st
	}
	replay := func(calls []string) bool {
		st := prepared()
		for _, call := range calls {
			var ok bool
			if st, ok = reviewrun.Step(st, kernelEvent(call), reviewrun.Params{}); !ok {
				return false
			}
		}
		return true
	}
	for _, seq := range archiveSequences {
		if !replay(seq) {
			t.Fatalf("the kernel refuses the proved sequence %q", seq)
		}
	}
	after := append(slices.Clone(archiveSequences[0]), "update running started")
	if replay(after) {
		t.Fatal("the kernel accepts an update of a finalized run")
	}
	if inProvedSet([]string{"update running started", "finalize started ok"}) {
		t.Fatal("a run that skips the launching update is in the proved set")
	}
}
