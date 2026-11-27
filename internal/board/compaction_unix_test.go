//go:build unix

package board

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A special file under a name a review producer writes is kept and reported.
func TestCompactionReportsASpecialFileUnderAKnownName(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-fifo")
	path := filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "reviews", run.RunID, "manifest.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	report := compact(t, root, false)
	requireExists(t, path, true)
	if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.HasPrefix(w, path+": ") }) {
		t.Fatalf("%s not reported: %v", path, report.Warnings)
	}
}

// A FIFO in place of a compaction receipt fails archive pruning at once: the
// FIFO is kept, and the board lock is free again.
func TestArchivePruneRefusesAFIFOReceiptWithoutBlocking(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-fifo-receipt")
	compact(t, root, false)
	receipt := compactionReceiptPath(root, run.RunID)
	if err := os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	relocateCards(t, root, "archived", id)
	done := make(chan error, 1)
	go func() {
		_, err := PruneArchived(root)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("prune accepted a FIFO receipt")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("prune blocked on a FIFO receipt")
	}
	requireExists(t, receipt, true)
	requireExists(t, reviewPath(root, "runs", run.RunID, "run.json"), true)
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = locks.close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ReviewRunCompacted(root, run.RunID); err == nil {
		t.Fatal("a FIFO receipt read as a receipt")
	}
}
