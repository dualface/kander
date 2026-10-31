package board

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// modelInfraError marks a failure of the model itself. A rejected command is an
// ordinary result; a failed read or copy must stop the test.
type modelInfraError struct{ err error }

func (e modelInfraError) Error() string { return "model infrastructure: " + e.err.Error() }
func (e modelInfraError) Unwrap() error { return e.err }

func modelInfra(err error) error {
	if err == nil {
		return nil
	}
	var infra modelInfraError
	if errors.As(err, &infra) {
		return err
	}
	return modelInfraError{err}
}

type modelCursor struct {
	state    string
	revision uint64
	auth     ExecutionAuthorization
	fence    ExecutionFence
}

// modelMember holds the facts that live outside the board: processes and the
// private cursor of a running launcher.
type modelMember struct {
	id       string
	label    string
	launcher *modelCursor
	original string // the todo text a launcher restores on rollback
	executor bool   // an executor process is alive
	unproven bool   // the liveness probe cannot confirm the executor (unknown)
	self     bool   // the current session owns the card (manual claim)
	stranded string // why a launcher ended without finishing its start attempt
}

// modelWorld is one concrete state: a private copy of a board plus the facts
// the board does not record. seq allocates identifiers along one path, so a
// path produces the same identifiers in every run.
type modelWorld struct {
	root    string
	group   bool
	members []*modelMember
	plans   []string
	stray   string // the run an old binary left outside the plan
	seq     int
	trace   []string
}

func (w *modelWorld) clone(root string) (*modelWorld, error) {
	if err := modelCopyTree(w.root, root); err != nil {
		return nil, modelInfra(err)
	}
	c := *w
	c.root = root
	c.members = make([]*modelMember, len(w.members))
	for i, m := range w.members {
		copied := *m
		if m.launcher != nil {
			cursor := *m.launcher
			copied.launcher = &cursor
		}
		c.members[i] = &copied
	}
	c.plans = slices.Clone(w.plans)
	c.trace = slices.Clone(w.trace)
	return &c, nil
}

// modelCopyTree skips what no later operation reads: lock files, which the board
// recreates on demand, and the retained journal of finished transactions, which
// grows with the path and dominates the size of a copy.
func modelCopyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from, to := filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if entry.Name() == "locks" || entry.Name() == "committed" {
				if err := os.Mkdir(to, 0o700); err != nil {
					return err
				}
				continue
			}
			if err := modelCopyTree(from, to); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (w *modelWorld) snapshot(m *modelMember) (Snapshot, error) {
	s, err := ReadSnapshot(w.root, m.id)
	return s, modelInfra(err)
}

// launcherEntry rebuilds the stale cursor a launcher holds, inside this copy.
func (w *modelWorld) launcherEntry(m *modelMember, s Snapshot) Entry {
	e := s.Entry
	e.State = m.launcher.state
	e.Path = joinBoard(w.root, m.launcher.state, baseEntryName(s.Entry))
	e.Document = filepath.Join(e.Path, "spec.md")
	e.Version = &Version{revision: m.launcher.revision, authorization: m.launcher.auth, fence: m.launcher.fence, warnings: entryWarningLog(s.Entry)}
	return e
}

func (w *modelWorld) nextID(prefix string) string {
	w.seq++
	return fmt.Sprintf("model-%s-%d", prefix, w.seq)
}

func (w *modelWorld) nextCommit() string {
	w.seq++
	return fmt.Sprintf("%040x", 0xc0+w.seq)
}

// modelFacts is everything a guard or the state key may read. One read serves
// the key, every guard and the invariant checks of a state.
type modelFacts struct {
	world    *modelWorld
	cards    []Snapshot
	attempts []ExecutionRecord
	ledgers  []executionLedger
	rounds   [][]Dispatch
	bound    []*Dispatch
	ckpt     *CoordinatorCheckpoint
	review   modelReviewFacts
	excluded []bool   // the member has an exclusion record
	copies   []string // the text of each member's plan copy, "" when it has none
}

// modelMemberFacts is the view of one member an invariant reads.
type modelMemberFacts struct {
	card   Snapshot
	ledger executionLedger
	rounds []Dispatch
}

