package obligations

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"testing"
)

type transitionVector struct {
	Case     string `json:"case"`
	Before   State  `json:"before"`
	After    State  `json:"after"`
	Member   int    `json:"member"`
	Event    string `json:"event"`
	Bounds   Bounds `json:"bounds"`
	Accepted bool   `json:"accepted"`
}

func readVectors(t *testing.T, path string, visit func(transitionVector)) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	count := 0
	for {
		var v transitionVector
		if err := d.Decode(&v); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		visit(v)
		count++
	}
	if count == 0 {
		t.Fatal("empty comparison input")
	}
	return count
}

func TestTransitionVectors(t *testing.T) {
	paths := []string{"testdata/tlc-vectors.jsonl"}
	if path := os.Getenv("KANDER_OBLIGATION_VECTORS"); path != "" {
		paths = append(paths, path)
	}
	for _, path := range paths {
		count := readVectors(t, path, func(v transitionVector) {
			original := slices.Clone(v.Before)
			after, ok := Step(v.Before, v.Member, v.Event, v.Bounds)
			if ok != v.Accepted || !slices.Equal(after, v.After) {
				t.Fatalf("%s member %d event %s: accepted=%v want %v; got %+v want %+v", v.Case, v.Member, v.Event, ok, v.Accepted, after, v.After)
			}
			if !slices.Equal(v.Before, original) {
				t.Fatal("Step changed its input")
			}
			for m := range after {
				if m != v.Member && after[m] != original[m] {
					t.Fatalf("event on member %d changed member %d", v.Member, m)
				}
			}
		})
		t.Logf("compared %d transitions from %s", count, path)
	}
}

func TestBaselineRejectsMutantCounterexamples(t *testing.T) {
	cases := map[string][]transitionVector{}
	readVectors(t, "testdata/tlc-vectors.jsonl", func(v transitionVector) {
		if v.Bounds.Mutation != 0 {
			cases[v.Case] = append(cases[v.Case], v)
		}
	})
	if len(cases) != 4 {
		t.Fatalf("want four mutation counterexamples, got %d", len(cases))
	}
	for name, trace := range cases {
		t.Run(name, func(t *testing.T) {
			state := Initial(len(trace[0].Before))
			for i, v := range trace {
				bounds := v.Bounds
				bounds.Mutation = 0
				after, ok := Step(state, v.Member, v.Event, bounds)
				if i == len(trace)-1 {
					if v.Event != "done" || ok || !slices.Equal(after, state) {
						t.Fatalf("baseline must refuse final done: %+v", v)
					}
				} else if !ok {
					t.Fatalf("baseline refused prefix event %d: %s", i, v.Event)
				}
				state = after
			}
		})
	}
}

func TestNoPlanRecoveryRequiresItsOwnDecision(t *testing.T) {
	state := Initial(1)
	bounds := Bounds{MaxCycle: 3, MaxBatch: 3}
	for _, event := range []string{"reset", "planNA", "close"} {
		var ok bool
		state, ok = Step(state, 0, event, bounds)
		if !ok {
			t.Fatal(event)
		}
	}
	for _, event := range []string{"done", "releaseDrop"} {
		if _, ok := Step(state, 0, event, bounds); ok {
			t.Fatalf("unexpectedly allowed %s", event)
		}
	}
	state, ok := Step(state, 0, "releaseRecovery", bounds)
	if !ok {
		t.Fatal("explicit no-role recovery decision was refused")
	}
	if after, ok := Step(state, 0, "done", bounds); !ok || after[0].Done != 1 {
		t.Fatal("released no-plan recovery did not complete")
	}
}

func TestInvalidInvocationIsRefused(t *testing.T) {
	state := Initial(1)
	for _, c := range []struct {
		member int
		event  string
		bounds Bounds
	}{
		{-1, "planNA", Bounds{3, 3, 0}},
		{1, "planNA", Bounds{3, 3, 0}},
		{0, "unknown", Bounds{3, 3, 0}},
		{0, "planNA", Bounds{4, 3, 0}},
		{0, "planNA", Bounds{3, 3, 5}},
	} {
		if after, ok := Step(state, c.member, c.event, c.bounds); ok || !slices.Equal(after, state) {
			t.Fatalf("invalid invocation changed state: %+v", c)
		}
	}
}
