package board

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/dualface/kander/internal/statemachine/reviewobligation"
	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

// Refinement tests: for every production entrance that goes through a review
// kernel, the projection after the write equals the kernel's step from the
// projection before it, and a refused entrance leaves the projection unchanged.
// Arrays are compared up to their longest length with absent entries reading as
// zero, because a projection sizes them to one above the plan length.

func sameObligation(a, b reviewobligation.State) bool {
	padded := func(s reviewobligation.State, n int) reviewobligation.State {
		s.Has = append(slices.Clone(s.Has), make([]bool, n-len(s.Has))...)
		s.Req = append(slices.Clone(s.Req), make([]reviewobligation.RoleSet, n-len(s.Req))...)
		s.Closed = append(slices.Clone(s.Closed), make([]bool, n-len(s.Closed))...)
		s.Cur = append(slices.Clone(s.Cur), make([]reviewobligation.RoleSet, n-len(s.Cur))...)
		return s
	}
	n := max(len(a.Has), len(b.Has))
	a, b = padded(a, n), padded(b, n)
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func samePlan(a, b reviewplan.State) bool {
	n := max(len(a.Closed), len(b.Closed))
	pad := func(s reviewplan.State) reviewplan.State {
		s.Closed = append(slices.Clone(s.Closed), make([]bool, n-len(s.Closed))...)
		s.Touched = append(slices.Clone(s.Touched), make([]bool, n-len(s.Touched))...)
		return s
	}
	return fmt.Sprint(pad(a)) == fmt.Sprint(pad(b))
}

// kernelSnapshot is the projection of every named member and of the plan.
type kernelSnapshot struct {
	bound   uint32
	members map[string]reviewobligation.State
	plan    *reviewplan.State
}

// snapshotKernels projects ids under the plan of ids[0], if any. bound 0 takes
// one above the plan's length.
func snapshotKernels(t *testing.T, root string, bound uint32, ids ...string) kernelSnapshot {
	t.Helper()
	snap := kernelSnapshot{bound: bound, members: map[string]reviewobligation.State{}}
	err := WithTransaction(root, reviewLedgerScope(ids, true), func(tx *Transaction) error {
		p, planned, err := readTaskPlan(tx, ids[0])
		if err != nil {
			return err
		}
		if snap.bound == 0 {
			snap.bound = kernelBound(p)
		}
		for _, id := range ids {
			st, err := projectObligation(tx, p, planned, id, snap.bound)
			if err != nil {
				return err
			}
			snap.members[id] = st
		}
		if planned {
			pk, err := projectPlan(tx, p, snap.bound)
			if err != nil {
				return err
			}
			snap.plan = &pk.state
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// kernelSteps is what one entrance means for the kernels: the events of each
// member and of the plan, in order. A missing member takes no event.
type kernelSteps struct {
	members map[string][]reviewobligation.Event
	plan    []reviewplan.Event
}

// refines runs op and checks the kernel correspondence of its write. With
// refused, op must fail and leave every projection unchanged.
func refines(t *testing.T, root string, ids []string, steps kernelSteps, refused bool, op func() error) {
	t.Helper()
	// A bound above every plan of these tests serves both sides, also a creation.
	before := snapshotKernels(t, root, 16, ids...)
	err := op()
	after := snapshotKernels(t, root, before.bound, ids...)
	if refused {
		if err == nil {
			t.Fatal("entrance accepted a refused event")
		}
		for _, id := range ids {
			if !sameObligation(before.members[id], after.members[id]) {
				t.Fatalf("refused entrance changed %s:\n%+v\n%+v", id, before.members[id], after.members[id])
			}
		}
		if before.plan != nil && !samePlan(*before.plan, *after.plan) {
			t.Fatalf("refused entrance changed the plan:\n%+v\n%+v", *before.plan, *after.plan)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		want := before.members[id]
		params := reviewobligation.Params{MaxBatch: before.bound}
		for _, e := range steps.members[id] {
			next, ok := reviewobligation.Step(want, e, params)
			if !ok {
				t.Fatalf("kernel refused %T for %s: %+v", e, id, want)
			}
			want = next
		}
		if !sameObligation(want, after.members[id]) {
			t.Fatalf("projection of %s differs from the kernel step:\nkernel     %+v\nprojection %+v", id, want, after.members[id])
		}
	}
	if len(steps.plan) == 0 && before.plan == nil {
		return
	}
	want := reviewplan.Initial(reviewplan.Params{MaxBatch: before.bound})
	if before.plan != nil {
		want = *before.plan
	}
	for _, e := range steps.plan {
		next, ok := reviewplan.Step(want, e, reviewplan.Params{MaxBatch: before.bound})
		if !ok {
			t.Fatalf("plan kernel refused %T: %+v", e, want)
		}
		want = next
	}
	if !samePlan(want, *after.plan) {
		t.Fatalf("plan projection differs from the kernel step:\nkernel     %+v\nprojection %+v", want, *after.plan)
	}
}

// obligationKernelFixture drives the hand-written shell functions of one member
// with a one-member plan. Plan admission, batch records, cycle changes and valid
// closures are fixture writes; drop, rebind, release and reset staging are the
// production functions.
type obligationKernelFixture struct {
	root, id string
	serial   int
}

func newObligationKernelFixture(t *testing.T) *obligationKernelFixture {
	t.Helper()
	root := tempBoard(t)
	return &obligationKernelFixture{root: root, id: gateCard(t, root, "kernel-refinement")}
}

const kernelFixturePlan = "kernel-plan"

func (f *obligationKernelFixture) plan(tx *Transaction) (ReviewPlan, bool, error) {
	return readTaskPlan(tx, f.id)
}

func (f *obligationKernelFixture) savePlan(tx *Transaction, p ReviewPlan) error {
	return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
}

func (f *obligationKernelFixture) newCycle(tx *Transaction) error {
	f.serial++
	s, err := tx.Snapshot(f.id)
	if err != nil {
		return err
	}
	body, err := setMetadata(s.Text, FieldStartedAt, fmt.Sprintf("2026-01-01 00:%02d", f.serial))
	if err != nil {
		return err
	}
	return tx.Put(f.id, "spec.md", body)
}

func (f *obligationKernelFixture) step(e reviewobligation.Event) error {
	scope := reviewLedgerScope([]string{f.id}, false)
	_, done := e.(reviewobligation.Done)
	scope.ExclusiveBoard = done
	return WithTransaction(f.root, scope, func(tx *Transaction) error {
		p, _, err := f.plan(tx)
		if err != nil {
			return err
		}
		switch e := e.(type) {
		case reviewobligation.Create:
			s, err := tx.Snapshot(f.id)
			if err != nil {
				return err
			}
			p = ReviewPlan{Schema: 1, PlanID: kernelFixturePlan, TaskIDs: []string{f.id}, Cycles: map[string]string{f.id: planCycle(s)}}
			if err = tx.PutGroup(reviewControlGroup, taskPlanName(f.id), reviewJSON(map[string]string{"plan_id": p.PlanID})); err != nil {
				return err
			}
			return f.savePlan(tx, p)
		case reviewobligation.AddBatch:
			f.serial++
			requirements := noReviewRequirements()
			if e.Roles.Has(reviewobligation.RolePmqa) {
				requirements["PMQA"] = "required"
			}
			b := ReviewPlanBatch{BatchID: fmt.Sprintf("batch-%d", f.serial), TaskIDs: []string{f.id}, Requirements: requirements}
			if err := tx.PutGroup(reviewControlGroup, reviewBatchName(b.BatchID), reviewJSON(ReviewBatch{Schema: 1, PlanID: p.PlanID, BatchID: b.BatchID, TaskIDs: b.TaskIDs, Requirements: requirements})); err != nil {
				return err
			}
			p.Batches = append(p.Batches, b)
			return f.savePlan(tx, p)
		case reviewobligation.Close:
			b := p.Batches[e.At]
			s, err := tx.Snapshot(f.id)
			if err != nil {
				return err
			}
			// The shell reads only closure existence and the conclusion cycles.
			runID := "run-" + b.BatchID
			c := ReviewClosure{
				Request: ReviewCloseRequest{Roles: map[string]ReviewRoleConclusion{"PMQA": {RunID: runID}}},
				View:    ReviewBatchView{Runs: []ReviewRunView{{Run: ReviewRun{ReviewInput: ReviewInput{RunID: runID}, MemberCycles: map[string]string{f.id: planCycle(s)}}}}},
			}
			return tx.PutGroup(reviewControlGroup, closureName(b.BatchID), reviewJSON(c))
		case reviewobligation.NewCycle:
			return f.newCycle(tx)
		case reviewobligation.Reset:
			if err := f.newCycle(tx); err != nil {
				return err
			}
			return stageRecoveryReview(tx, f.id, fmt.Sprintf("reset-%d", f.serial))
		case reviewobligation.Rebind:
			s, err := tx.Snapshot(f.id)
			if err != nil {
				return err
			}
			pk, err := projectPlan(tx, p, kernelBound(p))
			if err != nil {
				return err
			}
			p.Cycles, p.CycleBatchFrom = maps.Clone(p.Cycles), maps.Clone(p.CycleBatchFrom)
			changed := map[string]string{f.id: planCycle(s)}
			if err = rebindPlanCycles(tx, pk, &p, changed, changed); err != nil {
				return err
			}
			return f.savePlan(tx, p)
		case reviewobligation.Drop:
			pk, err := projectPlan(tx, p, kernelBound(p))
			if err != nil {
				return err
			}
			var ids []string
			for _, b := range p.Batches[e.Keep:] {
				ids = append(ids, b.BatchID)
			}
			decision := ""
			if e.Decision {
				decision = "fixture-user-decision"
			}
			p.CycleBatchFrom = maps.Clone(p.CycleBatchFrom)
			if err = dropPlanBatches(tx, pk, &p, ids, decision); err != nil {
				return err
			}
			return f.savePlan(tx, p)
		case reviewobligation.ReleaseDrop, reviewobligation.ReleaseRecovery:
			kind := releaseDropped
			if _, ok := e.(reviewobligation.ReleaseRecovery); ok {
				kind = releaseRecovery
			}
			facts, err := releaseObligation(tx, p, ReviewObligationRelease{Kind: kind, TaskIDs: []string{f.id}}, "fixture-user-decision")
			if err != nil {
				return err
			}
			if facts.err != nil {
				return facts.err
			}
			if err = persistRelease(tx, &p, kind, "fixture-user-decision", facts.kernels); err != nil {
				return err
			}
			return f.savePlan(tx, p)
		case reviewobligation.Done:
			return tx.Relocate(f.id, "done")
		}
		return fmt.Errorf("unmapped event %T", e)
	})
}

func (f *obligationKernelFixture) project(t *testing.T) reviewobligation.State {
	t.Helper()
	return snapshotKernels(t, f.root, 8, f.id).members[f.id]
}

// kernelFixtureEvents are the fixture's events from state s: one PMQA role, a
// batch that requires it or none, every close of an open batch with its
// conclusion of the current cycle, and every drop.
func kernelFixtureEvents(s reviewobligation.State) []reviewobligation.Event {
	pmqa := reviewobligation.RoleSet(0).With(reviewobligation.RolePmqa)
	events := []reviewobligation.Event{
		reviewobligation.Create{},
		reviewobligation.AddBatch{Member: true, Roles: 0},
		reviewobligation.AddBatch{Member: true, Roles: pmqa},
		reviewobligation.NewCycle{},
		reviewobligation.Reset{},
		reviewobligation.Rebind{},
		reviewobligation.ReleaseDrop{},
		reviewobligation.ReleaseRecovery{},
		reviewobligation.Done{PlanClear: true},
	}
	for i := uint64(0); i < s.Count; i++ {
		events = append(events, reviewobligation.Close{At: i, Current: s.Req[i], Covered: s.Req[i]})
	}
	for keep := uint64(1); keep < s.Count; keep++ {
		events = append(events, reviewobligation.Drop{Keep: keep, Decision: false}, reviewobligation.Drop{Keep: keep, Decision: true})
	}
	return events
}

type kernelTrace struct {
	state reviewobligation.State
	steps []reviewobligation.Event
}

// kernelTraces lists one shortest event sequence to every kernel state within
// depth events, breadth first.
func kernelTraces(params reviewobligation.Params, depth int) []kernelTrace {
	initial := reviewobligation.Initial(params)
	queue := []kernelTrace{{state: initial}}
	seen := map[string]bool{fmt.Sprint(initial): true}
	for at := 0; at < len(queue); at++ {
		trace := queue[at]
		if len(trace.steps) == depth {
			continue
		}
		for _, e := range kernelFixtureEvents(trace.state) {
			next, ok := reviewobligation.Step(trace.state, e, params)
			if !ok || seen[fmt.Sprint(next)] {
				continue
			}
			seen[fmt.Sprint(next)] = true
			queue = append(queue, kernelTrace{state: next, steps: append(slices.Clone(trace.steps), e)})
		}
	}
	return queue
}

// The shell functions realize every kernel state the fixture reaches: after each
// step of a shortest path, the projection of the board equals the kernel state.
func TestObligationKernelRefinement(t *testing.T) {
	depth, count := 6, 589
	switch os.Getenv("KANDER_OBLIGATION_REFINEMENT_FULL") {
	case "":
	case "1":
		depth, count = 8, 0
	default:
		t.Fatal("KANDER_OBLIGATION_REFINEMENT_FULL must be empty or 1")
	}
	params := reviewobligation.Params{MaxBatch: 3}
	traces := kernelTraces(params, depth)
	if count != 0 && len(traces) != count {
		t.Fatalf("state inventory changed: %d; re-audit coverage", len(traces))
	}
	checks, completions := 0, 0
	for i, trace := range traces {
		if len(trace.steps) == 0 {
			continue
		}
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newObligationKernelFixture(t)
			state := reviewobligation.Initial(params)
			for n, e := range trace.steps {
				next, ok := reviewobligation.Step(state, e, params)
				if !ok {
					t.Fatalf("invalid prefix: %v", trace.steps[:n+1])
				}
				if err := f.step(e); err != nil {
					t.Fatalf("%v: %v", trace.steps[:n+1], err)
				}
				state = next
				if got := f.project(t); !sameObligation(state, got) {
					t.Fatalf("%v:\nkernel     %+v\nprojection %+v", trace.steps[:n+1], state, got)
				}
				checks++
			}
			if state.Done {
				completions++
			}
		})
	}
	t.Logf("states=%d prefix_checks=%d completions=%d", len(traces), checks, completions)
	if completions == 0 {
		t.Fatal("refinement never reached completion")
	}
}