func (f *modelFacts) member(i int) modelMemberFacts {
	return modelMemberFacts{f.cards[i], f.ledgers[i], f.rounds[i]}
}

// chained reports a card whose chain still exists. Archive cleanup removes the
// records of a card, and the receipt stands in for them.
func (m modelMemberFacts) chained() bool {
	return m.card.Entry.State != "archived" || m.ledger.synthesized < len(m.ledger.chain)
}

type modelBatchFacts struct {
	id         string
	plan       string
	sealed     bool
	closed     bool
	base       string
	target     string
	advances   int
	required   []string
	members    []string
	live       []string // members that can still take a run
	runs       []modelRunFacts
	superseded map[string]ReviewSupersededRun // the closure's retired runs
}

type modelRunFacts struct {
	id       string
	role     string
	commit   string
	previous string
	report   string
	findings []string
	assigned map[string][]string // nil when the run has no assignment
	records  []ReviewDisposition
}

type modelReviewFacts struct {
	batches    []modelBatchFacts
	cycleOwed  []string // members whose rebound cycle has no batch
	reviewOwed []string // members whose recovery still needs an actual review
	foreign    []string // runs of a plan member outside every batch of that plan
	resolved   int      // unplanned batches a plan accepted
}

func (r modelRunFacts) recorded(finding, task string) bool {
	return slices.ContainsFunc(r.records, func(d ReviewDisposition) bool { return d.FindingID == finding && d.TaskID == task })
}

func (r modelRunFacts) undisposed() []string {
	var open []string
	for _, finding := range r.findings {
		for _, task := range r.assigned[finding] {
			if !r.recorded(finding, task) {
				open = append(open, task+"/"+finding)
			}
		}
	}
	return open
}

// head returns the run of role no other run of the batch continues.
func (b modelBatchFacts) head(role string) (modelRunFacts, bool) {
	var found modelRunFacts
	ok := false
	for _, r := range b.runs {
		if r.role != role {
			continue
		}
		if !slices.ContainsFunc(b.runs, func(o modelRunFacts) bool { return o.previous == r.id }) {
			found, ok = r, true
		}
	}
	return found, ok
}

func (w *modelWorld) readReview() (facts modelReviewFacts, err error) {
	err = WithTransaction(w.root, reviewScope(nil, true), func(tx *Transaction) error {
		names := []string{}
		for _, id := range w.plans {
			var p ReviewPlan
			ok, e := readReviewJSON(tx, planName(id), &p)
			if e != nil {
				return e
			}
			if !ok { // removed by archive cleanup
				continue
			}
			for _, b := range p.Batches {
				names = append(names, b.BatchID)
			}
			for _, member := range p.TaskIDs {
				gone, e := memberExcluded(tx, member)
				if e != nil {
					return e
				}
				if gone {
					continue
				}
				if cycleBatchMissing(p, member) {
					facts.cycleOwed = append(facts.cycleOwed, member)
				}
				owed, e := recoveryReviewOwed(tx, p, member)
				if e != nil {
					return e
				}
				if owed {
					facts.reviewOwed = append(facts.reviewOwed, member)
				}
			}
			if e := checkUnplannedReviewRuns(tx, p); e != nil {
				facts.foreign = append(facts.foreign, e.Error())
			}
			facts.resolved += len(p.ResolvedBatches)
		}
		for _, id := range names {
			batch, ok, e := modelReadBatch(tx, id)
			if e != nil {
				return e
			}
			if ok {
				facts.batches = append(facts.batches, batch)
			}
		}
		return nil
	})
	return facts, modelInfra(err)
}

