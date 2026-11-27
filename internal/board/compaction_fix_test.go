package board

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

// groupedCompactionBoard is a done card of group whose run settled; sibling
// is a second member of the group, still in working.
func groupedCompactionBoard(t *testing.T, slug string) (root string, run ReviewRun, sibling string) {
	t.Helper()
	root = tempBoard(t)
	group := "20261127-" + slug + "-group"
	id := pruneCard(t, root, slug, group)
	sibling = pruneCard(t, root, slug+"-sibling", group)
	run = settledCompactionRun(t, root, slug+"-run", id)
	relocateCards(t, root, "done", id)
	return root, run, sibling
}

// A member whose card the scan cannot list, or whose group is ambiguous, keeps
// every run of the group it may belong to.
func TestCompactionKeepsRunsOfGroupsWithUnreadableMembership(t *testing.T) {
	tests := map[string]func(t *testing.T, root, sibling string){
		"sibling without spec.md": func(t *testing.T, root, sibling string) {
			if err := os.Remove(filepath.Join(transactionSnapshot(t, root, sibling).Entry.Path, "spec.md")); err != nil {
				t.Fatal(err)
			}
		},
		"sibling with two group lines": func(t *testing.T, root, sibling string) {
			path := filepath.Join(transactionSnapshot(t, root, sibling).Entry.Path, "spec.md")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(string(data), "- TASK_GROUP: ", "- TASK_GROUP: 20261127-other-group\n- TASK_GROUP: ", 1)
			if err = os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, damage := range tests {
		t.Run(name, func(t *testing.T) {
			root, run, sibling := groupedCompactionBoard(t, "compact-member")
			// The sibling leaves working, so only its membership can keep the run.
			relocateCards(t, root, "done", sibling)
			damage(t, root, sibling)
			before := boardFiles(t, root)
			requireSkipped(t, compact(t, root, false), run.RunID, CompactionGroupSettled)
			if !maps.Equal(before, boardFiles(t, root)) {
				t.Fatal("an unreadable membership let compaction change the board")
			}
		})
	}
}

// A compacted abandoned run has no member copy, and check still validates its receipt.
func TestCheckReportsAnInvalidReceiptOfAnAbandonedRun(t *testing.T) {
	edits := map[string]func(*compactionReceipt){
		"run_id":   func(r *compactionReceipt) { r.RunID = "other-run" },
		"task_ids": func(r *compactionReceipt) { r.TaskIDs = []string{"20990101-other-task"} },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			b := newAbandonBoard(t)
			run := b.unpublished(t, "compact-abandoned")
			if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID)); err != nil {
				t.Fatal(err)
			}
			relocateCards(t, b.root, "done", b.id)
			if report := compact(t, b.root, false); len(report.Runs) != 1 || report.Runs[0].RunID != run.RunID {
				t.Fatalf("abandoned run not compacted: %+v", report)
			}
			requireNoReviewProblems(t, b.root, b.id)
			editReceipt(t, b.root, run.RunID, edit)
			problems, err := CheckReviewEvidence(b.root, []string{b.id})
			if err != nil || !slices.ContainsFunc(problems, func(p Problem) bool { return strings.Contains(p.Message, "invalid compaction receipt") }) {
				t.Fatalf("invalid receipt not reported: %v %+v", err, problems)
			}
		})
	}
}

// A caller that cancels while a board writer holds the lock compacts nothing.
func TestCompactionCancelledWhileWaitingForTheBoardLock(t *testing.T) {
	root, _, _ := compactionBoard(t, "compact-cancel")
	before := boardFiles(t, root)
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := CompactReviewRuns(ctx, root, CompactionOptions{OlderThan: 7 * 24 * time.Hour, Now: time.Now().Add(8 * 24 * time.Hour)})
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	err = <-done
	if closeErr := locks.close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want a cancellation, got %v", err)
	}
	if !maps.Equal(before, boardFiles(t, root)) {
		t.Fatal("a cancelled compaction changed the board")
	}
}

