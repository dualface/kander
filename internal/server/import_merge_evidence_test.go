package server

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/board/boardtest"
	"github.com/dualface/kander/v2/internal/client"
)

func TestImportMergeKeepsImmutableReviewEvidence(t *testing.T) {
	target, _ := newImportFixture(t, "existing")
	source := v1Board(t)
	id := workingImportTask(t, source)
	review := boardtest.ReviewedFinding(t, source, id)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	project, _ := importProject(t, s, target)
	result, err := s.Import("demo", "", source)
	if err != nil || result.AddedTasks != 2 {
		t.Fatalf("merge evidence: %+v %v", result, err)
	}
	origin := scanImportTestTree(t, source)
	destination := scanImportTestTree(t, project.board)
	for _, rel := range origin.files {
		key := board.ImportRecord(rel)
		if key.Kind == "review-run" || key.Kind == "review-batch" || key.Kind == "review-plan" ||
			key.Kind == "review-task" || key.Kind == "operation" {
			if destination.sums[rel] != origin.sums[rel] {
				t.Errorf("lost original record %s", rel)
			}
		}
	}
	original, err := board.ReadReviewOriginal(source, review.RunID, "report.md")
	if err != nil {
		t.Fatal(err)
	}
	merged, err := board.ReadReviewOriginal(project.board, review.RunID, "report.md")
	if err != nil || !bytes.Equal(original, merged) {
		t.Fatalf("imported report bytes differ: %v", err)
	}
	checked, err := board.CheckBoardForImportProblems(project.board)
	if err != nil || len(checked.Blocking) != 0 || len(checked.Warnings) != 0 {
		t.Fatalf("imported evidence did not check: %+v %v", checked, err)
	}
}

func TestImportMergeDoesNotRewriteExistingClaims(t *testing.T) {
	target := v1Board(t)
	source, _ := newImportFixture(t, "incoming")
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	s, h := openTest(t, t.TempDir(), func() time.Time { return time.Unix(0, now.Load()) })
	defer h.Close()
	defer s.Close()
	project, initial := importProject(t, s, target)
	id := workingImportTask(t, target)
	c, _ := client.New(h.URL, "host-a")
	remote := c.Project(initial.Project.ID)
	if _, err := remote.Takeover(t.Context(), "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-a", Owner: "codex", Decision: "user:test"}); err != nil {
		t.Fatal(err)
	}
	before := scanImportTestTree(t, project.board)
	// Import is not a restart and must not extend another execution's lease.
	now.Add(int64(time.Second))
	result, err := s.Import("demo", "", source)
	if err != nil || result.AddedTasks != 1 {
		t.Fatalf("merge: %+v %v", result, err)
	}
	assertOriginalImportFiles(t, before, scanImportTestTree(t, project.board))
}

func workingImportTask(t *testing.T, root string) string {
	t.Helper()
	view, err := board.InspectImportCards(root)
	if err != nil {
		t.Fatal(err)
	}
	for id, entry := range view.Entries {
		if entry.State == "working" {
			return id
		}
	}
	t.Fatal("fixture has no working task")
	return ""
}