func modelReadBatch(tx *Transaction, id string) (facts modelBatchFacts, exists bool, err error) {
	var b ReviewBatch
	if exists, err = readReviewJSON(tx, reviewBatchName(id), &b); err != nil || !exists {
		return
	}
	facts = modelBatchFacts{id: id, plan: b.PlanID, base: b.Base, target: b.TargetCommit, advances: len(b.Advances), members: b.TaskIDs}
	for _, member := range b.TaskIDs {
		gone, e := memberExcluded(tx, member)
		if e != nil {
			return facts, true, e
		}
		if !gone {
			facts.live = append(facts.live, member)
		}
	}
	for _, role := range slices.Sorted(maps.Keys(b.Requirements)) {
		if b.Requirements[role] == "required" {
			facts.required = append(facts.required, role)
		}
	}
	var closure ReviewClosure
	if facts.closed, err = readReviewJSON(tx, closureName(id), &closure); err != nil {
		return
	}
	facts.superseded = closure.Request.SupersededRuns
	if b.PlanID != "" {
		var p ReviewPlan
		if _, err = readReviewJSON(tx, planName(b.PlanID), &p); err != nil {
			return
		}
		facts.sealed = p.Sealed
	}
	runs, err := batchRuns(tx, b)
	if err != nil {
		return
	}
	slices.SortFunc(runs, func(x, y ReviewRun) int { return strings.Compare(x.CreatedAt+x.RunID, y.CreatedAt+y.RunID) })
	for _, run := range runs {
		r := modelRunFacts{id: run.RunID, role: run.Role, commit: run.Commit, previous: run.PreviousRunID, report: run.Hashes["report.md"]}
		findings, e := runFindings(tx, run)
		if e != nil {
			return facts, true, e
		}
		for _, item := range findings.all() {
			r.findings = append(r.findings, item.ID)
		}
		var ledger dispositionLedger
		ok, e := readReviewJSON(tx, ledgerName(run.RunID), &ledger)
		if e != nil {
			return facts, true, e
		}
		if ok {
			r.assigned, r.records = ledger.Assignment.Items, ledger.Records
		}
		facts.runs = append(facts.runs, r)
	}
	return facts, true, nil
}

func (w *modelWorld) read() (*modelFacts, error) {
	f := &modelFacts{world: w}
	for _, m := range w.members {
		s, err := w.snapshot(m)
		if err != nil {
			return nil, err
		}
		f.cards = append(f.cards, s)
		var attempt ExecutionRecord
		var ledger executionLedger
		var rounds []Dispatch
		err = WithTransaction(w.root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{m.id}, ReadOnly: true}, func(tx *Transaction) error {
			var e error
			if attempt, _, e = readExecutionRecord(tx, m.id); e != nil {
				return e
			}
			if ledger, e = readExecutionLedger(tx, s); e != nil {
				return e
			}
			rounds, e = dispatchRounds(tx, s)
			return e
		})
		if err != nil {
			return nil, modelInfra(err)
		}
		excluded, copied := false, ""
		err = WithTransaction(w.root, reviewScope([]string{m.id}, true), func(tx *Transaction) error {
			var e error
			if excluded, e = memberExcluded(tx, m.id); e != nil {
				return e
			}
			if copied, e = tx.Read(m.id, "reviews/plan.json"); errors.Is(e, os.ErrNotExist) {
				return nil
			}
			return e
		})
		if err != nil {
			return nil, modelInfra(err)
		}
		f.excluded = append(f.excluded, excluded)
		f.copies = append(f.copies, copied)
		f.attempts = append(f.attempts, attempt)
		f.ledgers = append(f.ledgers, ledger)
		f.rounds = append(f.rounds, rounds)
		var bound *Dispatch
		if a := authFrom(s.Text); a.DispatchID != "" {
			d, err := ReadDispatch(w.root, m.id, a.DispatchID)
			if err != nil {
				return nil, modelInfra(err)
			}
			bound = &d
		}
		f.bound = append(f.bound, bound)
	}
	var err error
	if f.ckpt, err = w.checkpoint(); err != nil {
		return nil, err
	}
	f.review, err = w.readReview()
	return f, err
}

// checkpoint returns nil for a single card, and for a group whose checkpoint
// the archive cleanup removed with its last member.
func (w *modelWorld) checkpoint() (*CoordinatorCheckpoint, error) {
	if !w.group {
		return nil, nil
	}
	var c CoordinatorCheckpoint
	exists := false
	err := WithTransaction(w.root, LockScope{Groups: []string{coordinatorGroup}, ReadOnly: true}, func(tx *Transaction) error {
		var e error
		c, exists, e = readCheckpoint(tx, coordinatorGroup)
		return e
	})
	if err != nil {
		return nil, modelInfra(err)
	}
	if !exists {
		return nil, nil
	}
	return &c, nil
}

func modelTerminal(state string) bool {
	return state == "done" || state == "archived" || state == "trash"
}

