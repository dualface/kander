package launch

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// withIntegrationTarget adds the target field after TASK_BRANCH. Like the group
// assignment of launchCoordinatorFixture, it is a fixture write that skips the
// contract decision a frozen card needs.
func withIntegrationTarget(t *testing.T, root, task, target string) {
	t.Helper()
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text := regexp.MustCompile(`(?m)^(- TASK_BRANCH:.*)$`).ReplaceAllString(s.Text, "$1\n- INTEGRATION_TARGET: "+target)
	if text == s.Text {
		t.Fatal("card header has no TASK_BRANCH line")
	}
	if err = writeCardFixture(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
}

// pushTarget makes `v2` exist on the bare origin at the delivery commit.
func pushTarget(t *testing.T, cwd, commit string) {
	t.Helper()
	gitIn(t, cwd, "branch", "v2", commit)
	origin := t.TempDir()
	gitIn(t, origin, "init", "-q", "--bare")
	gitIn(t, cwd, "remote", "add", "origin", origin)
	gitIn(t, cwd, "push", "-q", "origin", "v2")
	gitIn(t, cwd, "fetch", "-q", "origin")
}

// A group that merged back into a branch other than the mainline completes its
// wrap-up when its members declare that branch; without the field the same
// evidence is refused.
func TestWrapUpIntoNonMainlineTargetCompletesFromPrepareToReconcile(t *testing.T) {
	root, task, in, c := launchCoordinatorFixture(t)
	cwd := in.Evidence.WrapUp.Git.CWD
	pushTarget(t, cwd, in.Base)
	in.Evidence.WrapUp.Git.TargetRef = "refs/remotes/origin/v2"

	if _, err := PrepareBoundDispatch(root, in); err == nil {
		t.Fatal("v2 evidence accepted for a card with no target field")
	}
	withIntegrationTarget(t, root, task, "v2")

	d, c := launchFinishWrapUp(t, root, task, in, c)
	if _, err := os.Stat(launchWrapUpEvidence(root, c.GroupID, d.Input.ID)); err != nil {
		t.Fatal("move done wrote no Git evidence", err)
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil || s.Entry.State != "done" {
		t.Fatalf("card is %q: %v", s.Entry.State, err)
	}
	next, err := ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, d))
	if err != nil {
		t.Fatal(err)
	}
	if next.Members[task].Dispatch.PendingWrapUp {
		t.Fatal("completed wrap-up still pending")
	}
}

// The first delivery of a member may not come from the branch its card targets.
func TestFirstDeliveryRefusesTheTargetBranch(t *testing.T) {
	for _, target := range []string{"v2", ""} {
		t.Run("target "+target, func(t *testing.T) {
			root, task, in, c := launchCoordinatorFixture(t)
			cwd := in.Evidence.WrapUp.Git.CWD
			gitIn(t, cwd, "branch", "v2", in.Base)
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(s.Text, "- TASK_BRANCH: "+board.MetadataFrom(s.Text, "TASK_BRANCH"), "- TASK_BRANCH: v2", 1)
			if err = writeCardFixture(root, s.Entry, text); err != nil {
				t.Fatal(err)
			}
			if target != "" {
				withIntegrationTarget(t, root, task, target)
			}
			s, err = board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			r := board.CoordinatorReconcile{CWD: cwd, GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: {Revision: s.Revision, DeliveryCommit: in.Base}}}
			next, err := ReconcileCoordinator(context.Background(), root, r)
			if target != "" {
				if err == nil || next.Members[task].Blocked == nil {
					t.Fatal("a delivery from the target branch was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("the same branch name is an ordinary task branch without the field: %v", err)
			}
		})
	}
}
