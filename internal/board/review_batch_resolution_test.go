package board

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
)

const strayBatchID = "stray-b2"

// The commits of the blocked card: b1 ends at second, the stray batch reviews second
// to third, and the planned b3 reviews second to fourth, where the fix landed.
var (
	strayFirst  = strings.Repeat("a", 40)
	straySecond = strings.Repeat("b", 40)
	strayThird  = strings.Repeat("c", 40)
	strayFourth = strings.Repeat("d", 40)
)

type strayBoard struct {
	root    string
	ids     []string
	plan    string
	run     ReviewRun
	finding ReviewFinding
}

func strayFinding(tier string) ReviewFindings {
	f := emptyFindings()
	item := ReviewFinding{ID: "PMQA-001", Tier: tier, Text: "stale document", Evidence: "docs/x.md:1"}
	if mustFix(tier) {
		f.Findings = []ReviewFinding{item}
	} else {
		f.NonBlocking = []ReviewFinding{item}
	}
	return f
}

// withoutPlanPointer runs fn the way v1.0.0 did: requireNoStrayBatch sees no plan,
// and no public path can write a run outside the plan.
func withoutPlanPointer(t *testing.T, root string, ids []string, fn func()) {
	t.Helper()
	for _, id := range ids {
		path := control(root, "groups", reviewControlGroup, "task-plans", id+".json")
		if err := os.Rename(path, path+".moved"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Rename(path+".moved", path); err != nil {
				t.Fatal(err)
			}
		}()
	}
	fn()
}

func strayRunInput(ids []string, batch, run string) ReviewInput {
	in := archiveInput(ids, run, "PMQA")
	in.BatchID, in.Base, in.Commit = batch, straySecond, strayThird
	return in
}

