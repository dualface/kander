package board

import (
	"os"
	"testing"
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
