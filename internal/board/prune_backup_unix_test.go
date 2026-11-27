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

	"github.com/dualface/kander/v2/internal/config"
)

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

// A run kept for its backup whose run.json planning cannot read could still
// exist, so the round removes nothing: its batch, closure and plan stay. Once
// it reads again, the next round removes everything.
func TestArchivePruneRemovesNothingWhileAStuckRunIsUnreadable(t *testing.T) {
	f := &backupFixture{root: tempBoard(t)}
	a := f.compactedRun(t, "backup-unread-a")
	b := f.settledRun(t, "backup-unread-b")
	f.archive(t)
	f.backUp(t, a, true)
	if os.Geteuid() == 0 {
		t.Skip("root reads a run.json without read permission")
	}
	runFile := reviewPath(f.root, "runs", a.RunID, "run.json")
	if err := os.Chmod(runFile, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(runFile, 0o600) })

	report, err := PruneArchived(f.root)
	if err == nil || !strings.Contains(err.Error(), a.RunID) {
		t.Fatalf("want the error of %s, got %v", a.RunID, err)
	}
	if len(report.Runs) != 0 || len(report.Batches) != 0 || len(report.Plans) != 0 {
		t.Fatalf("pruned %+v while a stuck run was unreadable", report)
	}
	for _, run := range []ReviewRun{a, b} {
		requireRunPruned(t, f.root, run, false)
		requireExists(t, reviewPath(f.root, "closures", run.BatchID+".json"), true)
	}
	requireExists(t, compactionBackupPath(f.root, a.RunID), true)

	if err = os.Chmod(runFile, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err = PruneArchived(f.root)
	if err != nil || !slices.Equal(report.Runs, []string{a.RunID, b.RunID}) {
		t.Fatalf("retry: %+v %v", report, err)
	}
	requireExists(t, compactionBackupPath(f.root, a.RunID), false)
	requirePrunedCheck(t, f.root, f.cards...)
}

// Check reads each receipt by path, so a backup directory it cannot open
// costs no compaction line of a run whose receipt reads, and check still
// prints its output.
func TestCompactedReviewRunsIgnoresAnUnreadableBackupDirectory(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-unlisted")
	compact(t, root, false)
	if err := ensurePruneDir(root, "compaction-backups"); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root opens a directory without permissions")
	}
	if err := os.Chmod(backupDir(root), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(backupDir(root), 0o700) })
	compacted, err := CompactedReviewRuns(root, []string{id})
	if err != nil || !slices.Equal(compacted[id], []string{run.RunID}) {
		t.Fatalf("compacted runs %v %v, want %s", compacted, err, run.RunID)
	}
	_, stdout, _, err := CheckBoard(root, []string{id}, false)
	if marker := config.Text("board.check_review_compacted", id, run.RunID); err != nil || !strings.Contains(stdout, marker+"\n") {
		t.Fatalf("check output %q %v lacks %q", stdout, err, marker)
	}
}

// A symlink in place of a backup is listed as a link and never followed: only
// its run stays, the link and its target are left alone, and an unrelated
// batch still goes.
func TestArchivePruneKeepsOnlyTheRunOfASymlinkBackup(t *testing.T) {
	f := &backupFixture{root: tempBoard(t)}
	a := f.compactedRun(t, "backup-link-a")
	b := f.settledRun(t, "backup-link-b")
	f.archive(t)
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensurePruneDir(f.root, "compaction-backups"); err != nil {
		t.Fatal(err)
	}
	backup := compactionBackupPath(f.root, a.RunID)
	if err := os.Symlink(target, backup); err != nil {
		t.Fatal(err)
	}

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
	if link, err := os.Readlink(backup); err != nil || link != target {
		t.Fatalf("the symlink backup changed: %q %v", link, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "outside\n" {
		t.Fatalf("the link target changed: %q %v", data, err)
	}
}

// An entry of runs/ that fails its listing costs check one diagnostic, never
// its summary or the compaction lines it can still find through the receipts.
func TestCheckKeepsItsOutputWhenRunsCannotBeListed(t *testing.T) {
	root, id, run := compactionBoard(t, "compact-runs-link")
	compact(t, root, false)
	if err := os.Symlink(t.TempDir(), reviewPath(root, "runs", "zz-link")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := CheckBoard(root, []string{id}, false)
	if err != nil || code != 1 || len(stderr) == 0 {
		t.Fatalf("check: %d %q %v %v", code, stdout, stderr, err)
	}
	if marker := config.Text("board.check_review_compacted", id, run.RunID); !strings.Contains(stdout, marker+"\n") {
		t.Fatalf("check output %q lacks %q", stdout, marker)
	}
	if !slices.ContainsFunc(stderr, func(line string) bool { return strings.Contains(line, "zz-link") }) {
		t.Fatalf("check diagnostics %v do not name the link", stderr)
	}
}