// blocked reports a member the last reconcile did not accept.
func (f *modelFacts) blocked(i int) bool {
	return f.ckpt != nil && f.ckpt.Members[f.world.members[i].id].Blocked != nil
}

func (f *modelFacts) evidence(i int) bool {
	return f.attempts[i].ID != "" && f.cards[i].Revision > f.attempts[i].Revision
}

// memberKey abstracts one card. Revisions, identifiers and time stay out, so
// two paths that differ only in those reach one abstract state.
func (f *modelFacts) memberKey(i int) string {
	m, s := f.world.members[i], f.cards[i]
	attempt := "none"
	if f.attempts[i].ID != "" {
		attempt = f.attempts[i].Status
	}
	bound := "none"
	if d := f.bound[i]; d != nil {
		bound = d.Input.Kind + "/" + string(d.State)
		if d.WrapUpAuthority != nil {
			bound += "/grant"
		}
	}
	releases, _ := cardReleases(s)
	mirror := "-"
	if f.ckpt != nil {
		c := f.ckpt.Members[m.id]
		mirror = fmt.Sprintf("match=%v awaiting=%v dispatch=%s delivery=%v handoffs=%d blocked=%v confirmed=%v", c.Cycle == planCycle(s), c.AwaitingStart, modelMirrorDispatch(c, f.bound[i], releases), c.DeliveryCommit != "", len(c.Handoffs), c.Blocked != nil, c.Confirmed != nil)
	}
	settled := slices.ContainsFunc(f.rounds[i], func(d Dispatch) bool { return d.Settlement != nil })
	return fmt.Sprintf("col=%s excluded=%v attempt=%s evidence=%v owner=%s session=%q launcher=%v executor=%v unproven=%v self=%v stranded=%s bound=%s releases=%v settled=%v handoffs=%d mirror[%s] group=%s",
		s.Entry.State, f.excluded[i], attempt, f.evidence(i), MetadataFrom(s.Text, FieldOwner), MetadataFrom(s.Text, FieldSession),
		m.launcher != nil, m.executor, m.executor && m.unproven, m.self, m.stranded, bound, len(releases) > 0, settled,
		strings.Count(modelSection(s.Text, "LIFECYCLE_DECISION"), `"kind":"reclaim"`), mirror, modelGroupClass(TaskGroupFrom(s.Text)))
}

// modelMirrorDispatch relates the dispatch of a checkpoint mirror to the card. A
// reconcile that adopts a new round changes this relation, and the next release
// is verified against the dispatch the mirror holds.
func modelMirrorDispatch(c CoordinatorMember, bound *Dispatch, releases []DispatchRelease) string {
	switch {
	case c.Dispatch == nil:
		return "none"
	case bound != nil && bound.Input.ID == c.Dispatch.ID:
		return "current/" + string(c.Dispatch.State)
	case slices.ContainsFunc(releases, func(r DispatchRelease) bool { return r.Termination.Authorization.DispatchID == c.Dispatch.ID }):
		return "released"
	}
	return "stale/" + string(c.Dispatch.State)
}

func modelSection(text, name string) string { b, _ := SectionBody(text, name); return b }

func modelGroupClass(group string) string {
	switch group {
	case "":
		return "none"
	case coordinatorGroup:
		return "same"
	}
	return "other"
}

func (f *modelFacts) reviewKey() string {
	var parts []string
	for _, b := range f.review.batches {
		state := "open"
		if b.closed {
			state = "closed"
		}
		runs := []string{}
		for _, r := range b.runs {
			runs = append(runs, fmt.Sprintf("%s:f%d:a%v:u%d:p%v:at-target%v", r.role, len(r.findings), r.assigned != nil, len(r.undisposed()), r.previous != "", r.commit == b.target))
		}
		parts = append(parts, fmt.Sprintf("%s/%s/sealed=%v/adv=%d/%s", b.id, state, b.sealed, b.advances, strings.Join(runs, ",")))
	}
	parts = append(parts, fmt.Sprintf("cycle-owed=%d review-owed=%d foreign=%d resolved=%d", len(f.review.cycleOwed), len(f.review.reviewOwed), len(f.review.foreign), f.review.resolved))
	return strings.Join(parts, ";")
}

