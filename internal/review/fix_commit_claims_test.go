//go:build unix

package review

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// record submits one author record for a finding of a run, as the owner would.
func (f *mechanicalFixture) record(t *testing.T, runID string, item board.ReviewFinding, recordID, previous, fix string) {
	t.Helper()
	run, err := board.ReadReviewRun(f.root, runID)
	if err != nil {
		t.Fatal(err)
	}
	d := board.ReviewDisposition{RecordID: recordID, PreviousRecordID: previous, RunID: runID, BatchID: "batch", FindingID: item.ID, TaskID: mechanicalTaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: item.Text, Status: "fixed", Basis: "fixture fix", FixCommit: fix, Verification: "fixture verification"}
	s, err := board.ReadSnapshot(f.root, mechanicalTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.SubmitReviewDisposition(f.root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
}

func lowItem(id string) board.ReviewFinding {
	return board.ReviewFinding{ID: id, Tier: "low", Text: "Cleanup " + id, Evidence: "fixture.txt:1"}
}

// closeRefused runs the close command, requires it to fail without a closure, and returns its stderr.
func (f *mechanicalFixture) closeRefused(t *testing.T, r board.ReviewCloseRequest) string {
	t.Helper()
	code, out, stderr := captureRun(t, []string{"close", f.h.repo, dispositionJSON(t, f.h, "close", r)})
	if code == 0 {
		t.Fatalf("close accepted: %s %s", out, stderr)
	}
	progress, err := board.ReviewTaskProgress(f.root, mechanicalTaskID)
	if err != nil || progress.Status == "closed" {
		t.Fatalf("refused close published a closure: %+v %v", progress, err)
	}
	return stderr
}

func TestCloseListsEveryFixedRecordWhoseCommitLeftTheBatchHistory(t *testing.T) {
	f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, lowItem("L-1"), lowItem("L-2"))
	reviewed := f.h.head
	// The fix first lands on a side branch. The rebased copy carries the same patch on the delivery branch.
	gitRepo(t, f.h.repo, "checkout", "-q", "-b", "before-rebase")
	before := commitFile(t, f.h.repo, "fix.txt", "fixed\n", "fix")
	gitRepo(t, f.h.repo, "checkout", "-q", "-")
	after := commitFile(t, f.h.repo, "fix.txt", "fixed\n", "fix, rebased")
	f.record(t, "stable", f.items[0], "fix-l-1", "", before)
	f.record(t, "stable", f.items[1], "fix-l-2", "", before)
	f.advance(t, after, after)
	_, r := f.request(t, "stable", reviewed)

	stderr := f.closeRefused(t, r)
	for _, want := range []string{"Git ancestry", mechanicalTaskID, "stable", "L-1", "L-2", "fix-l-1", "fix-l-2", before, "patch-id", "snippets/execute-group-member.md"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in %s", want, stderr)
		}
	}
	if strings.Contains(stderr, "Git commit required") {
		t.Errorf("the missing-commit error replaced the record list: %s", stderr)
	}

	// The correction records point at the rebased commit, and the same batch closes.
	f.record(t, "stable", f.items[0], "fix-l-1-rebased", "fix-l-1", after)
	f.record(t, "stable", f.items[1], "fix-l-2-rebased", "fix-l-2", after)
	_, r = f.request(t, "stable", reviewed)
	commandOK(t, "close", f.h.repo, dispositionJSON(t, f.h, "close", r))
}

func TestCloseListsAFixCommitThatDoesNotExist(t *testing.T) {
	f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, lowItem("L-1"))
	reviewed := f.h.head
	gone := strings.Repeat("a", 40)
	f.record(t, "stable", f.items[0], "fix-l-1", "", gone)
	target := commitFile(t, f.h.repo, "fix.txt", "fixed\n", "fix")
	f.advance(t, target, target)
	_, r := f.request(t, "stable", reviewed)

	stderr := f.closeRefused(t, r)
	for _, want := range []string{"Git ancestry", "L-1", "fix-l-1", gone} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in %s", want, stderr)
		}
	}
	if strings.Contains(stderr, "Git commit required") {
		t.Errorf("a missing fix commit is listed as a record, not as a bare commit error: %s", stderr)
	}
}

// A must-fix finding of an earlier run needs its fix in the history of the conclusion run's commit.
// Here the fix reaches the batch target only through a merge, after the conclusion run reviewed.
func TestCloseListsAMustFixCommitMissingFromTheConclusionRun(t *testing.T) {
	f := newMechanicalFixture(t, map[string]string{"fixture.txt": "old\n"}, board.ReviewFinding{ID: "F-1", Tier: "medium", Text: "Fix F-1", Evidence: "fixture.txt:1"})
	gitRepo(t, f.h.repo, "checkout", "-q", "-b", "side")
	side := commitFile(t, f.h.repo, "side.txt", "fixed\n", "fix")
	gitRepo(t, f.h.repo, "checkout", "-q", "-")
	later := commitFile(t, f.h.repo, "later.txt", "later\n", "later delivery")
	f.record(t, "stable", f.items[0], "fix-f-1", "", side)
	f.advance(t, later, later)
	f.run(t, "second", "stable", later, nil)
	gitRepo(t, f.h.repo, "merge", "-q", "--no-ff", "-m", "merge the fix", "side")
	merged := gitRepo(t, f.h.repo, "rev-parse", "HEAD")
	f.advance(t, merged, side, merged)
	_, r := f.request(t, "second", later)

	stderr := f.closeRefused(t, r)
	for _, want := range []string{"Git ancestry", "F-1", "fix-f-1", side, "is not an ancestor of " + later} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in %s", want, stderr)
		}
	}
	if strings.Contains(stderr, "is not an ancestor of "+merged) {
		t.Errorf("the batch target edge holds and must not be listed: %s", stderr)
	}
}
