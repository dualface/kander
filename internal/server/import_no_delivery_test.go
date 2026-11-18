package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

// A v1 board whose unfinished card holds the closure of a no-delivery batch
// imports into a project, and the imported board checks clean: the closure
// keeps its no_delivery marker and its observed HEAD through the import.
func TestImportKeepsANoDeliveryClosureOfAnUnfinishedCard(t *testing.T) {
	source := v1Board(t)
	matches, err := filepath.Glob(filepath.Join(source, "working", "*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("working fixture: %v %v", matches, err)
	}
	id := filepath.Base(matches[0])
	target, observed := strings.Repeat("a", 40), strings.Repeat("c", 40)
	roles := map[string]string{"PMQA": "N/A: no delivery commit", "Security": "N/A: no delivery commit"}
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan", Author: "coordinator", Basis: "the card has no delivery commit", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: target, TargetCommit: target, Requirements: roles}}}
	if err := board.CreateReviewPlan(source, plan); err != nil {
		t.Fatal(err)
	}
	view, err := board.ReadReviewBatchView(source, "batch")
	if err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: "/repo", Head: target, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges, NoDelivery: true, ObservedHead: observed}
	if _, err := board.CloseReviewBatch(source, request, evidence); err != nil {
		t.Fatal(err)
	}
	if code, _, problems, err := board.CheckBoardForImport(source); err != nil || code != 0 {
		t.Fatalf("the source does not check: %d %v %v", code, problems, err)
	}

	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	result, err := s.Import("demo", "", source)
	if err != nil || result.Existing {
		t.Fatalf("import: %+v %v", result, err)
	}
	proj, err := s.project(result.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, problems, err := board.CheckBoard(proj.board, []string{id}, false); err != nil || code != 0 {
		t.Fatalf("the imported board does not check: %d %v %v", code, problems, err)
	}
	progress, err := board.ReviewTaskProgress(proj.board, id)
	if err != nil || progress.Status != "closed" {
		t.Fatalf("progress: %+v %v", progress, err)
	}
	closed, err := os.ReadFile(filepath.Join(proj.board, "working", id, "reviews", "batches", "batch", "closed.json"))
	if err != nil || !strings.Contains(string(closed), `"no_delivery": true`) || !strings.Contains(string(closed), `"observed_head": "`+observed+`"`) {
		t.Fatalf("the imported closure lost its marker: %v\n%s", err, closed)
	}
}
