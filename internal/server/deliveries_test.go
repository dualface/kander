package server_test

import (
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

// stageDelivery supplies authenticated producer facts to service-only tests.
// Actual push/fetch behavior is covered by the independent-clone Git test.
func stageDelivery(t *testing.T, project *client.Project, grant api.ClaimGrant, commit string) api.DeliveryEvidence {
	t.Helper()
	card, err := project.Show(t.Context(), grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	value := api.DeliveryEvidence{TaskID: card.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, CardRevision: card.Revision, Commit: commit, Ref: api.DeliveryRef(project.ID(), card.TaskID, grant.Claim.Generation, commit), Remote: "git@example.invalid:demo.git", ObservedAt: time.Now().UTC()}
	value, err = project.PublishDelivery(t.Context(), "", value, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDeliveryIsBoundToContentAndExecution(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	card := todoTask(t, a, "delivery-fence")
	grant, err := claim(t, a, card.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	commit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	value := stageDelivery(t, a, grant, commit)
	_, err = a.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "progress.md", Text: "Later content", ExpectedRevision: value.CardRevision}, &proof)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Move(t.Context(), "stale-delivery", card.TaskID, api.Move{State: "review", DeliveryCommit: commit}, &proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	f.clock.advance(91 * time.Second)
	successor, err := b.Takeover(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "explicit takeover", ExpectedGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.PublishDelivery(t.Context(), "late-delivery", value, proof)
	if !api.IsCode(err, api.CodeHeldByOther) && !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("late upload: %v", err)
	}
	_, err = b.Move(t.Context(), "old-generation-delivery", card.TaskID, api.Move{State: "review", DeliveryCommit: commit}, &client.Claim{Token: successor.Token, Generation: successor.Claim.Generation})
	expectCode(t, err, api.CodeEvidenceIncomplete)
}
