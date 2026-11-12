//go:build unix

package review

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

const mechanicalTaskID = "20260907-archive-test-task"

type mechanicalFixture struct {
	h       *reviewHarness
	root    string
	items   []board.ReviewFinding
	records map[string]string
}

func newMechanicalFixture(t *testing.T, files map[string]string, items ...board.ReviewFinding) *mechanicalFixture {
	t.Helper()
	return newMechanicalFixtureWithRequirements(t, files, map[string]string{"PMQA": "required", "Security": "N/A: fixture"}, items...)
}

func newMechanicalFixtureWithRequirements(t *testing.T, files map[string]string, requirements map[string]string, items ...board.ReviewFinding) *mechanicalFixture {
	t.Helper()
	h := newCodexHarness(t)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		h.head = commitFile(t, h.repo, name, files[name], "seed fixture")
	}
	h, root, _ := archiveHarnessFor(t, h, "codex")
	s, err := board.ReadSnapshot(root, mechanicalTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.WithTransaction(root, board.LockScope{Tasks: []string{mechanicalTaskID}}, func(tx *board.Transaction) error {
		return tx.Put(mechanicalTaskID, "spec.md", s.Text+"\n- OWNER: codex\n")
	}); err != nil {
		t.Fatal(err)
	}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan", Author: "main", Basis: "mechanical closure regression", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{mechanicalTaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{mechanicalTaskID}, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	f := &mechanicalFixture{h: h, root: root, items: items, records: map[string]string{}}
	f.run(t, "stable", "", h.head, items)
	return f
}

func (f *mechanicalFixture) run(t *testing.T, run, previous, commit string, items []board.ReviewFinding) {
	t.Helper()
	f.runRole(t, "PMQA", run, previous, commit, items)
}

