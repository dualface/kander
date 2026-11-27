//go:build unix

package board

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
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
