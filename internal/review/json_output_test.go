//go:build unix

package review

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/command/commandtest"
)

func jsonObject(t *testing.T, text string) map[string]any {
	t.Helper()
	var object map[string]any
	if !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 1 || json.Unmarshal([]byte(text), &object) != nil {
		t.Fatalf("not one JSON object and a newline: %q", text)
	}
	return object
}

// A review run with --json is one object that holds the report, with nothing on stderr; a run
// that failed before it published evidence is the error object.
func TestReviewRunWithJSON(t *testing.T) {
	h := newCodexHarness(t)
	code, stdout, stderr := h.review("codex", "PMQA", "确认改动正确", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	result := jsonObject(t, stdout)
	if result["exit_code"] != float64(0) || strings.TrimSpace(result["report"].(string)) == "" {
		t.Fatalf("result=%v", result)
	}
	// A role that does not exist is an argument error before any reviewer starts: the usage error object, the status of the text form.
	code, stdout, stderr = h.review("codex", "Nobody", "确认改动正确", "--json")
	if code != 2 || stdout != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	detail := jsonObject(t, stderr)["error"].(map[string]any)
	if detail["code"] != "usage" || !strings.Contains(detail["message"].(string), "Nobody") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestReviewSchemaWithJSON(t *testing.T) {
	newCodexHarness(t)
	code, stdout, stderr := captureRun(t, []string{"plan", "--schema", "--json"})
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	result := jsonObject(t, stdout)
	if result["command"] != "plan" || !strings.HasPrefix(result["schema"].(string), "command: plan\n") {
		t.Fatalf("result=%v", result)
	}
}

// A batch aggregated with --json closes with the view_hash of the printed object: the compact
// line is not the text the digest is taken over, so the object carries the digest itself.
func TestAggregateJSONViewHashClosesTheBatch(t *testing.T) {
	w := newRemoteWorld(t)
	w.plan(t, true, w.batch("batch", "", w.h.head, w.h.head))
	code, out, stderr := captureRun(t, []string{"aggregate", w.h.repo, "batch", "--json"})
	var printed struct {
		ViewHash string `json:"view_hash"`
		Batch    struct {
			Revision uint64 `json:"revision"`
		} `json:"batch"`
	}
	if err := json.Unmarshal([]byte(out), &printed); code != 0 || stderr != "" || err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("aggregate --json: %d %q %q %v", code, out, stderr, err)
	}
	if want := board.ReviewViewDigest(w.view(t, "batch")); printed.ViewHash != want {
		t.Fatalf("view_hash %q, want %q", printed.ViewHash, want)
	}
	// The digest of the compact line is not the one close compares.
	if board.ReviewDigest([]byte(out)) == printed.ViewHash {
		t.Fatal("the compact line hashes to the view digest")
	}
	r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: printed.Batch.Revision, ViewHash: printed.ViewHash, Author: "owner", Roles: map[string]board.ReviewRoleConclusion{}}
	closeJSON := commandOK(t, "close", w.h.repo, dispositionJSON(t, w.h, "close-json", r))
	if closureOf(t, closeJSON).BatchID != "batch" {
		t.Fatalf("close: %s", closeJSON)
	}
}

// A run that was only prepared in the archive is not published evidence: a failure after the
// preparation is the error object, with the run in its diagnostic, while a run whose evidence
// reached the cards is the result object with the status of the run.
func TestReviewExitSeparatesPreparedFromPublished(t *testing.T) {
	exit := func(code int, publish bool) (int, string, string) {
		c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
			resetJSONRun()
			var run board.ReviewRun
			run.RunID, run.BatchID, run.ExecutionStatus = "early-run", "batch", "ok"
			recordRun(run)
			if publish {
				markPublished()
			}
			userError("publication failed")
			return command.Exit(reviewExit(code))
		}}
		return commandtest.Capture(func() int { return commandtest.Run(c, "--json") })
	}
	code, stdout, stderr := exit(2, false)
	if code != 2 || stdout != "" {
		t.Fatalf("prepared only: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	detail := jsonObject(t, stderr)["error"].(map[string]any)
	if detail["code"] != "review_failed" || !strings.Contains(detail["message"].(string), "publication failed") || !strings.Contains(detail["message"].(string), "early-run") {
		t.Fatalf("prepared only: stderr=%q", stderr)
	}
	code, stdout, _ = exit(1, true)
	if result := jsonObject(t, stdout); code != 1 || result["run_id"] != "early-run" || result["exit_code"] != float64(1) {
		t.Fatalf("published: code=%d stdout=%q", code, stdout)
	}
}
