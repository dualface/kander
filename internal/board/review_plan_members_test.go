package board

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/statemachine/reviewobligation"
	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

// membersWorld is a plan of one started group card whose only batch closed
// without a review, and a second started card of the same group with no plan.
// candidate is the card a request adds; it is the second card unless a test names
// another one. also lists further cards a request names, and before is the capture
// a refused request is compared with.
type membersWorld struct {
	root          string
	first, second string
	candidate     string
	also          []string
	plan          ReviewPlan
	before        map[string]string
}

func newMembersWorld(t *testing.T, slug string, sealed bool) *membersWorld {
	t.Helper()
	useEnglish(t)
	root := tempBoard(t)
	w := &membersWorld{root: root, first: gateCard(t, root, slug+"-a"), second: gateCard(t, root, slug+"-b")}
	w.candidate = w.second
	p := ReviewPlan{Schema: 1, Sealed: sealed, PlanID: "plan-" + slug, Author: "coordinator", Basis: "fixture", CWD: "/repo", TaskIDs: []string{w.first}, Batches: []ReviewPlanBatch{
		{BatchID: "first", TaskIDs: []string{w.first}, Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "first", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	w.plan.PlanID = p.PlanID
	w.refresh(t)
	return w
}

func (w *membersWorld) refresh(t *testing.T) {
	t.Helper()
	w.plan = readOwedPlan(t, w.root, w.plan.PlanID)
}

func (w *membersWorld) extend(x ReviewPlanExtension) error {
	var err error
	if w.before, err = w.files(); err != nil {
		return err
	}
	x.PlanID, x.ExpectedRevision, x.Author, x.Basis = w.plan.PlanID, w.plan.Revision, "coordinator", "the second card started"
	return ExtendReviewPlan(w.root, x)
}

// edit rewrites the card text of id inside one transaction.
func (w *membersWorld) edit(t *testing.T, id string, change func(text string) (string, error)) {
	t.Helper()
	err := WithTransaction(w.root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		s, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		text, err := change(s.Text)
		if err != nil {
			return err
		}
		return tx.Put(id, "spec.md", text)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (w *membersWorld) setMetadata(t *testing.T, id, field, value string) {
	t.Helper()
	w.edit(t, id, func(text string) (string, error) { return setMetadata(text, field, value) })
}

func (w *membersWorld) relocate(t *testing.T, id, state string) {
	t.Helper()
	err := WithTransaction(w.root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error { return tx.Relocate(id, state) })
	if err != nil {
		t.Fatal(err)
	}
}

func (w *membersWorld) writeGroup(t *testing.T, name string, value any) {
	t.Helper()
	err := WithTransaction(w.root, reviewScope(nil, false), func(tx *Transaction) error {
		return tx.PutGroup(reviewControlGroup, name, reviewJSON(value))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (w *membersWorld) planOf(t *testing.T, id string) (planID string, exists bool) {
	t.Helper()
	err := WithTransaction(w.root, reviewScope(nil, true), func(tx *Transaction) error {
		p, ok, e := readTaskPlan(tx, id)
		planID, exists = p.PlanID, ok
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return planID, exists
}

func (w *membersWorld) groupFile(t *testing.T, name string) (string, bool) {
	t.Helper()
	var text string
	var ok bool
	err := WithTransaction(w.root, reviewScope(nil, true), func(tx *Transaction) error {
		raw, exists, e := tx.ReadGroup(reviewControlGroup, name)
		text, ok = string(raw), exists
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return text, ok
}

// absent is the capture value of a file that does not exist.
const absent = "<absent>"

// capture records the raw bytes a refused request must leave alone: the record of
// every batch of the plan, the plan copy of the first member, the card file of the
// candidate, and whether the candidate has a plan copy. extend runs it before the
// request; a test that calls the board directly runs it itself.
func (w *membersWorld) capture(t *testing.T) {
	t.Helper()
	var err error
	if w.before, err = w.files(); err != nil {
		t.Fatal(err)
	}
}

func (w *membersWorld) files() (map[string]string, error) {
	got := map[string]string{}
	cards := slices.Concat([]string{w.first, w.candidate}, w.also)
	err := WithTransaction(w.root, reviewScope(cards, true), func(tx *Transaction) error {
		for _, b := range w.plan.Batches {
			raw, exists, e := tx.ReadGroup(reviewControlGroup, reviewBatchName(b.BatchID))
			if e != nil {
				return e
			}
			got["batch record "+b.BatchID] = shownBytes(raw, exists)
		}
		files := []struct{ label, id, name string }{
			{"plan copy of the first member", w.first, "reviews/plan.json"},
			{"card file of the candidate", w.candidate, "spec.md"},
			{"plan copy of the candidate", w.candidate, "reviews/plan.json"},
		}
		for _, id := range w.also {
			files = append(files, struct{ label, id, name string }{"card file of " + id, id, "spec.md"}, struct{ label, id, name string }{"plan copy of " + id, id, "reviews/plan.json"})
		}
		for _, file := range files {
			text, e := tx.Read(file.id, file.name)
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
			got[file.label] = shownBytes([]byte(text), e == nil)
		}
		return nil
	})
	return got, err
}

func shownBytes(raw []byte, exists bool) string {
	if !exists {
		return absent
	}
	return string(raw)
}

// differences lists, by label, what changed since capture.
func (w *membersWorld) differences(t *testing.T) []string {
	t.Helper()
	if w.before == nil {
		t.Fatal("no capture before the request")
	}
	now, err := w.files()
	if err != nil {
		t.Fatal(err)
	}
	var changed []string
	for label, was := range w.before {
		if now[label] != was {
			changed = append(changed, label)
		}
	}
	slices.Sort(changed)
	return changed
}

// untouched checks that a refused request changed no plan, copy, pointer, cycle
// record, history, batch record, or candidate card file. The candidate keeps the
// plan pointer it had, if any.
func (w *membersWorld) untouched(t *testing.T, pointer string) {
	t.Helper()
	if changed := w.differences(t); len(changed) != 0 {
		t.Fatalf("a refused request changed: %v", changed)
	}
	if got := readOwedPlan(t, w.root, w.plan.PlanID); reviewJSON(got) != reviewJSON(w.plan) {
		t.Fatalf("plan changed: %+v", got)
	}
	if reviewJSON(readPlanCopy(t, w.root, w.first)) != reviewJSON(w.plan) {
		t.Fatal("the plan copy of the first member changed")
	}
	if id, ok := w.planOf(t, w.candidate); id != pointer || ok != (pointer != "") {
		t.Fatalf("the plan pointer of the candidate is %q", id)
	}
	if _, ok := w.groupFile(t, "tracked-cycles/"+w.candidate+".json"); ok != (pointer != "") {
		t.Fatal("the candidate got a cycle record")
	}
	if _, ok := w.groupFile(t, fmt.Sprintf("plan-history/%s/%d.json", w.plan.PlanID, w.plan.Revision+1)); ok {
		t.Fatal("a history record was written")
	}
}

func TestAddMembersJoinsAStartedGroupCard(t *testing.T) {
	w := newMembersWorld(t, "members-join", false)
	cycle := planCycle(transactionSnapshot(t, w.root, w.second))
	if err := w.extend(ReviewPlanExtension{AddMembers: []string{w.second, w.second}}); err != nil {
		t.Fatal(err)
	}
	got := readOwedPlan(t, w.root, w.plan.PlanID)
	if got.Revision != w.plan.Revision+1 || got.Sealed || strings.Join(got.TaskIDs, ",") != w.first+","+w.second || got.Cycles[w.second] != cycle || got.Cycles[w.first] != w.plan.Cycles[w.first] {
		t.Fatalf("plan: %+v", got)
	}
	if id, ok := w.planOf(t, w.second); !ok || id != w.plan.PlanID {
		t.Fatalf("pointer %q %v", id, ok)
	}
	if text, ok := w.groupFile(t, "tracked-cycles/"+w.second+".json"); !ok || !strings.Contains(text, cycle) {
		t.Fatalf("cycle record %q %v", text, ok)
	}
	for _, id := range []string{w.first, w.second} {
		if reviewJSON(readPlanCopy(t, w.root, id)) != reviewJSON(got) {
			t.Fatalf("the plan copy of %s differs from the plan", id)
		}
	}
	text, ok := w.groupFile(t, fmt.Sprintf("plan-history/%s/%d.json", got.PlanID, got.Revision))
	var history struct {
		Previous   ReviewPlan          `json:"previous"`
		Request    ReviewPlanExtension `json:"request"`
		RecordedAt string              `json:"recorded_at"`
	}
	if !ok {
		t.Fatal("no history record")
	}
	if err := DecodeReviewJSON([]byte(text), &history); err != nil {
		t.Fatal(err)
	}
	if reviewJSON(history.Previous) != reviewJSON(w.plan) || strings.Join(history.Request.AddMembers, ",") != w.second {
		t.Fatalf("history: %+v\nplan %+v", history, w.plan)
	}
	if problems := CheckReviewGate(w.root, []string{w.first, w.second}); len(problems) != 0 {
		t.Fatalf("%v", problems)
	}
}

// A new member joins the chain: the seal waits for its batch, the batch runs and
// closes under the existing rules, and the plan seals afterwards.
func TestAddMembersTakesABatchBeforeTheSeal(t *testing.T) {
	w := newMembersWorld(t, "members-batch", false)
	if err := w.extend(ReviewPlanExtension{AddMembers: []string{w.second}}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	doneReady := func() bool {
		snap := snapshotKernels(t, w.root, 8, w.second)
		ready, ok := reviewobligation.DoneReady(snap.members[w.second], reviewobligation.Params{MaxBatch: snap.bound})
		if !ok {
			t.Fatal("doneReady is undefined for the new member")
		}
		return ready
	}
	if doneReady() {
		t.Fatal("a member with no batch is ready")
	}
	if err := w.extend(ReviewPlanExtension{Seal: true}); err == nil || !strings.Contains(err.Error(), "every member assigned") {
		t.Fatalf("sealed with an unassigned member: %v", err)
	}
	batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: []string{w.second}, Base: sha("b"), TargetCommit: sha("c"), Requirements: archiveRequirements()}
	if err := w.extend(ReviewPlanExtension{Batch: &batch}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	if doneReady() {
		t.Fatal("a member is ready before its batch closes")
	}
	closeOwedBatch(t, w.root, batch)
	if !doneReady() {
		t.Fatal("a member is not ready after its batch closed")
	}
	if err := w.extend(ReviewPlanExtension{Seal: true}); err != nil {
		t.Fatal(err)
	}
	if problems := CheckReviewGate(w.root, []string{w.first, w.second}); len(problems) != 0 {
		t.Fatalf("%v", problems)
	}
}

func TestAddMembersRefusals(t *testing.T) {
	stray := func(t *testing.T, w *membersWorld) {
		in := archiveInput([]string{w.second}, "stray-run", "PMQA")
		in.BatchID, in.Base, in.Commit = "stray", sha("b"), sha("c")
		run, _, err := PrepareReviewRun(w.root, in, archiveRequirements(), nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		if run, err = FinalizeReviewRun(w.root, run, structuredReport(emptyFindings())); err != nil {
			t.Fatal(err)
		}
		publishRun(t, w.root, run.RunID)
	}
	cases := []struct {
		name    string
		sealed  bool
		pointer string
		setup   func(t *testing.T, w *membersWorld)
		add     func(w *membersWorld) []string
		want    func(w *membersWorld) []string
	}{
		{name: "sealed plan", sealed: true, want: func(w *membersWorld) []string { return []string{w.plan.PlanID, "is sealed"} }},
		{name: "backlog", setup: func(t *testing.T, w *membersWorld) { w.relocate(t, w.second, "backlog") }, want: func(w *membersWorld) []string { return []string{w.second, "is in backlog"} }},
		{name: "todo", setup: func(t *testing.T, w *membersWorld) { w.relocate(t, w.second, "todo") }, want: func(w *membersWorld) []string { return []string{w.second, "is in todo"} }},
		{name: "done", setup: func(t *testing.T, w *membersWorld) { w.relocate(t, w.second, "done") }, want: func(w *membersWorld) []string { return []string{w.second, "is in done"} }},
		{name: "archived", setup: func(t *testing.T, w *membersWorld) { w.relocate(t, w.second, "archived") }, want: func(w *membersWorld) []string { return []string{w.second, "is in archived"} }},
		{name: "trash", setup: func(t *testing.T, w *membersWorld) { w.relocate(t, w.second, "trash") }, want: func(w *membersWorld) []string { return []string{w.second, "is in trash"} }},
		{name: "another group", setup: func(t *testing.T, w *membersWorld) {
			w.setMetadata(t, w.second, FieldTaskGroup, "20260907-other-group")
		}, want: func(w *membersWorld) []string {
			return []string{w.second, "20260907-other-group", "20260907-evidence-group"}
		}},
		{name: "no group", setup: func(t *testing.T, w *membersWorld) { w.setMetadata(t, w.second, FieldTaskGroup, "") }, want: func(w *membersWorld) []string { return []string{w.second, `task group ""`} }},
		{name: "plan of an independent card", setup: func(t *testing.T, w *membersWorld) { w.setMetadata(t, w.first, FieldTaskGroup, "") }, want: func(w *membersWorld) []string {
			return []string{w.second, "share no task group"}
		}},
		{name: "already a member", add: func(w *membersWorld) []string { return []string{w.first} }, want: func(w *membersWorld) []string { return []string{w.first, "already a member"} }},
		{name: "member of another plan", pointer: "plan-other", setup: func(t *testing.T, w *membersWorld) {
			other := ReviewPlan{Schema: 1, PlanID: "plan-other", Author: "coordinator", Basis: "fixture", CWD: "/repo", TaskIDs: []string{w.second}, Batches: []ReviewPlanBatch{
				{BatchID: "other", TaskIDs: []string{w.second}, Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
			}}
			if err := CreateReviewPlan(w.root, other); err != nil {
				t.Fatal(err)
			}
		}, want: func(w *membersWorld) []string { return []string{w.second, "already belongs to review plan plan-other"} }},
		{name: "member exclusion", setup: func(t *testing.T, w *membersWorld) {
			w.writeGroup(t, exclusionName(w.second), ReviewExclusion{Schema: 1, ExclusionID: "exclusion-1", TaskID: w.second})
		}, want: func(w *membersWorld) []string { return []string{w.second, "member exclusion record"} }},
		{name: "run outside the plan", setup: stray, want: func(w *membersWorld) []string { return []string{w.second, "batch stray"} }},
		{name: "another report language", setup: func(t *testing.T, w *membersWorld) { w.setMetadata(t, w.second, FieldLanguage, "en") }, want: func(w *membersWorld) []string {
			return []string{w.second, "reports in en", "reports in zh-CN"}
		}},
		{name: "empty list", add: func(w *membersWorld) []string { return []string{} }, want: func(w *membersWorld) []string { return []string{"at least one task"} }},
		{name: "invalid task ID", add: func(w *membersWorld) []string { return []string{"Not A Task"} }, want: func(w *membersWorld) []string { return []string{"Not A Task"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newMembersWorld(t, "members-refuse", c.sealed)
			if c.setup != nil {
				c.setup(t, w)
			}
			ids := []string{w.second}
			if c.add != nil {
				ids = c.add(w)
			}
			err := w.extend(ReviewPlanExtension{AddMembers: ids})
			if err == nil {
				t.Fatal("accepted")
			}
			for _, want := range c.want(w) {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not name %q", err, want)
				}
			}
			w.untouched(t, c.pointer)
		})
	}
}

// One refused member refuses the whole request.
func TestAddMembersRefusesTheWholeRequest(t *testing.T) {
	w := newMembersWorld(t, "members-whole", false)
	third := gateCard(t, w.root, "members-whole-c")
	w.relocate(t, third, "todo")
	w.also = []string{third}
	err := w.extend(ReviewPlanExtension{AddMembers: []string{w.second, third}})
	if err == nil || !strings.Contains(err.Error(), third+" is in todo") || strings.Contains(err.Error(), w.second+" is in") {
		t.Fatalf("error: %v", err)
	}
	w.untouched(t, "")
	if _, ok := w.planOf(t, third); ok {
		t.Fatal("the refused member got a plan pointer")
	}
}

func TestAddMembersIsExclusiveAndVersioned(t *testing.T) {
	w := newMembersWorld(t, "members-exclusive", false)
	batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: []string{w.first}, Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()}
	others := map[string]ReviewPlanExtension{
		"batch":              {Batch: &batch},
		"seal":               {Seal: true},
		"rebind_cycles":      {RebindCycles: map[string]string{w.first: "x"}},
		"sync_targets":       {SyncTargets: map[string]string{"first": sha("c")}},
		"retarget_batches":   {RetargetBatches: map[string]ReviewBatchRetarget{"first": {PreviousBase: sha("a"), PreviousTarget: sha("b"), Base: sha("a"), Target: sha("c")}}},
		"drop_batches":       {DropBatches: []string{"first"}},
		"release_obligation": {ReleaseObligation: &ReviewObligationRelease{Kind: releaseDropped, TaskIDs: []string{w.first}}, Decision: "user"},
		"decision":           {Decision: "user"},
	}
	for name, x := range others {
		t.Run(name, func(t *testing.T) {
			x.AddMembers = []string{w.second}
			if err := w.extend(x); err == nil {
				t.Fatal("accepted")
			}
			w.untouched(t, "")
		})
	}
	t.Run("resolve_batch", func(t *testing.T) {
		x := ReviewPlanExtension{PlanID: w.plan.PlanID, ExpectedRevision: w.plan.Revision, Author: "coordinator", Basis: "fixture", AddMembers: []string{w.second},
			ResolveBatch: &ReviewBatchResolutionRequest{BatchID: "stray", CoveringBatchID: "first"}}
		w.capture(t)
		err := ResolveReviewBatch(w.root, x, ReviewResolutionGit{CWD: "/repo"})
		if err == nil || !strings.Contains(err.Error(), "excludes every other extend-plan operation") {
			t.Fatalf("error: %v", err)
		}
		w.untouched(t, "")
	})
	t.Run("stale revision", func(t *testing.T) {
		w.capture(t)
		err := ExtendReviewPlan(w.root, ReviewPlanExtension{PlanID: w.plan.PlanID, ExpectedRevision: w.plan.Revision + 1, Author: "coordinator", Basis: "fixture", AddMembers: []string{w.second}})
		want := fmt.Sprintf("expected_revision is %d, but plan %s is at revision %d", w.plan.Revision+1, w.plan.PlanID, w.plan.Revision)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error: %v", err)
		}
		w.untouched(t, "")
	})
}

// A request that names add_members adds its members to the authority set; one
// that does not leaves the members of the plan.
func TestReviewPlanExtensionTasks(t *testing.T) {
	p := ReviewPlan{TaskIDs: []string{"20260907-b-task"}}
	got, err := ReviewPlanExtensionTasks(p, ReviewPlanExtension{AddMembers: []string{"20260907-c-task", "20260907-a-task", "20260907-c-task.md"}})
	if err != nil || strings.Join(got, ",") != "20260907-a-task,20260907-b-task,20260907-c-task" {
		t.Fatalf("%v %v", got, err)
	}
	if got, err = ReviewPlanExtensionTasks(p, ReviewPlanExtension{Seal: true}); err != nil || strings.Join(got, ",") != "20260907-b-task" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err = ReviewPlanExtensionTasks(p, ReviewPlanExtension{AddMembers: []string{"not a task"}}); err == nil {
		t.Fatal("an invalid task ID was accepted")
	}
	if _, err = ReviewPlanExtensionTasks(p, ReviewPlanExtension{AddMembers: []string{}}); err == nil {
		t.Fatal("an empty list was accepted")
	}
}

// add_members refines the kernels: the plan event, the members already in the
// plan keep their state, and the new member's projection is the state `create`
// reaches followed by every batch of the plan with the member in none. A refused
// request leaves every projection as it was.
func TestAddMembersRefinesKernels(t *testing.T) {
	w := newMembersWorld(t, "members-refine", false)
	tail := ReviewPlanBatch{BatchID: "tail", PreviousBatchID: "first", TaskIDs: []string{w.first}, Base: sha("b"), TargetCommit: sha("c"), Requirements: archiveRequirements()}
	if err := w.extend(ReviewPlanExtension{Batch: &tail}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	ids := []string{w.first}
	project := func(planned bool, p ReviewPlan) reviewobligation.State {
		var st reviewobligation.State
		err := WithTransaction(w.root, reviewLedgerScope([]string{w.second}, true), func(tx *Transaction) (e error) {
			st, e = projectObligation(tx, p, planned, w.second, 16)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	beforeMember := project(false, w.plan)

	refines(t, w.root, []string{w.first, w.second}, kernelSteps{}, true, func() error {
		return ExtendReviewPlan(w.root, ReviewPlanExtension{PlanID: w.plan.PlanID, ExpectedRevision: w.plan.Revision + 1, AddMembers: []string{w.second}, Author: "coordinator", Basis: "stale"})
	})
	facts := reviewplan.AddMembers{Cas: true, Active: true, Free: true, Group: true, Language: true, NoUnplannedRuns: true}
	refines(t, w.root, ids, kernelSteps{
		plan: extendSteps(reviewplan.Extend{AddMembers: true}, reviewplan.Admit{Cas: true}, facts),
	}, false, func() error { return w.extend(ReviewPlanExtension{AddMembers: []string{w.second}}) })

	after := readOwedPlan(t, w.root, w.plan.PlanID)
	params := reviewobligation.Params{MaxBatch: 16}
	want := beforeMember
	for _, e := range []reviewobligation.Event{
		reviewobligation.Create{},
		reviewobligation.AddBatch{Member: false, Roles: noRoles},
		reviewobligation.Close{At: 0, Current: noRoles, Covered: noRoles},
		reviewobligation.AddBatch{Member: false, Roles: pmqaRoles},
	} {
		next, ok := reviewobligation.Step(want, e, params)
		if !ok {
			t.Fatalf("kernel refused %T: %+v", e, want)
		}
		want = next
	}
	got := project(true, after)
	if !sameObligation(want, got) {
		t.Fatalf("projection of the new member differs from the kernel step:\nkernel     %+v\nprojection %+v", want, got)
	}
	for _, query := range []func(reviewobligation.State, reviewobligation.Params) (bool, bool){reviewobligation.Assigned, reviewobligation.DoneReady, reviewobligation.CycleMissing, reviewobligation.CycleBatchOwed, reviewobligation.RecoveryOwed} {
		value, ok := query(got, params)
		if !ok {
			t.Fatalf("a query is undefined for the new member: %+v", got)
		}
		if value {
			t.Fatalf("a query holds for a member in no batch: %+v", got)
		}
	}
}

// A group with a checkpoint keeps reconciling while the plan grows: the mirror of
// the new member records the plan, a batch for it, and the seal, and the board
// check passes for every member.
func TestAddMembersKeepsTheCheckpointReconcilable(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "join-a"), recoveryMember(t, root, "join-b")}
	slices.Sort(ids)
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	p := ReviewPlan{Schema: 1, PlanID: "plan-join", Author: "coordinator", Basis: "fixture", CWD: "/repo", TaskIDs: ids[:1], Batches: []ReviewPlanBatch{
		{BatchID: "join-1", TaskIDs: ids[:1], Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "join-1", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if c.Members[ids[0]].Review == nil || c.Members[ids[1]].Review != nil {
		t.Fatalf("mirrors before the addition: %+v", c.Members)
	}
	extend := func(x ReviewPlanExtension) {
		t.Helper()
		stored := readOwedPlan(t, root, p.PlanID)
		x.PlanID, x.ExpectedRevision, x.Author, x.Basis = p.PlanID, stored.Revision, "coordinator", "the second member started"
		if err := ExtendReviewPlan(root, x); err != nil {
			t.Fatal(err)
		}
		c = coordinatorReconcile(t, root, c)
		if blocked := c.BlockedMembers(); len(blocked) != 0 {
			t.Fatalf("blocked after %+v: %v", x, blocked)
		}
	}
	extend(ReviewPlanExtension{AddMembers: ids[1:]})
	for _, id := range ids {
		if review := c.Members[id].Review; review == nil || review.PlanID != p.PlanID {
			t.Fatalf("mirror of %s: %+v", id, review)
		}
	}
	second := ReviewPlanBatch{BatchID: "join-2", PreviousBatchID: "join-1", TaskIDs: ids[1:], Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()}
	extend(ReviewPlanExtension{Batch: &second})
	if _, err := deadEndClose(t, root, "join-2", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	extend(ReviewPlanExtension{Seal: true})
	if review := c.Members[ids[1]].Review; !slices.Equal(review.Batches, []string{"join-2"}) {
		t.Fatalf("mirror of the new member: %+v", review)
	}
	if code, out, errs, err := CheckBoard(root, ids, false); err != nil || code != 0 {
		t.Fatalf("check: %d %s %v %v", code, out, errs, err)
	}
}

// A resolution of an unplanned batch covers the plan members of its time. A card
// with a run in that batch is not excused by it and cannot join afterwards.
func TestAddMembersRefusesARunInAResolvedBatch(t *testing.T) {
	useEnglish(t)
	s := defaultStrayBoard(t, "resolved-join")
	other := gateCard(t, s.root, "resolved-join-other")
	run := s.strayRun(t, strayRunInput(append(slices.Clone(s.ids), other), "stray-shared", "stray-shared-run"), archiveRequirements(), emptyFindings())
	s.mustResolve(t)
	x := s.request(t)
	x.ResolveBatch.BatchID = run.BatchID
	if err := s.resolve(x); err != nil {
		t.Fatal(err)
	}
	before := s.revision(t)
	err := ExtendReviewPlan(s.root, ReviewPlanExtension{PlanID: s.plan, ExpectedRevision: before, AddMembers: []string{other}, Author: "coordinator", Basis: "fixture"})
	if err == nil || !strings.Contains(err.Error(), other) || !strings.Contains(err.Error(), run.BatchID) {
		t.Fatalf("error: %v", err)
	}
	if got := s.revision(t); got != before {
		t.Fatalf("revision moved to %d", got)
	}
}