func (f *mechanicalFixture) runRole(t *testing.T, role, run, previous, commit string, items []board.ReviewFinding) {
	t.Helper()
	report := board.ReviewFindings{Findings: []board.ReviewFinding{}, NonBlocking: []board.ReviewFinding{}}
	assignment := board.ReviewAssignment{RunID: run, BatchID: "batch", Author: "main", Basis: "fixture attribution", Items: map[string][]string{}}
	for _, item := range items {
		if item.Tier == "low" {
			report.NonBlocking = append(report.NonBlocking, item)
		} else {
			report.Findings = append(report.Findings, item)
		}
		assignment.Items[item.ID] = []string{mechanicalTaskID}
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CODEX_REPORT", "```kander-findings\n"+string(data)+"\n```")
	args := []string{"codex", "--task", mechanicalTaskID, "--run-id", run, "--batch-id", "batch"}
	if previous != "" {
		args = append(args, "--previous-run-id", previous)
	}
	args = append(args, f.h.repo, f.h.base, commit, role, "Frozen mechanical closure goal")
	commandOK(t, args...)
	commandOK(t, "assign", f.h.repo, dispositionJSON(t, f.h, run+"-assignment", assignment))
}

func (f *mechanicalFixture) fixed(t *testing.T, index int, commit string, paths ...string) board.ReviewMechanicalAssessment {
	t.Helper()
	return f.fixedRun(t, "stable", index, commit, paths...)
}

func (f *mechanicalFixture) fixedRun(t *testing.T, runID string, index int, commit string, paths ...string) board.ReviewMechanicalAssessment {
	t.Helper()
	item := f.items[index]
	run, err := board.ReadReviewRun(f.root, runID)
	if err != nil {
		t.Fatal(err)
	}
	d := board.ReviewDisposition{RecordID: "fix-" + strings.ToLower(item.ID), RunID: runID, BatchID: "batch", FindingID: item.ID, TaskID: mechanicalTaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: item.Text, Status: "fixed", Basis: "verified fixture change", FixCommit: commit, Mechanical: item.Mechanical, Verification: "inspected fixture diff"}

	d.PreviousRecordID = f.records[runID+"/"+item.ID]
	if d.PreviousRecordID != "" {
		d.RecordID += "-updated"
	}
	s, err := board.ReadSnapshot(f.root, mechanicalTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.SubmitReviewDisposition(f.root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
	f.records[runID+"/"+item.ID] = d.RecordID
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--full-index", "--no-color", run.Commit, commit, "--"}
	slices.Sort(paths)
	for _, name := range paths {
		args = append(args, ":(literal)"+name)
	}
	patch, _, code, err := gitCommand(args, f.h.repo, "")
	if err != nil || code != 0 {
		t.Fatalf("diff: %d %v", code, err)
	}
	return board.ReviewMechanicalAssessment{RecordID: d.RecordID, Finding: board.FindingRef{RunID: d.RunID, FindingID: d.FindingID}, TaskID: d.TaskID, Author: "main", Category: d.Mechanical, ReportedCategory: item.Mechanical, ReportHash: d.ReportHash, FixCommit: commit, Basis: "independent classification", Facts: "verified category-specific fixture facts", Paths: paths, DiffHash: board.ReviewDigest([]byte(patch))}
}

func (f *mechanicalFixture) advance(t *testing.T, target string, commits ...string) {
	t.Helper()
	v, err := board.ReadReviewBatchView(f.root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	deliveries := map[string]string{}
	for _, commit := range commits {
		deliveries[commit] = mechanicalTaskID
	}
	a := board.ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: v.Batch.Revision, Advance: board.ReviewAdvance{PreviousTarget: v.Batch.TargetCommit, Target: target, Reason: "fixture fixes", Deliveries: deliveries}}
	commandOK(t, "advance", f.h.repo, dispositionJSON(t, f.h, "advance", a))
}

func (f *mechanicalFixture) request(t *testing.T, run, passed string, assessments ...board.ReviewMechanicalAssessment) (board.ReviewBatchView, board.ReviewCloseRequest) {
	t.Helper()
	v, err := board.ReadReviewBatchView(f.root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	return v, board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "main", Roles: map[string]board.ReviewRoleConclusion{"PMQA": {RunID: run, PassedAt: passed, Basis: "independently verified"}}, Mechanical: assessments}
}

func (f *mechanicalFixture) reject(t *testing.T, r board.ReviewCloseRequest, want ...string) {
	t.Helper()
	before, err := board.ReadReviewBatchView(f.root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr := captureRun(t, []string{"close", f.h.repo, dispositionJSON(t, f.h, "close", r)})
	if code == 0 {
		t.Fatal("unreviewed changes accepted")
	}
	for _, s := range want {
		if !strings.Contains(stderr, s) {
			t.Errorf("missing %q in %s", s, stderr)
		}
	}
	progress, err := board.ReviewTaskProgress(f.root, mechanicalTaskID)
	if err != nil || progress.Status == "closed" {
		t.Fatalf("rejected close published closure: %+v %v", progress, err)
	}
	after, err := board.ReadReviewBatchView(f.root, "batch")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected close mutated batch: %v", err)
	}
}

func mechanicalItem(id, category string) board.ReviewFinding {
	return board.ReviewFinding{ID: id, Tier: "medium", Text: "Remove obsolete fixture content " + id, Evidence: "fixture.txt:1", Mechanical: category}
}

func TestMechanicalClosureScopeGate(t *testing.T) {
	for _, tc := range []struct {
		name, category, before, after string
		extra, late, success          bool
	}{
		{name: "unreported files", category: "documentation", before: "old\n", after: "new\n", extra: true},
		{name: "unreported after fix commit", category: "documentation", before: "old\n", after: "new\n", extra: true, late: true},
		{name: "dead code additions", category: "dead-code", before: "old\n", after: "new\n"},
		{name: "redundant test additions", category: "redundant-test", before: "old\n", after: "new\n"},
		{name: "dead code pure deletion", category: "dead-code", before: "keep\nremove\n", after: "keep\n", success: true},
		{name: "redundant test pure deletion", category: "redundant-test", before: "keep\nremove\n", after: "keep\n", success: true},
		{name: "alignment changes", category: "dead-code", before: "a    = 1\nlong = 2\n", after: "a = 1\n", success: true},
		{name: "binary change", category: "dead-code", before: "old\x00bytes", after: "new\x00bytes"},
		{name: "documentation additions", category: "documentation", before: "old\n", after: "new\n", success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMechanicalFixture(t, map[string]string{"fixture.txt": tc.before}, mechanicalItem("M-1", tc.category))
			target := commitFile(t, f.h.repo, "fixture.txt", tc.after, "fix fixture")
			commits := []string{target}
			fix := target
			if tc.extra {
				for _, name := range []string{"unreported.txt", "another.txt"} {
					target = commitFile(t, f.h.repo, name, "extra logic\n", "extra change")
					commits = append(commits, target)
				}
				if !tc.late {
					fix = target
				}
			}
			a := f.fixed(t, 0, fix, "fixture.txt")
			f.advance(t, target, commits...)
			_, r := f.request(t, "stable", target, a)
			if tc.success {
				commandOK(t, "close", f.h.repo, dispositionJSON(t, f.h, "close", r))
				return
			}
			want := []string{"fixture.txt", "incremental review", "--previous-run-id stable"}
			if tc.extra {
				want = []string{"unreported.txt", "another.txt", "mechanical", "paths", "incremental review"}
			}
			f.reject(t, r, want...)
		})
	}
}

func TestMechanicalClosureAllRoleFixesMustPrecedePassedAt(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonblocking file in scope", true: "nonblocking fix after passed_at"}[late], func(t *testing.T) {
			low := board.ReviewFinding{ID: "L-1", Tier: "low", Text: "Additional cleanup", Evidence: "extra.txt:1"}
			f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, mechanicalItem("M-1", "documentation"), low)
			fix := commitFile(t, f.h.repo, "fixture.txt", "new\n", "mechanical fix")
			extra := commitFile(t, f.h.repo, "extra.txt", "logic\n", "nonblocking fix")
			a := f.fixed(t, 0, fix, "fixture.txt")
			f.fixed(t, 1, extra, "extra.txt")
			f.advance(t, extra, fix, extra)
			passed := extra
			if late {
				passed = fix
			}
			_, r := f.request(t, "stable", passed, a)
			if late {
				f.reject(t, r, "stable", "L-1", extra, "passed_at", "incremental review")
			} else {
				f.reject(t, r, "extra.txt", "paths", "incremental review")
			}
		})
	}
}

