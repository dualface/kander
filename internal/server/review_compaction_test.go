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
// removes the original it lists, as a compaction of a settled run would.
func markCompacted(t *testing.T, u unpublished, name string) {
	t.Helper()
	root, err := u.f.srv.BoardRoot(u.p.ID())
	if err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(root, ".kander", "groups")
	receipt := map[string]any{"schema": 1, "run_id": u.run.RunID, "task_ids": u.run.TaskIDs, "files": []map[string]any{{"location": "originals", "name": name, "bytes": 1}}, "compacted_at": "2026-11-27T00:00:00Z", "kander_version": "test"}
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
	if err = os.Remove(filepath.Join(control, "00000000-review-archive-group", "runs", u.run.RunID, "originals", name)); err != nil {
		t.Fatal(err)
	}
}

func compactionState(t *testing.T, p *client.Project, run string) bool {
	t.Helper()
	raw, err := p.ReviewEvidence(t.Context(), run, "compaction", "")
	if err != nil {
		t.Fatal(err)
	}
	var value api.ReviewCompaction
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value.Compacted
}

func TestCompactedRunReadsAndPublicationsCarryTheState(t *testing.T) {
	u := finalizedUnpublished(t)
	payload, err := json.Marshal(u.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := u.p.PreparePublication(t.Context(), api.PublicationPrepare{ID: "compaction-publish", Operation: "review-publish:" + u.run.RunID, Payload: payload, Members: []api.PublicationMember{{TaskID: u.grant.Task.TaskID, Revision: u.grant.Task.Revision, Generation: u.grant.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
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
}
