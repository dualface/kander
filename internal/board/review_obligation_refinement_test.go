package board

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	obligations "github.com/dualface/kander/experiments/review-obligations"
)

const obligationExperiment = "../../experiments/review-obligations/"

type obligationTrace struct {
	state obligations.Member
	steps []string
}

func obligationTraces(t *testing.T, bounds obligations.Bounds) []obligationTrace {
	t.Helper()
	raw, err := os.ReadFile(obligationExperiment + "transitions.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct{ Actions map[string]json.RawMessage }
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	events := slices.Sorted(maps.Keys(spec.Actions))
	initial := obligations.Initial(1)[0]
	queue := []obligationTrace{{state: initial}}
	seen := map[obligations.Member]bool{initial: true}
	for at := 0; at < len(queue); at++ {
		trace := queue[at]
		for _, event := range events {
			next, ok := obligations.Step(obligations.State{trace.state}, 0, event, bounds)
			if !ok || seen[next[0]] {
				continue
			}
			seen[next[0]] = true
			queue = append(queue, obligationTrace{state: next[0], steps: append(slices.Clone(trace.steps), event)})
		}
	}
	return queue
}

// This is the obligation portion of the generated done guard, including the
// separately represented no-plan debt. It does not predict the full board gate.
func abstractObligations(s obligations.Member) obligationObservation {
	drop := s.DropOwed == 1 && s.Closed <= s.DropFloor
	recovery := s.RecoverOwed == 1 && !(s.Closed > s.RecoverFloor && s.ClosedCycle == s.Cycle)
	hasRole := s.Required == 1 || s.Remembered == 1
	unplanned := s.Unplanned == 1 && !(hasRole && s.Closed > 0 && s.ClosedCycle == s.Cycle)
	return obligationObservation{drop, recovery || unplanned, recovery || unplanned && hasRole}
}

func TestObligationRefinement(t *testing.T) {
	bounds, count := obligations.Bounds{MaxCycle: 2, MaxBatch: 2}, 130
	switch os.Getenv("KANDER_OBLIGATION_REFINEMENT_FULL") {
	case "":
	case "1":
		bounds, count = obligations.Bounds{MaxCycle: 3, MaxBatch: 3}, 1964
	default:
		t.Fatal("KANDER_OBLIGATION_REFINEMENT_FULL must be empty or 1")
	}
	traces := obligationTraces(t, bounds)
	if len(traces) != count {
		t.Fatalf("state inventory changed: %d; re-audit coverage", len(traces))
	}
	checks, mismatches, unsafeCompletions := 0, 0, 0
	gapStates := map[obligations.Member]bool{}
	first := ""
	for i, trace := range traces {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newObligationFixture(t)
			state := obligations.Initial(1)
			releasedNoPlan := false
			for n, event := range trace.steps {
				next, ok := obligations.Step(state, 0, event, bounds)
				if !ok {
					t.Fatalf("invalid BFS prefix: %v", trace.steps[:n+1])
				}
				if err := f.step(event); err != nil {
					t.Fatalf("%v: %v", trace.steps[:n+1], err)
				}
				if event == "reset" && state[0].Plan == 0 {
					releasedNoPlan = false
				} else if event == "releaseRecovery" {
					releasedNoPlan = true
				}
				state = next
				got, err := f.observe()
				if err != nil {
					t.Fatal(err)
				}
				checks++
				want := abstractObligations(state[0])
				// Known gap: release clears the model's only no-plan marker.
				// Production retains the record; a later required role revives
				// its debt until a current-cycle required closure pays it. The
				// history bit is deliberately outside the generated state.
				s := state[0]
				gap := releasedNoPlan && s.Unplanned == 0 && s.RecoverOwed == 0 &&
					(s.Required == 1 || s.Remembered == 1) && !(s.Closed > 0 && s.ClosedCycle == s.Cycle)
				if gap {
					mismatches++
					gapStates[s] = true
					if first == "" {
						first = fmt.Sprintf("%s: got %+v model %+v", strings.Join(trace.steps[:n+1], " -> "), got, want)
					}
					want.Recovery, want.RecoveryRole = true, true
					if s.Done == 1 {
						unsafeCompletions++
					}
				}
				if got != want {
					t.Fatalf("unknown correspondence change: %v got=%+v expected=%+v known_gap=%v", trace.steps[:n+1], got, want, gap)
				}
			}
		})
	}
	t.Logf("states=%d prefix_checks=%d known_gap_checks=%d known_gap_states=%d model_only_completions=%d first=%s", len(traces), checks, mismatches, len(gapStates), unsafeCompletions, first)
	if mismatches == 0 || unsafeCompletions == 0 {
		t.Fatal("expected abstraction gap disappeared; re-audit before changing the result")
	}
}
