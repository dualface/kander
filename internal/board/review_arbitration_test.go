package board

import (
	"strings"
	"testing"
)

func arbitrationFixture(t *testing.T, status string) (string, string, ReviewRun, ReviewFinding, ReviewDisposition) {
	t.Helper()
	root := tempBoard(t)
	id := gateCard(t, root, "arbitration")
	gatePlan(t, root, []string{id}, archiveRequirements())
	input := archiveInput([]string{id}, "pmqa-arbitration", "PMQA")
	input.FindingsSchema = 1
	finding := ReviewFinding{ID: "PMQA-01", Tier: "medium", Text: "changed lifecycle can skip cleanup", Evidence: "worker.go:42"}
	run := gateRun(t, root, input, ReviewFindings{Findings: []ReviewFinding{finding}, NonBlocking: []ReviewFinding{}})
	assignGate(t, root, run, map[string][]string{finding.ID: []string{id}})
	d := gateRecord(t, root, run, id, finding, status)
	return root, id, run, finding, d
}

func arbitrationFor(run ReviewRun, id string, finding ReviewFinding, d ReviewDisposition) ReviewArbitration {
	report := "Independent inspection reproduced the disputed behavior."
	return ReviewArbitration{
		Schema:              1,
		ArbitrationID:       "arbiter-one",
		RunID:               run.RunID,
		FindingID:           finding.ID,
		BatchID:             run.BatchID,
		TaskID:              id,
		DispositionRecordID: d.RecordID,
		Arbiter:             "grok",
		Model:               "independent-model",
		Effort:              "high",
		Verdict:             "sustain",
		Basis:               "reproducer establishes the reviewer premise",
		Report:              report,
		ReportHash:           ReviewDigest([]byte(report)),
	}
}

func TestReviewArbitrationBindsDisputedMustFixDisposition(t *testing.T) {
	for _, status := range []string{"rejected", "unverifiable"} {
		t.Run(status, func(t *testing.T) {
			root, id, run, finding, d := arbitrationFixture(t, status)
			a := arbitrationFor(run, id, finding, d)
			if err := SubmitReviewArbitration(root, a); err != nil {
				t.Fatal(err)
			}
			// Exact replay is idempotent even though RecordedAt is assigned by Kander.
			if err := SubmitReviewArbitration(root, a); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReviewArbitrationRequiresIndependentArbiterAndExactEvidence(t *testing.T) {
	root, id, run, finding, d := arbitrationFixture(t, "rejected")
	for name, mutate := range map[string]func(*ReviewArbitration){
		"author":      func(a *ReviewArbitration) { a.Arbiter = d.Author },
		"reviewer":    func(a *ReviewArbitration) { a.Arbiter = run.Reviewer },
		"bad-hash":    func(a *ReviewArbitration) { a.ReportHash = strings.Repeat("0", 64) },
		"bad-verdict": func(a *ReviewArbitration) { a.Verdict = "pass" },
	} {
		t.Run(name, func(t *testing.T) {
			a := arbitrationFor(run, id, finding, d)
			mutate(&a)
			if err := SubmitReviewArbitration(root, a); err == nil {
				t.Fatal("invalid arbitration accepted")
			}
		})
	}
}

func TestReviewArbitrationRejectsStaleDisposition(t *testing.T) {
	root, id, run, finding, first := arbitrationFixture(t, "rejected")
	second := first
	second.RecordID = "record-arbitration-2"
	second.PreviousRecordID = first.RecordID
	second.Basis = "additional repository evidence still leaves the finding disputed"
	s := transactionSnapshot(t, root, id)
	if err := SubmitReviewDisposition(root, second, s.Revision); err != nil {
		t.Fatal(err)
	}
	a := arbitrationFor(run, id, finding, first)
	if err := SubmitReviewArbitration(root, a); err == nil {
		t.Fatal("arbitration bound to stale disposition")
	}
}

func TestReviewArbitrationRejectsNonMustFixFinding(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "arbitration-low")
	gatePlan(t, root, []string{id}, archiveRequirements())
	input := archiveInput([]string{id}, "pmqa-arbitration-low", "PMQA")
	input.FindingsSchema = 1
	finding := ReviewFinding{ID: "PMQA-LOW", Tier: "low", Text: "minor defect", Evidence: "worker.go:9"}
	run := gateRun(t, root, input, ReviewFindings{Findings: []ReviewFinding{}, NonBlocking: []ReviewFinding{finding}})
	assignGate(t, root, run, map[string][]string{finding.ID: []string{id}})
	d := gateRecord(t, root, run, id, finding, "rejected")
	a := arbitrationFor(run, id, finding, d)
	if err := SubmitReviewArbitration(root, a); err == nil {
		t.Fatal("non-must-fix finding entered arbitration")
	}
}
