//go:build unix

package review

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
)

// The close of a client of a server lists every fixed record whose fix commit left the batch
// history, and the same batch closes once the author appended the correction records.
func TestRemoteCloseListsRecordsWhoseFixCommitLeftTheHistoryUntilCorrected(t *testing.T) {
	w := newRemoteWorld(t)
	reviewed := w.h.head
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan", Author: "owner", Basis: "fix commit claims", CWD: w.h.repo, ReportLanguage: "en", TaskIDs: []string{w.task}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{w.task}, Base: w.h.base, TargetCommit: reviewed, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: fixture"}}}}
	commandOK(t, "plan", w.h.repo, dispositionJSON(t, w.h, "plan", plan))

	items := []board.ReviewFinding{lowItem("L-1"), lowItem("L-2")}
	data, err := json.Marshal(board.ReviewFindings{Findings: []board.ReviewFinding{}, NonBlocking: items})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CODEX_REPORT", "```kander-findings\n"+string(data)+"\n```")
	commandOK(t, "codex", "--task", w.task, "--run-id", "stable", "--batch-id", "batch", w.h.repo, w.h.base, reviewed, "PMQA", "Frozen goal")
	assignment := board.ReviewAssignment{RunID: "stable", BatchID: "batch", Author: "owner", Basis: "fixture attribution", Items: map[string][]string{"L-1": {w.task}, "L-2": {w.task}}}
	commandOK(t, "assign", w.h.repo, dispositionJSON(t, w.h, "assignment", assignment))

	var run board.ReviewRun
	w.resource(t, "runs", "stable", &run)
	record := func(item board.ReviewFinding, recordID, previous, fix string) {
		t.Helper()
		card, err := w.session.Project.Show(t.Context(), w.task)
		if err != nil {
			t.Fatal(err)
		}
		d := board.ReviewDisposition{RecordID: recordID, PreviousRecordID: previous, RunID: "stable", BatchID: "batch", FindingID: item.ID, TaskID: w.task, Author: "codex", ReportHash: run.Hashes["report.md"], Original: item.Text, Status: "fixed", Basis: "fixture fix", FixCommit: fix, Verification: "fixture verification"}
		commandOK(t, "disposition", w.h.repo, dispositionJSON(t, w.h, recordID, d), strconv.FormatUint(card.Revision, 10))
	}
	gitRepo(t, w.h.repo, "checkout", "-q", "-b", "before-rebase")
	before := commitFile(t, w.h.repo, "fix.txt", "fixed\n", "fix")
	gitRepo(t, w.h.repo, "checkout", "-q", "-")
	after := commitFile(t, w.h.repo, "fix.txt", "fixed\n", "fix, rebased")
	record(items[0], "fix-l-1", "", before)
	record(items[1], "fix-l-2", "", before)
	var batch board.ReviewBatch
	w.resource(t, "batches", "batch", &batch)
	advance := board.ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: batch.Revision, Advance: board.ReviewAdvance{PreviousTarget: reviewed, Target: after, Reason: "fixture fixes", Deliveries: map[string]string{after: w.task}}}
	commandOK(t, "advance", w.h.repo, dispositionJSON(t, w.h, "advance", advance))

	closeFile := func(name string) string {
		t.Helper()
		commandOK(t, "aggregate", w.h.repo, "batch")
		v := w.view(t, "batch")
		r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "owner", Roles: map[string]board.ReviewRoleConclusion{"PMQA": {RunID: "stable", PassedAt: reviewed, Basis: "independently verified"}}}
		return dispositionJSON(t, w.h, name, r)
	}
	code, _, stderr := captureRun(t, []string{"close", w.h.repo, closeFile("close-refused")})
	if code == 0 {
		t.Fatal("close accepted a fix commit outside the batch history")
	}
	for _, want := range []string{"Git ancestry", w.task, "stable", "L-1", "L-2", "fix-l-1", "fix-l-2", before, "patch-id", "snippets/execute-group-member.md"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in %s", want, stderr)
		}
	}
	if got := w.progress(t); got.Status == "closed" {
		t.Fatalf("the refused close left the batch closed: %+v", got)
	}

	record(items[0], "fix-l-1-rebased", "fix-l-1", after)
	record(items[1], "fix-l-2-rebased", "fix-l-2", after)
	commandOK(t, "close", w.h.repo, closeFile("close-corrected"))
	if got := w.progress(t); got.Status != "closed" {
		t.Fatalf("the corrected batch did not close: %+v", got)
	}
}
