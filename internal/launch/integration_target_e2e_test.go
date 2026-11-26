package launch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/board"
)

// The local board path remains available for historical machine-path evidence.
// The v2 CLI's service receipt path is exercised by the distributed tests.
func TestWrapUpIntoNonMainlineTargetCompletesFromPrepareToReconcile(t *testing.T) {
	root, task, in, c := launchCoordinatorFixture(t)
	w := in.Evidence.WrapUp
	origin := t.TempDir()
	gitIn(t, origin, "init", "-q", "--bare")
	gitIn(t, w.Git.CWD, "remote", "add", "origin", origin)
	gitIn(t, w.Git.CWD, "push", "-q", "origin", in.Base+":refs/heads/v2")
	gitIn(t, w.Git.CWD, "fetch", "-q", "origin")

	w.Git.TargetRef = "refs/remotes/origin/v2"
	w.Git.DispatchID, w.Git.TaskID = in.ID, task
	w.Git.VerifiedAt = time.Now().UTC()
	w.Artifact = board.ArtifactReference{TaskID: task, Path: "dispatches/" + in.ID + "/integration.json"}
	if _, err := board.PrepareDispatch(root, in); err == nil || !strings.Contains(err.Error(), "integration binding") {
		t.Fatalf("v2 evidence for a card without a target: %v", err)
	}

	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(s.Text, "\n- RESULT:", "\n- INTEGRATION_TARGET: v2\n- RESULT:", 1)
	if err := board.UpdateDocument(root, task, board.UpdateOptions{
		Document: "spec.md", Text: text, ExpectedRevision: s.Revision, ContractDecision: "Fixture targets v2",
	}); err != nil {
		t.Fatal(err)
	}
	d, err := board.PrepareDispatch(root, in)
	if err != nil {
		t.Fatal(err)
	}
	observedWrapUp := false
	verify := func(ctx context.Context, facts board.CoordinatorGitFacts) error {
		observedWrapUp = true
		if facts.IntegrationTarget != "v2" || facts.Integration.TargetRef != w.Git.TargetRef {
			t.Fatalf("unexpected Git facts: %+v", facts)
		}
		return verifyWrapUpGit(ctx, "", facts)
	}
	request := func(d board.Dispatch) board.CoordinatorReconcile {
		t.Helper()
		s, err := board.ReadSnapshot(root, task)
		if err != nil {
			t.Fatal(err)
		}
		o := board.CoordinatorObservation{Revision: s.Revision, DispatchID: d.Input.ID, Epoch: d.Authorization.Epoch, Base: d.Input.Base}
		if d.Completed != nil {
			o.DeliveryCommit = d.Completed.DeliveryCommit
		}
		return board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: o}}
	}
	c, err = board.ReconcileCoordinator(t.Context(), root, request(d), verify)
	if err != nil || !observedWrapUp {
		t.Fatalf("reconcile did not verify wrap-up: %v", err)
	}
	s, err = board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	s, err = board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text = strings.ReplaceAll(s.Text, "<FILL_IN>", "测试完成")
	text = strings.Replace(text, "## SUMMARY\n", "## SUMMARY\n\n测试交付和收尾完成。\n", 1)
	if err := board.UpdateDocument(root, task, board.UpdateOptions{
		Document: "spec.md", Text: text, ExpectedRevision: s.Revision, Authorization: d.Authorization,
	}); err != nil {
		t.Fatal(err)
	}
	s, err = board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := board.MoveWithOptions(s.Entry, root, "done", board.MoveOptions{
		Authorization: d.Authorization, Result: "completed", DeliveryCommit: in.Base,
	}); err != nil {
		t.Fatal(err)
	}
	witness := filepath.Join(root, ".kander", "groups", c.GroupID, "wrap-up-git", d.Input.ID+".json")
	if _, err := os.Stat(witness); err != nil {
		t.Fatal("move done wrote no Git evidence", err)
	}
	d, err = board.ReadDispatch(root, task, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := board.ReconcileCoordinator(t.Context(), root, request(d), verify)
	if err != nil || next.Members[task].Dispatch.PendingWrapUp {
		t.Fatalf("completed wrap-up still pending: %v", err)
	}
}

func TestFirstDeliveryRefusesTheTargetBranch(t *testing.T) {
	for _, tc := range []struct {
		name, target, branch string
		blocked              bool
	}{
		{"delivery from target", "v2", "v2", true},
		{"same branch without target field", "", "v2", false},
		{"delivery from member branch", "v2", "member", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, task, in, c := launchCoordinatorFixture(t)
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			oldBranch := board.MetadataFrom(s.Text, board.FieldTaskBranch)
			text := strings.Replace(s.Text, "- TASK_BRANCH: "+oldBranch+"\n", "- TASK_BRANCH: "+tc.branch+"\n", 1)
			if tc.target != "" {
				text = strings.Replace(text, "\n- RESULT:", "\n- INTEGRATION_TARGET: "+tc.target+"\n- RESULT:", 1)
			}
			if err := board.UpdateDocument(root, task, board.UpdateOptions{
				Document: "spec.md", Text: text, ExpectedRevision: s.Revision, ContractDecision: "Fixture declares its integration target",
			}); err != nil {
				t.Fatal(err)
			}
			s, err = board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			req := board.CoordinatorReconcile{
				GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority,
				Members: map[string]board.CoordinatorObservation{task: {Revision: s.Revision, DeliveryCommit: in.Base}},
			}
			verified := false
			next, err := board.ReconcileCoordinator(t.Context(), root, req, func(_ context.Context, facts board.CoordinatorGitFacts) error {
				verified = true
				if facts.Delivery == nil || facts.Delivery.Branch != tc.branch || facts.IntegrationTarget != tc.target {
					t.Fatalf("unexpected first-delivery Git facts: %+v", facts)
				}
				return nil
			})
			member := next.Members[task]
			if tc.blocked {
				if member.Blocked == nil || verified || member.DeliveryCommit != "" {
					t.Fatalf("first delivery from the target was accepted: %+v, %v", member, err)
				}
				return
			}
			if err != nil || !verified || member.DeliveryCommit != in.Base {
				t.Fatalf("first delivery was not accepted and verified: %+v, %v", member, err)
			}
		})
	}
}
