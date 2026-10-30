package board

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// modelBounds are the search limits of one scenario. explore counts operations
// from a seed; exit counts operations from a state to a finished board, with one
// F.done as a single operation.
type modelBounds struct {
	explore int
	exit    int
}

// modelBatchSize bounds the states expanded between two merges, and with them
// the copies on disk at one time.
const modelBatchSize = 64

type modelEdge struct {
	op    string
	child int
	exit  bool // false for an operation that does not count as a way out
}

// modelNode is one abstract state. Its world is released after expansion; the
// diagnostics stay for classification.
type modelNode struct {
	key       string
	world     *modelWorld
	facts     *modelFacts
	depth     int
	seed      string
	focus     string // the member whose operations this state's seed explores
	trace     []string
	edges     []modelEdge
	finished  bool
	narrow    []string // limits the operations of a state that needs no full expansion
	tailed    bool     // the F.done operations of this state ran
	expanded  bool     // every operation of this state ran
	diagnosis modelDiagnosis
}

// modelSearch is the bounded graph of one scenario. The search keeps one world
// per abstract state, so an exit costs edges of the graph instead of a walk per
// state. Exits are searched on demand: most states finish through F.done, and
// only the states that do not pay for a wider search.
type modelSearch struct {
	bounds     modelBounds
	ops        []modelOp
	nodes      []*modelNode
	index      map[string]int
	scratch    string
	nextWorld  atomic.Int64
	violations []string
}

type modelChild struct {
	op       string
	exit     bool
	finished bool
	narrow   []string
	key      string
	world    *modelWorld
	facts    *modelFacts
}

type modelExpansion struct {
	children   []modelChild
	violations []string
	err        error
}

func (s *modelSearch) worldRoot() string {
	return filepath.Join(s.scratch, fmt.Sprintf("w%d", s.nextWorld.Add(1)))
}

// goalReached reports whether every card is terminal and, when a card rests in
// done, the checkpoint accepts the final board. A group with no card in done
// owes no exit: archive cleanup may already have removed what a reconcile reads.
func (s *modelSearch) goalReached(w *modelWorld, f *modelFacts) (bool, error) {
	delivered := false
	for _, card := range f.cards {
		if !modelTerminal(card.Entry.State) {
			return false, nil
		}
		delivered = delivered || card.Entry.State == "done"
	}
	if !delivered {
		return true, nil
	}
	c, err := w.clone(s.worldRoot())
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(c.root)
	err = c.reconcile()
	var infra modelInfraError
	if errors.As(err, &infra) {
		return false, err
	}
	return err == nil, nil
}

func (n *modelNode) wants(op modelOp, tailOnly bool) bool {
	if n.narrow != nil && !slices.Contains(n.narrow, op.name) {
		return false
	}
	if op.name == modelTailOp {
		return !n.tailed
	}
	return !tailOnly
}

func (s *modelSearch) expand(n *modelNode, tailOnly bool) (x modelExpansion) {
	for _, op := range s.ops {
		if !n.wants(op, tailOnly) {
			continue
		}
		targets := []int{-1}
		if !op.shared {
			targets = targets[:0]
			for i, m := range n.world.members {
				if n.focus == "" || n.focus == m.label || op.name == modelTailOp {
					targets = append(targets, i)
				}
			}
		}
		for _, i := range targets {
			if !op.guard(n.facts, i) {
				continue
			}
			c, err := n.world.clone(s.worldRoot())
			if err != nil {
				x.err = err
				return x
			}
			var m *modelMember
			name := op.name
			if i >= 0 {
				m = c.members[i]
				if len(c.members) > 1 {
					name = m.label + ":" + name
				}
			}
			if err = op.apply(c, m, n.facts); err != nil {
				os.RemoveAll(c.root)
				var infra modelInfraError
				if errors.As(err, &infra) {
					x.err = fmt.Errorf("%s: %w", name, err)
					return x
				}
				continue
			}
			f, err := c.read()
			if err != nil {
				x.err = fmt.Errorf("%s: %w", name, err)
				return x
			}
			finished, err := s.goalReached(c, f)
			if err != nil {
				x.err = fmt.Errorf("%s: %w", name, err)
				return x
			}
			x.violations = append(x.violations, modelCheckInvariants(n.facts, f, name)...)
			child := modelChild{op: name, exit: !op.abandons, narrow: op.narrows, finished: finished, key: f.key(), world: c, facts: f}
			// The index only grows between batches, so this read is safe and the merge
			// sees the same result. Dropping the copy here keeps a batch's disk use
			// to its new states: most children repeat a known state.
			if _, known := s.index[child.key]; known {
				os.RemoveAll(c.root)
				child.world, child.facts = nil, nil
			}
			x.children = append(x.children, child)
		}
	}
	return x
}

