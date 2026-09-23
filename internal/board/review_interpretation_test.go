package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func receiverRun(t *testing.T, root string, input ReviewInput, report string) ReviewRun {
	t.Helper()
	input.FindingsSchema = FindingsSchemaReceiver
	run, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	run, err = FinalizeReviewRun(root, run, []byte(report))
	if err != nil || run.ExecutionStatus != "ok" || run.ExitCode != 0 {
		t.Fatalf("execution facts: %+v %v", run, err)
	}
	return publishRun(t, root, run.RunID)
}

func zeroInterpretation(run ReviewRun, quote ReviewReportQuote) ReviewInterpretation {
	return ReviewInterpretation{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "receiver", Basis: "Read the full report; the cited statement explicitly confirms both sections contain no findings.", Complete: true, Findings: emptyFindings(), NoFindings: &quote}
}

func TestReceiverInterpretationCompletesWithoutRerunningReviewer(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "receiver-one"), gateCard(t, root, "receiver-two")}
	gatePlan(t, root, ids, archiveRequirements())
	report := "Review complete.\n```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```No findings in either section.\n"
	run := receiverRun(t, root, archiveInput(ids, "receiver", "PMQA"), report)
	if pending, err := ReviewNeedsInterpretation(root, run.RunID); err != nil || !pending {
		t.Fatalf("pending: %v %v", pending, err)
	}
	view, err := PublishReviewDisposition(root, run.BatchID)
	if err != nil || len(view.Runs) != 1 || view.Runs[0].FindingsStatus != "pending" || view.Runs[0].Findings != nil {
		t.Fatalf("pending aggregate: %+v %v", view, err)
	}
	if err := AssignReviewFindings(root, ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "receiver", Basis: "must wait", Items: map[string][]string{}}); err == nil {
		t.Fatal("pending report assigned")
	}
	if _, err := ReviewIncrementalContext(root, run.RunID); err == nil {
		t.Fatal("pending report became incremental predecessor")
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err == nil {
		t.Fatal("pending report closed")
	}
	for _, id := range ids {
		p, err := ReviewTaskProgress(root, id)
		if err != nil || !slices.Contains(p.Pending, "interpretation:"+run.RunID) {
			t.Fatalf("progress: %+v %v", p, err)
		}
		// A targeted check must acquire all interpretation member locks.
		if problems, err := CheckReviewEvidence(root, []string{id}); err != nil || len(problems) > 0 {
			t.Fatalf("pending is not corruption: %v %v", problems, err)
		}
		s := transactionSnapshot(t, root, id)
		updateSnapshot(t, root, s, strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\nComplete.", 1))
		s = transactionSnapshot(t, root, id)
		if _, err := MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"}); err == nil {
			t.Fatal("pending report completed card")
		}
	}
	m := zeroInterpretation(run, ReviewReportQuote{StartLine: 3, EndLine: 4, Quote: "{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```No findings in either section."})
	if err := InterpretReview(root, m); err != nil {
		t.Fatal(err)
	}
	if err := InterpretReview(root, m); err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	changed := m
	changed.Author = "another-receiver"
	if err := InterpretReview(root, changed); err == nil {
		t.Fatal("interpretation overwritten")
	}
	assignGate(t, root, run, map[string][]string{})
	view, err = PublishReviewDisposition(root, run.BatchID)
	if err != nil || view.Runs[0].FindingsStatus != "ready" || view.Runs[0].Interpretation.Author != m.Author {
		t.Fatalf("ready aggregate: %+v %v", view, err)
	}
	context, err := ReviewIncrementalContext(root, run.RunID)
	if err != nil || !strings.Contains(string(context), report) || !strings.Contains(string(context), "\"interpretation\"") {
		t.Fatalf("incremental context lost original or reading: %s %v", context, err)
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	if err := InterpretReview(root, m); err != nil {
		t.Fatalf("identical retry after close: %v", err)
	}
	for _, id := range ids {
		s := transactionSnapshot(t, root, id)
		if _, err := MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"}); err != nil {
			t.Fatal(err)
		}
		if problems, err := CheckReviewEvidence(root, []string{id}); err != nil || len(problems) > 0 {
			t.Fatalf("interpreted targeted check: %v %v", problems, err)
		}
	}
	after, err := ReadReviewRun(root, run.RunID)
	if err != nil || !reflect.DeepEqual(run, after) {
		t.Fatalf("original run changed: %v", err)
	}
	saved, err := ReadReviewOriginal(root, run.RunID, "report.md")
	if err != nil || string(saved) != report {
		t.Fatal("original report changed")
	}
}

