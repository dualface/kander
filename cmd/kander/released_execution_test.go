package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

func TestReleasedFixedExecutionCannotBecomeAnAnonymousWriter(t *testing.T) {
	for _, state := range []string{"done", "trash"} {
		t.Run(state, func(t *testing.T) {
			session, _ := moveServiceFixture(t)
			task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "released-writer", Title: "Released writer", Language: "en"})
			if err != nil {
				t.Fatal(err)
			}
			task = readyServiceCard(t, session, task)
			owner, found, err := session.Credential(task.TaskID)
			if err != nil || !found {
				t.Fatalf("owner: %v", err)
			}
			if state == "done" {
				closeNonGitFixturePlan(t, session, task)
			}
			task, err = session.Project.Show(t.Context(), task.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "report.md", Text: "Original completion evidence", ExpectedRevision: task.Revision}, owner.Claim()); err != nil {
				t.Fatal(err)
			}
			args := []string{"move", task.TaskID, state, "--result", "completed"}
			if state == "trash" {
				args = []string{"move", task.TaskID, state, "--result", "trashed", "--reason", "test", "--decision", "test"}
			}
			runKander(t, args...).require(t, "terminal transition")
			if err := remote.RemoveCredential(session.Home, owner); err != nil {
				t.Fatal(err)
			}
			t.Setenv(remote.EnvExecutionID, owner.ExecutionID)
			before, err := session.Project.Show(t.Context(), task.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			late := filepath.Join(t.TempDir(), "late.md")
			if err := os.WriteFile(late, []byte("Stale executor overwrote the report"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, spelling := range []string{task.TaskID, task.TaskID + ".md"} {
				current, err := session.Project.Show(t.Context(), task.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				output := runKander(t, "update", spelling, "--document", "report.md", "--file", late, "--expect-revision", fmt.Sprint(current.Revision))
				if output.code == 0 {
					t.Errorf("released fixed execution updated %s: %s", spelling, output.stdout)
				}
			}
			if _, err := session.ClaimsFor([]string{task.TaskID}); !api.IsCode(err, api.CodeLeaseInvalid) {
				t.Errorf("shared write proof resolution downgraded the fixed execution: %v", err)
			}
			_, err = session.MoveTask(t.Context(), task.TaskID, api.Move{State: "archived", Result: "completed", Reason: "stale agent", Decision: "stale context"})
			if !api.IsCode(err, api.CodeLeaseInvalid) {
				t.Errorf("released MoveTask did not retain its failed identity: %v", err)
			}
			after, err := session.Project.Show(t.Context(), task.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			report, err := session.Project.Document(t.Context(), task.TaskID, "report.md")
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision || after.State != before.State || report.Text != "Original completion evidence" {
				t.Errorf("stale writer changed committed evidence: before=%s/r%d after=%s/r%d report=%q", before.State, before.Revision, after.State, after.Revision, report.Text)
			}
			// Reads still work after release; authority failure is specific to writes.
			runKander(t, "show", "--json", task.TaskID).require(t, "read released task")
			other, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "never-held", Title: "Never held", Language: "en"})
			if err != nil {
				t.Fatal(err)
			}
			if proof, err := session.ClaimsFor([]string{other.TaskID}); err != nil || proof != nil {
				t.Fatalf("never-held member was confused with a released execution: %+v %v", proof, err)
			}
			t.Setenv(remote.EnvExecutionID, "")
			current, err := session.Project.Show(t.Context(), task.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			runKander(t, "update", task.TaskID, "--document", "report.md", "--file", late, "--expect-revision", fmt.Sprint(current.Revision)).require(t, "interactive unclaimed CAS remains available")

		})
	}
}
