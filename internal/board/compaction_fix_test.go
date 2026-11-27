package board

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
	if report, err := PruneArchived(root); err == nil {
		t.Fatalf("prune did not report the stuck receipt: %+v", report)
	}
	requireExists(t, reviewPath(root, "runs", run.RunID, "run.json"), true)
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

// A compacted stored input other than the frozen contexts reads as compacted;
// before compaction it stays unreadable as before.
func TestCompactedStoredInputReadsAsCompacted(t *testing.T) {
	root, _, run := compactionBoard(t, "compact-input")
	_, err := ReadReviewInput(root, run.RunID, "evidence.txt")
	requireBoardCode(t, err, "board.review_evidence_invalid")
	_, err = ReadReviewInput(root, run.RunID, "../run.json")
	requireBoardCode(t, err, "board.review_evidence_invalid")
	compact(t, root, false)
	_, err = ReadReviewInput(root, run.RunID, "evidence.txt")
	requireBoardCode(t, err, "board.review_compacted")
	_, err = ReadReviewInput(root, run.RunID, "unknown.txt")
	requireBoardCode(t, err, "board.review_evidence_invalid")
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