func TestInterpretationRejectsInvalidEvidenceWithoutPublishing(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "invalid-reading")}
	gatePlan(t, root, ids, archiveRequirements())
	run := receiverRun(t, root, archiveInput(ids, "receiver", "PMQA"), "No findings in either section.\r\n")
	for name, mutate := range map[string]func(*ReviewInterpretation){
		"wrong hash":    func(m *ReviewInterpretation) { m.ReportHash = strings.Repeat("a", 64) },
		"wrong run":     func(m *ReviewInterpretation) { m.RunID = "foreign" },
		"no author":     func(m *ReviewInterpretation) { m.Author = "" },
		"no basis":      func(m *ReviewInterpretation) { m.Basis = "" },
		"partial":       func(m *ReviewInterpretation) { m.Complete = false },
		"missing array": func(m *ReviewInterpretation) { m.Findings.NonBlocking = nil },
		"no zero proof": func(m *ReviewInterpretation) { m.NoFindings = nil },
		"bad quote":     func(m *ReviewInterpretation) { m.NoFindings.Quote = "invented" },
		"bad range":     func(m *ReviewInterpretation) { m.NoFindings.StartLine = 0 },
		"extra range":   func(m *ReviewInterpretation) { m.NoFindings.EndLine = 3 },
		"unused location": func(m *ReviewInterpretation) {
			m.Locations = []LegacyFindingLocation{{FindingID: "PMQA-01"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := zeroInterpretation(run, ReviewReportQuote{StartLine: 1, EndLine: 1, Quote: "No findings in either section."})
			mutate(&m)
			if err := InterpretReview(root, m); err == nil {
				t.Fatal("invalid interpretation accepted")
			}
			s := transactionSnapshot(t, root, ids[0])
			if _, err := os.Stat(filepath.Join(s.Entry.Path, interpretationPath(run.RunID))); !os.IsNotExist(err) {
				t.Fatal("rejected interpretation left evidence")
			}
		})
	}
	m := zeroInterpretation(run, ReviewReportQuote{StartLine: 1, EndLine: 1, Quote: "No findings in either section."})
	if err := InterpretReview(root, m); err != nil {
		t.Fatalf("CRLF quote normalization: %v", err)
	}
}

