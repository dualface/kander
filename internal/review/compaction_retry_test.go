//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// A retry with the ID of a compacted run is refused before any reviewer
// starts, with the text that names the compaction; a receipt that cannot be
// validated refuses it too, never reading as "not compacted".
func TestRetryOfACompactedRunIsRefusedWithoutAReviewer(t *testing.T) {
	for name, tasks := range map[string]func(board.ReviewRun) []string{
		"valid receipt":   func(run board.ReviewRun) []string { return run.TaskIDs },
		"invalid receipt": func(board.ReviewRun) []string { return []string{"20990101-other-task"} },
	} {
		t.Run(name, func(t *testing.T) { retryCompactedRun(t, tasks) })
	}
}

func retryCompactedRun(t *testing.T, tasks func(board.ReviewRun) []string) {
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
	receipt, err := json.Marshal(map[string]any{"schema": 1, "run_id": run.RunID, "task_ids": tasks(run), "files": []map[string]any{{"location": "originals", "name": "evidence.txt", "bytes": 1}}, "compacted_at": "2026-11-27T00:00:00Z", "kander_version": "test"})
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
	want := config.Text("board.review_run_compacted", run.RunID)
	if !slices.Equal(tasks(run), run.TaskIDs) {
		want = "invalid compaction receipt"
	}
	if code != 2 || out != "" || !strings.Contains(stderr, want) {
		t.Fatalf("retry: %d %q %s", code, out, stderr)
	}
	if _, err = os.Stat(h.argvLog); !os.IsNotExist(err) {
		t.Fatalf("the retry started a reviewer: %v", err)
	}
}

// The same retries against a server: the client reads the run resource, which
// succeeds, and refuses from its compacted or compaction_error field.
func TestRemoteRetryOfACompactedRunIsRefusedWithoutAReviewer(t *testing.T) {
	for name, tasks := range map[string]func([]string) []string{
		"valid receipt":   func(ids []string) []string { return ids },
		"invalid receipt": func([]string) []string { return []string{"20990101-other-task"} },
	} {
		t.Run(name, func(t *testing.T) {
			h, session, task, requirements := remoteReviewHarness(t, nil)
			t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
			args := []string{"codex", "--task", task.TaskID, "--run-id", "remote-run", "--batch-id", "remote-batch", "--requirements-file", requirements, h.repo, h.base, h.head, "PMQA", "Frozen remote task"}
			if code, _, stderr := captureRun(t, args); code != 0 {
				t.Fatalf("first review: %d %s", code, stderr)
			}
			ids := []string{task.TaskID}
			writeServerReceipt(t, filepath.Join(h.root, "server"), "remote-run", tasks(ids))
			raw, err := session.Project.ReviewResource(t.Context(), "runs", "remote-run")
			if err != nil {
				t.Fatalf("the run resource failed: %v", err)
			}
			var state struct {
				RunID string `json:"run_id"`
			}
			if err = json.Unmarshal(raw, &state); err != nil || state.RunID != "remote-run" {
				t.Fatalf("run resource: %s %v", raw, err)
			}
			if err = os.Remove(h.argvLog); err != nil {
				t.Fatal(err)
			}
			want := config.Text("board.review_run_compacted", "remote-run")
			if !slices.Equal(tasks(ids), ids) {
				want = "invalid compaction receipt"
			}
			code, out, stderr := captureRun(t, args)
			if code != 2 || out != "" || !strings.Contains(stderr, want) {
				t.Fatalf("retry: %d %q %s", code, out, stderr)
			}
			if _, err = os.Stat(h.argvLog); !os.IsNotExist(err) {
				t.Fatalf("the retry started a reviewer: %v", err)
			}
		})
	}
}

// writeServerReceipt writes a compaction receipt for run into the one board
// below the server data directory that holds the run.
func writeServerReceipt(t *testing.T, data, run string, tasks []string) {
	t.Helper()
	var root string
	suffix := filepath.Join(".kander", "groups", "00000000-review-archive-group", "runs", run)
	err := filepath.WalkDir(data, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && strings.HasSuffix(path, string(filepath.Separator)+suffix) {
			root = strings.TrimSuffix(path, string(filepath.Separator)+suffix)
			return filepath.SkipAll
		}
		return err
	})
	if err != nil || root == "" {
		t.Fatalf("no board holds %s: %v", run, err)
	}
	receipt, err := json.Marshal(map[string]any{"schema": 1, "run_id": run, "task_ids": tasks, "files": []map[string]any{}, "compacted_at": "2026-11-27T00:00:00Z", "kander_version": "test"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".kander", "groups", "00000000-prune-group", "compactions")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, run+".json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
}
