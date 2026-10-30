package board

// The composite state model test. It runs the real board commands over private
// board copies and walks the abstract states they reach. A state with no way to
// done inside the bounds is a dead end, and it must be on the allow list.
// docs/model-test.md explains how to read a failure and how to add an operation.

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

const modelDepthEnv = "KANDER_MODEL_DEPTH"

// Search bounds. The measurements that chose them are in the report of card
// 20261030-composite-model-test-task.
var (
	modelDefaultBounds = modelScenarioBounds{single: modelBounds{explore: 3, exit: 2}, group: modelBounds{explore: 2, exit: 2}}
	modelFullBounds    = modelScenarioBounds{single: modelBounds{explore: 4, exit: 3}, group: modelBounds{explore: 3, exit: 3}}
)

type modelScenarioBounds struct{ single, group modelBounds }

// modelSeed names a state a path of supported commands reaches. Dead ends sit
// deep in the protocol, so the search starts from these states as well: depth
// counts operations from the nearest seed. The seed path itself is not searched.
//
// The two members are interchangeable, so a focused seed tries the operations of
// one member only, plus F.done of both.
type modelSeed struct {
	name  string
	focus string
	steps []string
}

func modelOps() []modelOp {
	return slices.Concat(modelLifecycleOps(), modelDispatchOps(), modelReviewOps())
}

func modelSingleSeeds() []modelSeed {
	return []modelSeed{
		{name: "todo"},
		{name: "claimed", steps: []string{"H.claim"}},
		{name: "finding", steps: []string{"H.claim", "R.planRequired", "R.run", "R.assign"}},
		{name: "chained", steps: []string{"H.claim", "R.planChained"}},
		{name: "unresolved", steps: []string{"L.begin", "X.write", "L.fail"}},
		{name: "close-failed", steps: []string{"L.begin", "L.closeFailed"}},
		{name: "two-roots", steps: []string{"H.claim", "R.planRequired", "R.run", "R.assign", "R.runClean"}},
	}
}

func modelGroupSeeds() []modelSeed {
	claimed := []string{"A:H.claim", "B:H.claim", "C.reconcile"}
	review := slices.Concat(claimed, []string{"A:X.review", "B:X.review", "C.reconcile"})
	launching := []string{"A:L.begin", "B:H.claim", "C.reconcile"}
	round := slices.Concat(review, []string{"A:D.prepareSync", "A:D.accept", "A:D.complete", "C.reconcile"})
	return []modelSeed{
		{name: "todo"},
		{name: "claimed", focus: "A", steps: claimed},
		{name: "review", focus: "A", steps: review},
		{name: "launching", focus: "A", steps: launching},
		{name: "rolled-back", focus: "A", steps: slices.Concat(launching, []string{"A:L.fail"})},
		{name: "unresolved", focus: "A", steps: slices.Concat(launching, []string{"A:X.write", "A:L.fail", "C.reconcile"})},
		{name: "finding", focus: "B", steps: slices.Concat(claimed, []string{"R.planRequired", "R.run", "B:R.assign", "C.reconcile"})},
		{name: "round", focus: "A", steps: round},
		{name: "round-released", focus: "A", steps: slices.Concat(round, []string{"A:D.prepareSync", "A:D.cancel", "A:H.return"})},
		{name: "closed", focus: "A", steps: slices.Concat(claimed, []string{"R.planNA", "R.close", "A:X.review", "B:X.review", "C.reconcile"})},
		{name: "chained", focus: "B", steps: slices.Concat(claimed, []string{"R.planChained", "B:X.review", "C.reconcile"})},
		{name: "rebound", focus: "A", steps: slices.Concat(claimed, []string{"R.planNA", "R.close", "A:X.review", "B:X.review", "C.reconcile", "B:F.done", "A:H.reclaimDecision", "R.rebind", "C.reconcile"})},
		{name: "excluded", focus: "A", steps: slices.Concat(claimed, []string{"R.planRequired", "R.run", "B:R.assign", "B:T.trash", "C.reconcile"})},
	}
}

func modelModeBounds() modelScenarioBounds {
	if os.Getenv(modelDepthEnv) == "full" {
		return modelFullBounds
	}
	return modelDefaultBounds
}

func modelSearches(t *testing.T, bounds modelScenarioBounds, ops []modelOp) []modelRun {
	t.Helper()
	modelFastTemp(t)
	return []modelRun{
		modelExplore(t, "single", modelInitial(t, false, 1), modelSingleSeeds(), bounds.single, ops),
		modelExplore(t, "group", modelInitial(t, true, 2), modelGroupSeeds(), bounds.group, ops),
	}
}

// modelVerdict compares the search with the allow list. It returns one failure
// per unlisted state, per listed dead end the search no longer reaches, per
// dead end the search reaches outside its class, and per invariant violation.
func modelVerdict(runs []modelRun, list []modelDeadEnd) []string {
	var failures []string
	hits := map[string]int{}
	for _, r := range runs {
		failures = append(failures, r.violations...)
		for _, n := range r.dead {
			id := modelClassify(list, n.diagnosis)
			if id == "" {
				failures = append(failures, fmt.Sprintf("unlisted dead end in %s\n  path: %s\n  state: %s\n  why: %s", r.name, strings.Join(n.trace, " -> "), n.key, n.diagnosis))
				continue
			}
			hits[id]++
		}
	}
	for _, entry := range list {
		if entry.class == modelReached && hits[entry.id] == 0 {
			failures = append(failures, entry.id+": the model no longer reaches this dead end; remove the row if the fix is deliberate (owner "+entry.owner+")")
		}
	}
	return failures
}

func TestModelCompositeStates(t *testing.T) {
	runs := modelSearches(t, modelModeBounds(), modelOps())
	found := map[string][]*modelNode{}
	for _, r := range runs {
		t.Logf("%s: operations=%d members=%d explore=%d exit=%d states=%d expanded=%d dead=%d seconds=%.1f", r.name, r.operations, r.members, r.bounds.explore, r.bounds.exit, r.states, r.expanded, len(r.dead), r.elapsed.Seconds())
		for _, n := range r.dead {
			id := modelClassify(modelDeadEnds, n.diagnosis)
			found[id] = append(found[id], n)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(found)) {
		first := found[id][0]
		t.Logf("%s: %d states; first: %s; %s", id, len(found[id]), strings.Join(first.trace, " -> "), first.diagnosis)
	}
	t.Logf("result digest: %s", modelDigest(runs, func(n *modelNode) string { return modelClassify(modelDeadEnds, n.diagnosis) }))
	for _, failure := range modelVerdict(runs, modelDeadEnds) {
		t.Error(failure)
	}
	for _, failure := range modelRowsAreNeeded(runs, modelDeadEnds) {
		t.Error(failure)
	}
}

// modelRowsAreNeeded proves that each model row carries weight. Without a row
// the verdict must report an unlisted state; otherwise another row hides the
// dead end, and repairing one of the two would go unnoticed.
func modelRowsAreNeeded(runs []modelRun, list []modelDeadEnd) []string {
	var failures []string
	for at, entry := range list {
		if entry.class != modelReached {
			continue
		}
		without := slices.Delete(slices.Clone(list), at, at+1)
		if !slices.ContainsFunc(modelVerdict(runs, without), func(f string) bool { return strings.HasPrefix(f, "unlisted dead end") }) {
			failures = append(failures, entry.id+": another row covers every state of this row; narrow one of them")
		}
	}
	return failures
}
