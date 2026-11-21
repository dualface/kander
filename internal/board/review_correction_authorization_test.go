package board

import (
	"strings"
	"testing"
)

// An accepted sync round authorizes the current owner to append a correction record for a fix
// commit that a rebase rewrote. The round carries only the correction: nothing else about the
// authorization changes, so the old record stays and the retired epoch stays refused.
func TestAcceptedSyncRoundAuthorizesACorrectionRecord(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "correction-record")
	gatePlan(t, root, []string{id}, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "Needs a fix", Evidence: "x:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput([]string{id}, "pm", "PMQA"), findings)
	assignGate(t, root, run, map[string][]string{f.ID: {id}})
	s := transactionSnapshot(t, root, id)
	text, _ := setMetadata(s.Text, FieldSession, "codex correction-session")
	if err := fixtureWrite(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	record := func(recordID, previous, fix string, a *ExecutionAuthorization) ReviewDisposition {
		return ReviewDisposition{RecordID: recordID, PreviousRecordID: previous, RunID: run.RunID, BatchID: run.BatchID, FindingID: f.ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "fixed", Basis: "fixture fix", FixCommit: fix, Verification: "fixture verification", Authorization: a}
	}
	s = transactionSnapshot(t, root, id)
	before, after := strings.Repeat("c", 40), strings.Repeat("d", 40)
	if err := SubmitReviewDisposition(root, record("fix", "", before, nil), s.Revision); err != nil {
		t.Fatal(err)
	}

	grant := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "correction-round"))
	if _, err := dispatchMove(t, root, grant, "working"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	if err := SubmitReviewDisposition(root, record("fix-rebased", "fix", after, nil), s.Revision); err == nil {
		t.Fatal("a bound card took a correction record without the round authorization")
	}
	if err := SubmitReviewDisposition(root, record("fix-rebased", "fix", after, &grant.Authorization), s.Revision); err != nil {
		t.Fatalf("the accepted sync round refused its correction record: %v", err)
	}

	v, err := ReadReviewBatchView(root, run.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	latest := latestViewRecords(v.Runs[0])
	if len(latest) != 1 || latest[0].RecordID != "fix-rebased" || latest[0].FixCommit != after || len(v.Runs[0].Records) != 2 {
		t.Fatalf("the correction did not become the latest record next to the original: %+v", v.Runs[0].Records)
	}
}
