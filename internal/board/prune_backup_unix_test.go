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

// backupFixture is a board with archived, settled runs whose receipts the
// tests move between compactions/ and compaction-backups/.
type backupFixture struct {
	root  string
	cards []string
}

// compactedRun adds a card with one settled run and compacts it.
func (f *backupFixture) compactedRun(t *testing.T, slug string) ReviewRun {
	t.Helper()
	run := f.settledRun(t, slug)
	compact(t, f.root, false)
	if _, err := os.Stat(compactionReceiptPath(f.root, run.RunID)); err != nil {
		t.Fatalf("%s not compacted: %v", run.RunID, err)
	}
	return run
}

// settledRun adds a done card with one settled run and no compaction receipt.
func (f *backupFixture) settledRun(t *testing.T, slug string) ReviewRun {
	t.Helper()
	id := pruneCard(t, f.root, slug, "")
	run := settledCompactionRun(t, f.root, slug+"-run", id)
	relocateCards(t, f.root, "done", id)
	f.cards = append(f.cards, id)
	return run
}

func (f *backupFixture) archive(t *testing.T) {
	t.Helper()
	relocateCards(t, f.root, "archived", f.cards...)
}

// backUp copies the receipt of run to its backup; with move it also removes
// the receipt, the state an interrupted round leaves.
func (f *backupFixture) backUp(t *testing.T, run ReviewRun, move bool) {
	t.Helper()
	receipt := compactionReceiptPath(f.root, run.RunID)
	data, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensurePruneDir(f.root, "compaction-backups"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(compactionBackupPath(f.root, run.RunID), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if move {
		if err = os.Remove(receipt); err != nil {
			t.Fatal(err)
		}
	}
}

// readOnlyDir makes dir readable but not writable until the test ends or
// restore runs.
func readOnlyDir(t *testing.T, dir string) (restore func()) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so a backup could still be removed")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	restore = func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(restore)
	return restore
}

func backupDir(root string) string {
	return filepath.Dir(compactionBackupPath(root, "x"))
}

func requireRunPruned(t *testing.T, root string, run ReviewRun, want bool) {
	t.Helper()
	paths := []string{reviewPath(root, "runs", run.RunID), reviewPath(root, "batches", run.BatchID+".json"), reviewPath(root, "plans", "plan-"+run.RunID+".json")}
	for _, path := range paths {
		requireExists(t, path, !want)
	}
}

func requireReceipt(t *testing.T, root string, run ReviewRun) {
	t.Helper()
	requireExists(t, compactionReceiptPath(root, run.RunID), true)
	if _, exists, err := readCompactionReceipt(root, run.RunID); err != nil || !exists {
		t.Fatalf("receipt of %s: exists=%v %v", run.RunID, exists, err)
	}
}

func pruneWithin(t *testing.T, root string, bound time.Duration) (PruneReport, error) {
	t.Helper()
	type result struct {
		report PruneReport
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := PruneArchived(root)
		done <- result{report, err}
	}()
	select {
	case r := <-done:
		return r.report, r.err
	case <-time.After(bound):
		t.Fatal("prune blocked past the bound")
		return PruneReport{}, nil
	}
}

// A FIFO in place of a compaction backup fails the round at once: the run,
// its receipt and the FIFO stay, and the board lock is free again.
func TestArchivePruneRefusesAFIFOBackupWithoutBlocking(t *testing.T) {
	f := &backupFixture{root: tempBoard(t)}
	run := f.compactedRun(t, "backup-fifo")
	f.archive(t)
	backup := compactionBackupPath(f.root, run.RunID)
	if err := ensurePruneDir(f.root, "compaction-backups"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(backup, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pruneWithin(t, f.root, 2*time.Second); err == nil || !strings.Contains(err.Error(), backup) {
		t.Fatalf("prune accepted a FIFO backup: %v", err)
	}
	requireExists(t, reviewPath(f.root, "runs", run.RunID, "run.json"), true)
	requireReceipt(t, f.root, run)
	var st syscall.Stat_t
	if err := syscall.Lstat(backup, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		t.Fatalf("the FIFO backup did not stay: %v", err)
	}
	if _, err := pruneWithin(t, f.root, 2*time.Second); err == nil {
		t.Fatal("the second round accepted the FIFO backup")
	}
}

// A backup that cannot be removed keeps only its own run: an unrelated batch
// goes in the same round, and once the backup can go, so does the run.
func TestArchivePruneKeepsOnlyTheRunOfAStuckBackup(t *testing.T) {
	f := &backupFixture{root: tempBoard(t)}
	a := f.compactedRun(t, "backup-stuck-a")
	b := f.settledRun(t, "backup-stuck-b")
	f.archive(t)
	f.backUp(t, a, true)
	restore := readOnlyDir(t, backupDir(f.root))
	backup := compactionBackupPath(f.root, a.RunID)

	report, err := PruneArchived(f.root)
	if err == nil || !strings.Contains(err.Error(), backup) {
		t.Fatalf("want the error of %s, got %v", backup, err)
	}
	if !slices.Equal(report.Runs, []string{b.RunID}) {
		t.Fatalf("pruned runs %v, want only %s", report.Runs, b.RunID)
	}
	requireRunPruned(t, f.root, b, true)
	requireRunPruned(t, f.root, a, false)
	requireReceipt(t, f.root, a)

	restore()
	report, err = PruneArchived(f.root)
	if err != nil || !slices.Equal(report.Runs, []string{a.RunID}) {
		t.Fatalf("retry: %+v %v", report, err)
	}
	requireExists(t, backup, false)
	requireRunPruned(t, f.root, a, true)
	requirePrunedCheck(t, f.root, f.cards...)
}

// The backup of a run that is gone and cannot be removed is reported, and an
// unrelated batch still goes.
func TestArchivePruneReportsTheStuckBackupOfAGoneRun(t *testing.T) {
	f := &backupFixture{root: tempBoard(t)}
	b := f.settledRun(t, "backup-gone-b")
	f.archive(t)
	gone := "backup-gone-a-run"
	if err := ensurePruneDir(f.root, "compaction-backups"); err != nil {
		t.Fatal(err)
	}
	backup := compactionBackupPath(f.root, gone)
	if err := os.WriteFile(backup, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readOnlyDir(t, backupDir(f.root))

	report, err := PruneArchived(f.root)
	if err == nil || !strings.Contains(err.Error(), backup) {
		t.Fatalf("want the error of %s, got %v", backup, err)
	}
	if !slices.Equal(report.Runs, []string{b.RunID}) {
		t.Fatalf("pruned runs %v, want only %s", report.Runs, b.RunID)
	}
	requireRunPruned(t, f.root, b, true)
	requireExists(t, backup, true)
}

// A run kept for its backup stays kept when the receipt loop keeps another
// run and plans again. A's backup cannot be removed; C's receipt cannot be
// removed (compactions/ is read-only; ensurePruneDir makes the backup
// directory writable again before C's backup is written).
func TestArchivePruneKeepsBackupStuckRunsWhenPlanningAgain(t *testing.T) {
	f := &backupFixture{root: tempBoard(t)}
	a := f.compactedRun(t, "backup-replan-a")
	c := f.compactedRun(t, "backup-replan-c")
	if a.BatchID == c.BatchID {
		t.Fatal("A and C must be in different batches")
	}
	f.archive(t)
	f.backUp(t, a, false)
	readOnlyDir(t, backupDir(f.root))
	readOnlyDir(t, filepath.Dir(compactionReceiptPath(f.root, c.RunID)))

	report, err := PruneArchived(f.root)
	if err == nil || !strings.Contains(err.Error(), compactionBackupPath(f.root, a.RunID)) || !strings.Contains(err.Error(), compactionReceiptPath(f.root, c.RunID)) {
		t.Fatalf("want the errors of A's backup and C's receipt, got %v", err)
	}
	if len(report.Runs) != 0 {
		t.Fatalf("pruned runs %v, want none", report.Runs)
	}
	for _, run := range []ReviewRun{a, c} {
		requireRunPruned(t, f.root, run, false)
		requireReceipt(t, f.root, run)
	}
}