func (s *modelSearch) add(world *modelWorld, facts *modelFacts, key string, depth int, seed modelSeed, trace []string) int {
	s.nodes = append(s.nodes, &modelNode{key: key, world: world, facts: facts, depth: depth, seed: seed.name, focus: seed.focus, trace: trace})
	s.index[key] = len(s.nodes) - 1
	return len(s.nodes) - 1
}

// expandAll runs one batch in parallel and merges the results in the fixed node
// and operation order, so the first path to a state and the identifiers on that
// path do not depend on scheduling. It returns the states the batch added.
func (s *modelSearch) expandAll(ids []int, tailOnly bool) ([]int, error) {
	var added []int
	for batch := range slices.Chunk(ids, modelBatchSize) {
		more, err := s.expandBatch(batch, tailOnly)
		if err != nil {
			return nil, err
		}
		added = append(added, more...)
	}
	return added, nil
}

func (s *modelSearch) expandBatch(ids []int, tailOnly bool) ([]int, error) {
	results := make([]modelExpansion, len(ids))
	modelParallel(len(ids), func(at int) { results[at] = s.expand(s.nodes[ids[at]], tailOnly) })
	var added, released []int
	var discarded []string
	for at, id := range ids {
		n, x := s.nodes[id], results[at]
		if x.err != nil {
			return nil, x.err
		}
		n.tailed = true
		n.expanded = n.expanded || !tailOnly
		s.violations = append(s.violations, x.violations...)
		for _, child := range x.children {
			known, seen := s.index[child.key]
			if seen {
				if child.world != nil {
					discarded = append(discarded, child.world.root)
				}
			} else {
				known = s.add(child.world, child.facts, child.key, n.depth+1, modelSeed{name: n.seed, focus: n.focus}, append(slices.Clone(n.trace), child.op))
				s.nodes[known].narrow, s.nodes[known].finished = child.narrow, child.finished
				added = append(added, known)
			}
			n.edges = append(n.edges, modelEdge{child.op, known, child.exit})
		}
		if n.expanded {
			released = append(released, id)
		}
	}
	s.release(released)
	modelParallel(len(discarded), func(at int) { os.RemoveAll(discarded[at]) })
	return added, nil
}

func (s *modelSearch) release(ids []int) {
	modelParallel(len(ids), func(at int) {
		n := s.nodes[ids[at]]
		if n.world != nil {
			os.RemoveAll(n.world.root)
			n.world = nil
			n.facts = nil
		}
	})
}