func (s *strayBoard) strayRun(t *testing.T, in ReviewInput, requirements map[string]string, f ReviewFindings) ReviewRun {
	t.Helper()
	var published ReviewRun
	withoutPlanPointer(t, s.root, s.ids, func() {
		run, _, err := PrepareReviewRun(s.root, in, requirements, nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		if run, err = FinalizeReviewRun(s.root, run, structuredReport(f)); err != nil {
			t.Fatal(err)
		}
		published = publishRun(t, s.root, run.RunID)
	})
	return published
}

func (s *strayBoard) closeClean(t *testing.T, batch string, base, commit string, ids []string) {
	t.Helper()
	in := archiveInput(ids, batch+"-run", "PMQA")
	in.BatchID, in.Base, in.Commit = batch, base, commit
	run := gateRun(t, s.root, in, emptyFindings())
	assignGate(t, s.root, run, map[string][]string{})
	if _, err := deadEndClose(t, s.root, batch, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil); err != nil {
		t.Fatal(err)
	}
}

// newStrayBoard builds the blocked card: b1 closed, the stray batch has one run with
// one finding, and b3 is planned with the same base as the stray batch.
func newStrayBoard(t *testing.T, slug string, stray, cover map[string]string, coverIDs func([]string) []string, tier string, members ...string) *strayBoard {
	t.Helper()
	s := &strayBoard{root: tempBoard(t)}
	for _, member := range members {
		s.ids = append(s.ids, gateCard(t, s.root, slug+"-"+member))
	}
	slices.Sort(s.ids)
	s.plan = "plan-" + slug
	p := ReviewPlan{Schema: 1, PlanID: s.plan, Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: s.ids, Batches: []ReviewPlanBatch{{BatchID: "b1", TaskIDs: s.ids, Base: strayFirst, TargetCommit: straySecond, Requirements: archiveRequirements()}}}
	if err := CreateReviewPlan(s.root, p); err != nil {
		t.Fatal(err)
	}
	s.closeClean(t, "b1", strayFirst, straySecond, s.ids)
	findings := strayFinding(tier)
	s.finding = findings.all()[0]
	s.run = s.strayRun(t, strayRunInput(s.ids, strayBatchID, "stray-run"), stray, findings)
	b3 := ReviewPlanBatch{BatchID: "b3", PreviousBatchID: "b1", TaskIDs: coverIDs(s.ids), Base: straySecond, TargetCommit: strayFourth, Requirements: cover}
	if err := ExtendReviewPlan(s.root, ReviewPlanExtension{PlanID: s.plan, ExpectedRevision: s.revision(t), Batch: &b3, Author: "coordinator", Basis: "next batch"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func sameMembers(ids []string) []string { return ids }

func defaultStrayBoard(t *testing.T, slug string) *strayBoard {
	t.Helper()
	s := newStrayBoard(t, slug, archiveRequirements(), archiveRequirements(), sameMembers, "medium", "one")
	s.closeClean(t, "b3", straySecond, strayFourth, s.ids)
	return s
}

func (s *strayBoard) revision(t *testing.T) uint64 {
	t.Helper()
	p, err := ReadReviewPlan(s.root, s.plan)
	if err != nil {
		t.Fatal(err)
	}
	return p.Revision
}

func (s *strayBoard) fixed() ReviewResolvedFinding {
	return ReviewResolvedFinding{RunID: s.run.RunID, FindingID: s.finding.ID, Status: "fixed", Basis: "fix commit updates the document", FixCommit: strayFourth, Verification: "b3 re-reviewed the fix"}
}

func (s *strayBoard) request(t *testing.T, findings ...ReviewResolvedFinding) ReviewPlanExtension {
	t.Helper()
	return ReviewPlanExtension{PlanID: s.plan, ExpectedRevision: s.revision(t), Author: "claude", Basis: "b3 reviewed the range of the stray batch", ResolveBatch: &ReviewBatchResolutionRequest{BatchID: strayBatchID, CoveringBatchID: "b3", Findings: findings}}
}

func (s *strayBoard) resolve(x ReviewPlanExtension) error {
	edges, err := ReviewBatchResolutionEdges(s.root, x)
	if err != nil {
		return err
	}
	return ResolveReviewBatch(s.root, x, ReviewResolutionGit{CWD: "/repo", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
}

func (s *strayBoard) mustResolve(t *testing.T) {
	t.Helper()
	s.mustResolveWith(t, s.fixed())
}

func (s *strayBoard) mustResolveWith(t *testing.T, findings ...ReviewResolvedFinding) {
	t.Helper()
	if err := s.resolve(s.request(t, findings...)); err != nil {
		t.Fatal(err)
	}
}

func (s *strayBoard) seal(t *testing.T) {
	t.Helper()
	if err := ExtendReviewPlan(s.root, ReviewPlanExtension{PlanID: s.plan, ExpectedRevision: s.revision(t), Seal: true, Author: "coordinator", Basis: "seal"}); err != nil {
		t.Fatal(err)
	}
}

func (s *strayBoard) planFile(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(control(s.root, "groups", reviewControlGroup, "plans", s.plan+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (s *strayBoard) problems(id string) string {
	var text []string
	for _, problem := range CheckReviewGate(s.root, []string{id}) {
		text = append(text, problem.Message)
	}
	return strings.Join(text, "\n")
}

// strayEvidence collects every byte the resolution promises to leave alone.
func (s *strayBoard) strayEvidence(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	add := func(dir string) {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			files[path] = string(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add(control(s.root, "groups", reviewControlGroup, "runs", s.run.RunID))
	data, err := os.ReadFile(control(s.root, "groups", reviewControlGroup, "batches", strayBatchID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	files["batch"] = string(data)
	for _, id := range s.ids {
		card := transactionSnapshot(t, s.root, id)
		add(filepath.Join(card.Entry.Path, "reviews", s.run.RunID))
		indexes, err := ParseReviewIndexes(card.Text)
		if err != nil {
			t.Fatal(err)
		}
		for _, index := range indexes {
			if index.BatchID == strayBatchID {
				files["index "+id] = fmt.Sprint(index)
			}
		}
	}
	return files
}

func TestUnresolvedStrayBatchKeepsTheGateClosed(t *testing.T) {
	s := defaultStrayBoard(t, "unresolved")
	id := s.ids[0]
	if text := s.problems(id); !strings.Contains(text, "unplanned execution batch: "+strayBatchID) {
		t.Fatalf("check: %q", text)
	}
	s.seal(t)
	if err := deadEndDone(t, s.root, id); err == nil || !strings.Contains(err.Error(), "unplanned execution batch: "+strayBatchID) {
		t.Fatalf("move done: %v", err)
	}
}

func TestResolvedStrayBatchLetsTheCardFinish(t *testing.T) {
	s := defaultStrayBoard(t, "resolved")
	id := s.ids[0]
	before, err := ReadReviewPlan(s.root, s.plan)
	if err != nil {
		t.Fatal(err)
	}
	evidence := s.strayEvidence(t)
	s.mustResolve(t)
	if text := s.problems(id); strings.Contains(text, "unplanned") {
		t.Fatalf("check: %q", text)
	}
	progress, err := ReviewTaskProgress(s.root, id)
	if err != nil || len(progress.ResolvedBatches) != 1 {
		t.Fatalf("progress: %+v %v", progress, err)
	}
	if got := progress.ResolvedBatches[0].Request.Findings; !reflect.DeepEqual(got, []ReviewResolvedFinding{s.fixed()}) {
		t.Fatalf("finding result: %+v", got)
	}
	after, err := ReadReviewPlan(s.root, s.plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Batches, before.Batches) || after.Sealed != before.Sealed || after.Revision != before.Revision+1 {
		t.Fatalf("plan moved: %+v", after)
	}
	if got := s.strayEvidence(t); !reflect.DeepEqual(got, evidence) {
		t.Fatal("stray evidence changed")
	}
	s.seal(t)
	if err = deadEndDone(t, s.root, id); err != nil {
		t.Fatal(err)
	}
	if text := s.problems(id); text != "" {
		t.Fatalf("done card: %q", text)
	}
}

func TestResolutionRecordsHistoryAndBindsTheBatch(t *testing.T) {
	s := defaultStrayBoard(t, "history")
	s.mustResolve(t)
	revision := s.revision(t)
	data, err := os.ReadFile(control(s.root, "groups", reviewControlGroup, "plan-history", s.plan, fmt.Sprintf("%d.json", revision)))
	if err != nil {
		t.Fatal(err)
	}
	var history struct {
		Resolution ReviewBatchResolution `json:"resolution"`
	}
	if err = json.Unmarshal(data, &history); err != nil {
		t.Fatal(err)
	}
	r := history.Resolution
	if r.Request.BatchID != strayBatchID || r.Request.CoveringBatchID != "b3" || r.Author != "claude" || r.Basis == "" {
		t.Fatalf("history: %+v", r)
	}
	if r.BatchRevision != 1 || !slices.Equal(r.Runs, []ReviewResolvedRun{{RunID: s.run.RunID, ReportHash: s.run.Hashes["report.md"]}}) {
		t.Fatalf("binding: %+v", r)
	}
	if !reflect.DeepEqual(r.Git.Edges, []ReviewGitEdge{{straySecond, straySecond}, {strayThird, strayFourth}, {strayFourth, strayFourth}}) {
		t.Fatalf("edges: %+v", r.Git.Edges)
	}
	if err = s.resolve(s.request(t, s.fixed())); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_repeat", strayBatchID, s.plan)) {
		t.Fatalf("second resolution: %v", err)
	}
}

func TestResolutionOfASealedPlanKeepsTheSeal(t *testing.T) {
	s := defaultStrayBoard(t, "sealed")
	s.seal(t)
	s.mustResolve(t)
	p, err := ReadReviewPlan(s.root, s.plan)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Sealed {
		t.Fatal("seal lost")
	}
	if err = deadEndDone(t, s.root, s.ids[0]); err != nil {
		t.Fatal(err)
	}
}

func TestResolutionRequiresProvenance(t *testing.T) {
	s := defaultStrayBoard(t, "provenance")
	before := s.planFile(t)
	for name, mutate := range map[string]func(*ReviewPlanExtension){
		"author":   func(x *ReviewPlanExtension) { x.Author = " " },
		"basis":    func(x *ReviewPlanExtension) { x.Basis = "" },
		"revision": func(x *ReviewPlanExtension) { x.ExpectedRevision = 0 },
		"stale":    func(x *ReviewPlanExtension) { x.ExpectedRevision-- },
		"seal":     func(x *ReviewPlanExtension) { x.Seal = true },
	} {
		t.Run(name, func(t *testing.T) {
			x := s.request(t, s.fixed())
			mutate(&x)
			if err := s.resolve(x); err == nil {
				t.Fatal("accepted")
			}
			if err := ResolveReviewBatch(s.root, x, ReviewResolutionGit{CWD: "/repo", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err == nil {
				t.Fatal("accepted without the edges call")
			}
			if !slices.Equal(s.planFile(t), before) {
				t.Fatal("plan changed")
			}
		})
	}
}

func TestResolutionWithoutVerifiedEvidenceIsRefused(t *testing.T) {
	s := defaultStrayBoard(t, "evidence")
	x := s.request(t, s.fixed())
	if err := ExtendReviewPlan(s.root, x); err == nil {
		t.Fatal("plain extend-plan took a resolution")
	}
	edges, err := ReviewBatchResolutionEdges(s.root, x)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for name, git := range map[string]ReviewResolutionGit{
		"edges": {CWD: "/repo", VerifiedAt: now, Edges: edges[1:]},
		"cwd":   {CWD: "/other", VerifiedAt: now, Edges: edges},
		"time":  {CWD: "/repo", VerifiedAt: "soon", Edges: edges},
	} {
		if err = ResolveReviewBatch(s.root, x, git); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestResolutionRefusesFindingsOutsideItsRules(t *testing.T) {
	s := defaultStrayBoard(t, "findings")
	fixed := s.fixed()
	rejected := func(mutate func(*ReviewResolvedFinding)) ReviewResolvedFinding {
		f := ReviewResolvedFinding{RunID: fixed.RunID, FindingID: fixed.FindingID, Status: "rejected", Basis: "text"}
		mutate(&f)
		return f
	}
	edit := func(mutate func(*ReviewResolvedFinding)) ReviewResolvedFinding {
		f := fixed
		mutate(&f)
		return f
	}
	for name, findings := range map[string][]ReviewResolvedFinding{
		"missing":             {},
		"unknown":             {fixed, edit(func(f *ReviewResolvedFinding) { f.FindingID = "PMQA-404" })},
		"duplicate":           {fixed, fixed},
		"basis only":          {rejected(func(*ReviewResolvedFinding) {})},
		"blank decision":      {rejected(func(f *ReviewResolvedFinding) { f.Decision = "  " })},
		"no fix commit":       {edit(func(f *ReviewResolvedFinding) { f.FixCommit = "" })},
		"short fix commit":    {edit(func(f *ReviewResolvedFinding) { f.FixCommit = "9c80ecd" })},
		"no verification":     {edit(func(f *ReviewResolvedFinding) { f.Verification = "" })},
		"fix equals reviewed": {edit(func(f *ReviewResolvedFinding) { f.FixCommit = s.run.Commit })},
		"deferred must-fix":   {rejected(func(f *ReviewResolvedFinding) { f.Status, f.Decision = "deferred", "user-1" })},
		"unknown status":      {rejected(func(f *ReviewResolvedFinding) { f.Status, f.Decision = "waived", "user-1" })},
		"empty basis":         {rejected(func(f *ReviewResolvedFinding) { f.Basis, f.Decision = "", "user-1" })},
		"evidence on rejected": {rejected(func(f *ReviewResolvedFinding) {
			f.Decision, f.FixCommit = "user-1", strayFourth
		})},
	} {
		t.Run(name, func(t *testing.T) {
			if err := s.resolve(s.request(t, findings...)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	if err := s.resolve(s.request(t, rejected(func(f *ReviewResolvedFinding) { f.Decision = "user-1" }))); err != nil {
		t.Fatalf("rejected with a user decision: %v", err)
	}
}

func TestResolutionOfANonBlockingFinding(t *testing.T) {
	s := newStrayBoard(t, "low", archiveRequirements(), archiveRequirements(), sameMembers, "low", "one")
	s.closeClean(t, "b3", straySecond, strayFourth, s.ids)
	base := ReviewResolvedFinding{RunID: s.run.RunID, FindingID: s.finding.ID, Status: "accepted", Basis: "low impact"}
	if err := s.resolve(s.request(t, base)); err == nil {
		t.Fatal("unknown status accepted")
	}
	base.Status, base.Basis = "deferred", ""
	if err := s.resolve(s.request(t, base)); err == nil {
		t.Fatal("empty basis accepted")
	}
	base.Basis = "low impact"
	if err := s.resolve(s.request(t, base)); err != nil {
		t.Fatal(err)
	}
}

func TestResolutionRefusesAnUnrelatedOrOpenBatch(t *testing.T) {
	s := newStrayBoard(t, "cover", archiveRequirements(), archiveRequirements(), sameMembers, "medium", "one")
	x := s.request(t, s.fixed())
	if err := s.resolve(x); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_cover_open", "b3")) {
		t.Fatalf("open cover: %v", err)
	}
	x.ResolveBatch.CoveringBatchID = "foreign"
	if err := s.resolve(x); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_cover_unknown", "foreign", s.plan)) {
		t.Fatalf("foreign cover: %v", err)
	}
	x.ResolveBatch.BatchID = "b1"
	if err := s.resolve(x); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_planned", "b1", s.plan)) {
		t.Fatalf("planned batch: %v", err)
	}
	x.ResolveBatch.BatchID = "absent"
	if err := s.resolve(x); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_missing", "absent")) {
		t.Fatalf("absent batch: %v", err)
	}
}

func TestResolutionRefusesAWeakerCover(t *testing.T) {
	stray := map[string]string{"PMQA": "required", "Security": "required"}
	s := newStrayBoard(t, "role", stray, archiveRequirements(), sameMembers, "medium", "one")
	s.closeClean(t, "b3", straySecond, strayFourth, s.ids)
	if err := s.resolve(s.request(t, s.fixed())); err == nil || !strings.Contains(err.Error(), "Security") {
		t.Fatalf("role: %v", err)
	}
	notApplicable := newStrayBoard(t, "not-applicable", archiveRequirements(), noReviewRequirements(), sameMembers, "medium", "one")
	if _, err := deadEndClose(t, notApplicable.root, "b3", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := notApplicable.resolve(notApplicable.request(t, notApplicable.fixed())); err == nil || !strings.Contains(err.Error(), "PMQA") {
		t.Fatalf("all N/A cover: %v", err)
	}
}

func TestResolutionNeedsEveryRemainingMemberInTheCover(t *testing.T) {
	first := func(ids []string) []string { return ids[:1] }
	s := newStrayBoard(t, "member", archiveRequirements(), archiveRequirements(), first, "medium", "one", "two")
	s.closeClean(t, "b3", straySecond, strayFourth, s.ids[:1])
	if err := s.resolve(s.request(t, s.fixed())); err == nil || !strings.Contains(err.Error(), s.ids[1]) {
		t.Fatalf("uncovered member: %v", err)
	}
	deadEndExit(t, s.root, s.ids[1], "trash")
	if err := s.resolve(s.request(t, s.fixed())); err != nil {
		t.Fatalf("excluded member still counted: %v", err)
	}
}

func TestResolutionRefusesABatchWithNoPlanMember(t *testing.T) {
	s := defaultStrayBoard(t, "no-member")
	other := gateCard(t, s.root, "foreign")
	in := strayRunInput([]string{other}, "foreign-batch", "foreign-run")
	run, _, err := PrepareReviewRun(s.root, in, archiveRequirements(), nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = FinalizeReviewRun(s.root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	publishRun(t, s.root, run.RunID)
	x := s.request(t)
	x.ResolveBatch.BatchID = "foreign-batch"
	if err = s.resolve(x); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_no_member", "foreign-batch", s.plan)) {
		t.Fatalf("foreign batch: %v", err)
	}
}

func TestResolutionRefusesABatchWithNoRun(t *testing.T) {
	s := defaultStrayBoard(t, "no-run")
	p, err := ReadReviewPlan(s.root, s.plan)
	if err != nil {
		t.Fatal(err)
	}
	empty := ReviewBatch{Schema: 1, BatchID: "empty", TaskIDs: s.ids, Base: straySecond, TargetCommit: strayThird, ReportLanguage: p.ReportLanguage, Requirements: archiveRequirements(), Revision: 1}
	if err = os.WriteFile(control(s.root, "groups", reviewControlGroup, "batches", "empty.json"), []byte(reviewJSON(empty)), 0o600); err != nil {
		t.Fatal(err)
	}
	x := s.request(t)
	x.ResolveBatch.BatchID = "empty"
	if err = s.resolve(x); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_no_run", "empty")) {
		t.Fatalf("empty batch: %v", err)
	}
}

func TestResolutionWaitsForInterpretationAndPublication(t *testing.T) {
	s := defaultStrayBoard(t, "pending")
	withoutPlanPointer(t, s.root, s.ids, func() {
		in := strayRunInput(s.ids, strayBatchID, "stray-readable")
		in.FindingsSchema = FindingsSchemaReceiver
		run, _, err := PrepareReviewRun(s.root, in, nil, nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		if run, err = FinalizeReviewRun(s.root, run, []byte("readable report without the fence\n")); err != nil {
			t.Fatal(err)
		}
		publishRun(t, s.root, run.RunID)
	})
	if err := s.resolve(s.request(t, s.fixed())); err == nil || !strings.Contains(err.Error(), "kander review interpret") {
		t.Fatalf("pending interpretation: %v", err)
	}

	u := defaultStrayBoard(t, "unpublished")
	withoutPlanPointer(t, u.root, u.ids, func() {
		run, _, err := PrepareReviewRun(u.root, strayRunInput(u.ids, strayBatchID, "stray-unpublished"), nil, nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		if _, err = FinalizeReviewRun(u.root, run, structuredReport(emptyFindings())); err != nil {
			t.Fatal(err)
		}
	})
	if err := u.resolve(u.request(t, u.fixed())); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_publication", "stray-unpublished")) {
		t.Fatalf("unpublished run: %v", err)
	}
}

func TestResolutionIsVoidAfterTheBatchChanges(t *testing.T) {
	addRun := func(t *testing.T, s *strayBoard) {
		s.strayRun(t, strayRunInput(s.ids, strayBatchID, "stray-late"), nil, emptyFindings())
	}
	bumpRevision := func(t *testing.T, s *strayBoard) {
		path := control(s.root, "groups", reviewControlGroup, "batches", strayBatchID+".json")
		var b ReviewBatch
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = DecodeReviewJSON(data, &b); err != nil {
			t.Fatal(err)
		}
		b.Revision++
		if err = os.WriteFile(path, []byte(reviewJSON(b)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, change := range map[string]func(*testing.T, *strayBoard){"new run": addRun, "revision": bumpRevision} {
		t.Run(name, func(t *testing.T) {
			s := defaultStrayBoard(t, "void")
			s.mustResolve(t)
			s.seal(t)
			change(t, s)
			id := s.ids[0]
			if text := s.problems(id); !strings.Contains(text, strayBatchID) {
				t.Fatalf("check: %q", text)
			}
			if err := deadEndDone(t, s.root, id); err == nil || !strings.Contains(err.Error(), strayBatchID) {
				t.Fatalf("move done: %v", err)
			}
		})
	}
}

func TestSecondStrayBatchStaysBlocked(t *testing.T) {
	s := defaultStrayBoard(t, "second")
	s.strayRun(t, strayRunInput(s.ids, "stray-b4", "stray-second"), archiveRequirements(), emptyFindings())
	s.mustResolve(t)
	s.seal(t)
	if err := deadEndDone(t, s.root, s.ids[0]); err == nil || !strings.Contains(err.Error(), "unplanned execution batch: stray-b4") {
		t.Fatalf("second stray: %v", err)
	}
}

func TestPlanInputCannotCarryAResolution(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "forged")
	p := ReviewPlan{Schema: 1, PlanID: "forged", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: strayFirst, TargetCommit: straySecond, Requirements: noReviewRequirements()}}, ResolvedBatches: []ReviewBatchResolution{{Request: ReviewBatchResolutionRequest{BatchID: "stray"}}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadReviewPlan(root, "forged")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.ResolvedBatches) != 0 {
		t.Fatalf("forged resolution stored: %+v", stored.ResolvedBatches)
	}
}

// The gate holds no lock on a batch member outside the plan, so a resolved batch
// must validate from control records alone.
func TestResolvedBatchWithAMemberOutsideThePlan(t *testing.T) {
	s := defaultStrayBoard(t, "outside")
	other := gateCard(t, s.root, "outside-other")
	run := s.strayRun(t, strayRunInput(append(slices.Clone(s.ids), other), "stray-shared", "stray-shared-run"), archiveRequirements(), emptyFindings())
	s.mustResolve(t)
	x := s.request(t)
	x.ResolveBatch.BatchID = run.BatchID
	if err := s.resolve(x); err != nil {
		t.Fatal(err)
	}
	if text := s.problems(s.ids[0]); text != "" {
		t.Fatalf("check: %q", text)
	}
}

// review progress fails on the stray batch, so a reclaim can only rebind after it.
func TestResolutionComesBeforeTheRebindOfAReclaimedCard(t *testing.T) {
	s := defaultStrayBoard(t, "reclaimed")
	id := s.ids[0]
	deadEndReclaim(t, s.root, id)
	if _, err := ReviewTaskProgress(s.root, id); err == nil || !strings.Contains(err.Error(), "unplanned execution batch") {
		t.Fatalf("progress: %v", err)
	}
	s.mustResolve(t)
	deadEndRebind(t, s.root, id)
}

func TestArchiveCleanupRemovesTheResolutionWithThePlan(t *testing.T) {
	s := defaultStrayBoard(t, "pruned")
	id := s.ids[0]
	s.mustResolve(t)
	s.seal(t)
	if err := deadEndDone(t, s.root, id); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, s.root, id, "archived")
	for _, path := range []string{control(s.root, "groups", reviewControlGroup, "plans", s.plan+".json"), control(s.root, "groups", reviewControlGroup, "plan-history", s.plan)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s kept: %v", path, err)
		}
	}
	if text := s.problems(id); text != "" {
		t.Fatalf("archived card: %q", text)
	}
}

// failedRun ends a stray run the way a runtime cleanup failure does: the reviewer
// wrote its report, and the run is failed all the same.
func (s *strayBoard) failedRun(t *testing.T, run string, report []byte) {
	t.Helper()
	withoutPlanPointer(t, s.root, s.ids, func() {
		failed, _, err := PrepareReviewRun(s.root, strayRunInput(s.ids, strayBatchID, run), nil, nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		failed.LaunchStatus, failed.ExecutionStatus, failed.ExitCode, failed.FailureReason = "started", "failed", 2, "runtime cleanup failed"
		if _, err = FinalizeReviewRun(s.root, failed, report); err != nil {
			t.Fatal(err)
		}
		publishRun(t, s.root, run)
	})
}

func TestResolutionCountsTheReportOfAFailedRun(t *testing.T) {
	s := defaultStrayBoard(t, "failed-report")
	found := ReviewFindings{Findings: []ReviewFinding{{ID: "PMQA-002", Tier: "high", Text: "real defect", Evidence: "x.go:1"}}, NonBlocking: []ReviewFinding{}}
	s.failedRun(t, "stray-failed", structuredReport(found))
	if err := s.resolve(s.request(t, s.fixed())); err == nil || !strings.Contains(err.Error(), "stray-failed/PMQA-002") {
		t.Fatalf("finding of a failed run skipped: %v", err)
	}
	second := s.fixed()
	second.RunID, second.FindingID = "stray-failed", "PMQA-002"
	second.FixCommit, second.Verification = "", ""
	if err := s.resolve(s.request(t, s.fixed(), second)); err == nil {
		t.Fatal("must-fix of a failed run closed by basis text")
	}
	second.FixCommit, second.Verification = strayFourth, "b3 reviewed the fix"
	s.mustResolveWith(t, s.fixed(), second)
}

func TestResolutionRefusesAFailedRunItCannotRead(t *testing.T) {
	s := defaultStrayBoard(t, "failed-unreadable")
	s.failedRun(t, "stray-unreadable", []byte("half a report\n"))
	if err := s.resolve(s.request(t, s.fixed())); err == nil || !strings.Contains(err.Error(), config.Text("board.review_resolve_failed_report", "stray-unreadable")) {
		t.Fatalf("unreadable report: %v", err)
	}
}

func TestResolutionAcceptsAFailedRunWithNoReport(t *testing.T) {
	s := defaultStrayBoard(t, "failed-silent")
	s.failedRun(t, "stray-silent", nil)
	s.mustResolve(t)
	p, err := ReadReviewPlan(s.root, s.plan)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.ResolvedBatches[0].Runs, ReviewResolvedRun{RunID: "stray-silent"}) {
		t.Fatalf("failed run not bound: %+v", p.ResolvedBatches[0].Runs)
	}
}

// The trashed member keeps the plan, so the plan still revalidates the batch of the
// archived member, which alone would pass for settled.
func TestArchiveCleanupKeepsABatchAKeptPlanResolved(t *testing.T) {
	s := newStrayBoard(t, "kept", archiveRequirements(), archiveRequirements(), sameMembers, "medium", "one", "two")
	s.closeClean(t, "b3", straySecond, strayFourth, s.ids)
	archived, trashed := s.ids[0], s.ids[1]
	s.strayRun(t, strayRunInput([]string{archived}, "stray-solo", "solo-run"), archiveRequirements(), emptyFindings())
	s.mustResolve(t)
	solo := s.request(t)
	solo.ResolveBatch.BatchID = "stray-solo"
	if err := s.resolve(solo); err != nil {
		t.Fatal(err)
	}
	s.seal(t)
	deadEndExit(t, s.root, trashed, "trash")
	if err := deadEndDone(t, s.root, archived); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, s.root, archived, "archived")
	for _, path := range []string{control(s.root, "groups", reviewControlGroup, "batches", "stray-solo.json"), control(s.root, "groups", reviewControlGroup, "runs", "solo-run", "run.json"), control(s.root, "groups", reviewControlGroup, "plans", s.plan+".json")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if text := s.problems(archived); text != "" {
		t.Fatalf("archived card: %q", text)
	}
}
