package board

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The owner table of the card that added this test; the repair cards shrink it.
var modelExpectedOwners = map[string]string{
	"DE-01": cardIssue22, "DE-03": cardIssue22, "DE-04": cardIssue22, "DE-15": cardIssue22,
	"DE-02": cardLedger, "DE-07": cardLedger, "DE-09": cardLedger, "DE-14": cardLedger, "DE-16": cardLedger, "DE-17": cardLedger, "DE-19": cardLedger, "DE-20": cardLedger,
	"DE-08": cardEvidence, "DE-10": cardEvidence, "DE-11": cardEvidence, "DE-12": cardEvidence, "DE-13": cardEvidence,
	"DE-18": cardRecovery,
}

func TestModelRegistriesAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, entry := range modelDeadEnds {
		if seen[entry.id] {
			t.Errorf("%s listed twice", entry.id)
		}
		seen[entry.id] = true
		if want, ok := modelExpectedOwners[entry.id]; ok && entry.owner != want {
			t.Errorf("%s: owner %s, want %s", entry.id, entry.owner, want)
		}
		switch entry.class {
		case modelReached:
			if entry.covers == nil {
				t.Errorf("%s: a model entry needs a state rule", entry.id)
			}
		case modelDirected:
			package_, test, ok := strings.Cut(entry.proof, ":")
			if !ok || !modelTestExists(t, package_, test) {
				t.Errorf("%s: directed test %q not found", entry.id, entry.proof)
			}
		case modelOutside:
			if entry.proof == "" {
				t.Errorf("%s: an outside entry needs the reason no test reaches it", entry.id)
			}
		default:
			t.Errorf("%s: unknown class %q", entry.id, entry.class)
		}
	}
	for id := range modelExpectedOwners {
		if !seen[id] {
			t.Errorf("%s is missing from the allow list", id)
		}
	}
	for _, closed := range []string{"DE-05", "DE-06"} {
		if seen[closed] {
			t.Errorf("%s was closed by #20 and must stay off the allow list", closed)
		}
	}
	var ids []string
	for _, inv := range modelInvariants {
		ids = append(ids, inv.id)
		if (inv.check == nil) == (inv.owner == "") {
			t.Errorf("%s: an invariant has a check or an owner card, never both or neither", inv.id)
		}
	}
	if !slices.Equal(ids, []string{"I1", "I2", "I3", "I4", "I5", "I6", "I7", "I8", "I9"}) {
		t.Errorf("invariant table %v", ids)
	}
}

// modelTestExists scans the sources: the launch package imports this one, so
// this test cannot reference its directed tests directly.
func modelTestExists(t *testing.T, relative, name string) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", relative, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "func "+name+"(t *testing.T)") {
			return true
		}
	}
	return false
}

// modelPoison breaks a card copy the done gate verifies. It stands for a new
// transition that strands a card: nothing on the allow list covers it.
func modelPoison() modelOp {
	return modelOp{name: "Z.poison", cli: "injected dead end", shared: true, narrows: []string{modelTailOp},
		guard: func(f *modelFacts, _ int) bool { return f.review.planned() },
		apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
			return os.WriteFile(filepath.Join(f.world.root, f.cards[0].Entry.State, baseEntryName(f.cards[0].Entry), "reviews", "plan.json"), []byte("{}\n"), 0o600)
		}}
}

// A removed row, a repaired dead end and an injected dead end each fail the
// verdict, so the allow list can neither go stale nor hide a new dead end.
func TestModelVerdictRejectsListDrift(t *testing.T) {
	modelFastTemp(t)
	start := modelInitial(t, false, 1)
	listed := []modelDeadEnd{modelDeadEnds[slices.IndexFunc(modelDeadEnds, func(e modelDeadEnd) bool { return e.id == "DE-08" })]}
	seeds := []modelSeed{{name: "stray", steps: []string{"H.claim", "R.planNA", "R.runStray"}}}
	runs := []modelRun{modelExplore(t, "stray", start, seeds, modelBounds{explore: 1, exit: 1}, modelOps())}
	if failures := modelVerdict(runs, listed); len(failures) != 0 {
		t.Fatalf("the listed dead end failed the verdict: %v", failures)
	}
	if failures := modelVerdict(runs, nil); len(failures) == 0 || !strings.Contains(failures[0], "unlisted dead end") {
		t.Fatalf("a dead end removed from the list passed: %v", failures)
	}
	gone := append(slices.Clone(listed), modelDeadEnd{id: "DE-11", owner: cardEvidence, class: modelReached, covers: func(modelDiagnosis) bool { return false }})
	if failures := modelVerdict(runs, gone); len(failures) != 1 || !strings.Contains(failures[0], "no longer reaches") {
		t.Fatalf("a listed dead end that disappeared passed: %v", failures)
	}
	seeds = []modelSeed{{name: "injected", steps: []string{"H.claim", "R.planNA", "Z.poison"}}}
	runs = []modelRun{modelExplore(t, "injected", start, seeds, modelBounds{explore: 1, exit: 1}, append(modelOps(), modelPoison()))}
	if failures := modelVerdict(runs, listed); !slices.ContainsFunc(failures, func(f string) bool { return strings.Contains(f, "unlisted dead end") }) {
		t.Fatalf("an injected dead end passed: %v", failures)
	}
}

