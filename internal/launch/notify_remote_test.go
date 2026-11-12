package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

func TestRemoteNotifyFreezesProposalAndHolderPreparesItAtomically(t *testing.T) {
	_, session, card, forbidden, _ := remoteLifecycleFixture(t, "remote-notification")
	grant, err := session.Project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "notification-holder", Owner: "claude", Start: &api.StartInput{Session: "claude notification", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: session.ServerURL, ProjectID: session.Info.ID, TaskID: card.TaskID, ClientID: session.Client.ClientID(), ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.Execution(t.Context(), "", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	options := DispatchOptions{ID: "notification-sync", Kind: "sync", Base: strings.Repeat("a", 40)}
	request, err := NotifyRemote(card.TaskID, "Inspect this synchronization", "", "", true, 120, options)
	if err != nil || request.Status != api.RequestOpen || request.Dispatch == nil {
		t.Fatalf("queued proposal: %+v %v", request, err)
	}
	if request.AcceptWithin != api.DurationOf(120*time.Second) || request.Dispatch.ConfirmBy.Sub(request.Dispatch.CreatedAt) != 120*time.Second {
		t.Fatal("service did not freeze the relative acceptance window")
	}
	replay, err := NotifyRemote(card.TaskID, "Inspect this synchronization", "", "", true, 240, options)
	if err != nil || replay.ID != request.ID || !replay.Dispatch.ConfirmBy.Equal(request.Dispatch.ConfirmBy) {
		t.Fatalf("retry reset the proposal: %+v %v", replay, err)
	}
	if _, err := NotifyRemote(card.TaskID, "Different material", "", "", true, 120, options); !api.IsCode(err, api.CodeIdempotencyMismatch) {
		t.Fatalf("proposal changed: %v", err)
	}
	prepared, err := session.Project.DispatchAction(t.Context(), "prepare-proposal", card.TaskID, "prepare", api.DispatchCommand{Input: request.Dispatch, CardRevision: request.CardRevision}, *credential.Claim())
	if err != nil || prepared.State != board.DispatchPrepared {
		t.Fatalf("holder prepare: %+v %v", prepared, err)
	}
	requests, err := session.Project.Requests(t.Context(), card.TaskID)
	if err != nil || len(requests) != 1 || requests[0].Status != api.RequestAccepted || requests[0].Revision != request.Revision+1 {
		t.Fatalf("proposal and dispatch did not commit together: %+v %v", requests, err)
	}
	current, err := session.Project.Show(t.Context(), card.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := session.Project.DispatchAction(t.Context(), "prepare-new-http-key", card.TaskID, "prepare", api.DispatchCommand{Input: request.Dispatch, CardRevision: current.Revision}, *credential.Claim())
	if err != nil || again.Revision != prepared.Revision {
		t.Fatalf("formal preparation replay: %+v %v", again, err)
	}
	if _, err := session.Project.DispatchAction(t.Context(), "end-proposal", card.TaskID, "end", api.DispatchCommand{DispatchID: prepared.Input.ID, Revision: prepared.Revision, State: board.DispatchCancelled, Reason: "fixture cancellation"}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	options.ID = "stale-notification"
	stale, err := NotifyRemote(card.TaskID, "Inspect the next version", "", "", true, 120, options)
	if err != nil {
		t.Fatal(err)
	}
	current, err = session.Project.PutDocument(t.Context(), "later-progress", card.TaskID, api.Document{Path: "progress.md", Text: "Card changed after proposal", ExpectedRevision: stale.CardRevision}, credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Project.AnswerRequest(t.Context(), client.NewKey(), card.TaskID, stale.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: stale.Revision}, *credential.Claim())
	if !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("stale proposal reserved a terminal effect: %v", err)
	}
	_, err = session.Project.DispatchAction(t.Context(), "stale-formal-preparation", card.TaskID, "prepare", api.DispatchCommand{Input: stale.Dispatch, CardRevision: current.Revision}, *credential.Claim())
	if !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("stale proposal acquired formal authority: %v", err)
	}
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "collaborator"))
	if _, err := NotifyRemote(card.TaskID, "Foreign pane", "", "%1", true, 120, DispatchOptions{}); err == nil {
		t.Fatal("foreign pane override reached a terminal")
	}
	plain, err := NotifyRemote(card.TaskID, "A shared message", "", "", true, 120, DispatchOptions{})
	if err != nil || plain.Status != api.RequestOpen || plain.Dispatch != nil || plain.TargetGeneration != credential.Generation {
		t.Fatalf("foreign request: %+v %v", plain, err)
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("notification touched local board: %v", err)
	}
}
