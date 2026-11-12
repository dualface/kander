package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func TestDispatchServiceKeepsOwnershipAndBusinessEpochAsSeparateFences(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "remote-dispatch")
	grant, err := a.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "dispatch-execution", Owner: "codex", Start: &api.StartInput{Session: "codex dispatch-session", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	task, err = a.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, proof)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(task.Text, "- TASK_BRANCH:", "- TASK_BRANCH: remote-dispatch-branch", 1)
	task, err = a.PutDocument(t.Context(), "task-branch", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, &proof)
	if err != nil {
		t.Fatal(err)
	}
	input := board.DispatchInput{ID: "sync-round", TaskID: task.TaskID, Kind: "sync", Message: "Synchronize the verified target commit", Base: strings.Repeat("a", 40)}
	request := api.DispatchCommand{Input: &input, CardRevision: task.Revision}
	_, err = b.DispatchAction(t.Context(), "foreign-prepare", task.TaskID, "prepare", request, client.Claim{})
	expectCode(t, err, api.CodeHeldByOther)
	prepared, err := a.DispatchAction(t.Context(), "prepare-round", task.TaskID, "prepare", request, proof)
	if err != nil || prepared.State != board.DispatchPrepared {
		t.Fatalf("prepare: %+v %v", prepared, err)
	}
	snapshot, err := a.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := a.DispatchAction(t.Context(), "attempt", task.TaskID, "attempt", api.DispatchCommand{DispatchID: prepared.Input.ID, Revision: prepared.Revision, CardRevision: snapshot.Revision}, proof)
	if err != nil || attempt.State != board.DispatchUnknown || attempt.Attempts != 1 {
		t.Fatalf("attempt: %+v %v", attempt, err)
	}
	_, err = a.Move(t.Context(), "wrong-business-epoch", task.TaskID, api.Move{State: "working", Authorization: board.ExecutionAuthorization{DispatchID: prepared.Input.ID, Epoch: prepared.Authorization.Epoch + 1}}, &proof)
	expectCode(t, err, api.CodeBoardRejected)
	working, err := a.Move(t.Context(), "accept", task.TaskID, api.Move{State: "working", Authorization: prepared.Authorization}, &proof)
	if err != nil {
		t.Fatal(err)
	}
	document := api.Document{Path: "progress.md", Text: "Verified synchronization facts", ExpectedRevision: working.Revision}
	_, err = a.PutDocument(t.Context(), "missing-epoch", task.TaskID, document, &proof)
	expectCode(t, err, api.CodeBoardRejected)
	document.Authorization = prepared.Authorization
	if _, err := a.PutDocument(t.Context(), "author-progress", task.TaskID, document, &proof); err != nil {
		t.Fatal(err)
	}
	stageDelivery(t, a, grant, strings.Repeat("b", 40))
	reviewed, err := a.Move(t.Context(), "complete", task.TaskID, api.Move{State: "review", DeliveryCommit: strings.Repeat("b", 40), Authorization: prepared.Authorization}, &proof)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := a.Dispatch(t.Context(), task.TaskID, input.ID)
	if err != nil || completed.State != board.DispatchCompleted || completed.Completed == nil || completed.Completed.CardRevision != reviewed.Revision {
		t.Fatalf("business completion: %+v %v", completed, err)
	}
	replay, err := a.DispatchAction(t.Context(), "prepare-round", task.TaskID, "prepare", request, proof)
	if err != nil || replay.State != board.DispatchPrepared || replay.Revision != prepared.Revision {
		t.Fatal("prepare retry did not return its original transaction result")
	}
	f.clock.advance(91 * time.Second)
	successor, err := b.Takeover(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "dispatch-successor", Owner: "codex", Decision: "explicit takeover fixture", ExpectedGeneration: grant.Claim.Generation, Dispatch: &board.DispatchTakeover{Authorization: completed.Authorization, Revision: completed.Revision, Mode: "settle-completed"}})
	if err != nil {
		t.Fatal(err)
	}
	nextProof := client.Claim{Token: successor.Token, Generation: successor.Claim.Generation}
	_, err = a.DispatchAction(t.Context(), "late-settlement", task.TaskID, "settle", api.DispatchCommand{DispatchID: input.ID, Revision: completed.Revision}, proof)
	if !api.IsCode(err, api.CodeHeldByOther) && !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("old execution settled a new holder's binding: %v", err)
	}
	settled, err := b.Dispatch(t.Context(), task.TaskID, input.ID)
	if err != nil || settled.Settlement == nil || settled.Settlement.CardRevision != successor.Task.Revision {
		t.Fatalf("handoff did not atomically settle completed work: %+v %v", settled, err)
	}
	_, err = b.PutDocument(t.Context(), "successor-record", task.TaskID, api.Document{Path: "handoff.md", Text: "Successor inspected completed work without repeating it", ExpectedRevision: successor.Task.Revision}, &nextProof)
	if err != nil {
		t.Fatal(err)
	}
}
