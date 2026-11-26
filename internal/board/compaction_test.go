package board

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	kfs "github.com/dualface/kander/v2/internal/fs"
)

// compactionMarker is in every file compaction removes, so a test can look
// for those bytes anywhere else on the board.
const compactionMarker = "compaction-marker-7f3a"

// settledCompactionRun publishes a run with every artifact in its own planned
// batch and closes the batch; the member cards stay where they are.
func settledCompactionRun(t *testing.T, root, runID string, ids ...string) ReviewRun {
	t.Helper()
	batch := runID + "-batch"
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + runID, Author: "coordinator", Basis: "compaction fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{{BatchID: batch, TaskIDs: ids, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: archiveRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	input := archiveInput(ids, runID, "PMQA")
	input.BatchID = batch
	run, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"prompt.txt", "review-contract.md", "evidence.txt"} {
		if err = StoreReviewArtifact(root, runID, name, []byte(name+" "+compactionMarker+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	staging, err := ReviewStaging(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"output.raw", "stdout.log", "error.log"} {
		if err = os.WriteFile(filepath.Join(staging, name), []byte(name+" "+compactionMarker+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	run = publishRun(t, root, runID)
	assignGate(t, root, run, map[string][]string{})
	v, err := ReadReviewBatchView(root, batch)
	if err != nil {
		t.Fatal(err)
	}
	r := ReviewCloseRequest{BatchID: batch, ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PMQA": passRole(run)}}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}); err != nil {
		t.Fatal(err)
	}
	return run
}

func relocateCards(t *testing.T, root, state string, ids ...string) {
	t.Helper()
	err := WithTransaction(root, LockScope{Tasks: ids, ExclusiveBoard: true}, func(tx *Transaction) error {
		for _, id := range ids {
			if err := tx.Relocate(id, state); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// compactionBoard is one done card with one settled run.
func compactionBoard(t *testing.T, slug string) (root, id string, run ReviewRun) {
	t.Helper()
	root = tempBoard(t)
	id = pruneCard(t, root, slug, "")
	run = settledCompactionRun(t, root, slug+"-run", id)
	relocateCards(t, root, "done", id)
	return root, id, run
}

func compactAt(t *testing.T, root string, now time.Time, dry bool) (CompactionReport, error) {
	t.Helper()
	return CompactReviewRuns(context.Background(), root, CompactionOptions{OlderThan: 7 * 24 * time.Hour, Now: now, DryRun: dry, KanderVersion: "test"})
}

func compact(t *testing.T, root string, dry bool) CompactionReport {
	t.Helper()
	report, err := compactAt(t, root, time.Now().Add(8*24*time.Hour), dry)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// boardFiles maps every regular file below root, lock files aside, to its digest.
func boardFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".kander/locks/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// compactedPath reports whether rel is in the deletion set of run.
func compactedPath(rel, run string) bool {
	control := ".kander/groups/" + reviewControlGroup + "/runs/" + run + "/"
	if rest, ok := strings.CutPrefix(rel, control); ok {
		dir, name, _ := strings.Cut(rest, "/")
		return dir == "inputs" || dir == "staging" || dir == "originals" && slices.Contains(compactedNames, name)
	}
	if i := strings.Index(rel, "/reviews/"+run+"/"); i >= 0 && !strings.HasPrefix(rel, ".kander/") {
		return slices.Contains(compactedNames, rel[i+len("/reviews/"+run+"/"):])
	}
	return false
}

// requireCompacted checks that exactly the deletion set of run went and that
// the only new file is its receipt.
func requireCompacted(t *testing.T, root string, before map[string]string, run string) int {
	t.Helper()
	after := boardFiles(t, root)
	removed := 0
	for rel, sum := range before {
		got, kept := after[rel]
		switch {
		case compactedPath(rel, run) && kept:
			t.Fatalf("%s was not removed", rel)
		case compactedPath(rel, run):
			removed++
		case !kept:
			t.Fatalf("%s was removed", rel)
		case got != sum:
			t.Fatalf("%s changed", rel)
		}
	}
	for rel := range after {
		if _, old := before[rel]; !old && rel != ".kander/groups/"+pruneControlGroup+"/compactions/"+run+".json" {
			t.Fatalf("unexpected new file %s", rel)
		}
	}
	return removed
}

func requireNoReviewProblems(t *testing.T, root string, ids ...string) {
	t.Helper()
	if problems, err := CheckReviewEvidence(root, ids); err != nil || len(problems) > 0 {
		t.Fatalf("check: %v %+v", err, problems)
	}
}

func TestCompactionRemovesOnlyTheCompactedSet(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-set")
	requireNoReviewProblems(t, root, id)
	code, stdout, stderr, err := CheckBoard(root, []string{id}, false)
	if err != nil {
		t.Fatal(err)
	}
	before := boardFiles(t, root)

	dry := compact(t, root, true)
	if !maps.Equal(before, boardFiles(t, root)) {
		t.Fatal("dry run changed the board")
	}
	if len(dry.Runs) != 1 || dry.Runs[0].RunID != run.RunID || dry.Files == 0 || dry.Bytes == 0 || dry.Err != nil {
		t.Fatalf("dry run: %+v", dry)
	}

	report := compact(t, root, false)
	if !slices.Equal(report.Runs, dry.Runs) || report.Files != dry.Files || report.Bytes != dry.Bytes || report.Err != nil || len(report.Warnings) > 0 {
		t.Fatalf("run %+v differs from dry run %+v", report, dry)
	}
	if removed := requireCompacted(t, root, before, run.RunID); removed != report.Files {
		t.Fatalf("removed %d files, reported %d", removed, report.Files)
	}
	// Nothing went through the journal, so the removed bytes cannot be there.
	for rel := range boardFiles(t, root) {
		if strings.HasPrefix(rel, ".kander/operations/") {
			if _, old := before[rel]; !old {
				t.Fatalf("compaction wrote journal record %s", rel)
			}
		}
	}

	requireNoReviewProblems(t, root, id)
	code2, stdout2, stderr2, err := CheckBoard(root, []string{id}, false)
	if err != nil || code2 != code || !slices.Equal(stderr2, stderr) {
		t.Fatalf("check changed: %d %v %v (was %d %v)", code2, stderr2, err, code, stderr)
	}
	marker := config.Text("board.check_review_compacted", id, run.RunID)
	if stdout2 != stdout+marker+"\n" {
		t.Fatalf("check output %q, want %q plus %q", stdout2, stdout, marker)
	}
	if again := compact(t, root, false); again.Files != 0 || len(again.Runs) != 0 || len(again.Skipped) != 0 {
		t.Fatalf("second compaction: %+v", again)
	}
}

func editRun(t *testing.T, root, runID string, edit func(*ReviewRun)) {
	t.Helper()
	path := reviewPath(root, "runs", runID, "run.json")
	var run ReviewRun
	if err := readControlJSON(root, path, &run); err != nil {
		t.Fatal(err)
	}
	edit(&run)
	if err := writeJSON(root, path, run, true); err != nil {
		t.Fatal(err)
	}
}

func requireSkipped(t *testing.T, report CompactionReport, run string, condition CompactionCondition) {
	t.Helper()
	if report.Files != 0 || len(report.Runs) != 0 {
		t.Fatalf("compaction removed files: %+v", report)
	}
	i := slices.IndexFunc(report.Skipped, func(s SkippedRun) bool { return s.RunID == run })
	if i < 0 || !slices.Contains(report.Skipped[i].Conditions, condition) || report.Skipped[i].Reason == "" {
		t.Fatalf("%s not skipped for condition %d: %+v", run, condition, report.Skipped)
	}
}

func TestCompactionKeepsRunsThatAreNotSettled(t *testing.T) {
	tests := []struct {
		name      string
		condition CompactionCondition
		arrange   func(t *testing.T) (root string, run string, now time.Time)
	}{
		{"not finalized", CompactionRunFinal, func(t *testing.T) (string, string, time.Time) {
			root, _, run := compactionBoard(t, "compact-phase")
			editRun(t, root, run.RunID, func(r *ReviewRun) { r.Phase = "running" })
			return root, run.RunID, time.Time{}
		}},
		{"not published", CompactionPublished, func(t *testing.T) (string, string, time.Time) {
			root, id, run := compactionBoard(t, "compact-unpublished")
			editRun(t, root, run.RunID, func(r *ReviewRun) { r.Published[id] = false })
			return root, run.RunID, time.Time{}
		}},
		{"batch open", CompactionBatchClosed, func(t *testing.T) (string, string, time.Time) {
			root, _, run := compactionBoard(t, "compact-open")
			if err := os.Remove(reviewPath(root, "closures", run.BatchID+".json")); err != nil {
				t.Fatal(err)
			}
			return root, run.RunID, time.Time{}
		}},
		{"member working", CompactionMembersFinal, func(t *testing.T) (string, string, time.Time) {
			root := tempBoard(t)
			id := pruneCard(t, root, "compact-working", "")
			return root, settledCompactionRun(t, root, "compact-working-run", id).RunID, time.Time{}
		}},
		{"group unsettled", CompactionGroupSettled, func(t *testing.T) (string, string, time.Time) {
			root := tempBoard(t)
			group := "20261127-compact-group"
			id := pruneCard(t, root, "compact-grouped", group)
			pruneCard(t, root, "compact-sibling", group)
			run := settledCompactionRun(t, root, "compact-grouped-run", id)
			relocateCards(t, root, "done", id)
			return root, run.RunID, time.Time{}
		}},
		{"inside retention", CompactionRetention, func(t *testing.T) (string, string, time.Time) {
			root, _, run := compactionBoard(t, "compact-recent")
			return root, run.RunID, time.Now()
		}},
		{"unsettled successor", CompactionSuccessor, func(t *testing.T) (string, string, time.Time) {
			root, id, run := compactionBoard(t, "compact-chain")
			successor := ReviewRun{Schema: 1, ReviewInput: ReviewInput{RunID: "compact-chain-next", BatchID: run.BatchID, PreviousRunID: run.RunID, TaskIDs: []string{id}}, Phase: "prepared"}
			if err := kfs.EnsurePrivateDirectory(root, reviewPath(root, "runs", successor.RunID), true); err != nil {
				t.Fatal(err)
			}
			if err := writeJSON(root, reviewPath(root, "runs", successor.RunID, "run.json"), successor, false); err != nil {
				t.Fatal(err)
			}
			return root, run.RunID, time.Time{}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, run, now := tt.arrange(t)
			if now.IsZero() {
				now = time.Now().Add(8 * 24 * time.Hour)
			}
			before := boardFiles(t, root)
			report, err := compactAt(t, root, now, false)
			if err != nil {
				t.Fatal(err)
			}
			requireSkipped(t, report, run, tt.condition)
			if !maps.Equal(before, boardFiles(t, root)) {
				t.Fatal("an unsettled run changed the board")
			}
		})
	}
}

func TestCompactionSkipsABusyRunWithoutWaiting(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-busy")
	release, err := LockReviewRun(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	report := compact(t, root, false)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("compaction waited %v", elapsed)
	}
	requireSkipped(t, report, run.RunID, CompactionRunLock)
	if err = release(); err != nil {
		t.Fatal(err)
	}
	if report = compact(t, root, false); len(report.Runs) != 1 || len(report.Skipped) != 0 {
		t.Fatalf("released run: %+v", report)
	}
	requireNoReviewProblems(t, root, id)
}

func TestCompactionResumesAfterEachInterruptedStep(t *testing.T) {
	for _, step := range []string{"receipts", "copies", "control"} {
		t.Run(step, func(t *testing.T) {
			root, id, run := compactionBoard(t, "compact-"+step)
			before := boardFiles(t, root)
			interrupted := errors.New("interrupted")
			compactionInterrupt = func(s string) error {
				if s == step {
					return interrupted
				}
				return nil
			}
			t.Cleanup(func() { compactionInterrupt = nil })
			first, err := compactAt(t, root, time.Now().Add(8*24*time.Hour), false)
			if !errors.Is(err, interrupted) || !errors.Is(first.Err, interrupted) {
				t.Fatalf("first round: %+v %v", first, err)
			}
			requireNoReviewProblems(t, root, id)
			compactionInterrupt = nil
			second := compact(t, root, false)
			if removed := requireCompacted(t, root, before, run.RunID); removed != first.Files+second.Files {
				t.Fatalf("removed %d files, reported %d + %d", removed, first.Files, second.Files)
			}
			requireNoReviewProblems(t, root, id)
			if third := compact(t, root, false); third.Files != 0 || len(third.Runs) != 0 {
				t.Fatalf("third round: %+v", third)
			}
		})
	}
}

func TestCompactionProcessesRunsInBatches(t *testing.T) {
	root := tempBoard(t)
	var ids []string
	for _, slug := range []string{"compact-batch-a", "compact-batch-b", "compact-batch-c"} {
		id := pruneCard(t, root, slug, "")
		ids = append(ids, id)
		settledCompactionRun(t, root, slug+"-run", id)
	}
	relocateCards(t, root, "done", ids...)
	old := compactionBatchSize
	compactionBatchSize = 1
	t.Cleanup(func() { compactionBatchSize = old })
	var rounds int
	compactionInterrupt = func(s string) error {
		if s == "control" {
			rounds++
		}
		return nil
	}
	t.Cleanup(func() { compactionInterrupt = nil })
	report := compact(t, root, false)
	if len(report.Runs) != 3 || rounds != 3 {
		t.Fatalf("rounds %d: %+v", rounds, report)
	}
	requireNoReviewProblems(t, root, ids...)
}

func TestCheckStillReportsDamageOfCompactedRuns(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, root, id string, run ReviewRun)
		want   string
	}{
		{"receipt names another run", func(t *testing.T, root, id string, run ReviewRun) {
			editReceipt(t, root, run.RunID, func(r *compactionReceipt) { r.RunID = "other-run" })
		}, "invalid compaction receipt"},
		{"receipt names other members", func(t *testing.T, root, id string, run ReviewRun) {
			editReceipt(t, root, run.RunID, func(r *compactionReceipt) { r.TaskIDs = []string{"20990101-other-task"} })
		}, "invalid compaction receipt"},
		{"unlisted original removed", func(t *testing.T, root, id string, run ReviewRun) {
			if err := os.Remove(reviewPath(root, "runs", run.RunID, "originals", "report.md")); err != nil {
				t.Fatal(err)
			}
		}, "report.md: hash mismatch"},
		{"listed original changed", func(t *testing.T, root, id string, run ReviewRun) {
			if err := os.WriteFile(reviewPath(root, "runs", run.RunID, "originals", "evidence.txt"), []byte("tampered\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "evidence.txt: hash mismatch"},
		{"listed card copy changed", func(t *testing.T, root, id string, run ReviewRun) {
			card := transactionSnapshot(t, root, id).Entry.Path
			if err := os.WriteFile(filepath.Join(card, "reviews", run.RunID, "error.log"), []byte("tampered\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "error.log: hash mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, id, run := compactionBoard(t, "compact-damage")
			compact(t, root, false)
			tt.damage(t, root, id, run)
			problems, err := CheckReviewEvidence(root, []string{id})
			if err != nil || !slices.ContainsFunc(problems, func(p Problem) bool { return strings.Contains(p.Message, tt.want) }) {
				t.Fatalf("want %q: %v %+v", tt.want, err, problems)
			}
		})
	}
}

func editReceipt(t *testing.T, root, runID string, edit func(*compactionReceipt)) {
	t.Helper()
	path := compactionReceiptPath(root, runID)
	var r compactionReceipt
	if err := readControlJSON(root, path, &r); err != nil {
		t.Fatal(err)
	}
	edit(&r)
	if err := writeJSON(root, path, r, true); err != nil {
		t.Fatal(err)
	}
}

func requireBoardCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestCompactedRunReadsAndRetries(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-read")
	if compacted, err := ReviewRunCompacted(root, run.RunID); err != nil || compacted {
		t.Fatalf("before: %v %v", compacted, err)
	}
	compact(t, root, false)
	if compacted, err := ReviewRunCompacted(root, run.RunID); err != nil || !compacted {
		t.Fatalf("after: %v %v", compacted, err)
	}
	_, err := ReadReviewOriginal(root, run.RunID, "evidence.txt")
	requireBoardCode(t, err, "board.review_compacted")
	_, err = ReadReviewInput(root, run.RunID, "task-context.md")
	requireBoardCode(t, err, "board.review_compacted")
	if report, err := ReadReviewOriginal(root, run.RunID, "report.md"); err != nil || len(report) == 0 {
		t.Fatalf("report: %q %v", report, err)
	}
	_, _, err = PrepareReviewRun(root, archiveInput([]string{id}, run.RunID, "PMQA"), nil, nil, archiveOriginals(), "test")
	requireBoardCode(t, err, "board.review_run_compacted")
	if err = ReviewPublicationComplete(root, run.RunID); err != nil {
		t.Fatalf("publication of a compacted run: %v", err)
	}
}

func TestArchivePruneRemovesCompactedRunAndReceipt(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-prune")
	compact(t, root, false)
	relocateCards(t, root, "archived", id)
	report, err := PruneArchived(root)
	if err != nil || report.Err != nil || !slices.Equal(report.Runs, []string{run.RunID}) {
		t.Fatalf("prune: %+v %v", report, err)
	}
	requireExists(t, compactionReceiptPath(root, run.RunID), false)
	requireExists(t, reviewPath(root, "runs", run.RunID), false)
	requirePrunedCheck(t, root, id)
}