// run explores every state inside the explore bound, then widens the search
// only around the states that have no exit yet.
func (s *modelSearch) run(seeds []modelSeed, start *modelWorld) error {
	level := []int{}
	for _, seed := range seeds {
		w, err := s.applySeed(start, seed)
		if err != nil {
			return fmt.Errorf("seed %s: %w", seed.name, err)
		}
		f, err := w.read()
		if err != nil {
			return err
		}
		key := f.key()
		if _, seen := s.index[key]; seen {
			os.RemoveAll(w.root)
			continue
		}
		id := s.add(w, f, key, 0, seed, w.trace)
		if s.nodes[id].finished, err = s.goalReached(w, f); err != nil {
			return err
		}
		level = append(level, id)
	}
	var err error
	for depth := 0; depth < s.bounds.explore; depth++ {
		if level, err = s.expandAll(level, false); err != nil {
			return err
		}
	}
	for {
		reach := s.demand()
		var tail, full, idle []int
		for id, n := range s.nodes {
			switch {
			case n.world == nil:
			case !reach[id]:
				idle = append(idle, id)
			case !n.tailed:
				tail = append(tail, id)
			case !n.expanded:
				full = append(full, id)
			}
		}
		s.release(idle)
		// One batch per pass: the next pass releases what this one resolved.
		switch {
		case len(tail) > 0:
			_, err = s.expandBatch(tail[:min(len(tail), modelBatchSize)], true)
		case len(full) > 0:
			_, err = s.expandBatch(full[:min(len(full), modelBatchSize)], false)
		default:
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// demand returns the states whose expansion can still give an exit to a state
// inside the explore bound: those within exit-1 steps of a state with no exit.
func (s *modelSearch) demand() map[int]bool {
	distance := s.exitDistances()
	reach := map[int]bool{}
	var frontier []int
	for id, n := range s.nodes {
		if d, ok := distance[id]; n.depth <= s.bounds.explore && (!ok || d > s.bounds.exit) {
			reach[id] = true
			frontier = append(frontier, id)
		}
	}
	for hop := 1; hop < s.bounds.exit; hop++ {
		var next []int
		for _, id := range frontier {
			for _, e := range s.nodes[id].edges {
				if e.exit && !reach[e.child] {
					reach[e.child] = true
					next = append(next, e.child)
				}
			}
		}
		frontier = next
	}
	return reach
}

// modelParallel caps the workers: board paths are opened component by component,
// and past eight workers the kernel's path walk contention costs more than the
// extra workers return (measured in the report of card 20261030-composite-model-test-task).
func modelParallel(count int, task func(int)) {
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), 8) {
		wg.Go(func() {
			for at := range work {
				task(at)
			}
		})
	}
	for at := range count {
		work <- at
	}
	close(work)
	wg.Wait()
}

func (s *modelSearch) applySeed(start *modelWorld, seed modelSeed) (*modelWorld, error) {
	w, err := start.clone(s.worldRoot())
	if err != nil {
		return nil, err
	}
	for _, step := range seed.steps {
		name, label := step, ""
		if before, after, ok := strings.Cut(step, ":"); ok {
			label, name = before, after
		}
		f, err := w.read()
		if err != nil {
			return nil, err
		}
		i := -1
		for at, m := range w.members {
			if m.label == label || label == "" && !s.opShared(name) && at == 0 {
				i = at
				break
			}
		}
		op, ok := s.op(name)
		if !ok {
			return nil, fmt.Errorf("unknown operation %s", name)
		}
		if !op.guard(f, i) {
			return nil, fmt.Errorf("%s: guard refused", step)
		}
		var m *modelMember
		if i >= 0 {
			m = w.members[i]
		}
		if err = op.apply(w, m, f); err != nil {
			return nil, fmt.Errorf("%s: %w", step, err)
		}
		w.trace = append(w.trace, step)
	}
	return w, nil
}

func (s *modelSearch) op(name string) (modelOp, bool) {
	at := slices.IndexFunc(s.ops, func(op modelOp) bool { return op.name == name })
	if at < 0 {
		return modelOp{}, false
	}
	return s.ops[at], true
}

func (s *modelSearch) opShared(name string) bool { op, ok := s.op(name); return ok && op.shared }

// exitDistances answers, for every state, how many operations reach a finished
// board. An archive or a trash is not a way out, so those edges do not count. A
// state with no answer is absent.
func (s *modelSearch) exitDistances() map[int]int {
	reverse := make([][]int, len(s.nodes))
	for id, n := range s.nodes {
		for _, e := range n.edges {
			if e.exit {
				reverse[e.child] = append(reverse[e.child], id)
			}
		}
	}
	distance := map[int]int{}
	queue := []int{}
	for id, n := range s.nodes {
		if n.finished {
			distance[id] = 0
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, parent := range reverse[id] {
			if _, seen := distance[parent]; !seen {
				distance[parent] = distance[id] + 1
				queue = append(queue, parent)
			}
		}
	}
	return distance
}

// deadStates returns the states inside the explore bound with no exit inside
// the exit bound. Sorted keys make the list comparable between runs.
func (s *modelSearch) deadStates() []*modelNode {
	distance := s.exitDistances()
	var dead []*modelNode
	for id, n := range s.nodes {
		if n.depth > s.bounds.explore {
			continue
		}
		if d, ok := distance[id]; ok && d <= s.bounds.exit {
			continue
		}
		dead = append(dead, n)
	}
	slices.SortFunc(dead, func(a, b *modelNode) int { return strings.Compare(a.key, b.key) })
	return dead
}

// modelDiagnosis records why the board refuses to go on from a dead state.
type modelDiagnosis struct {
	facts  *modelFacts
	mirror string   // the reconcile result of the state itself
	tails  []string // the tail result of each card, on one copy in member order
	final  string   // the reconcile result after the tails
}

func (d modelDiagnosis) String() string {
	return "reconcile=" + d.mirror + " | tails=" + strings.Join(d.tails, ", ") + " | final=" + d.final
}

// has reports whether any recorded refusal contains text.
func (d modelDiagnosis) has(text string) bool {
	return strings.Contains(d.mirror, text) || strings.Contains(d.final, text) || slices.ContainsFunc(d.tails, func(tail string) bool { return strings.Contains(tail, text) })
}

// diagnose replays the path to a dead state on a private copy.
func (s *modelSearch) diagnose(start *modelWorld, n *modelNode) (d modelDiagnosis, err error) {
	w, err := s.applySeed(start, modelSeed{name: n.seed, steps: n.trace})
	if err != nil {
		return d, fmt.Errorf("replay %s: %w", strings.Join(n.trace, " -> "), err)
	}
	defer os.RemoveAll(w.root)
	if d.facts, err = w.read(); err != nil {
		return d, err
	}
	var infra modelInfraError
	check := func(err error) (string, error) {
		if errors.As(err, &infra) {
			return "", err
		}
		return modelSummary(err), nil
	}
	if d.mirror, err = check(w.reconcile()); err != nil {
		return d, err
	}
	for _, m := range w.members {
		tail, err := check(w.finishMember(m))
		if err != nil {
			return d, err
		}
		d.tails = append(d.tails, tail)
	}
	d.final, err = check(w.reconcile())
	return d, err
}

type modelRun struct {
	name       string
	operations int
	members    int
	bounds     modelBounds
	states     int
	expanded   int
	dead       []*modelNode
	violations []string
	elapsed    time.Duration
}

func modelExplore(t *testing.T, name string, start *modelWorld, seeds []modelSeed, bounds modelBounds, ops []modelOp) modelRun {
	t.Helper()
	begin := time.Now()
	s := &modelSearch{bounds: bounds, ops: ops, index: map[string]int{}, scratch: t.TempDir()}
	if err := s.run(seeds, start); err != nil {
		t.Fatal(err)
	}
	r := modelRun{name: name, operations: len(ops), members: len(start.members), bounds: bounds, states: len(s.nodes), dead: s.deadStates(), violations: s.violations}
	failures := make([]error, len(r.dead))
	modelParallel(len(r.dead), func(at int) { r.dead[at].diagnosis, failures[at] = s.diagnose(start, r.dead[at]) })
	if err := errors.Join(failures...); err != nil {
		t.Fatal(err)
	}
	for _, n := range s.nodes {
		if n.expanded {
			r.expanded++
		}
	}
	r.elapsed = time.Since(begin)
	return r
}

func modelDigest(runs []modelRun, classify func(*modelNode) string) string {
	h := sha256.New()
	for _, r := range runs {
		fmt.Fprintf(h, "%s %d %d\n", r.name, r.states, len(r.dead))
		for _, n := range r.dead {
			fmt.Fprintf(h, "%s %s\n", classify(n), n.key)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
