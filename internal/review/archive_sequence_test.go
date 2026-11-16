//go:build unix

package review

import (
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

// archiveSequences is the set the Lean machine accepts
// (Proofs/ReviewRun.lean, archiveSequences): the sequences of calls the driver
// makes on one run, written as the persisted values.
var archiveSequences = [][]string{
	{"update launching unknown", "update running started", "finalize started ok"},
	{"update launching unknown", "update running started", "finalize started failed"},
	{"update launching unknown", "finalize not_started not_started"},
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

// The calls the execution driver makes on a run are the sequences the Lean
// machine proves accepted, and the kernel accepts each of them.
func TestArchiveCallSequencesAreTheProvedSet(t *testing.T) {
	h, root, _ := archiveHarness(t)
	id := "20260907-archive-test-task"
	prepare := func(runID string) (*recordingStore, *archiveExecution) {
		t.Helper()
		store := &recordingStore{reviewStore: localReviewStore{root: root}}
		input := board.ReviewInput{RunID: runID, BatchID: "batch-" + runID, TaskIDs: []string{id}, Role: "PMQA", Reviewer: "codex",
			Model: "m", Effort: "high", CWD: h.repo, Base: h.base, Commit: h.head, ReportLanguage: "zh-CN"}
		files := map[string][]byte{"task-context.md": []byte("task"), "review-context.md": []byte("context")}
		run, fresh, err := store.Prepare(input, map[string]string{"PMQA": "required", "Security": "N/A: project"}, nil, files, "test")
		if err != nil || !fresh {
			t.Fatalf("%v %v", fresh, err)
		}
		staging, err := store.Staging(runID)
		if err != nil {
			t.Fatal(err)
		}
		return store, &archiveExecution{store: store, root: root, run: run, staging: staging, report: []byte("PASS\n")}
	}
	cases := map[string]func(*archiveExecution){
		"ok": func(a *archiveExecution) {
			_ = a.phase("launching", "unknown")
			_ = a.phase("running", "started")
			a.finish(0)
		},
		"failed": func(a *archiveExecution) {
			_ = a.phase("launching", "unknown")
			_ = a.phase("running", "started")
			a.finish(1)
		},
		"launch failure": func(a *archiveExecution) {
			_ = a.phase("launching", "unknown")
			a.run.LaunchStatus = "not_started"
			a.finish(127)
		},
		"recover from prepared": func(a *archiveExecution) { a.recover() },
		"recover from launching": func(a *archiveExecution) {
			_ = a.phase("launching", "unknown")
			a.recover()
		},
		"recover from running": func(a *archiveExecution) {
			_ = a.phase("launching", "unknown")
			_ = a.phase("running", "started")
			a.recover()
		},
	}
	for name, drive := range cases {
		t.Run(name, func(t *testing.T) {
			store, a := prepare(strings.ReplaceAll(name, " ", "-"))
			drive(a)
			found := false
			for _, want := range archiveSequences {
				if strings.Join(want, ";") == strings.Join(store.calls, ";") {
					found = true
				}
			}
			if !found {
				t.Fatalf("calls %q are not a proved sequence", store.calls)
			}
		})
	}
	// The kernel accepts every proved sequence from a prepared run.
	for _, seq := range archiveSequences {
		st, _ := reviewrun.Step(reviewrun.Initial(reviewrun.Params{}), reviewrun.Create{Ids: true, Cards: true, Batch: true, Role: true}, reviewrun.Params{})
		for _, call := range seq {
			var ok bool
			if st, ok = reviewrun.Step(st, kernelEvent(call), reviewrun.Params{}); !ok {
				t.Fatalf("the kernel refuses %q in %q", call, seq)
			}
		}
	}
	// A call outside the set is refused: an update after the run is finalized.
	last := archiveSequences[0]
	st, _ := reviewrun.Step(reviewrun.Initial(reviewrun.Params{}), reviewrun.Create{Ids: true, Cards: true, Batch: true, Role: true}, reviewrun.Params{})
	for _, call := range last {
		st, _ = reviewrun.Step(st, kernelEvent(call), reviewrun.Params{})
	}
	if _, ok := reviewrun.Step(st, kernelEvent("update running started"), reviewrun.Params{}); ok {
		t.Fatal("the kernel accepts an update of a finalized run")
	}
}
