package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"testing"

	obligations "github.com/dualface/kander/experiments/review-obligations"
)

type obligationTLCVector struct {
	Case     string             `json:"case"`
	Before   obligations.State  `json:"before"`
	After    obligations.State  `json:"after"`
	Member   int                `json:"member"`
	Event    string             `json:"event"`
	Bounds   obligations.Bounds `json:"bounds"`
	Accepted bool               `json:"accepted"`
}

// TLC's mutation traces are intentionally unsafe in the generated kernel. Real
// obligation functions must still report debt at their final modeled done.
func TestObligationRefinementTLC(t *testing.T) {
	f, err := os.Open(obligationExperiment + "testdata/tlc-vectors.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	traces := map[string][]obligationTLCVector{}
	order := []string{}
	for {
		var v obligationTLCVector
		if err := d.Decode(&v); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		// The original two-member model contains independent plans. Replaying
		// each member separately makes no claim about shared-plan isolation.
		key := fmt.Sprintf("%s/member-%d", v.Case, v.Member)
		if _, exists := traces[key]; !exists {
			order = append(order, key)
		}
		traces[key] = append(traces[key], v)
	}
	if len(order) != 8 {
		t.Fatalf("TLC inventory changed: %v", order)
	}
	transitions, mutants, witnesses := 0, 0, 0
	for _, name := range order {
		t.Run(name, func(t *testing.T) {
			fixture := newObligationFixture(t)
			state := obligations.Initial(1)
			trace := traces[name]
			for i, v := range trace {
				if !v.Accepted || v.Before[v.Member] != state[0] {
					t.Fatal("broken TLC trace prefix")
				}
				next, ok := obligations.Step(state, 0, v.Event, v.Bounds)
				if !ok || !slices.Equal(next, obligations.State{v.After[v.Member]}) {
					t.Fatalf("TLC/generated kernel differ at %d: %s", i, v.Event)
				}
				if err := fixture.step(v.Event); err != nil {
					t.Fatal(err)
				}
				got, err := fixture.observe()
				if err != nil {
					t.Fatal(err)
				}
				transitions++
				if v.Bounds.Mutation == 0 && got != abstractObligations(next[0]) {
					t.Fatalf("witness differs: event=%s actual=%+v model=%+v", v.Event, got, abstractObligations(next[0]))
				}
				if i == len(trace)-1 {
					if v.Event != "done" || next[0].Done != 1 || got.Drop {
						t.Fatalf("unexpected final state: %s %+v", v.Event, got)
					}
					if v.Bounds.Mutation != 0 {
						if !got.Recovery || !got.RecoveryRole {
							t.Fatalf("production failed to retain recovery debt for mutation %d", v.Bounds.Mutation)
						}
						mutants++
					} else {
						if got.Recovery {
							t.Fatal("valid witness still owes recovery")
						}
						witnesses++
					}
				}
				state = next
			}
		})
	}
	if transitions != 52 || mutants != 4 || witnesses != 4 {
		t.Fatalf("incomplete TLC coverage: transitions=%d mutants=%d member_witnesses=%d", transitions, mutants, witnesses)
	}
	t.Logf("TLC transitions=%d detected_mutants=%d debt_free_member_witnesses=%d", transitions, mutants, witnesses)
}
