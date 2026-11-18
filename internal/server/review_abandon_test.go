package server_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// unpublished is a finalized run of one member that was never published: the
// state the abandon retires. proof is the run's writer proof, held[0] the direct
// proof of the member's holder, which carries no writer secret.
type unpublished struct {
	f      *fixture
	p      *client.Project
	grant  api.ClaimGrant
	run    board.ReviewRun
	writer client.Claim
	holder client.Claim
}

func finalizedUnpublished(t *testing.T) unpublished {
	t.Helper()
	f := newFixture(t)
	p := f.project("host-a")
	grant, run, writer := prepareRuntime(t, f, p)
	raw, err := p.ReviewRuntime(t.Context(), "abandon-finalize", run.RunID, "finalize", runtimeFinalization(run), &writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	return unpublished{f: f, p: p, grant: grant, run: run, writer: writer, holder: client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}}
}

func (u unpublished) request() board.ReviewAbandonRequest {
	return board.ReviewAbandonRequest{RunID: u.run.RunID, Decision: "user decision 2026-11-19", Reason: "the writer can no longer publish"}
}

func (u unpublished) record(t *testing.T) board.ReviewRun {
	t.Helper()
	raw, err := u.p.ReviewResource(t.Context(), "runs", u.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var run board.ReviewRun
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	return run
}

// files lists the files below the server data directory whose path holds part.
func (u unpublished) files(t *testing.T, part string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(u.f.dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(filepath.ToSlash(path), part) {
			found = append(found, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func (u unpublished) abandonedState(t *testing.T) {
	t.Helper()
	if run := u.record(t); run.Abandonment == nil {
		t.Fatalf("the run is not abandoned: %+v", run)
	}
}

func (u unpublished) notAbandoned(t *testing.T) {
	t.Helper()
	if run := u.record(t); run.Abandonment != nil || run.Published[u.grant.Task.TaskID] {
		t.Fatalf("a refused abandon changed the run: %+v", run)
	}
}

// Pre-flight check of the root cause: each way the writer is lost, the publish
// does not succeed, so a run that stays unpublished needs another way out.
func TestPublishFailsOnceTheWriterIsLost(t *testing.T) {
	t.Run("the claim was taken over", func(t *testing.T) {
		u := finalizedUnpublished(t)
		u.f.clock.advance(91 * time.Second)
		successor := u.f.project("host-b")
		if _, err := successor.Takeover(t.Context(), "", u.grant.Task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", ExpectedGeneration: u.grant.Claim.Generation, Decision: "explicit test takeover"}); err != nil {
			t.Fatal(err)
		}
		if _, err := u.p.ReviewRuntime(t.Context(), "publish-after-takeover", u.run.RunID, "publish", map[string]string{}, &u.writer); err == nil {
			t.Fatal("the publish succeeded after the claim moved on")
		}
		u.notAbandoned(t)
	})
	t.Run("another writer secret", func(t *testing.T) {
		u := finalizedUnpublished(t)
		foreign := u.writer
		foreign.ReviewWriter = strings.Repeat("b", 64)
		_, err := u.p.ReviewRuntime(t.Context(), "publish-foreign-secret", u.run.RunID, "publish", map[string]string{}, &foreign)
		expectCode(t, err, api.CodeHeldByOther)
	})
	t.Run("another client", func(t *testing.T) {
		u := finalizedUnpublished(t)
		other := u.f.project("host-b")
		if _, err := other.ReviewRuntime(t.Context(), "publish-other-client", u.run.RunID, "publish", map[string]string{}, &u.writer); err == nil {
			t.Fatal("the publish succeeded for another client")
		}
	})
}

func TestAbandonByTheHolderWritesOnceWithoutPublicationOrRequest(t *testing.T) {
	u := finalizedUnpublished(t)
	before, err := u.p.Show(t.Context(), u.grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	publications := len(u.files(t, "/publications/"))
	first, err := u.p.ReviewAction(t.Context(), "abandon-once", "abandon", u.request(), &u.holder)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	if err := json.Unmarshal(first, &body); err != nil || body["run_id"] != u.run.RunID {
		t.Fatalf("response body: %s %v", first, err)
	}
	u.abandonedState(t)
	if got := len(u.files(t, "/publications/")); got != publications {
		t.Fatalf("a direct abandon created a joint publication: %d to %d", publications, got)
	}
	requests, err := u.p.Requests(t.Context(), u.grant.Task.TaskID)
	if err != nil || len(requests) != 0 {
		t.Fatalf("a direct abandon created a collaboration request: %+v %v", requests, err)
	}
	receipt := u.files(t, "/receipts/")
	var saved []string
	for _, path := range receipt {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "review-abandon") {
			saved = append(saved, string(data))
		}
	}
	if len(saved) != 1 || !strings.Contains(saved[0], u.run.RunID) {
		t.Fatalf("saved receipt: %q", saved)
	}

	runBefore := u.record(t)
	again, err := u.p.ReviewAction(t.Context(), "abandon-once", "abandon", u.request(), &u.holder)
	if err != nil || !sameJSON(t, first, again) {
		t.Fatalf("the retry did not return the saved result: %s %v", again, err)
	}
	if after := u.record(t); !jsonEqual(t, runBefore, after) {
		t.Fatalf("the retry rewrote the run: %+v", after)
	}
	var receiptsAfter []string
	for _, path := range u.files(t, "/receipts/") {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "review-abandon") {
			receiptsAfter = append(receiptsAfter, string(data))
		}
	}
	if len(receiptsAfter) != 1 || receiptsAfter[0] != saved[0] {
		t.Fatalf("the retry changed the receipt: %q", receiptsAfter)
	}
	after, err := u.p.Show(t.Context(), u.grant.Task.TaskID)
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("an abandon changed the member card revision: %+v %v", after, err)
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	return string(x) == string(y)
}

func TestAbandonNeedsNoWriterCredentialAndBlocksALaterPublish(t *testing.T) {
	u := finalizedUnpublished(t)
	// The holder presents no writer secret: it was lost with the host that prepared the run.
	if _, err := u.p.ReviewAction(t.Context(), "abandon-no-writer", "abandon", u.request(), &u.holder); err != nil {
		t.Fatal(err)
	}
	_, err := u.p.ReviewRuntime(t.Context(), "publish-after-abandon", u.run.RunID, "publish", map[string]string{}, &u.writer)
	if err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Fatalf("the publish of an abandoned run: %v", err)
	}
	after, err := u.p.Show(t.Context(), u.grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if indexes, err := board.ParseReviewIndexes(after.Text); err != nil || len(indexes) != 0 {
		t.Fatalf("the member card has a copy: %+v %v", indexes, err)
	}
}

func TestAbandonAfterPublishIsRefused(t *testing.T) {
	u := finalizedUnpublished(t)
	if _, err := u.p.ReviewRuntime(t.Context(), "publish-first", u.run.RunID, "publish", map[string]string{}, &u.writer); err != nil {
		t.Fatal(err)
	}
	if _, err := u.p.ReviewAction(t.Context(), "abandon-late", "abandon", u.request(), &u.holder); err == nil {
		t.Fatal("a published run was abandoned")
	}
	if run := u.record(t); run.Abandonment != nil {
		t.Fatalf("a published run records an abandonment: %+v", run)
	}
}

func TestAbandonAfterTheClaimWasTakenOver(t *testing.T) {
	u := finalizedUnpublished(t)
	u.f.clock.advance(91 * time.Second)
	successor := u.f.project("host-b")
	taken, err := successor.Takeover(t.Context(), "", u.grant.Task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", ExpectedGeneration: u.grant.Claim.Generation, Decision: "explicit test takeover"})
	if err != nil {
		t.Fatal(err)
	}
	// The run belongs to host-a, and its member now has a newer generation.
	if _, err := u.p.ReviewAction(t.Context(), "abandon-old-generation", "abandon", u.request(), &u.holder); err == nil {
		t.Fatal("the credential of the old generation abandoned the run")
	}
	u.notAbandoned(t)
	current := client.Claim{Token: taken.Token, Generation: taken.Claim.Generation}
	if _, err := successor.ReviewAction(t.Context(), "abandon-new-generation", "abandon", u.request(), &current); err != nil {
		t.Fatalf("the holder of the new generation was refused: %v", err)
	}
	u.abandonedState(t)
}

func TestAbandonByAnotherProcessIsRefused(t *testing.T) {
	u := finalizedUnpublished(t)
	_, err := u.f.project("host-b").ReviewAction(t.Context(), "abandon-stranger", "abandon", u.request(), nil)
	expectCode(t, err, api.CodeHeldByOther)
	u.notAbandoned(t)
	if _, err := u.p.ReviewAction(t.Context(), "abandon-forged", "abandon", u.request(), &client.Claim{Token: strings.Repeat("0", 64), Generation: u.grant.Claim.Generation}); err == nil {
		t.Fatal("a forged token abandoned the run")
	}
	u.notAbandoned(t)
}

func TestAbandonWithoutALiveMemberIsRefusedForEveryCaller(t *testing.T) {
	u := finalizedUnpublished(t)
	if _, err := u.p.Move(t.Context(), "leave-batch", u.grant.Task.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "left the batch", Decision: "fixture decision"}, &u.holder); err != nil {
		t.Fatal(err)
	}
	for name, proof := range map[string]*client.Claim{"holder": &u.holder, "nobody": nil} {
		if _, err := u.p.ReviewAction(t.Context(), "abandon-no-live-"+name, "abandon", u.request(), proof); err == nil {
			t.Fatalf("%s abandoned a run without a live member", name)
		}
	}
	u.notAbandoned(t)
}

// Two live members: every holder confirms the same material before it is written.
func TestAbandonOfAJointRunNeedsEveryHolder(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	first := todoTask(t, a, "abandon-joint-first", "20261119-abandon-group")
	second := todoTask(t, b, "abandon-joint-second", "20261119-abandon-group")
	one, err := claim(t, a, first.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	two, err := claim(t, b, second.TaskID, "host-b")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("a", 64)
	prepare := api.ReviewPrepare{Input: board.ReviewInput{RunID: "abandon-joint-run", BatchID: "abandon-joint-batch", TaskIDs: []string{first.TaskID, second.TaskID}, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "project:" + a.ID(), Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}, Originals: map[string][]byte{"task-context.md": []byte("frozen tasks"), "review-context.md": []byte("context")}, Version: "test", WriterHash: board.ReviewDigest([]byte(secret))}
	members := []api.PublicationMember{{TaskID: first.TaskID, Revision: one.Task.Revision, Generation: one.Claim.Generation}, {TaskID: second.TaskID, Revision: two.Task.Revision, Generation: two.Claim.Generation}}
	holders := map[string]struct {
		p     *client.Project
		grant api.ClaimGrant
	}{first.TaskID: {a, one}, second.TaskID: {b, two}}
	confirm := func(publication api.Publication, only ...string) {
		t.Helper()
		for _, member := range publication.Members {
			if len(only) > 0 && member.TaskID != only[0] {
				continue
			}
			h := holders[member.TaskID]
			if _, err := h.p.ConfirmPublication(t.Context(), publication.ID, api.PublicationApproval{TaskID: member.TaskID, Digest: publication.Digest, Revision: member.Revision, Generation: member.Generation}, client.Claim{Token: h.grant.Token, Generation: h.grant.Claim.Generation}); err != nil {
				t.Fatal(err)
			}
		}
	}
	payload, _ := json.Marshal(prepare)
	material, err := a.PreparePublication(t.Context(), api.PublicationPrepare{ID: "abandon-joint-prepare", Operation: "review-prepare", Payload: payload, Members: members})
	if err != nil {
		t.Fatal(err)
	}
	confirm(material)
	raw, err := a.ReviewRuntime(t.Context(), "abandon-joint-prepare", prepare.Input.RunID, "prepare", prepare, &client.Claim{PublicationID: material.ID, ReviewWriter: secret})
	if err != nil {
		t.Fatal(err)
	}
	var preparation board.ReviewPreparation
	if err := json.Unmarshal(raw, &preparation); err != nil {
		t.Fatal(err)
	}
	writer := client.Claim{Token: one.Token, Generation: one.Claim.Generation, ReviewWriter: secret}
	if _, err := a.ReviewRuntime(t.Context(), "abandon-joint-finalize", prepare.Input.RunID, "finalize", runtimeFinalization(preparation.Run), &writer); err != nil {
		t.Fatal(err)
	}

	request := board.ReviewAbandonRequest{RunID: prepare.Input.RunID, Decision: "user decision 2026-11-19", Reason: "the writer can no longer publish"}
	body, _ := json.Marshal(request)
	joint, err := a.PreparePublication(t.Context(), api.PublicationPrepare{ID: "abandon-joint-material", Operation: "review-abandon", Payload: body, Members: members})
	if err != nil {
		t.Fatal(err)
	}
	proof := &client.Claim{PublicationID: joint.ID}
	if _, err := a.ReviewAction(t.Context(), "abandon-joint-direct", "abandon", request, &client.Claim{Token: one.Token, Generation: one.Claim.Generation}); err == nil {
		t.Fatal("one holder abandoned a joint run alone")
	}
	confirm(joint, first.TaskID)
	if _, err := a.ReviewAction(t.Context(), "abandon-joint-partial", "abandon", request, proof); err == nil {
		t.Fatal("the abandon was written before every holder confirmed")
	}
	stored := func() board.ReviewRun {
		raw, err := a.ReviewResource(t.Context(), "runs", request.RunID)
		if err != nil {
			t.Fatal(err)
		}
		var run board.ReviewRun
		if err := json.Unmarshal(raw, &run); err != nil {
			t.Fatal(err)
		}
		return run
	}
	if stored().Abandonment != nil {
		t.Fatal("a partial confirmation wrote the abandonment")
	}
	confirm(joint, second.TaskID)
	if _, err := a.ReviewAction(t.Context(), "abandon-joint-full", "abandon", request, proof); err != nil {
		t.Fatalf("the confirmed abandon was refused: %v", err)
	}
	if run := stored(); run.Abandonment == nil || !run.Published[first.TaskID] || !run.Published[second.TaskID] {
		t.Fatalf("the joint run is not abandoned: %+v", run)
	}
}

// The coordinator process of a single-card batch has no credential of the
// member's generation: the holder confirms the publication, then the abandon
// runs with the same arguments.
func TestAbandonThroughTheCoordinatorWaitsForTheHolder(t *testing.T) {
	u := finalizedUnpublished(t)
	coordinator := u.f.project("coordinator")
	request := u.request()
	body, _ := json.Marshal(request)
	publication, err := coordinator.PreparePublication(t.Context(), api.PublicationPrepare{ID: "abandon-coordinated", Operation: "review-abandon", Payload: body, Members: []api.PublicationMember{{TaskID: u.grant.Task.TaskID, Revision: u.grant.Task.Revision, Generation: u.grant.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	proof := &client.Claim{PublicationID: publication.ID}
	_, err = coordinator.ReviewAction(t.Context(), "abandon-unconfirmed", "abandon", request, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	u.notAbandoned(t)
	if _, err := u.p.ConfirmPublication(t.Context(), publication.ID, api.PublicationApproval{TaskID: u.grant.Task.TaskID, Digest: publication.Digest, Revision: u.grant.Task.Revision, Generation: u.grant.Claim.Generation}, u.holder); err != nil {
		t.Fatal(err)
	}
	// Another run of the same material is refused: the confirmation names this request.
	other := request
	other.Decision = "another decision"
	if _, err := coordinator.ReviewAction(t.Context(), "abandon-other-material", "abandon", other, proof); err == nil {
		t.Fatal("a confirmation of other material abandoned the run")
	}
	u.notAbandoned(t)
	if _, err := coordinator.ReviewAction(t.Context(), "abandon-confirmed", "abandon", request, proof); err != nil {
		t.Fatalf("the confirmed abandon was refused: %v", err)
	}
	u.abandonedState(t)
}

func TestPrepareOfAnAbandonedRunIsRefused(t *testing.T) {
	u := finalizedUnpublished(t)
	if _, err := u.p.ReviewAction(t.Context(), "abandon-then-prepare", "abandon", u.request(), &u.holder); err != nil {
		t.Fatal(err)
	}
	request := api.ReviewPrepare{Input: board.ReviewInput{RunID: u.run.RunID, BatchID: u.run.BatchID, TaskIDs: u.run.TaskIDs, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "/independent/clone", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}, Originals: map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}, Version: "test", WriterHash: board.ReviewDigest([]byte(strings.Repeat("a", 64)))}
	_, err := u.p.ReviewRuntime(t.Context(), "prepare-abandoned", u.run.RunID, "prepare", request, &u.writer)
	if err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Fatalf("the prepare of an abandoned run: %v", err)
	}
}

// A publication the holder confirmed before the abandon is refused afterwards: the
// confirmed material does not publish an abandoned run, and writes nothing.
func TestConfirmedPublishIsRefusedAfterTheAbandon(t *testing.T) {
	u := finalizedUnpublished(t)
	before, err := u.p.Show(t.Context(), u.grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(u.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := u.p.PreparePublication(t.Context(), api.PublicationPrepare{ID: "confirmed-before-abandon", Operation: "review-publish:" + u.run.RunID, Payload: payload, Members: []api.PublicationMember{{TaskID: u.grant.Task.TaskID, Revision: before.Revision, Generation: u.grant.Claim.Generation}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.p.ConfirmPublication(t.Context(), publication.ID, api.PublicationApproval{TaskID: u.grant.Task.TaskID, Digest: publication.Digest, Revision: before.Revision, Generation: u.grant.Claim.Generation}, u.holder); err != nil {
		t.Fatal(err)
	}
	if _, err := u.p.ReviewAction(t.Context(), "abandon-after-confirm", "abandon", u.request(), &u.holder); err != nil {
		t.Fatal(err)
	}
	proof := &client.Claim{PublicationID: publication.ID, ReviewWriter: u.writer.ReviewWriter}
	_, err = u.p.ReviewRuntime(t.Context(), "publish-confirmed-material", u.run.RunID, "publish", map[string]string{}, proof)
	if err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Fatalf("the confirmed material published an abandoned run: %v", err)
	}
	after, err := u.p.Show(t.Context(), u.grant.Task.TaskID)
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("the refused publish changed the member card: %+v %v", after, err)
	}
	if indexes, err := board.ParseReviewIndexes(after.Text); err != nil || len(indexes) != 0 {
		t.Fatalf("the member card has a copy: %+v %v", indexes, err)
	}
	if got, err := u.p.Publication(t.Context(), publication.ID); err != nil || got.Status == "published" {
		t.Fatalf("the publication was marked published: %+v %v", got, err)
	}
}