func TestMechanicalClosureDocumentationExemptsOnlyDeclaredFiles(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrelated documentation", true: "shared documentation"}[shared], func(t *testing.T) {
			f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, mechanicalItem("M-1", "dead-code"), mechanicalItem("M-2", "documentation"))
			fix := commitFile(t, f.h.repo, "fixture.txt", "new\n", "mixed change")
			doc := commitFile(t, f.h.repo, "notes.md", "documentation\n", "document change")
			a := f.fixed(t, 0, fix, "fixture.txt")
			paths := []string{"notes.md"}
			if shared {
				paths = append(paths, "fixture.txt")
			}
			b := f.fixed(t, 1, doc, paths...)
			f.advance(t, doc, fix, doc)
			_, r := f.request(t, "stable", doc, a, b)
			if shared {
				commandOK(t, "close", f.h.repo, dispositionJSON(t, f.h, "close", r))
			} else {
				f.reject(t, r, "fixture.txt", "incremental review")
			}
		})
	}
}

func TestMechanicalClosureChecksAdditionsThroughPassedAt(t *testing.T) {
	f := newMechanicalFixture(t, map[string]string{"fixture.txt": "keep\nremove\n"}, mechanicalItem("M-1", "dead-code"))
	fix := commitFile(t, f.h.repo, "fixture.txt", "keep\n", "delete dead code")
	later := commitFile(t, f.h.repo, "fixture.txt", "keep\nnew logic\n", "later addition")
	a := f.fixed(t, 0, fix, "fixture.txt")
	f.advance(t, later, fix, later)
	_, r := f.request(t, "stable", later, a)
	f.reject(t, r, "fixture.txt", "incremental review")
}

func TestMechanicalClosureRejectedScopeCanBeReviewedIncrementally(t *testing.T) {
	f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, mechanicalItem("M-1", "dead-code"))
	fix := commitFile(t, f.h.repo, "fixture.txt", "new logic\n", "logic fix")
	a := f.fixed(t, 0, fix, "fixture.txt")
	f.advance(t, fix, fix)
	_, r := f.request(t, "stable", fix, a)
	f.reject(t, r, "fixture.txt")
	f.run(t, "second", "stable", fix, nil)
	_, r = f.request(t, "second", fix)
	commandOK(t, "close", f.h.repo, dispositionJSON(t, f.h, "close", r))
}

