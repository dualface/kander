package server_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func prepareRuntime(t *testing.T, f *fixture, p *client.Project) (api.ClaimGrant, board.ReviewRun, client.Claim) {
	t.Helper()
	task := todoTask(t, p, "runtime-evidence")
	grant, err := claim(t, p, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("a", 64)
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation, ReviewWriter: secret}
	request := api.ReviewPrepare{Input: board.ReviewInput{RunID: "runtime-run", BatchID: "runtime-batch", TaskIDs: []string{task.TaskID}, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "/independent/clone", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}, Originals: map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}, Version: "test", WriterHash: board.ReviewDigest([]byte(secret))}
	raw, err := p.ReviewRuntime(t.Context(), "prepare", request.Input.RunID, "prepare", request, &proof)
	if err != nil {
		t.Fatal(err)
	}
	var prepared board.ReviewPreparation
	if err := json.Unmarshal(raw, &prepared); err != nil {
		t.Fatal(err)
	}
	if !prepared.Fresh || prepared.Run.CWD != "project:"+p.ID() {
		t.Fatalf("invalid preparation: %+v", prepared)
	}

	retry, err := p.ReviewRuntime(t.Context(), "prepare-again", request.Input.RunID, "prepare", request, &proof)
	if err != nil {
		t.Fatal(err)
	}
	var replay board.ReviewPreparation
	if err := json.Unmarshal(retry, &replay); err != nil || replay.Fresh {
		t.Fatalf("prepared identity authorized another reviewer: %+v %v", replay, err)
	}
	request.Originals["task-context.md"] = []byte("changed intent")
	_, err = p.ReviewRuntime(t.Context(), "changed-intent", request.Input.RunID, "prepare", request, &proof)
	expectCode(t, err, api.CodeIdempotencyMismatch)
	return grant, prepared.Run, proof
}
func runtimeFinalization(run board.ReviewRun) api.ReviewFinalize {
	run.LaunchStatus = "started"
	run.ExecutionStatus = "ok"
	run.ExitCode = 0
	request := api.ReviewFinalize{Run: run, Report: []byte("PASS\n")}
	for _, name := range []string{"output.raw", "stdout.log", "error.log"} {
		data := []byte(name + ": collected output\n")
		request.Files = append(request.Files, api.ReviewArtifact{Name: name, Data: data, Digest: board.ReviewDigest(data)})
	}
	return request
}
func TestReviewRuntimeUploadsExactOriginalsAndRetriesAfterRestart(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	grant, run, proof := prepareRuntime(t, f, p)
	before, err := p.Show(t.Context(), grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	request := runtimeFinalization(run)
	corrupt := request
	corrupt.Files = append([]api.ReviewArtifact(nil), request.Files...)
	corrupt.Files[0].Digest = strings.Repeat("0", 64)
	_, err = p.ReviewRuntime(t.Context(), "corrupt", run.RunID, "finalize", corrupt, &proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	raw, err := p.ReviewResource(t.Context(), "runs", run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var unchanged board.ReviewRun
	if err := json.Unmarshal(raw, &unchanged); err != nil || unchanged.Phase != "prepared" {
		t.Fatalf("corrupt upload finalized: %+v %v", unchanged, err)
	}
	raw, err = p.ReviewRuntime(t.Context(), "finalize", run.RunID, "finalize", request, &proof)
	if err != nil {
		t.Fatal(err)
	}
	var final board.ReviewRun
	if err := json.Unmarshal(raw, &final); err != nil {
		t.Fatal(err)
	}
	if final.Hashes["output.raw"] != request.Files[0].Digest || final.Phase != "finalized" {
		t.Fatalf("uploaded bytes not frozen: %+v", final)
	}
	after, err := p.Show(t.Context(), grant.Task.TaskID)
	if err != nil || after.Revision != before.Revision {
		t.Fatal("runtime upload changed card revision")
	}
	dir, clock := f.dir, f.clock
	f.stop()
	f = start(t, dir, clock)
	p = f.project("host-a")
	replay, err := p.ReviewRuntime(t.Context(), "finalize", run.RunID, "finalize", request, &proof)
	var replayed board.ReviewRun
	if err == nil {
		err = json.Unmarshal(replay, &replayed)
	}
	if err != nil || !reflect.DeepEqual(replayed, final) {
		t.Fatalf("lost exact finalization receipt: %s %v", replay, err)
	}
	if _, err = p.ReviewRuntime(t.Context(), "publish", run.RunID, "publish", map[string]string{}, &proof); err != nil {
		t.Fatal(err)
	}
	evidence, err := p.ReviewEvidence(t.Context(), run.RunID, "original", "output.raw")
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err := json.Unmarshal(evidence, &data); err != nil || string(data) != string(request.Files[0].Data) {
		t.Fatalf("original changed: %q %v", data, err)
	}
	published, err := p.Show(t.Context(), grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := board.ParseReviewIndexes(published.Text)
	if err != nil || len(indexes) != 1 || published.Revision != before.Revision+1 {
		t.Fatalf("publication: %+v %v", indexes, err)
	}
	if _, err = p.ReviewRuntime(t.Context(), "publish", run.RunID, "publish", map[string]string{}, &proof); err != nil {
		t.Fatal(err)
	}
}
func TestReviewRuntimeRejectsForeignWriterAndLateGeneration(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	grant, run, proof := prepareRuntime(t, f, p)
	foreign := proof
	foreign.ReviewWriter = strings.Repeat("b", 64)
	_, err := p.ReviewRuntime(t.Context(), "foreign", run.RunID, "finalize", runtimeFinalization(run), &foreign)
	expectCode(t, err, api.CodeHeldByOther)
	f.clock.advance(91 * time.Second)
	successor := f.project("host-b")
	if _, err := successor.Takeover(t.Context(), "", grant.Task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", ExpectedGeneration: grant.Claim.Generation, Decision: "explicit test takeover"}); err != nil {
		t.Fatal(err)
	}
	_, err = p.ReviewRuntime(t.Context(), "late", run.RunID, "finalize", runtimeFinalization(run), &proof)
	expectCode(t, err, api.CodeLeaseInvalid)
	raw, err := p.ReviewResource(t.Context(), "runs", run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var saved board.ReviewRun
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Phase != "prepared" {
		t.Fatalf("old generation changed run: %+v %v", saved, err)
	}
}
