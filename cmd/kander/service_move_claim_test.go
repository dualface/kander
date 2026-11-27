package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
)

// TestServiceCLIMoveRefusesClaimOptionsItWouldIgnore pins the usage errors of `--owner`: a claim
// only carries the owner and the decision, so every other move option, and a target other than
// working, is refused before the server sees a request.
func TestServiceCLIMoveRefusesClaimOptionsItWouldIgnore(t *testing.T) {
	session, _ := moveServiceFixture(t)
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "claim-refusals", Title: "Claim refusals", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	todoServiceCard(t, session, task)
	before, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil || before.State != "todo" || before.Claim != nil {
		t.Fatalf("fixture: %+v %v", before, err)
	}
	revision := strconv.FormatUint(before.Revision, 10)
	for _, extra := range [][]string{
		{"todo", "--owner", "codex"},
		{"working", "--owner", "codex", "--expect-revision", revision},
		{"working", "--owner", "codex", "--result", "completed"},
		{"working", "--owner", "codex", "--reason", "r"},
		{"working", "--owner", "codex", "--duplicate-of", task.TaskID},
		{"working", "--owner", "codex", "--delivery-commit", strings.Repeat("a", 40)},
		{"working", "--owner", "codex", "--disposition", "reviews/a.json"},
	} {
		args := append([]string{"move", task.TaskID}, extra...)
		output := runKander(t, args...)
		if output.code != 2 || !strings.Contains(output.stderr, "--owner") {
			t.Fatalf("%v: exit=%d stderr=%q", extra, output.code, output.stderr)
		}
		after, err := session.Project.Show(t.Context(), task.TaskID)
		if err != nil || after.State != before.State || after.Revision != before.Revision || after.Claim != nil {
			t.Fatalf("%v changed the card: %+v %v", extra, after, err)
		}
	}

	runKander(t, "move", task.TaskID, "working", "--owner", "codex").require(t, "claim")
	claimed, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil || claimed.State != "working" || claimed.Claim == nil || claimed.Claim.Owner != "codex" || claimed.Claim.Status != api.ClaimActive {
		t.Fatalf("claim: %+v %v", claimed, err)
	}

	other, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "archive-regression", Title: "Archive regression", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	runKander(t, "move", other.TaskID, "archived", "--result", "cancelled", "--reason", "r", "--decision", "d").require(t, "archive")
	if archived, err := session.Project.Show(t.Context(), other.TaskID); err != nil || archived.State != "archived" {
		t.Fatalf("archive: %+v %v", archived, err)
	}
}
