//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// A retry with the ID of a compacted run is refused before any reviewer
// starts, with the text that names the compaction.
func TestRetryOfACompactedRunIsRefusedWithoutAReviewer(t *testing.T) {
	h, root, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	if code, _, stderr := captureRun(t, args); code != 0 {
		t.Fatalf("first review: %d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(root, ".kander", "groups")
	receipt, err := json.Marshal(map[string]any{"schema": 1, "run_id": run.RunID, "task_ids": run.TaskIDs, "files": []map[string]any{{"location": "originals", "name": "evidence.txt", "bytes": 1}}, "compacted_at": "2026-11-27T00:00:00Z", "kander_version": "test"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(control, "00000000-prune-group", "compactions")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, run.RunID+".json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(control, "00000000-review-archive-group", "runs", run.RunID, "originals", "evidence.txt")); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(h.argvLog); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := captureRun(t, args)
	if code != 2 || out != "" || !strings.Contains(stderr, config.Text("board.review_run_compacted", run.RunID)) {
		t.Fatalf("retry: %d %q %s", code, out, stderr)
	}
	if _, err = os.Stat(h.argvLog); !os.IsNotExist(err) {
		t.Fatalf("the retry started a reviewer: %v", err)
	}
}