func TestMechanicalHistoricalVerificationKeepsLegacyScope(t *testing.T) {
	for _, category := range []string{"documentation", "dead-code"} {
		t.Run(category, func(t *testing.T) {
			f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, mechanicalItem("M-1", category))
			fix := commitFile(t, f.h.repo, "fixture.txt", "new logic\n", "legacy fix")
			extra := commitFile(t, f.h.repo, "unreported.txt", "unreported\n", "legacy extra")
			a := f.fixed(t, 0, fix, "fixture.txt")
			f.advance(t, extra, fix, extra)
			v, r := f.request(t, "stable", extra, a)
			edges, _, err := board.ReviewClosureEdges(v, r)
			if err != nil {
				t.Fatal(err)
			}
			claims, err := verifyMechanicalGitContext(t.Context(), f.h.repo, v, r)
			if err != nil {
				t.Fatal(err)
			}
			// Publish the legacy evidence through the board, bypassing only the new entry gate.
			c, err := board.CloseReviewBatch(f.root, r, board.ReviewGitEvidence{CWD: f.h.repo, Head: extra, Edges: edges, Mechanical: claims, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)})
			if err != nil {
				t.Fatal(err)
			}
			if err = VerifyClosedReviewGit(t.Context(), f.h.repo, c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMechanicalClosureCombinesDeclarationsAcrossRoles(t *testing.T) {
	f := newMechanicalFixtureWithRequirements(t, map[string]string{"fixture.txt": "keep\nremove\n"}, map[string]string{"PMQA": "required", "Security": "required"}, mechanicalItem("M-1", "dead-code"))
	f.items = append(f.items, mechanicalItem("S-1", "documentation"))
	f.runRole(t, "Security", "security", "", f.h.head, f.items[1:])
	fix := commitFile(t, f.h.repo, "fixture.txt", "keep\n", "dead code fix")
	doc := commitFile(t, f.h.repo, "notes.md", "documentation\n", "documentation fix")
	a := f.fixed(t, 0, fix, "fixture.txt")
	b := f.fixedRun(t, "security", 1, doc, "notes.md")
	f.advance(t, doc, fix, doc)
	_, r := f.request(t, "stable", doc, a, b)
	r.Roles["Security"] = board.ReviewRoleConclusion{RunID: "security", PassedAt: doc, Basis: "independent documentation verification"}
	commandOK(t, "close", f.h.repo, dispositionJSON(t, f.h, "close", r))
}

func TestMechanicalClosureIncludesFixedRecordsOfPriorRuns(t *testing.T) {
	low := board.ReviewFinding{ID: "L-1", Tier: "low", Text: "Earlier cleanup", Evidence: "extra.txt:1"}
	f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, low)

	run, err := board.ReadReviewRun(f.root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(f.root, mechanicalTaskID)
	if err != nil {
		t.Fatal(err)
	}
	deferred := board.ReviewDisposition{RecordID: "deferred", RunID: "stable", BatchID: "batch", FindingID: low.ID, TaskID: mechanicalTaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: low.Text, Status: "deferred", Basis: "separate nonblocking cleanup"}
	if err = board.SubmitReviewDisposition(f.root, deferred, snapshot.Revision); err != nil {
		t.Fatal(err)
	}
	f.records["stable/"+low.ID] = deferred.RecordID
	reviewed := commitFile(t, f.h.repo, "earlier.md", "earlier delivery\n", "earlier delivery")
	f.advance(t, reviewed, reviewed)
	f.items = append(f.items, mechanicalItem("M-1", "documentation"))
	f.run(t, "second", "stable", reviewed, f.items[1:])
	fix := commitFile(t, f.h.repo, "fixture.txt", "new\n", "mechanical fix")
	extra := commitFile(t, f.h.repo, "extra.txt", "other logic\n", "earlier nonblocking fix")
	a := f.fixedRun(t, "second", 1, fix, "fixture.txt")
	f.fixed(t, 0, extra, "extra.txt")
	f.advance(t, extra, fix, extra)
	_, r := f.request(t, "second", fix, a)
	f.reject(t, r, "stable", "L-1", extra, "passed_at", "--previous-run-id second")
}
