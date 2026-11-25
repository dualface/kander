package board

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const sealedRule = `"Carrying Conclusions Forward"`

// closedSealedPlan creates a sealed plan whose only batch is closed, the state
// in which an executor that sealed early can no longer add a batch.
func closedSealedPlan(t *testing.T, slug string) (string, ReviewPlan) {
	t.Helper()
	root := tempBoard(t)
	id := gateCard(t, root, slug)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "sealed-" + slug, Author: "coordinator", Basis: "sealed fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: noReviewRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadReviewPlan(root, p.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	return root, stored
}

func nextBatch(p ReviewPlan) *ReviewPlanBatch {
	return &ReviewPlanBatch{BatchID: "second", PreviousBatchID: "batch", TaskIDs: p.TaskIDs, Base: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40), Requirements: noReviewRequirements()}
}

// planBytes returns the raw bytes of the authoritative plan record and of every
// member copy, so a refusal that rewrote either, even to equal JSON, is caught.
func planBytes(t *testing.T, root string, p ReviewPlan) [][]byte {
	t.Helper()
	paths := []string{control(root, "groups", reviewControlGroup, "plans", p.PlanID+".json")}
	for _, id := range p.TaskIDs {
		paths = append(paths, filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "reviews", "plan.json"))
	}
	var out [][]byte
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, data)
	}
	return out
}

func requirePlanUnchanged(t *testing.T, root string, p ReviewPlan, before [][]byte) {
	t.Helper()
	after := planBytes(t, root, p)
	if !slices.EqualFunc(before, after, func(a, b []byte) bool { return string(a) == string(b) }) {
		t.Fatal("refused request rewrote the plan record or a member copy")
	}
}

func TestSealedPlanRefusalNamesTheSealAndTheWayOut(t *testing.T) {
	root, p := closedSealedPlan(t, "sealed-refusal")
	before := planBytes(t, root, p)
	err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Batch: nextBatch(p), Author: "codex", Basis: "later commits"})
	if err == nil {
		t.Fatal("sealed plan took a new batch")
	}
	want := t2("board.review_plan_sealed", p.PlanID)
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), sealedRule) {
		t.Fatalf("sealed refusal does not name the seal and the rule: %v", err)
	}
	if strings.Contains(err.Error(), "CAS") {
		t.Fatalf("sealed refusal still reads as a revision conflict: %v", err)
	}
	requirePlanUnchanged(t, root, p, before)
}

func TestStalePlanRevisionIsReportedWithBothRevisions(t *testing.T) {
	root, p := closedSealedPlan(t, "stale-revision")
	before := planBytes(t, root, p)
	stale := p.Revision - 1
	err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: stale, Batch: nextBatch(p), Author: "codex", Basis: "stale view"})
	if err == nil {
		t.Fatal("stale revision accepted")
	}
	want := t2("board.review_plan_revision_conflict", p.PlanID, stale, p.Revision)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("revision conflict does not name both revisions: %v", err)
	}
	// A stale view is reported before the seal, so the caller re-reads first.
	if strings.Contains(err.Error(), sealedRule) {
		t.Fatalf("revision conflict mentions the seal: %v", err)
	}
	requirePlanUnchanged(t, root, p, before)
}

func TestMissingPlanIsReportedAsMissing(t *testing.T) {
	root := tempBoard(t)
	err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: "absent", ExpectedRevision: 1, Seal: true, Author: "codex", Basis: "no plan"})
	if err == nil || !strings.Contains(err.Error(), "missing plan") {
		t.Fatalf("missing plan not reported as missing: %v", err)
	}
	if strings.Contains(err.Error(), sealedRule) || strings.Contains(err.Error(), "CAS") {
		t.Fatalf("missing plan reads as a seal or revision conflict: %v", err)
	}
	if _, err := os.Stat(control(root, "groups", reviewControlGroup, "plans", "absent.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("refused request created the missing plan: %v", err)
	}
}

func TestStaleResolutionReportsTheRevisionConflict(t *testing.T) {
	s := defaultStrayBoard(t, "stale-resolution")
	x := s.request(t, s.fixed())
	current := x.ExpectedRevision
	x.ExpectedRevision--
	p, err := ReadReviewPlan(s.root, s.plan)
	if err != nil {
		t.Fatal(err)
	}
	before := planBytes(t, s.root, p)
	err = s.resolve(x)
	if err == nil {
		t.Fatal("stale resolution accepted")
	}
	want := t2("board.review_plan_revision_conflict", s.plan, x.ExpectedRevision, current)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("stale resolution does not report the revision conflict: %v", err)
	}
	requirePlanUnchanged(t, s.root, p, before)
}

// t2 renders a board message the way the code under test does.
func t2(id string, args ...any) string { return t(id, args...) }
