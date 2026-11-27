package server_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// markCompacted gives a finalized run the receipt compaction writes and
// removes the original and the inputs it lists, as a compaction of a settled
// run would.
func markCompacted(t *testing.T, u unpublished, name string) {
	t.Helper()
	root, err := u.f.srv.BoardRoot(u.p.ID())
	if err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(root, ".kander", "groups")
	receipt := map[string]any{"schema": 1, "run_id": u.run.RunID, "task_ids": u.run.TaskIDs, "files": []map[string]any{{"location": "inputs", "name": "*", "bytes": 1}, {"location": "originals", "name": name, "bytes": 1}}, "compacted_at": "2026-11-27T00:00:00Z", "kander_version": "test"}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(control, "00000000-prune-group", "compactions")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, u.run.RunID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(control, "00000000-review-archive-group", "runs", u.run.RunID)
	if err = os.Remove(filepath.Join(run, "originals", name)); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(filepath.Join(run, "inputs")); err != nil {
		t.Fatal(err)
	}
}

func compactionState(t *testing.T, p *client.Project, run string) bool {
	t.Helper()
	raw, err := p.ReviewResource(t.Context(), "runs", run)
	if err != nil {
		t.Fatal(err)
	}
	var value api.ReviewRunState
	if err = json.Unmarshal(raw, &value); err != nil || value.RunID != run {
		t.Fatalf("run resource: %+v %v", value.ReviewRun, err)
	}
	return value.Compacted
}

func TestCompactedRunReadsAndPublicationsCarryTheState(t *testing.T) {
	u := finalizedUnpublished(t)
	payload, err := json.Marshal(u.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	members := []api.PublicationMember{{TaskID: u.grant.Task.TaskID, Revision: u.grant.Task.Revision, Generation: u.grant.Claim.Generation}}
	prepared, err := u.p.PreparePublication(t.Context(), api.PublicationPrepare{ID: "compaction-publish", Operation: "review-publish:" + u.run.RunID, Payload: payload, Members: members})
	if err != nil {
		t.Fatal(err)
	}
	// Materials that name the run deeper: the conclusion of a joint close, and
	// the predecessor of an incremental prepare.
	nested := map[string]string{
		"review-close:runtime-batch": `{"request":{"batch_id":"runtime-batch","roles":{"PMQA":{"run_id":"` + u.run.RunID + `","passed_at":"b","basis":"x"}}},"evidence":{}}`,
		"review-prepare":             `{"input":{"run_id":"runtime-next","previous_run_id":"` + u.run.RunID + `"}}`,
		// A failed run that a close resolves names the compacted run as its key.
		"review-close:failed-batch": `{"request":{"batch_id":"failed-batch","resolved_failures":{"` + u.run.RunID + `":"runtime-replacement"}},"evidence":{}}`,
	}
	var nestedIDs []string
	for operation, material := range nested {
		p, err := u.p.PreparePublication(t.Context(), api.PublicationPrepare{ID: "compaction-" + strings.NewReplacer(":", "-").Replace(operation), Operation: operation, Payload: json.RawMessage(material), Members: members})
		if err != nil {
			t.Fatal(err)
		}
		nestedIDs = append(nestedIDs, p.ID)
	}
	_, err = u.p.ReviewEvidence(t.Context(), u.run.RunID, "input", "evidence.txt")
	if err == nil || api.IsCode(err, api.CodeReviewCompacted) {
		t.Fatalf("an uncompacted stored input: %v", err)
	}
	before, err := u.p.Publication(t.Context(), prepared.ID)
	if err != nil || before.CompactedRuns != nil || compactionState(t, u.p, u.run.RunID) {
		t.Fatalf("an uncompacted run is marked: %+v %v", before.CompactedRuns, err)
	}
	if raw, err := json.Marshal(before); err != nil || strings.Contains(string(raw), "compacted_runs") {
		t.Fatalf("an uncompacted publication names the field: %s %v", raw, err)
	}

	markCompacted(t, u, "error.log")
	if !compactionState(t, u.p, u.run.RunID) {
		t.Fatal("the compacted run is not reported")
	}
	_, err = u.p.ReviewEvidence(t.Context(), u.run.RunID, "original", "error.log")
	expectCode(t, err, api.CodeReviewCompacted)
	if _, err = u.p.ReviewEvidence(t.Context(), u.run.RunID, "original", "stdout.log"); err != nil {
		t.Fatalf("an unlisted original: %v", err)
	}
	after, err := u.p.Publication(t.Context(), prepared.ID)
	if err != nil || !slices.Equal(after.CompactedRuns, []string{u.run.RunID}) {
		t.Fatalf("publication state: %+v %v", after.CompactedRuns, err)
	}
	if after.Digest != before.Digest {
		t.Fatal("the compaction state changed the material")
	}
	for _, id := range nestedIDs {
		if got, err := u.p.Publication(t.Context(), id); err != nil || !slices.Equal(got.CompactedRuns, []string{u.run.RunID}) {
			t.Fatalf("%s: %+v %v", id, got.CompactedRuns, err)
		}
	}
	for _, name := range []string{"evidence.txt", "task-context.md"} {
		_, err = u.p.ReviewEvidence(t.Context(), u.run.RunID, "input", name)
		expectCode(t, err, api.CodeReviewCompacted)
	}
}

// A receipt that cannot be validated never hides the run's metadata, and the
// compaction state it cannot prove is reported as an error.
func TestRunResourceKeepsMetadataWhenTheReceiptIsInvalid(t *testing.T) {
	u := finalizedUnpublished(t)
	markCompacted(t, u, "error.log")
	root, err := u.f.srv.BoardRoot(u.p.ID())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".kander", "groups", "00000000-prune-group", "compactions", u.run.RunID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err = json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt["task_ids"] = []string{"20990101-other-task"}
	if data, err = json.Marshal(receipt); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := u.p.ReviewResource(t.Context(), "runs", u.run.RunID)
	if err != nil {
		t.Fatalf("an invalid receipt hid the run: %v", err)
	}
	var value api.ReviewRunState
	if err = json.Unmarshal(raw, &value); err != nil || value.RunID != u.run.RunID || value.Phase != u.run.Phase {
		t.Fatalf("run metadata: %+v %v", value.ReviewRun, err)
	}
	if value.Compacted || !strings.Contains(value.CompactionError, "invalid compaction receipt") {
		t.Fatalf("compaction state: compacted=%v error=%q", value.Compacted, value.CompactionError)
	}
	// A close material that resolves the failed run keeps its material too.
	material := `{"request":{"batch_id":"failed-batch","resolved_failures":{"` + u.run.RunID + `":"runtime-replacement"}},"evidence":{}}`
	members := []api.PublicationMember{{TaskID: u.grant.Task.TaskID, Revision: u.grant.Task.Revision, Generation: u.grant.Claim.Generation}}
	prepared, err := u.p.PreparePublication(t.Context(), api.PublicationPrepare{ID: "compaction-invalid-close", Operation: "review-close:failed-batch", Payload: json.RawMessage(material), Members: members})
	if err != nil {
		t.Fatal(err)
	}
	got, err := u.p.Publication(t.Context(), prepared.ID)
	if err != nil {
		t.Fatalf("an invalid receipt hid the publication: %v", err)
	}
	if got.Digest != prepared.Digest || len(got.CompactedRuns) != 0 || !strings.Contains(got.CompactionErrors[u.run.RunID], "invalid compaction receipt") {
		t.Fatalf("publication: digest %s, compacted %v, errors %v", got.Digest, got.CompactedRuns, got.CompactionErrors)
	}
}