func TestModelInvariantI7RejectsAdvanceOverOpenFinding(t *testing.T) {
	run := modelRunFacts{id: "run", findings: []string{"PMQA-01"}, assigned: map[string][]string{"PMQA-01": {"card"}}}
	before := &modelFacts{review: modelReviewFacts{batches: []modelBatchFacts{{id: "batch", target: modelCommit(1), runs: []modelRunFacts{run}}}}}
	after := &modelFacts{review: modelReviewFacts{batches: []modelBatchFacts{{id: "batch", target: modelCommit(2), runs: []modelRunFacts{run}}}}}
	if modelCheckI7(before, after) == nil {
		t.Fatal("an advance over an undisposed finding passed")
	}
	run.records = []ReviewDisposition{{FindingID: "PMQA-01", TaskID: "card"}}
	before.review.batches[0].runs[0] = run
	if err := modelCheckI7(before, after); err != nil {
		t.Fatal(err)
	}
}

func modelWalk(t *testing.T, s *modelSearch, w *modelWorld, steps ...string) *modelWorld {
	t.Helper()
	w, err := s.applySeed(w, modelSeed{steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func modelRefuses(t *testing.T, s *modelSearch, w *modelWorld, step string) {
	t.Helper()
	copied, err := s.applySeed(w, modelSeed{steps: []string{step}})
	var infra modelInfraError
	if err == nil || errors.As(err, &infra) {
		t.Fatalf("%s: want a refusal, got %v (%v)", step, err, copied != nil)
	}
}

func modelAllDone(t *testing.T, w *modelWorld) {
	t.Helper()
	f, err := w.read()
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range f.cards {
		if s.Entry.State != "done" {
			t.Fatalf("%s ended in %s", w.members[i].label, s.Entry.State)
		}
	}
	if err = w.reconcile(); err != nil {
		t.Fatal(err)
	}
}

// DE-05 and DE-06 were closed by #20. The search reports any state with their
// signature as unlisted; this test walks both arrival paths to done.
func TestModelConfirmsIssue20ClosedItsDeadEnds(t *testing.T) {
	modelFastTemp(t)
	start := modelInitial(t, true, 2)
	s := &modelSearch{ops: modelOps(), index: map[string]int{}, scratch: t.TempDir()}
	t.Run("DE-05 reclaim of a never dispatched member", func(t *testing.T) {
		w := modelWalk(t, s, start, "A:H.claim", "B:H.claim", "C.reconcile", "R.planNA", "A:X.review", "C.reconcile")
		modelRefuses(t, s, w, "A:H.reclaim")
		w = modelWalk(t, s, w, "A:H.reclaimDecision", "R.rebind", "C.reconcile", "A:F.done", "B:F.done", "C.reconcile")
		modelAllDone(t, w)
		c, err := ReadCoordinatorCheckpoint(w.root, coordinatorGroup)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Members[w.members[0].id].Handoffs) != 1 {
			t.Fatalf("the reclaim left no handoff: %+v", c.Members[w.members[0].id])
		}
	})
	t.Run("DE-06 advance before the fix round", func(t *testing.T) {
		w := modelWalk(t, s, start, "A:H.claim", "B:H.claim", "C.reconcile", "R.planRequired", "R.run", "B:R.assign", "C.reconcile", "B:X.review", "C.reconcile")
		modelRefuses(t, s, w, "R.advance")
		w = modelWalk(t, s, w, "B:D.prepareFix", "B:D.accept", "B:X.dispose", "B:D.complete", "R.advance", "R.runIncremental", "C.reconcile", "A:F.done", "B:F.done", "C.reconcile")
		modelAllDone(t, w)
	})
}