func TestInterpretedFindingsRetainDispositionAndLineageGates(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "interpreted-items")
	gatePlan(t, root, []string{id}, archiveRequirements())
	report := "PMQA-01 medium: missing error handling at a.go:1.\nPMQA-02 low: unclear message at a.go:2."
	run := receiverRun(t, root, archiveInput([]string{id}, "receiver", "PMQA"), report)
	f := ReviewFindings{Findings: []ReviewFinding{{ID: "PMQA-01", Tier: "medium", Text: "Missing error handling", Evidence: "a.go:1"}}, NonBlocking: []ReviewFinding{{ID: "PMQA-02", Tier: "low", Text: "Unclear message", Evidence: "a.go:2"}}}
	m := ReviewInterpretation{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "receiver", Basis: "Read the entire report; both findings transcribed with their severity and source.", Complete: true, Findings: f, Locations: []LegacyFindingLocation{{FindingID: "PMQA-01", StartLine: 1, EndLine: 1, Quote: strings.Split(report, "\n")[0]}, {FindingID: "PMQA-02", StartLine: 2, EndLine: 2, Quote: strings.Split(report, "\n")[1]}}}
	for name, mutate := range map[string]func(*ReviewInterpretation){
		"missing location":   func(m *ReviewInterpretation) { m.Locations = m.Locations[:1] },
		"foreign location":   func(m *ReviewInterpretation) { m.Locations[0].FindingID = "PMQA-03" },
		"duplicate location": func(m *ReviewInterpretation) { m.Locations[1] = m.Locations[0] },
		"duplicate ID":       func(m *ReviewInterpretation) { m.Findings.NonBlocking[0].ID = "PMQA-01" },
		"invalid tier":       func(m *ReviewInterpretation) { m.Findings.Findings[0].Tier = "low" },
		"rewritten quote":    func(m *ReviewInterpretation) { m.Locations[0].Quote = "Different finding" },
		"false zero": func(m *ReviewInterpretation) {
			m.NoFindings = &ReviewReportQuote{StartLine: 1, EndLine: 1, Quote: m.Locations[0].Quote}
		},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var bad ReviewInterpretation
			if err := DecodeReviewJSON(data, &bad); err != nil {
				t.Fatal(err)
			}
			mutate(&bad)
			if err := InterpretReview(root, bad); err == nil {
				t.Fatal("invalid item interpretation accepted")
			}
		})
	}
	m.Findings.Findings[0].Lineage = &FindingRef{RunID: "foreign", FindingID: "PMQA-01"}
	if err := InterpretReview(root, m); err == nil {
		t.Fatal("foreign lineage accepted")
	}
	m.Findings.Findings[0].Lineage = nil
	if err := InterpretReview(root, m); err != nil {
		t.Fatal(err)
	}
	assignGate(t, root, run, map[string][]string{"PMQA-01": {id}, "PMQA-02": {id}})
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err == nil {
		t.Fatal("undisposed interpreted findings closed")
	}
	gateRecord(t, root, run, id, f.Findings[0], "rejected")
	d := ReviewDisposition{RecordID: "advisory", RunID: run.RunID, BatchID: run.BatchID, FindingID: f.NonBlocking[0].ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.NonBlocking[0].Text, Status: "deferred", Basis: "Optional wording change deferred."}
	if err := SubmitReviewDisposition(root, d, transactionSnapshot(t, root, id).Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	// A changed card-side interpretation invalidates check and closed evidence.
	s := transactionSnapshot(t, root, id)
	if err := os.WriteFile(filepath.Join(s.Entry.Path, interpretationPath(run.RunID)), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if problems, err := CheckReviewEvidence(root, []string{id}); err != nil || len(problems) == 0 {
		t.Fatalf("tampering not detected: %v %v", problems, err)
	}
	if _, err := ReviewTaskProgress(root, id); err == nil {
		t.Fatal("closed evidence ignored interpretation corruption")
	}
}

func TestInterpretedPredecessorSupportsStructuredIncrementalReview(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "interpreted-lineage")
	gatePlan(t, root, []string{id}, archiveRequirements())
	report := "PMQA-01 low: unclear message at a.go:1."
	run := receiverRun(t, root, archiveInput([]string{id}, "first", "PMQA"), report)
	f := emptyFindings()
	f.NonBlocking = []ReviewFinding{{ID: "PMQA-01", Tier: "low", Text: "Unclear message", Evidence: "a.go:1"}}
	m := ReviewInterpretation{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "receiver", Basis: "Full report read; the only finding is transcribed.", Complete: true, Findings: f, Locations: []LegacyFindingLocation{{FindingID: "PMQA-01", StartLine: 1, EndLine: 1, Quote: report}}}
	if err := InterpretReview(root, m); err != nil {
		t.Fatal(err)
	}
	assignGate(t, root, run, map[string][]string{"PMQA-01": {id}})
	gateRecord(t, root, run, id, f.NonBlocking[0], "deferred")
	source, err := ReviewIncrementalContext(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	input := run.ReviewInput
	input.RunID, input.PreviousRunID, input.ReviewedCommit, input.Commit = "next", run.RunID, run.Commit, strings.Repeat("c", 40)
	originals := archiveOriginals()
	originals["review-context.md"] = MergeReviewContext(source, "")
	advance := &ReviewAdvance{PreviousTarget: run.Commit, Target: input.Commit, Reason: "new delivery", Deliveries: map[string]string{input.Commit: id}}
	next, _, err := PrepareReviewRun(root, input, nil, advance, originals, "test")
	if err != nil {
		t.Fatal(err)
	}
	f.NonBlocking[0].Lineage = &FindingRef{RunID: run.RunID, FindingID: "PMQA-01"}
	next.LaunchStatus, next.ExecutionStatus = "started", "ok"
	next, err = FinalizeReviewRun(root, next, structuredReport(f))
	if err != nil || next.ExecutionStatus != "ok" {
		t.Fatalf("interpreted predecessor rejected: %+v %v", next, err)
	}
	next = publishRun(t, root, next.RunID)
	assignGate(t, root, next, map[string][]string{"PMQA-01": {id}})
	gateRecord(t, root, next, id, f.NonBlocking[0], "deferred")
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(next)}); err != nil {
		t.Fatal(err)
	}
}

func TestInterpretationCannotReplaceStructuredOrHistoricalReports(t *testing.T) {
	for _, schema := range []int{0, 1, FindingsSchemaReceiver} {
		t.Run(string(rune('0'+schema)), func(t *testing.T) {
			root := tempBoard(t)
			id := gateCard(t, root, "stable-report")
			gatePlan(t, root, []string{id}, archiveRequirements())
			input := archiveInput([]string{id}, "stable", "PMQA")
			input.FindingsSchema = schema
			run := gateRun(t, root, input, emptyFindings())
			m := zeroInterpretation(run, ReviewReportQuote{StartLine: 3, EndLine: 3, Quote: "{"})
			if err := InterpretReview(root, m); err == nil {
				t.Fatal("stable structured/historical report replaced")
			}
			assignGate(t, root, run, map[string][]string{})
			v, err := ReadReviewBatchView(root, "batch")
			if err != nil || schema < FindingsSchemaReceiver && v.Runs[0].FindingsStatus != "" {
				t.Fatalf("historical view changed: %+v %v", v, err)
			}
		})
	}
}
