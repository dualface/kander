package remote_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

func TestIntegrationReceiptGatesDoneAndPortableWrapUp(t *testing.T) {
	f := newIntegrationFixture(t)
	if _, err := f.a.Project.Move(t.Context(), "premature-done", f.task.TaskID, api.Move{State: "done"}, f.credential.Claim()); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("done without final integration: %v", err)
	}
	result, err := f.a.Integrate(t.Context(), f.first, f.options("completion-gate"))
	if err != nil {
		t.Fatal(err)
	}
	g := board.DispatchIntegration{IntegrationID: result.ID, IntegrationExecution: result.Request.ExecutionID, IntegrationGeneration: result.Generation, DispatchID: "portable-wrap", TaskID: f.task.TaskID, CWD: "project:" + f.a.Info.ID, SourceCommit: f.commit, ReviewTarget: f.commit, ReviewBase: f.base, TargetCommit: result.ObservedTarget, TargetRef: result.Request.TargetRef, Author: "coordinator", Basis: "published integration receipt", VerifiedAt: time.Now().UTC()}
	if err := f.b.VerifyWrapUpIntegration(t.Context(), f.second, g); err != nil {
		t.Fatal(err)
	}
	forged := g
	forged.IntegrationGeneration++
	if err := f.b.VerifyWrapUpIntegration(t.Context(), f.second, forged); err == nil {
		t.Fatal("forged generation accepted")
	}
	input := board.DispatchInput{ID: g.DispatchID, TaskID: f.task.TaskID, Kind: "wrap-up", Message: "Record completion and clean the execution workspace", Base: f.commit, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Artifact: board.ArtifactReference{TaskID: f.task.TaskID, Path: "dispatches/" + g.DispatchID + "/integration.json"}, Git: g}}}
	forgedInput := input
	copyBinding := *input.Evidence.WrapUp
	copyBinding.Git = forged
	forgedInput.Evidence.WrapUp = &copyBinding
	if _, err := f.a.Project.DispatchAction(t.Context(), "forged-wrap-receipt", f.task.TaskID, "prepare", api.DispatchCommand{Input: &forgedInput, CardRevision: f.task.Revision}, *f.credential.Claim()); err == nil {
		t.Fatal("service accepted forged published receipt")
	}
	prepared, err := f.a.Project.DispatchAction(t.Context(), "portable-wrap-receipt", f.task.TaskID, "prepare", api.DispatchCommand{Input: &input, CardRevision: f.task.Revision}, *f.credential.Claim())
	if err != nil || prepared.Input.Evidence.WrapUp.Git.TargetRef != "refs/heads/v2" {
		t.Fatalf("portable wrap-up: %+v %v", prepared, err)
	}
	accepted, err := f.a.Project.Move(t.Context(), "accept-portable-wrap", f.task.TaskID, api.Move{State: "working", Authorization: prepared.Authorization}, f.credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.a.Project.PutDocument(t.Context(), "wrap-completion-report", f.task.TaskID, api.Document{Path: "report.md", Text: "Verified portable wrap-up through both independent clones.", ExpectedRevision: accepted.Revision, Authorization: prepared.Authorization}, f.credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	f.credential.Worktree = f.first
	if err := f.a.SaveCredential(f.credential); err != nil {
		t.Fatal(err)
	}
	completion := api.Move{State: "done", Result: "completed", Authorization: prepared.Authorization, DeliveryCommit: f.commit}
	done, err := f.a.MoveTask(t.Context(), f.task.TaskID, completion)
	if err != nil || done.MoveReceipt == nil || done.MoveReceipt.Replayed || done.MoveReceipt.Dispatch == nil || done.MoveReceipt.Dispatch.State != board.DispatchCompleted {
		t.Fatalf("wrap-up completion: %+v %v", done, err)
	}
	replay, err := f.a.MoveTask(t.Context(), f.task.TaskID, completion)
	if err != nil || replay.MoveReceipt == nil || !replay.MoveReceipt.Replayed || replay.Revision != done.Revision {
		t.Fatalf("released dispatch receipt: %+v %v", replay, err)
	}
	if err := remote.RemoveCredential(f.a.Home, f.credential); err != nil {
		t.Fatal(err)
	}
	t.Setenv(remote.EnvExecutionID, f.credential.ExecutionID)
	historical, err := f.a.MoveTask(t.Context(), f.task.TaskID, completion)
	if err != nil || historical.MoveReceipt == nil || !historical.MoveReceipt.Replayed || historical.Revision != done.Revision {
		t.Fatalf("released fixed execution could not read the exact completed receipt: %+v %v", historical.MoveReceipt, err)
	}
	forgedCompletion := completion
	forgedCompletion.DeliveryCommit = f.base
	if _, err := f.a.Project.Move(t.Context(), "forged-completed-replay", f.task.TaskID, forgedCompletion, f.credential.Claim()); err == nil {
		t.Fatal("released holder changed the completion payload")
	}
	if _, err := f.a.Project.PutDocument(t.Context(), "late-after-wrap", f.task.TaskID, api.Document{Path: "late.md", Text: "stale execution", ExpectedRevision: done.Revision, Authorization: prepared.Authorization}, f.credential.Claim()); err == nil {
		t.Fatal("receipt replay restored mutation authority")
	}
}
func TestIntegrationReceiptAllowsDoneButRewoundRemoteBlocksClient(t *testing.T) {
	f := newIntegrationFixture(t)
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("done-after-integration")); err != nil {
		t.Fatal(err)
	}
	// A client must inspect the real remote even after central publication.
	deliveryGit(t, f.second, "push", "--force-with-lease=refs/heads/v2:"+f.commit, "origin", f.base+":refs/heads/v2")
	f.credential.Worktree = f.first
	if err := f.a.SaveCredential(f.credential); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.MoveTask(t.Context(), f.task.TaskID, api.Move{State: "done"}); err == nil {
		t.Fatal("rewound remote passed completion")
	}
	if _, err := f.b.FetchDelivery(t.Context(), f.second, f.task.TaskID, f.commit); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.second, "push", "origin", f.commit+":refs/heads/v2")
	if _, err := f.a.Project.PutDocument(t.Context(), "completion-report", f.task.TaskID, api.Document{Path: "report.md", Text: "Completed and verified through the common remote.", ExpectedRevision: f.task.Revision}, f.credential.Claim()); err != nil {
		t.Fatal(err)
	}
	value, err := f.a.MoveTask(t.Context(), f.task.TaskID, api.Move{State: "done", Result: "completed"})
	if err != nil || value.State != "done" || value.Claim.Status != api.ClaimReleased {
		t.Fatalf("final completion: %+v %v", value, err)
	}
	for _, dir := range []string{f.first, f.second} {
		if _, err := os.Stat(filepath.Join(dir, "kanban")); !os.IsNotExist(err) {
			t.Fatal("completion materialized a local board")
		}
	}
}