func (f *modelFacts) key() string {
	parts := []string{}
	for i, m := range f.world.members {
		parts = append(parts, m.label+"{"+f.memberKey(i)+"}")
	}
	parts = append(parts, "review{"+f.reviewKey()+"}")
	return strings.Join(parts, " ")
}

func (f *modelFacts) rebindNeeded(i int) bool {
	if modelTerminal(f.cards[i].Entry.State) {
		return false
	}
	p, err := ReviewTaskProgress(f.world.root, f.world.members[i].id)
	return err == nil && len(p.RebindCycles) > 0
}

// reconcile observes every member through the same request the orchestrator
// sends. Git is structural in this model: the verifier accepts every fact.
func (w *modelWorld) reconcile() error {
	return w.reconcileWith(func(context.Context, CoordinatorGitFacts) error { return nil })
}

// reconcileAll is the reconcile of a finished group: a blocked member is a failure.
func (w *modelWorld) reconcileAll() error {
	if err := w.reconcile(); err != nil {
		return err
	}
	c, err := w.checkpoint()
	if err != nil || c == nil {
		return err
	}
	if blocked := c.BlockedMembers(); len(blocked) > 0 {
		return errors.New("blocked: " + c.Members[blocked[0]].Blocked.Reason)
	}
	return nil
}

// reconcileWith publishes whatever the request decides: a blocked member is a
// result of the command, and only a refused request discards the copy.
func (w *modelWorld) reconcileWith(verify func(context.Context, CoordinatorGitFacts) error) error {
	checkpoint, err := w.checkpoint()
	if err != nil || checkpoint == nil {
		return err
	}
	c := *checkpoint
	r := CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]CoordinatorObservation{}}
	for _, id := range checkpointIDs(c) {
		s, err := ReadSnapshot(w.root, id)
		if err != nil {
			return modelInfra(err)
		}
		o := CoordinatorObservation{Revision: s.Revision}
		if a := authFrom(s.Text); a.DispatchID != "" {
			d, err := ReadDispatch(w.root, id, a.DispatchID)
			if err != nil {
				return modelInfra(err)
			}
			o.DispatchID, o.Epoch, o.Base = a.DispatchID, a.Epoch, d.Input.Base
			if d.Completed != nil {
				o.DeliveryCommit = d.Completed.DeliveryCommit
			}
		}
		r.Members[id] = o
	}
	_, err = ReconcileCoordinator(context.Background(), w.root, r, verify)
	var blocked *CoordinatorBlockedError
	if errors.As(err, &blocked) {
		return nil
	}
	return err
}

// modelSummary keeps a short, stable error text for a state's diagnostics.
func modelSummary(err error) string {
	if err == nil {
		return "ok"
	}
	text := err.Error()
	if len(text) > 160 {
		text = text[:160]
	}
	return text
}

func modelTodoCard(t *testing.T, root, slug, group string) Snapshot {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text, err := setMetadata(strings.ReplaceAll(readyText(s), "<FILL_IN>", "fixture"), FieldTaskGroup, group)
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: model-test", 1)
	updateSnapshot(t, root, s, text)
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err = MoveEntry(s.Entry, root, "todo"); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

// modelInitial builds the single root every seed starts from.
func modelInitial(t *testing.T, group bool, count int) *modelWorld {
	t.Helper()
	root := tempBoard(t)
	w := &modelWorld{root: root, group: group}
	name := ""
	if group {
		name = coordinatorGroup
	}
	ids := []string{}
	for i := range count {
		label := string(rune('A' + i))
		s := modelTodoCard(t, root, "model-"+strings.ToLower(label), name)
		w.members = append(w.members, &modelMember{id: s.Entry.TaskID, label: label})
		ids = append(ids, s.Entry.TaskID)
	}
	if group {
		coordinatorClaim(t, root, ids...)
		if err := w.reconcile(); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// modelDispatchWindow returns the creation time and acceptance deadline of a
// dispatch. Time stays out of the abstract state: a deadline the search cannot
// outlive keeps a slow machine from refusing what a fast one accepts.
func modelDispatchWindow() (time.Time, time.Time) {
	now := time.Now().UTC()
	return now, now.Add(24 * time.Hour)
}
