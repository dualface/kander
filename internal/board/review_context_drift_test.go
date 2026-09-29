package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func driftOriginals() map[string][]byte {
	originals := archiveOriginals()
	originals["task-context.md"] = []byte("原任务\r\n\n## REVIEWS\n\n- appended index\n")
	return originals
}

func frozenSnapshotPath(t *testing.T, root, id, runID string) string {
	t.Helper()
	return filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "reviews", runID, "task-context.md")
}

// A later run of the batch that passes a drifted task context names the frozen snapshot, which hashes to
// the batch value, and passing that snapshot clears the binding check.
func TestReviewTaskContextDriftNamesFrozenSnapshot(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "drift-snapshot")
	first := finalizedRun(t, root, archiveInput([]string{id}, "drift-first", "PMQA"))
	publishRun(t, root, first.RunID)

	_, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "drift-next", "PMQA"), nil, nil, driftOriginals(), "test")
	want := frozenSnapshotPath(t, root, id, first.RunID)
	if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "batch") || strings.Contains(err.Error(), "batch binding conflict") {
		t.Fatalf("drift error = %v, want the snapshot %s", err, want)
	}
	if _, exists, err := LookupReviewRun(root, "drift-next"); err != nil || exists {
		t.Fatalf("drifted run persisted: %v %v", exists, err)
	}
	snapshot, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := ReadReviewBatch(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if ReviewDigest(snapshot) != batch.TaskContextHash {
		t.Fatalf("snapshot hash %s, batch %s", ReviewDigest(snapshot), batch.TaskContextHash)
	}

	originals := archiveOriginals()
	originals["task-context.md"] = snapshot
	if _, fresh, err := PrepareReviewRun(root, archiveInput([]string{id}, "drift-next", "PMQA"), nil, nil, originals, "test"); err != nil || !fresh {
		t.Fatalf("frozen snapshot rejected: %v %v", fresh, err)
	}
}

// The named previous run wins when it qualifies; otherwise the earliest finalized run is used.
func TestReviewTaskContextDriftPrefersPreviousRun(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "drift-previous")
	early := finalizedRun(t, root, archiveInput([]string{id}, "drift-early", "PMQA"))
	publishRun(t, root, early.RunID)
	late := finalizedRun(t, root, archiveInput([]string{id}, "drift-late", "PMQA"))
	publishRun(t, root, late.RunID)

	_, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "drift-plain", "PMQA"), nil, nil, driftOriginals(), "test")
	if want := frozenSnapshotPath(t, root, id, early.RunID); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("without a previous run: %v, want %s", err, want)
	}

	input := archiveInput([]string{id}, "drift-incremental", "PMQA")
	input.PreviousRunID = late.RunID
	input.ReviewedCommit = late.Commit
	_, _, err = PrepareReviewRun(root, input, nil, nil, driftOriginals(), "test")
	if want := frozenSnapshotPath(t, root, id, late.RunID); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("with a previous run: %v, want %s", err, want)
	}
}

// Without a finalized, published run there is no snapshot to name, so the error asks for a new batch.
func TestReviewTaskContextDriftWithoutSnapshot(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "drift-none")
	if _, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "drift-prepared", "PMQA"), archiveRequirements(), nil, archiveOriginals(), "test"); err != nil {
		t.Fatal(err)
	}
	_, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "drift-after", "PMQA"), nil, nil, driftOriginals(), "test")
	if err == nil || !strings.Contains(err.Error(), "batch") || strings.Contains(err.Error(), "task-context.md") || strings.Contains(err.Error(), "batch binding conflict") {
		t.Fatalf("no-snapshot error = %v", err)
	}
	if _, exists, err := LookupReviewRun(root, "drift-after"); err != nil || exists {
		t.Fatalf("drifted run persisted: %v %v", exists, err)
	}
}

// Any other binding mismatch still reports the generic conflict, even when the task context also drifted.
func TestReviewOtherBindingConflictsUnchanged(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "drift-other")
	first := finalizedRun(t, root, archiveInput([]string{id}, "other-first", "PMQA"))
	publishRun(t, root, first.RunID)
	changed := map[string]string{"PMQA": "required", "Security": "required"}
	for name, mutate := range map[string]func(*ReviewInput, *map[string]string){
		"base":         func(in *ReviewInput, _ *map[string]string) { in.Base = strings.Repeat("c", 40) },
		"requirements": func(_ *ReviewInput, req *map[string]string) { *req = changed },
	} {
		for _, originals := range []map[string][]byte{archiveOriginals(), driftOriginals()} {
			input := archiveInput([]string{id}, "other-"+name, "PMQA")
			var requirements map[string]string
			mutate(&input, &requirements)
			_, _, err := PrepareReviewRun(root, input, requirements, nil, originals, "test")
			if err == nil || !strings.Contains(err.Error(), "batch binding conflict") {
				t.Fatalf("%s mismatch: %v", name, err)
			}
		}
	}
}