// A round interrupted after the last file of a tree but before the tree itself
// leaves an empty directory; dry run and execution report that run alike.
func TestCompactionReportsAResumedEmptyDirectoryLikeItsDryRun(t *testing.T) {
	root, _, run := compactionBoard(t, "compact-empty")
	compact(t, root, false)
	inputs := reviewPath(root, "runs", run.RunID, "inputs")
	if err := os.Mkdir(inputs, 0o700); err != nil {
		t.Fatal(err)
	}
	dry := compact(t, root, true)
	if len(dry.Runs) != 1 || dry.Runs[0] != (CompactedRun{RunID: run.RunID}) {
		t.Fatalf("dry run: %+v", dry)
	}
	if report := compact(t, root, false); !slices.Equal(report.Runs, dry.Runs) || report.Files != 0 {
		t.Fatalf("run %+v differs from dry run %+v", report, dry)
	}
	requireExists(t, inputs, false)
	if again := compact(t, root, false); len(again.Runs) != 0 {
		t.Fatalf("third round: %+v", again)
	}
}

// A compaction receipt that archive pruning cannot remove keeps its run, and
// the next round removes both.
func TestArchivePruneKeepsARunWhoseReceiptCannotBeRemoved(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-stuck")
	compact(t, root, false)
	receipt := compactionReceiptPath(root, run.RunID)
	data, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	// A directory in place of the receipt makes its removal fail.
	if err = os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(receipt, 0o700); err != nil {
		t.Fatal(err)
	}
	relocateCards(t, root, "archived", id)
	if report, err := PruneArchived(root); err == nil || slices.Contains(report.Runs, run.RunID) {
		t.Fatalf("prune did not keep the run of the stuck receipt: %+v %v", report, err)
	}
	// The kept run still reads its batch, its closure and its plan.
	requireExists(t, reviewPath(root, "runs", run.RunID, "run.json"), true)
	for _, path := range []string{reviewPath(root, "batches", run.BatchID+".json"), reviewPath(root, "closures", run.BatchID+".json"), reviewPath(root, "plans", "plan-"+run.RunID+".json")} {
		requireExists(t, path, true)
	}
	if _, err := ReadReviewBatch(root, run.BatchID); err != nil {
		t.Fatalf("the batch of the kept run: %v", err)
	}
	if err = os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(receipt, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if report, err := PruneArchived(root); err != nil || !slices.Contains(report.Runs, run.RunID) {
		t.Fatalf("retry: %+v %v", report, err)
	}
	requireExists(t, receipt, false)
	requireExists(t, reviewPath(root, "runs", run.RunID), false)
	requirePrunedCheck(t, root, id)
}

// Any stored input other than the frozen contexts that a compaction removed
// with inputs/ reads as compacted; before compaction, or under an unsafe name,
// it stays unreadable as before.
func TestCompactedStoredInputReadsAsCompacted(t *testing.T) {
	root, _, run := compactionBoard(t, "compact-input")
	if err := os.WriteFile(reviewPath(root, "runs", run.RunID, "inputs", "notes.txt"), []byte("historical input\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unsafe := []string{"", "../run.json", ".hidden", "a/b", `a\b`, "name.", "c:d"}
	for _, name := range append([]string{"evidence.txt", "notes.txt", "never-written.txt"}, unsafe...) {
		_, err := ReadReviewInput(root, run.RunID, name)
		requireBoardCode(t, err, "board.review_evidence_invalid")
	}
	compact(t, root, false)
	for _, name := range []string{"evidence.txt", "notes.txt", "never-written.txt"} {
		_, err := ReadReviewInput(root, run.RunID, name)
		requireBoardCode(t, err, "board.review_compacted")
	}
	for _, name := range unsafe {
		_, err := ReadReviewInput(root, run.RunID, name)
		requireBoardCode(t, err, "board.review_evidence_invalid")
	}
}

// A run whose only removal fails makes no progress: it is skipped, not counted.
func TestCompactionCountsOnlyRunsWithACompletedRemoval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block removal on Windows")
	}
	root, _, run := compactionBoard(t, "compact-stalled")
	compact(t, root, false)
	dir := reviewPath(root, "runs", run.RunID)
	if err := os.Mkdir(filepath.Join(dir, "inputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	report, err := compactAt(t, root, time.Now().Add(8*24*time.Hour), false)
	if err == nil || len(report.Runs) != 0 {
		t.Fatalf("a failed removal counted as progress: %+v %v", report, err)
	}
	if i := slices.IndexFunc(report.Skipped, func(s SkippedRun) bool { return s.RunID == run.RunID }); i < 0 || !slices.Equal(report.Skipped[i].Conditions, []CompactionCondition{CompactionRemoval}) {
		t.Fatalf("the run is not skipped for its removal: %+v", report.Skipped)
	}
}

// An entry of a protected directory that no producer writes is kept and reported.
func TestCompactionReportsUnknownEntriesItKeeps(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-unknown")
	stray := []string{
		reviewPath(root, "runs", run.RunID, "originals", "notes.txt"),
		filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "reviews", run.RunID, "notes.txt"),
	}
	for _, path := range stray {
		if err := os.WriteFile(path, []byte("operator note\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report := compact(t, root, false)
	if len(report.Runs) != 1 {
		t.Fatalf("run not compacted: %+v", report)
	}
	for _, path := range stray {
		requireExists(t, path, true)
		if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.HasPrefix(w, path+": ") }) {
			t.Fatalf("%s not reported: %v", path, report.Warnings)
		}
	}
}

// twoRoleCompactionBoard is a done card with two settled runs in one batch,
// one per required role.
func twoRoleCompactionBoard(t *testing.T, slug string) (root, id string, runs []ReviewRun) {
	t.Helper()
	root = tempBoard(t)
	id = pruneCard(t, root, slug, "")
	batch := slug + "-batch"
	requirements := map[string]string{"PMQA": "required", "Security": "required"}
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + slug, Author: "coordinator", Basis: "two-role compaction fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: batch, TaskIDs: []string{id}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: requirements}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	roles := map[string]ReviewRoleConclusion{}
	for _, role := range []string{"PMQA", "Security"} {
		input := archiveInput([]string{id}, slug+"-"+strings.ToLower(role), role)
		input.BatchID = batch
		run, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if err = StoreReviewArtifact(root, run.RunID, "evidence.txt", []byte(compactionMarker+"\n")); err != nil {
			t.Fatal(err)
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		if run, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
			t.Fatal(err)
		}
		run = publishRun(t, root, run.RunID)
		assignGate(t, root, run, map[string][]string{})
		roles[role] = passRole(run)
		runs = append(runs, run)
	}
	v, err := ReadReviewBatchView(root, batch)
	if err != nil {
		t.Fatal(err)
	}
	r := ReviewCloseRequest{BatchID: batch, ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: roles}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}); err != nil {
		t.Fatal(err)
	}
	relocateCards(t, root, "done", id)
	return root, id, runs
}

// When one receipt of a batch cannot be removed, the other run of the batch
// stays with it and keeps its receipt, so its originals still read as compacted.
func TestArchivePruneRestoresTheReceiptOfARunItKeeps(t *testing.T) {
	root, id, runs := twoRoleCompactionBoard(t, "compact-pair")
	if report := compact(t, root, false); len(report.Runs) != 2 {
		t.Fatalf("pair not compacted: %+v", report)
	}
	kept, stuck := runs[0], runs[1]
	receipt := compactionReceiptPath(root, stuck.RunID)
	data, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(receipt, 0o700); err != nil {
		t.Fatal(err)
	}
	relocateCards(t, root, "archived", id)
	if report, err := PruneArchived(root); err == nil || len(report.Runs) != 0 {
		t.Fatalf("prune removed runs of a batch it keeps: %+v %v", report, err)
	}
	requireExists(t, compactionReceiptPath(root, kept.RunID), true)
	_, err = ReadReviewOriginal(root, kept.RunID, "evidence.txt")
	requireBoardCode(t, err, "board.review_compacted")
	if err = os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(receipt, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if report, err := PruneArchived(root); err != nil || !slices.Equal(report.Runs, []string{kept.RunID, stuck.RunID}) {
		t.Fatalf("retry: %+v %v", report, err)
	}
	for _, run := range runs {
		requireExists(t, compactionReceiptPath(root, run.RunID), false)
		requireExists(t, reviewPath(root, "runs", run.RunID), false)
	}
	requirePrunedCheck(t, root, id)
}

// interruptedPrune stops an archive cleanup after it removed the receipts of
// a two-run batch: runs[0] keeps only its backup, and runs[1] has a directory
// in place of its receipt, whose bytes data holds.
func interruptedPrune(t *testing.T, slug string) (root, id string, runs []ReviewRun, data []byte) {
	t.Helper()
	root, id, runs = twoRoleCompactionBoard(t, slug)
	compact(t, root, false)
	receipt := compactionReceiptPath(root, runs[1].RunID)
	data, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(receipt, 0o700); err != nil {
		t.Fatal(err)
	}
	relocateCards(t, root, "archived", id)
	halted := errors.New("halted")
	pruneInterrupt = func(string) error { return halted }
	t.Cleanup(func() { pruneInterrupt = nil })
	if _, err = PruneArchived(root); !errors.Is(err, halted) {
		t.Fatalf("want the interruption, got %v", err)
	}
	pruneInterrupt = nil
	return root, id, runs, data
}

// An archive cleanup stopped after it removed the receipts leaves their
// backups: while another receipt of the batch stays stuck, the kept run still
// reads as compacted, and the next rounds restore and finally remove it.
func TestArchivePruneKeepsReceiptsAcrossAnInterruption(t *testing.T) {
	root, id, runs, data := interruptedPrune(t, "compact-halt")
	kept, stuck := runs[0], runs[1]
	receipt := compactionReceiptPath(root, stuck.RunID)
	requireExists(t, compactionReceiptPath(root, kept.RunID), false)
	requireExists(t, compactionBackupPath(root, kept.RunID), true)
	_, err := ReadReviewOriginal(root, kept.RunID, "evidence.txt")
	requireBoardCode(t, err, "board.review_compacted")

	if report, err := PruneArchived(root); err == nil || len(report.Runs) != 0 {
		t.Fatalf("a stuck receipt let the batch go: %+v %v", report, err)
	}
	requireExists(t, compactionReceiptPath(root, kept.RunID), true)
	requireExists(t, compactionBackupPath(root, kept.RunID), false)
	_, err = ReadReviewOriginal(root, kept.RunID, "evidence.txt")
	requireBoardCode(t, err, "board.review_compacted")

	if err = os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(receipt, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if report, err := PruneArchived(root); err != nil || len(report.Runs) != 2 {
		t.Fatalf("retry: %+v %v", report, err)
	}
	for _, run := range runs {
		requireExists(t, compactionReceiptPath(root, run.RunID), false)
		requireExists(t, compactionBackupPath(root, run.RunID), false)
		requireExists(t, reviewPath(root, "runs", run.RunID), false)
	}
	requirePrunedCheck(t, root, id)
}

// A run that an interruption left with only its backup reads as compacted in
// check, as it does with its receipt.
func TestCompactedReviewRunsFindsARunWithOnlyItsBackup(t *testing.T) {
	root, id, runs, _ := interruptedPrune(t, "compact-backup-only")
	kept := runs[0]
	compacted, err := CompactedReviewRuns(root, []string{id})
	if err != nil || !slices.Equal(compacted[id], []string{kept.RunID}) {
		t.Fatalf("compacted runs %v %v, want %s", compacted, err, kept.RunID)
	}
	_, stdout, _, err := CheckBoard(root, []string{id}, false)
	if err != nil {
		t.Fatal(err)
	}
	if marker := config.Text("board.check_review_compacted", id, kept.RunID); !strings.Contains(stdout, marker+"\n") {
		t.Fatalf("check output %q lacks %q", stdout, marker)
	}
}

// A run with both its receipt and its backup is listed once; a receipt that
// is invalid wins over a valid backup, as for every other reader.
func TestCompactedReviewRunsPrefersTheReceiptOverItsBackup(t *testing.T) {
	other := func(t *testing.T, data []byte) []byte {
		var r compactionReceipt
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		r.TaskIDs = []string{"20260101-other-member-task"}
		return []byte(reviewJSON(r))
	}
	tests := map[string]struct {
		receipt func(t *testing.T, backup []byte) []byte
		want    bool
	}{
		"valid receipt":          {func(_ *testing.T, backup []byte) []byte { return backup }, true},
		"unparsable receipt":     {func(*testing.T, []byte) []byte { return []byte("{not json\n") }, false},
		"receipt of other cards": {other, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			root, id, runs, _ := interruptedPrune(t, "compact-both")
			kept := runs[0]
			backup, err := os.ReadFile(compactionBackupPath(root, kept.RunID))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(compactionReceiptPath(root, kept.RunID), tc.receipt(t, backup), 0o600); err != nil {
				t.Fatal(err)
			}
			compacted, err := CompactedReviewRuns(root, []string{id})
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			if tc.want {
				want = []string{kept.RunID}
			}
			if !slices.Equal(compacted[id], want) {
				t.Fatalf("compacted runs %v, want %v", compacted[id], want)
			}
		})
	}
}
