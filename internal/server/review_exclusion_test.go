package server_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func TestReviewWriterContinuesAfterAuditedMemberExclusion(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	first := todoTask(t, a, "excluded-runtime-first", "20261112-runtime-group")
	second := todoTask(t, b, "excluded-runtime-second", "20261112-runtime-group")
	one, err := claim(t, a, first.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	two, err := claim(t, b, second.TaskID, "host-b")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("a", 64)
	request := api.ReviewPrepare{Input: board.ReviewInput{RunID: "excluded-runtime-run", BatchID: "excluded-runtime-batch", TaskIDs: []string{first.TaskID, second.TaskID}, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "project:" + a.ID(), Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}, Originals: map[string][]byte{"task-context.md": []byte("frozen tasks"), "review-context.md": []byte("context")}, Version: "test", WriterHash: board.ReviewDigest([]byte(secret))}
	payload, _ := json.Marshal(request)
	publication, err := a.PreparePublication(t.Context(), api.PublicationPrepare{ID: "prepare-excluded-runtime", Operation: "review-prepare", Payload: payload, Members: []api.PublicationMember{{TaskID: first.TaskID, Revision: one.Task.Revision, Generation: one.Claim.Generation}, {TaskID: second.TaskID, Revision: two.Task.Revision, Generation: two.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, member := range publication.Members {
		grant, p := one, a
		if i == 1 {
			grant, p = two, b
		}
		if _, err := p.ConfirmPublication(t.Context(), publication.ID, api.PublicationApproval{TaskID: member.TaskID, Digest: publication.Digest, Revision: member.Revision, Generation: member.Generation}, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := a.ReviewRuntime(t.Context(), "prepare-excluded", request.Input.RunID, "prepare", request, &client.Claim{PublicationID: publication.ID, ReviewWriter: secret})
	if err != nil {
		t.Fatal(err)
	}
	var preparation board.ReviewPreparation
	if err := json.Unmarshal(raw, &preparation); err != nil {
		t.Fatal(err)
	}
	removed, err := b.Move(t.Context(), "exclude-member", second.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "work no longer required", Decision: "explicit fixture withdrawal"}, &client.Claim{Token: two.Token, Generation: two.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: one.Token, Generation: one.Claim.Generation, ReviewWriter: secret}
	if _, err := a.ReviewRuntime(t.Context(), "finalize-excluded", request.Input.RunID, "finalize", runtimeFinalization(preparation.Run), &proof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReviewRuntime(t.Context(), "publish-excluded", request.Input.RunID, "publish", map[string]string{}, &proof); err != nil {
		t.Fatal(err)
	}
	retained, err := b.Show(t.Context(), second.TaskID)
	if err != nil || retained.Revision != removed.Revision {
		t.Fatalf("publication rewrote an excluded member: %+v %v", retained, err)
	}
	current, err := a.Show(t.Context(), first.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := board.ParseReviewIndexes(current.Text)
	if err != nil || len(indexes) != 1 {
		t.Fatalf("remaining member lost publication: %+v %v", indexes, err)
	}
}
