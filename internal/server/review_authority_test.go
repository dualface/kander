package server_test

import (
	"encoding/json"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func TestReviewPlanUsesIndependentProofsAndStableProjectIdentity(t *testing.T) {
	fixture := newFixture(t)
	project := fixture.project("host-a")
	first := todoTask(t, project, "review-first", "20261112-joint-group")
	second := todoTask(t, project, "review-second", "20261112-joint-group")
	one, err := project.Claim(t.Context(), "", first.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-first", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := project.Claim(t.Context(), "", second.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-second", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	requirements := map[string]string{"PMQA": "N/A: authority test", "Security": "N/A: repository policy"}
	plan := board.ReviewPlan{Schema: 1, PlanID: "proof-plan", Author: "codex", Basis: "test service identity and per-card authority", CWD: "/clone-a/repo", ReportLanguage: "en", TaskIDs: []string{first.TaskID, second.TaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "proof-batch", TaskIDs: []string{first.TaskID, second.TaskID}, Base: "N/A", TargetCommit: "N/A", Requirements: requirements}}}
	_, err = project.ReviewPlan(t.Context(), "single-proof", plan, &client.Claim{Token: one.Token, Generation: one.Claim.Generation})
	expectCode(t, err, api.CodeHeldByOther)
	proofs := &client.Claim{Members: map[string]api.ClaimProof{
		first.TaskID:  {Token: one.Token, Generation: one.Claim.Generation},
		second.TaskID: {Token: two.Token, Generation: two.Claim.Generation},
	}}
	_, err = project.ReviewPlan(t.Context(), "both-proofs", plan, proofs)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	canonical := plan
	canonical.CWD = "project:" + project.ID()
	payload, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := project.PreparePublication(t.Context(), api.PublicationPrepare{ID: "proof-publication", Operation: "review-plan", Payload: payload, Members: []api.PublicationMember{{TaskID: first.TaskID, Revision: one.Task.Revision, Generation: one.Claim.Generation}, {TaskID: second.TaskID, Revision: two.Task.Revision, Generation: two.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, member := range prepared.Members {
		proof := proofs.Members[member.TaskID]
		if _, err := project.ConfirmPublication(t.Context(), prepared.ID, api.PublicationApproval{TaskID: member.TaskID, Revision: member.Revision, Generation: member.Generation, Digest: prepared.Digest}, client.Claim{Token: proof.Token, Generation: proof.Generation}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			_, err := project.ReviewPlan(t.Context(), "partial-confirmation", plan, &client.Claim{PublicationID: prepared.ID})
			expectCode(t, err, api.CodeEvidenceIncomplete)
			current, err := project.Show(t.Context(), first.TaskID)
			if err != nil || current.Revision != one.Task.Revision {
				t.Fatalf("partial confirmation published a card: %+v %v", current, err)
			}
		}
	}
	if _, err := project.ReviewPlan(t.Context(), "confirmed-plan", plan, &client.Claim{PublicationID: prepared.ID}); err != nil {
		t.Fatal(err)
	}
	published, err := project.Publication(t.Context(), prepared.ID)
	if err != nil || published.Status != "published" || len(published.Result) == 0 {
		t.Fatalf("publication receipt was not committed: %+v %v", published, err)
	}
	raw, err := project.ReviewResource(t.Context(), "plans", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	var stored board.ReviewPlan
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.CWD != "project:"+project.ID() {
		t.Fatalf("plan identity depends on client CWD: %s", stored.CWD)
	}
	// The same logical plan can be retried from a different clone without
	// changing its server identity or the immutable historical originals.
	plan.CWD = "/clone-b/another-path"
	if _, err := project.ReviewPlan(t.Context(), "other-clone", plan, &client.Claim{PublicationID: prepared.ID}); err != nil {
		t.Fatal(err)
	}
	proofs.Members[second.TaskID] = api.ClaimProof{Token: one.Token, Generation: one.Claim.Generation}
	_, err = project.ReviewAction(t.Context(), "bad-extension", "extend-plan", board.ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: stored.Revision, Seal: true, Author: "codex", Basis: "seal test"}, proofs)
	expectCode(t, err, api.CodeHeldByOther)
}
