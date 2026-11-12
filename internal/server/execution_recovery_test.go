package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func TestLaunchEvidenceSurvivesServerRestartAndLaterLauncherWrites(t *testing.T) {
	f := newFixture(t)
	project := f.project("host-a")
	card := todoTask(t, project, "restart-start-cursor")
	grant, err := project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "restart-launch", Owner: "codex", Start: &api.StartInput{Session: "codex durable-session", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	state, err := project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || state.Standing.Evidence {
		t.Fatalf("initial state: %+v %v", state, err)
	}
	card, err = project.Execution(t.Context(), "", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionWindow, Window: "tmux:$1:@1:%2"}, proof)
	if err != nil {
		t.Fatal(err)
	}
	state, err = project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || state.Standing.Evidence {
		t.Fatalf("own window write counted as executor evidence: %+v %v", state, err)
	}
	if _, err := project.PutDocument(t.Context(), "executor-progress", card.TaskID, api.Document{Path: "progress.md", Text: "Executor has started author work", ExpectedRevision: card.Revision}, &proof); err != nil {
		t.Fatal(err)
	}
	f.stop()
	f = start(t, f.dir, f.clock)
	project = f.project("host-a")
	state, err = project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || !state.Standing.Evidence {
		t.Fatalf("restart erased executor evidence: %+v %v", state, err)
	}
	if _, err := project.Execution(t.Context(), "late-launcher-fact", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionWindow, Window: "tmux:$1:@1:%3"}, proof); err != nil {
		t.Fatal(err)
	}
	f.stop()
	f = start(t, f.dir, f.clock)
	project = f.project("host-a")
	state, err = project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || !state.Standing.Evidence {
		t.Fatalf("own later write erased latched evidence: %+v %v", state, err)
	}
	_, err = project.Execution(t.Context(), "unsafe-rollback", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	if _, err := project.Execution(t.Context(), "record-unresolved", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionUnresolve, Reason: "late launch failed after executor progress"}, proof); err != nil {
		t.Fatal(err)
	}
	state, err = project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || state.Record.Status != board.ExecutionUnresolved {
		t.Fatalf("unresolved state: %+v %v", state, err)
	}
	progress, err := project.Document(t.Context(), card.TaskID, "progress.md")
	if err != nil || !strings.Contains(progress.Text, "Executor has started") {
		t.Fatalf("progress lost: %+v %v", progress, err)
	}
	adopt := api.ExecutionUpdate{Action: api.ExecutionAdopt, Decision: "explicit adoption fixture", ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Presence: &api.AgentObservation{Status: "alive", ObservedAt: time.Now().Add(24 * time.Hour)}}
	// The client's wall clock may differ by a day. Only the server-clock ticket
	// bounds freshness; a forged or stale ticket still refuses the same body.
	bad := adopt
	bad.ObservationTicket += "forged"
	_, err = project.Execution(t.Context(), "forged-observation", card.TaskID, bad, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	f.clock.advance(31 * time.Second)
	_, err = project.Execution(t.Context(), "stale-observation", card.TaskID, adopt, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	state, err = project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil {
		t.Fatal(err)
	}
	adopt.ObservationTicket = state.ObservationTicket
	if _, err := project.Execution(t.Context(), "confirmed-adoption", card.TaskID, adopt, proof); err != nil {
		t.Fatal(err)
	}
	state, err = project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || state.Record.Status != board.ExecutionRunning {
		t.Fatalf("adopted state: %+v %v", state, err)
	}
}

func TestRestartedLauncherWithoutExecutorEvidenceCanRollback(t *testing.T) {
	f := newFixture(t)
	project := f.project("host-a")
	card := todoTask(t, project, "restart-clean-launch")
	grant, err := project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "clean-launch", Owner: "codex", Start: &api.StartInput{Session: "codex pending-session", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	f.stop()
	f = start(t, f.dir, f.clock)
	project = f.project("host-a")
	state, err := project.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || state.Standing.Evidence {
		t.Fatalf("clean restart state: %+v %v", state, err)
	}
	rolled, err := project.Execution(t.Context(), "rollback-once", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, proof)
	if err != nil || rolled.State != "todo" || rolled.Claim == nil || rolled.Claim.Status != api.ClaimReleased {
		t.Fatalf("rollback: %+v %v", rolled, err)
	}
	replay, err := project.Execution(t.Context(), "rollback-once", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, proof)
	if err != nil || replay.Revision != rolled.Revision {
		t.Fatalf("rollback replay: %+v %v", replay, err)
	}
}
