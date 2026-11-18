package server_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// A joint publication with one member is the path a group coordinator uses for
// a single-card batch: the holder confirms, a client without the card token
// publishes. These tests pin that the server accepts it unchanged.

// sameJSON compares values, not bytes: the saved receipt is stored compact.
func sameJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

func TestSingleMemberPlanPublicationPublishesWithoutCardToken(t *testing.T) {
	f := newFixture(t)
	holder, coordinator := f.project("host-a"), f.project("coordinator")
	task := todoTask(t, holder, "single-plan", "20261118-single-group")
	grant, err := claim(t, holder, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	plan := board.ReviewPlan{Schema: 1, PlanID: "single-plan", Author: "coordinator", Basis: "single member fixture", CWD: "project:" + holder.ID(), ReportLanguage: "en", TaskIDs: []string{task.TaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "single-batch", TaskIDs: []string{task.TaskID}, Base: "N/A", TargetCommit: "N/A", Requirements: map[string]string{"PMQA": "N/A: publication fixture", "Security": "N/A: repository policy"}}}}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := coordinator.PreparePublication(t.Context(), api.PublicationPrepare{ID: "single-plan-material", Operation: "review-plan", Payload: payload, Members: []api.PublicationMember{{TaskID: task.TaskID, Revision: grant.Task.Revision, Generation: grant.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	proof := &client.Claim{PublicationID: publication.ID}
	_, err = coordinator.ReviewPlan(t.Context(), "single-unconfirmed", plan, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)

	if _, err := holder.ConfirmPublication(t.Context(), publication.ID, api.PublicationApproval{TaskID: task.TaskID, Revision: grant.Task.Revision, Generation: grant.Claim.Generation, Digest: publication.Digest}, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	first, err := coordinator.ReviewPlan(t.Context(), "single-publish", plan, proof)
	if err != nil {
		t.Fatalf("one-member publication was refused: %v", err)
	}
	published, err := coordinator.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := coordinator.ReviewPlan(t.Context(), "single-retry", plan, proof)
	if err != nil || !sameJSON(t, first, again) {
		t.Fatalf("retry did not return the saved result: first=%s again=%s %v", first, again, err)
	}
	after, err := coordinator.Show(t.Context(), task.TaskID)
	if err != nil || after.Revision != published.Revision {
		t.Fatalf("retry changed the card revision: %+v %v", after, err)
	}
}

func TestSingleMemberRunPublicationPublishesWithoutCardToken(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	grant, run, direct := prepareRuntime(t, f, p)
	finalization := runtimeFinalization(run)
	if _, err := p.ReviewRuntime(t.Context(), "single-finalize", run.RunID, "finalize", finalization, &direct); err != nil {
		t.Fatal(err)
	}
	before, err := p.Show(t.Context(), grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := p.PreparePublication(t.Context(), api.PublicationPrepare{ID: "single-run-material", Operation: "review-publish:" + run.RunID, Payload: payload, Members: []api.PublicationMember{{TaskID: grant.Task.TaskID, Revision: before.Revision, Generation: grant.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	// The client keeps the run's private writer but presents no card token.
	proof := &client.Claim{PublicationID: publication.ID, ReviewWriter: direct.ReviewWriter}
	_, err = p.ReviewRuntime(t.Context(), "single-run-unconfirmed", run.RunID, "publish", map[string]string{}, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)

	if _, err := p.ConfirmPublication(t.Context(), publication.ID, api.PublicationApproval{TaskID: grant.Task.TaskID, Revision: before.Revision, Generation: grant.Claim.Generation, Digest: publication.Digest}, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	first, err := p.ReviewRuntime(t.Context(), "single-run-publish", run.RunID, "publish", map[string]string{}, proof)
	if err != nil {
		t.Fatalf("one-member run publication was refused: %v", err)
	}
	published, err := p.Show(t.Context(), grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := board.ParseReviewIndexes(published.Text)
	if err != nil || len(indexes) != 1 || published.Revision != before.Revision+1 {
		t.Fatalf("run was not published once: %+v %v", indexes, err)
	}
	again, err := p.ReviewRuntime(t.Context(), "single-run-retry", run.RunID, "publish", map[string]string{}, proof)
	if err != nil || !sameJSON(t, first, again) {
		t.Fatalf("retry did not return the saved result: first=%s again=%s %v", first, again, err)
	}
	after, err := p.Show(t.Context(), grant.Task.TaskID)
	if err != nil || after.Revision != published.Revision {
		t.Fatalf("retry changed the card revision: %+v %v", after, err)
	}
}
