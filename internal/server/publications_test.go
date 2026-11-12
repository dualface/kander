package server_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

type jointReview struct {
	fixture       *fixture
	first, second *client.Project
	plan          board.ReviewPlan
	publication   api.Publication
	grants        map[string]api.ClaimGrant
}

func prepareJointReview(t *testing.T) jointReview {
	t.Helper()
	fixture := newFixture(t)
	a, b := fixture.project("host-a"), fixture.project("host-b")
	first := todoTask(t, a, "joint-one", "20261112-joint-group")
	second := todoTask(t, b, "joint-two", "20261112-joint-group")
	one, err := a.Claim(t.Context(), "", first.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "first-execution", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := b.Claim(t.Context(), "", second.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "second-execution", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	plan := board.ReviewPlan{Schema: 1, PlanID: "joint-plan", Author: "coordinator", Basis: "joint authority fixture", CWD: "project:" + a.ID(), ReportLanguage: "en", TaskIDs: []string{first.TaskID, second.TaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "joint-batch", TaskIDs: []string{first.TaskID, second.TaskID}, Base: "N/A", TargetCommit: "N/A", Requirements: map[string]string{"PMQA": "N/A: publication fixture", "Security": "N/A: repository policy"}}}}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := a.PreparePublication(t.Context(), api.PublicationPrepare{ID: "joint-material", Operation: "review-plan", Payload: payload, Members: []api.PublicationMember{{TaskID: first.TaskID, Revision: one.Task.Revision, Generation: one.Claim.Generation}, {TaskID: second.TaskID, Revision: two.Task.Revision, Generation: two.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	return jointReview{fixture: fixture, first: a, second: b, plan: plan, publication: publication, grants: map[string]api.ClaimGrant{first.TaskID: one, second.TaskID: two}}
}

func (j *jointReview) confirm(t *testing.T, index int) {
	t.Helper()
	member := j.publication.Members[index]
	grant := j.grants[member.TaskID]
	project := j.first
	if index == 1 {
		project = j.second
	}
	value, err := project.ConfirmPublication(t.Context(), j.publication.ID, api.PublicationApproval{TaskID: member.TaskID, Revision: member.Revision, Generation: member.Generation, Digest: j.publication.Digest}, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	j.publication = value
}

func TestJointReviewRecoversPartialConfirmationAndPublishesAtomically(t *testing.T) {
	joint := prepareJointReview(t)
	joint.confirm(t, 0)
	id := joint.first.ID()
	dir, clock := joint.fixture.dir, joint.fixture.clock
	joint.fixture.stop()
	joint.fixture = start(t, dir, clock)
	joint.first, joint.second = joint.fixture.project("host-a"), joint.fixture.project("host-b")
	if joint.first.ID() != id {
		t.Fatal("restart changed project identity")
	}
	recovered, err := joint.first.Publication(t.Context(), joint.publication.ID)
	if err != nil || len(recovered.Approvals) != 1 || recovered.Digest != joint.publication.Digest {
		t.Fatalf("partial confirmation was not recovered: %+v %v", recovered, err)
	}
	joint.publication = recovered
	joint.confirm(t, 1)
	// The coordinator need not possess either holder's bearer token after each
	// holder independently confirmed the immutable operation.
	observer := joint.fixture.project("coordinator")
	if _, err := observer.ReviewPlan(t.Context(), "joint-commit", joint.plan, &client.Claim{PublicationID: joint.publication.ID}); err != nil {
		t.Fatal(err)
	}
	first, err := observer.Show(t.Context(), joint.plan.TaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := observer.Show(t.Context(), joint.plan.TaskIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID == "" || first.OperationID != second.OperationID {
		t.Fatal("members were not published in one redo transaction")
	}
	committed, err := observer.Publication(t.Context(), joint.publication.ID)
	if err != nil || committed.Status != "published" || len(committed.Result) == 0 {
		t.Fatalf("joint publication lost its receipt: %+v %v", committed, err)
	}
	if _, err := observer.ReviewPlan(t.Context(), "joint-retry", joint.plan, &client.Claim{PublicationID: joint.publication.ID}); err != nil {
		t.Fatal(err)
	}
	again, err := observer.Show(t.Context(), first.TaskID)
	if err != nil || again.Revision != first.Revision {
		t.Fatal("publication retry changed the card version")
	}
}

func TestJointReviewRejectsChangedVersionsAndExecutions(t *testing.T) {
	for _, change := range []string{"revision", "takeover", "expiry"} {
		t.Run(change, func(t *testing.T) {
			joint := prepareJointReview(t)
			joint.confirm(t, 0)
			joint.confirm(t, 1)
			firstID, secondID := joint.plan.TaskIDs[0], joint.plan.TaskIDs[1]
			first, second := joint.grants[firstID], joint.grants[secondID]
			expected := api.CodeLeaseInvalid
			switch change {
			case "revision":
				own := client.Claim{Token: first.Token, Generation: first.Claim.Generation}
				if _, err := joint.first.PutDocument(t.Context(), "", firstID, api.Document{Path: "report.md", Text: "new execution evidence", ExpectedRevision: first.Task.Revision}, &own); err != nil {
					t.Fatal(err)
				}
				expected = api.CodeRevisionConflict
			case "takeover":
				joint.fixture.clock.advance(45 * time.Second)
				if _, err := joint.first.Renew(t.Context(), firstID, client.Claim{Token: first.Token, Generation: first.Claim.Generation}); err != nil {
					t.Fatal(err)
				}
				joint.fixture.clock.advance(46 * time.Second)
				successor := joint.fixture.project("host-c")
				if _, err := successor.Takeover(t.Context(), "", secondID, api.ClaimRequest{ClientID: "host-c", ExecutionID: "successor", Owner: "codex", ExpectedGeneration: second.Claim.Generation, Decision: "explicit test takeover"}); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				joint.fixture.clock.advance(91 * time.Second)
			}
			_, err := joint.first.ReviewPlan(t.Context(), "stale-joint", joint.plan, &client.Claim{PublicationID: joint.publication.ID})
			expectCode(t, err, expected)
			publication, err := joint.first.Publication(t.Context(), joint.publication.ID)
			if err != nil || publication.Status != "prepared" {
				t.Fatal("stale joint operation published evidence")
			}
			unchanged, err := joint.first.Show(t.Context(), secondID)
			if err != nil || unchanged.Revision != second.Task.Revision {
				t.Fatalf("rejected joint operation changed another member: %+v %v", unchanged, err)
			}
		})
	}
}
