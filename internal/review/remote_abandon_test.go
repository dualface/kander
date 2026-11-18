//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

// The coordinator of a single-card batch is no holder: its abandon waits for the
// holder's confirmation, leaves a review request on the member card, and succeeds
// with the same arguments once the holder confirmed. The holder abandons directly.
func TestCoordinatorAbandonWaitsForTheHolder(t *testing.T) {
	r := newCoordinatedRun(t, 1)
	unlock, err := r.store.Lock("coordinated-run")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
	originals := map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}
	_, _, err = r.store.Prepare(r.input(), requirements, nil, originals, "test")
	r.awaitHolder(err)
	run, _, err := r.store.Prepare(r.input(), requirements, nil, originals, "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = r.store.Finalize(run, []byte(emptyStructuredReview)); err != nil {
		t.Fatal(err)
	}

	request := board.ReviewAbandonRequest{RunID: run.RunID, Decision: "user decision 2026-11-19", Reason: "the writer secret was lost"}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "abandon.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reviewRequests := func(session *remote.Session) int {
		t.Helper()
		requests, err := session.Project.Requests(t.Context(), r.ids[0])
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, request := range requests {
			if request.Kind == api.RequestReview {
				count++
			}
		}
		return count
	}
	before := reviewRequests(r.holder)
	_, err = remoteEvidenceAction(t.Context(), r.coordinate, "/host-b/clone", "abandon", input, nil)
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("want a wait for the holder: %v", err)
	}
	id := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(err.Error())
	if id == "" {
		t.Fatalf("wait omits the publication: %v", err)
	}
	if got := reviewRequests(r.holder); got != before+1 {
		t.Fatalf("the abandon left %d new review requests on the member card, want 1", got-before)
	}
	var stored board.ReviewRun
	if err := readRemoteResource(t.Context(), r.coordinate, "runs", run.RunID, &stored); err != nil || stored.Abandonment != nil {
		t.Fatalf("the run changed before the holder confirmed: %+v %v", stored, err)
	}
	if _, err := r.holder.ConfirmReviewPublication(t.Context(), id, r.ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteEvidenceAction(t.Context(), r.coordinate, "/host-b/clone", "abandon", input, nil); err != nil {
		t.Fatalf("the confirmed abandon was refused: %v", err)
	}
	if err := readRemoteResource(t.Context(), r.coordinate, "runs", run.RunID, &stored); err != nil || stored.Abandonment == nil || stored.Abandonment.Decision != request.Decision {
		t.Fatalf("the run is not abandoned: %+v %v", stored, err)
	}
}

func TestHolderAbandonsDirectlyThroughTheCommand(t *testing.T) {
	r := newCoordinatedRun(t, 1)
	unlock, err := r.store.Lock("coordinated-run")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
	originals := map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}
	_, _, err = r.store.Prepare(r.input(), requirements, nil, originals, "test")
	r.awaitHolder(err)
	run, _, err := r.store.Prepare(r.input(), requirements, nil, originals, "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = r.store.Finalize(run, []byte(emptyStructuredReview)); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(board.ReviewAbandonRequest{RunID: run.RunID, Decision: "user decision 2026-11-19", Reason: "the writer secret was lost"})
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "abandon.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	requests := func() int {
		list, err := r.holder.Project.Requests(t.Context(), r.ids[0])
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	before := requests()
	if _, err := remoteEvidenceAction(t.Context(), r.holder, "/host-a/clone", "abandon", input, nil); err != nil {
		t.Fatalf("the holder was refused: %v", err)
	}
	var stored board.ReviewRun
	if err := readRemoteResource(t.Context(), r.holder, "runs", run.RunID, &stored); err != nil || stored.Abandonment == nil {
		t.Fatalf("the run is not abandoned: %+v %v", stored, err)
	}
	if got := requests(); got != before {
		t.Fatalf("a direct abandon created %d requests", got-before)
	}
}

// With several live members a process that holds no member and coordinates no
// group is refused before any joint material or request exists; the coordinator
// of the group still gets its wait.
func TestStrangerCannotAskForAJointAbandon(t *testing.T) {
	r := newCoordinatedRunKeeping(t, 2, 2, true)
	unlock, err := r.store.Lock("coordinated-run")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
	originals := map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}
	input := r.input()
	input.TaskIDs = r.ids
	_, _, err = r.store.Prepare(input, requirements, nil, originals, "test")
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("want a wait for the holders: %v", err)
	}
	publication := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(err.Error())
	for _, id := range r.ids {
		if _, err := r.holder.ConfirmReviewPublication(t.Context(), publication, id); err != nil {
			t.Fatal(err)
		}
	}
	run, _, err := r.store.Prepare(input, requirements, nil, originals, "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = r.store.Finalize(run, []byte(emptyStructuredReview)); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(board.ReviewAbandonRequest{RunID: run.RunID, Decision: "user decision 2026-11-19", Reason: "the writer secret was lost"})
	if err != nil {
		t.Fatal(err)
	}
	abandonFile := filepath.Join(t.TempDir(), "abandon.json")
	if err := os.WriteFile(abandonFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	requests := func() int {
		total := 0
		for _, id := range r.ids {
			list, err := r.holder.Project.Requests(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			total += len(list)
		}
		return total
	}
	before := requests()
	_, err = remoteEvidenceAction(t.Context(), r.stranger, "/host-c/clone", "abandon", abandonFile, nil)
	if !api.IsCode(err, api.CodeHeldByOther) {
		t.Fatalf("a stranger was not refused: %v", err)
	}
	if got := requests(); got != before {
		t.Fatalf("a stranger created %d requests", got-before)
	}
	_, err = remoteEvidenceAction(t.Context(), r.coordinate, "/host-b/clone", "abandon", abandonFile, nil)
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("the coordinator was not asked to wait: %v", err)
	}
	if got := requests(); got != before+len(r.ids) {
		t.Fatalf("the coordinator's wait left %d requests, want one per holder", got-before)
	}
}
