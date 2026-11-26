//go:build unix

package review

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/remote"
)

// abandonRequest is the request `kander review abandon` reads for run.
func abandonRequest(run string) board.ReviewAbandonRequest {
	return board.ReviewAbandonRequest{RunID: run, Decision: "user decision 2026-11-19", Reason: "the writer secret was lost"}
}

// The command entry (Run, runDispositionMode, runRemoteDisposition) abandons a
// finalized run of a card that has no plan: the stray run a planless card can open
// through the same entry as every other review.
func TestAbandonRunsThroughTheCommandEntry(t *testing.T) {
	w := newRemoteWorld(t)
	store, err := newRemoteReviewStore(w.session)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := store.Lock("stray-lost")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	input := board.ReviewInput{RunID: "stray-lost", BatchID: "stray", TaskIDs: []string{w.task}, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", Base: w.h.base, Commit: w.h.head, ReportLanguage: "en"}
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
	originals := map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}
	run, _, err := store.Prepare(input, requirements, nil, originals, "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = store.Finalize(run, []byte(emptyStructuredReview)); err != nil {
		t.Fatal(err)
	}
	var result struct {
		RunID string `json:"run_id"`
	}
	out := commandOK(t, "abandon", w.h.repo, dispositionJSON(t, w.h, "abandon", abandonRequest(run.RunID)))
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.RunID != run.RunID {
		t.Fatalf("the command did not name the abandoned run: %q %v", out, err)
	}
	var stored board.ReviewRun
	if err := readRemoteResource(t.Context(), w.session, "runs", run.RunID, &stored); err != nil || stored.Abandonment == nil {
		t.Fatalf("the run is not abandoned: %+v %v", stored, err)
	}
}

// Without a configured project the local board has no abandon: the command fails
// before it reads the request.
func TestAbandonNeedsAConfiguredProject(t *testing.T) {
	h := newCodexHarness(t)
	t.Chdir(h.repo)
	code, _, stderr := captureRun(t, []string{"abandon", h.repo, dispositionJSON(t, h, "abandon", abandonRequest("stray-lost"))})
	if code == 0 || stderr == "" {
		t.Fatalf("a local abandon was accepted: %d %q", code, stderr)
	}
	if want := remote.ErrNotConfigured.Error(); !strings.Contains(stderr, want) {
		t.Fatalf("the failure does not say %q: %q", want, stderr)
	}
}
