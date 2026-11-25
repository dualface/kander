package server_test

import (
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/client"
)

func TestExecutionWorkspaceAcceptsTheCurrentAndTheLegacyTaskBranch(t *testing.T) {
	f := newFixture(t)
	project := f.project("host-a")
	for name, branch := range map[string]func(project, task string, generation uint64) string{
		"current": api.ExecutionBranch,
		"legacy":  api.LegacyExecutionBranch,
	} {
		card := todoTask(t, project, name+"-task-branch")
		grant, err := project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: name + "-branch", Owner: "codex", Start: &api.StartInput{Session: "codex session", Window: "tmux:$1:@1:%1"}})
		if err != nil {
			t.Fatal(err)
		}
		proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
		want := branch(project.ID(), card.TaskID, grant.Claim.Generation)
		recorded, err := project.Execution(t.Context(), "", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionWorkspace, Branch: want, ExpectedRevision: grant.Task.Revision}, proof)
		if err != nil {
			t.Fatalf("%s name %q refused: %v", name, want, err)
		}
		if !strings.Contains(recorded.Text, "- TASK_BRANCH: "+want+"\n") {
			t.Fatalf("%s name is not recorded on the card:\n%s", name, recorded.Text)
		}
	}
}

func TestExecutionWorkspaceRefusesAnotherGenerationTaskOrProject(t *testing.T) {
	f := newFixture(t)
	project := f.project("host-a")
	card := todoTask(t, project, "refused-task-branch")
	grant, err := project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "refused-branch", Owner: "codex", Start: &api.StartInput{Session: "codex session", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	generation, id := grant.Claim.Generation, project.ID()
	for name, branch := range map[string]string{
		"current, other generation": api.ExecutionBranch(id, card.TaskID, generation+1),
		"current, other task":       api.ExecutionBranch(id, "20261118-other-task", generation),
		"current, other project":    api.ExecutionBranch("p-other", card.TaskID, generation),
		"legacy, other generation":  api.LegacyExecutionBranch(id, card.TaskID, generation+1),
		"legacy, other task":        api.LegacyExecutionBranch(id, "20261118-other-task", generation),
		"legacy, other project":     api.LegacyExecutionBranch("p-other", card.TaskID, generation),
		"empty":                     "",
	} {
		_, err := project.Execution(t.Context(), "", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionWorkspace, Branch: branch, ExpectedRevision: grant.Task.Revision}, proof)
		if !api.IsCode(err, api.CodeEvidenceIncomplete) {
			t.Fatalf("%s: %q: want %s, got %v", name, branch, api.CodeEvidenceIncomplete, err)
		}
	}
}
