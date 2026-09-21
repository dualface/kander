package board

import (
	"strings"
	"testing"
)

func TestDispositionErrorsNameTheFailingField(t *testing.T) {
	run := ReviewRun{ReviewInput: ReviewInput{RunID: "run", BatchID: "batch"}, Hashes: map[string]string{"report.md": "hash"}}
	item := ReviewFinding{ID: "f1", Tier: "low", Text: "original text"}
	assignment := ReviewAssignment{Items: map[string][]string{"f1": {"task"}}, Owners: map[string]string{"task": "codex"}}
	record := ReviewDisposition{
		RecordID: "rec", RunID: "run", FindingID: "f1", BatchID: "batch", TaskID: "task",
		Author: "codex", ReportHash: "hash", Original: "original text", Status: "deferred", Basis: "checked",
	}
	if err := validateDisposition(record, run, item, assignment); err != nil {
		t.Fatal(err)
	}
	record.Author = "claude"
	err := validateDisposition(record, run, item, assignment)
	if err == nil || !strings.Contains(err.Error(), `author "claude"`) || !strings.Contains(err.Error(), `owner "codex"`) {
		t.Fatal(err)
	}
	record.Author = "codex"
	record.ReportHash = "other"
	err = validateDisposition(record, run, item, assignment)
	if err == nil || !strings.Contains(err.Error(), "report_hash") {
		t.Fatal(err)
	}
	record.ReportHash = "hash"
	record.Status = "fixed"
	record.FixCommit = "2026-09-22T00:30:00Z"
	record.Verification = "go test"
	err = validateDisposition(record, run, item, assignment)
	if err == nil || !strings.Contains(err.Error(), "fix_commit") {
		t.Fatal(err)
	}
	record.FixCommit = strings.Repeat("ab", 20)
	record.Verification = " "
	err = validateDisposition(record, run, item, assignment)
	if err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatal(err)
	}
}

func TestPassedAtNamesCommitRatherThanTimestamp(t *testing.T) {
	sha := strings.Repeat("ab", 20)
	view := ReviewBatchView{Schema: 1, Batch: ReviewBatch{
		BatchID: "batch", Revision: 1, Base: sha, TargetCommit: sha, TaskIDs: []string{"task"},
		Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository rule"},
	}, Runs: []ReviewRunView{{
		Run: ReviewRun{ReviewInput: ReviewInput{RunID: "pm", BatchID: "batch", Role: "PMQA", Commit: sha}, ExecutionStatus: "ok"},
	}}}
	request := ReviewCloseRequest{
		BatchID: "batch", ExpectedRevision: 1, ViewHash: ReviewViewDigest(view), Author: "coordinator",
		Roles: map[string]ReviewRoleConclusion{"PMQA": {RunID: "pm", PassedAt: "2026-09-22T00:30:00Z", Basis: "checked"}},
	}
	_, _, err := ReviewClosureEdges(view, request)
	if err == nil || !strings.Contains(err.Error(), "passed_at") || !strings.Contains(err.Error(), "not a timestamp") {
		t.Fatal(err)
	}
	request.Roles["PMQA"] = ReviewRoleConclusion{RunID: "missing", PassedAt: sha, Basis: "checked"}
	_, _, err = ReviewClosureEdges(view, request)
	if err == nil || !strings.Contains(err.Error(), "required successful role conclusion: PMQA") {
		t.Fatal(err)
	}
}

func TestSameRunFixNamesMechanicalField(t *testing.T) {
	runSHA := strings.Repeat("ab", 20)
	fixSHA := strings.Repeat("cd", 20)
	view := ReviewBatchView{Schema: 1, Batch: ReviewBatch{
		BatchID: "batch", Revision: 1, Base: runSHA, TargetCommit: fixSHA, TaskIDs: []string{"task"},
		Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository rule"},
	}, Runs: []ReviewRunView{{
		Run:        ReviewRun{ReviewInput: ReviewInput{RunID: "pm", BatchID: "batch", Role: "PMQA", Commit: runSHA}, ExecutionStatus: "ok"},
		Findings:   &ReviewFindings{Findings: []ReviewFinding{{ID: "f1", Tier: "blocking", Text: "text"}}, NonBlocking: []ReviewFinding{}},
		Assignment: &ReviewAssignment{Items: map[string][]string{"f1": {"task"}}},
		Records: []ReviewDisposition{{
			RecordID: "rec", RunID: "pm", FindingID: "f1", TaskID: "task", Status: "fixed", FixCommit: fixSHA,
		}},
	}}}
	request := ReviewCloseRequest{
		BatchID: "batch", ExpectedRevision: 1, ViewHash: ReviewViewDigest(view), Author: "coordinator",
		Roles: map[string]ReviewRoleConclusion{"PMQA": {RunID: "pm", PassedAt: fixSHA, Basis: "checked"}},
	}
	_, _, err := ReviewClosureEdges(view, request)
	if err == nil || !strings.Contains(err.Error(), "disposition.mechanical") {
		t.Fatal(err)
	}
}
