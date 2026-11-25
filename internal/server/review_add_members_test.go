package server_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func readPlan(t *testing.T, p *client.Project, id string) board.ReviewPlan {
	t.Helper()
	raw, err := p.ReviewResource(t.Context(), "plans", id)
	if err != nil {
		t.Fatal(err)
	}
	var plan board.ReviewPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

// An extend-plan with add_members is a joint review write: the new member's
// holder confirms the same publication, and a publication that names only the
// members of the plan is refused.
func TestAddMembersNeedsTheConfirmationOfTheNewHolder(t *testing.T) {
	f := newFixture(t)
	hostA, hostB, coordinator := f.project("host-a"), f.project("host-b"), f.project("coordinator")
	first := todoTask(t, hostA, "add-first", "20261118-add-group")
	second := todoTask(t, hostB, "add-second", "20261118-add-group")
	grantA, err := claim(t, hostA, first.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	grantB, err := claim(t, hostB, second.TaskID, "host-b")
	if err != nil {
		t.Fatal(err)
	}
	plan := board.ReviewPlan{Schema: 1, PlanID: "add-plan", Author: "coordinator", Basis: "one member first", CWD: "project:" + hostA.ID(), ReportLanguage: "en", TaskIDs: []string{first.TaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "add-batch", TaskIDs: []string{first.TaskID}, Base: "N/A", TargetCommit: "N/A", Requirements: map[string]string{"PMQA": "N/A: add_members fixture", "Security": "N/A: repository policy"}}}}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	created, err := coordinator.PreparePublication(t.Context(), api.PublicationPrepare{ID: "add-plan-material", Operation: "review-plan", Payload: payload, Members: []api.PublicationMember{{TaskID: first.TaskID, Revision: grantA.Task.Revision, Generation: grantA.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	holderA := client.Claim{Token: grantA.Token, Generation: grantA.Claim.Generation}
	holderB := client.Claim{Token: grantB.Token, Generation: grantB.Claim.Generation}
	if _, err = hostA.ConfirmPublication(t.Context(), created.ID, api.PublicationApproval{TaskID: first.TaskID, Revision: grantA.Task.Revision, Generation: grantA.Claim.Generation, Digest: created.Digest}, holderA); err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.ReviewPlan(t.Context(), "add-plan", plan, &client.Claim{PublicationID: created.ID}); err != nil {
		t.Fatal(err)
	}
	stored := readPlan(t, coordinator, plan.PlanID)

	request := board.ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: stored.Revision, AddMembers: []string{second.TaskID}, Author: "coordinator", Basis: "the second card started"}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	members := func(ids ...string) []api.PublicationMember {
		var out []api.PublicationMember
		for _, id := range ids {
			current, err := coordinator.Show(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			generation := grantA.Claim.Generation
			if id == second.TaskID {
				generation = grantB.Claim.Generation
			}
			out = append(out, api.PublicationMember{TaskID: id, Revision: current.Revision, Generation: generation})
		}
		return out
	}
	confirm := func(p *client.Project, publication api.Publication, id string, proof client.Claim) {
		t.Helper()
		for _, m := range publication.Members {
			if m.TaskID == id {
				if _, err := p.ConfirmPublication(t.Context(), publication.ID, api.PublicationApproval{TaskID: id, Revision: m.Revision, Generation: m.Generation, Digest: publication.Digest}, proof); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("publication does not name %s", id)
	}
	unchanged := func() {
		t.Helper()
		if got := readPlan(t, coordinator, plan.PlanID); got.Revision != stored.Revision || !slices.Equal(got.TaskIDs, stored.TaskIDs) {
			t.Fatalf("the plan changed: %+v", got)
		}
	}

	// A publication of the plan's members alone leaves the new holder out.
	narrow, err := coordinator.PreparePublication(t.Context(), api.PublicationPrepare{ID: "add-narrow", Operation: "review-extend-plan", Payload: body, Members: members(first.TaskID)})
	if err != nil {
		t.Fatal(err)
	}
	confirm(hostA, narrow, first.TaskID, holderA)
	_, err = coordinator.ReviewAction(t.Context(), "add-narrow", "extend-plan", request, &client.Claim{PublicationID: narrow.ID})
	expectCode(t, err, api.CodeEvidenceIncomplete)
	unchanged()

	joint, err := coordinator.PreparePublication(t.Context(), api.PublicationPrepare{ID: "add-joint", Operation: "review-extend-plan", Payload: body, Members: members(first.TaskID, second.TaskID)})
	if err != nil {
		t.Fatal(err)
	}
	proof := &client.Claim{PublicationID: joint.ID}
	_, err = coordinator.ReviewAction(t.Context(), "add-unconfirmed", "extend-plan", request, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	confirm(hostA, joint, first.TaskID, holderA)
	_, err = coordinator.ReviewAction(t.Context(), "add-old-holder", "extend-plan", request, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	unchanged()

	confirm(hostB, joint, second.TaskID, holderB)
	if _, err = coordinator.ReviewAction(t.Context(), "add-confirmed", "extend-plan", request, proof); err != nil {
		t.Fatalf("the confirmed addition was refused: %v", err)
	}
	got := readPlan(t, coordinator, plan.PlanID)
	if got.Revision != stored.Revision+1 || !slices.Equal(got.TaskIDs, []string{first.TaskID, second.TaskID}) {
		t.Fatalf("plan: %+v", got)
	}
	again, err := coordinator.ReviewAction(t.Context(), "add-retry", "extend-plan", request, proof)
	if err != nil {
		t.Fatalf("the retry of a published addition was refused: %v", err)
	}
	if readPlan(t, coordinator, plan.PlanID).Revision != got.Revision {
		t.Fatalf("the retry changed the plan: %s", again)
	}
}
