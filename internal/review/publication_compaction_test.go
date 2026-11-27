package review

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/dualface/kander/v2/api"
)

func compactionView(t *testing.T, publication api.Publication) map[string]any {
	t.Helper()
	view, err := newPublicationView(publication, func(api.PublicationMember) (bool, error) { return false, nil })
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

// The view marks the runs the server reports compacted, and prints nothing new
// for a material whose runs are not compacted.
func TestPublicationViewMarksCompactedRuns(t *testing.T) {
	publication := api.Publication{PublicationPrepare: api.PublicationPrepare{ID: "publication-1", Operation: "review-publish:run-1", Payload: json.RawMessage(`"run-1"`), Members: []api.PublicationMember{{TaskID: "20261127-view-task", Revision: 1, Generation: 1}}}, Status: "prepared", Digest: "digest"}
	plain := compactionView(t, publication)
	if _, present := plain["compacted_runs"]; present {
		t.Fatalf("an uncompacted material prints compacted_runs: %v", plain)
	}
	publication.CompactionErrors = map[string]string{"run-2": "invalid compaction receipt"}
	if errs, _ := compactionView(t, publication)["compaction_errors"].(map[string]any); errs["run-2"] != "invalid compaction receipt" {
		t.Fatalf("compaction_errors = %v", errs)
	}
	publication.CompactionErrors = nil
	publication.CompactedRuns = []string{"run-1"}
	marked := compactionView(t, publication)
	runs, _ := marked["compacted_runs"].([]any)
	if !slices.Equal(runs, []any{"run-1"}) {
		t.Fatalf("compacted_runs = %v", marked["compacted_runs"])
	}
	delete(marked, "compacted_runs")
	left, _ := json.Marshal(plain)
	right, _ := json.Marshal(marked)
	if string(left) != string(right) {
		t.Fatalf("the mark changed other fields:\n%s\n%s", left, right)
	}
}
