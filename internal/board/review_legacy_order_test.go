package board

import (
	"strings"
	"testing"
)

func TestLegacyMappingIdentifiesUnmappedPredecessor(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "legacy-order")
	first := finalizedRun(t, root, archiveInput([]string{id}, "legacy-first", "PM"))
	publishRun(t, root, first.RunID)
	input := archiveInput([]string{id}, "legacy-second", "PM")
	input.PreviousRunID, input.ReviewedCommit = first.RunID, first.Commit
	second := finalizedRun(t, root, input)
	publishRun(t, root, second.RunID)
	mapping := func(run ReviewRun, findingID string) LegacyFindingMap {
		f := emptyFindings()
		f.NonBlocking = []ReviewFinding{{ID: findingID, Tier: "suggest", Text: "PASS", Evidence: "report.md:1"}}
		return LegacyFindingMap{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "human", Basis: "Complete original mapping", Complete: true, Findings: f, Locations: []LegacyFindingLocation{{FindingID: findingID, StartLine: 1, EndLine: 1, Quote: "PASS"}}}
	}
	next := mapping(second, "OLD-02")
	next.Findings.NonBlocking[0].Lineage = &FindingRef{RunID: first.RunID, FindingID: "OLD-01"}
	err := MapLegacyReview(root, next)
	if err == nil {
		t.Fatal("successor mapped without predecessor")
	}
	t.Logf("successor-first diagnostic: %v", err)
	if !strings.Contains(err.Error(), first.RunID) || !strings.Contains(err.Error(), "legacy mapping required") {
		t.Errorf("diagnostic does not identify unmapped predecessor: %v", err)
	}
	if err := MapLegacyReview(root, mapping(first, "OLD-01")); err != nil {
		t.Fatal(err)
	}
	if err := MapLegacyReview(root, next); err != nil {
		t.Fatal(err)
	}
	for _, run := range []ReviewRun{first, second} {
		data, err := ReadReviewOriginal(root, run.RunID, "report.md")
		if err != nil || string(data) != "PASS\n" {
			t.Fatalf("original %s changed: %q %v", run.RunID, data, err)
		}
	}
}
