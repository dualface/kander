package remote_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

// linearReviewedGroup reviews a group of two members whose deliveries stack:
// the second builds on the first, so the joint review range stays linear.
func linearReviewedGroup(t *testing.T) (f *integrationFixture, ids []string, options remote.IntegrationOptions, tip string) {
	t.Helper()
	f = newIntegrationGitFixture(t)
	group := "20261117-contain-group"
	ids, owners := integrationGroupCards(t, f, group)
	if _, err := f.a.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "contain-session", Basis: "explicitly coordinate the complete group", Members: ids}, "explicit group coordination"); err != nil {
		t.Fatal(err)
	}
	options = remote.IntegrationOptions{OperationID: "initialize-contain-group", Purpose: "initialize", TargetRef: "refs/heads/group/" + group, Commit: f.base, GroupID: group, Decision: "initialize the confirmed common anchor"}
	if _, err := f.a.Integrate(t.Context(), f.first, options); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.PublishCommit(t.Context(), f.first, ids[0], f.commit, owners[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.FetchDelivery(t.Context(), f.second, ids[0], f.commit); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.second, "checkout", "-q", "-B", "stacked", f.commit)
	tip = commitFile(t, f.second, "second.txt", "second member work\n", "Second member work")
	if _, err := f.b.PublishCommit(t.Context(), f.second, ids[1], tip, owners[1]); err != nil {
		t.Fatal(err)
	}
	for i, commit := range []string{f.commit, tip} {
		session := []*remote.Session{f.a, f.b}[i]
		if _, err := session.Project.Move(t.Context(), "", ids[i], api.Move{State: "review", DeliveryCommit: commit}, owners[i].Claim()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.a.FetchDelivery(t.Context(), f.first, ids[1], tip); err != nil {
		t.Fatal(err)
	}
	options.OperationID, options.Purpose, options.Commit, options.Contributors = "assemble-stacked-group", "assemble", tip, ids
	if _, err := f.a.Integrate(t.Context(), f.first, options); err != nil {
		t.Fatal(err)
	}
	integrationJointClose(t, f, ids, tip)
	options.OperationID, options.Purpose, options.TargetRef, options.Contributors = "deliver-stacked-group", "deliver", "refs/heads/v2", nil
	return f, ids, options, tip
}

func (f *integrationFixture) verifyGroupReceipts(ids []string, result api.Integration) {
	f.t.Helper()
	if err := f.b.VerifyIntegration(f.t.Context(), f.second, result); err != nil {
		f.t.Fatalf("group receipt verification: %v", err)
	}
	for _, id := range ids {
		value, err := f.b.Project.TaskIntegration(f.t.Context(), id)
		if err != nil || value.Request.ExecutionID != result.Request.ExecutionID {
			f.t.Fatalf("member receipt of %s: %+v %v", id, value, err)
		}
		g := board.DispatchIntegration{IntegrationID: result.ID, IntegrationExecution: result.Request.ExecutionID, IntegrationGeneration: result.Generation, DispatchID: "group-wrap-" + id, TaskID: id, CWD: "project:" + f.a.Info.ID, SourceCommit: result.Request.Commit, ReviewTarget: result.Request.Commit, ReviewBase: f.base, TargetCommit: result.ObservedTarget, TargetRef: result.Request.TargetRef, Author: "coordinator", Basis: "published integration receipt", VerifiedAt: time.Now().UTC()}
		if err := f.b.VerifyWrapUpIntegration(f.t.Context(), f.second, g); err != nil {
			f.t.Fatalf("wrap-up evidence of %s: %v", id, err)
		}
	}
}

func TestIntegrationGroupViaCarrierAfterTheTargetAdvanced(t *testing.T) {
	f, ids, options, tip := linearReviewedGroup(t)
	foreign := f.advanceTarget("other.txt")
	deliveryGit(t, f.first, "fetch", "-q", "origin")
	deliveryGit(t, f.first, "checkout", "-q", "--detach", tip)
	deliveryGit(t, f.first, "merge", "--no-edit", "origin/v2")
	carrier := deliveryGit(t, f.first, "rev-parse", "HEAD")
	deliveryGit(t, f.first, "push", "origin", carrier+":refs/heads/group-carrier")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("a group delivery with a diverged target needed no carrier")
	}
	if got := f.targetHead(); got != foreign {
		t.Fatal("rejected group delivery moved the target")
	}
	options.OperationID, options.ViaCommit = "deliver-stacked-group-via", carrier
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Containment != api.IntegrationVia || result.Request.Commit != tip || result.ObservedTarget != carrier || f.targetHead() != carrier {
		t.Fatalf("receipt: %+v", result)
	}
	f.verifyGroupReceipts(ids, result)
}

func TestIntegrationGroupReplayedRangeAlreadyInTheTarget(t *testing.T) {
	f, ids, options, tip := linearReviewedGroup(t)
	deliveryGit(t, f.second, "fetch", "-q", "origin")
	_, target := f.pickOnTarget(f.commit, tip)
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	replay := result.Request.Replay
	if result.Request.Containment != api.IntegrationReplayed || replay == nil || replay.ReviewBase != f.base || replay.ReviewTarget != tip || replay.ReplayTarget != target {
		t.Fatalf("receipt: %+v", result.Request)
	}
	if result.ObservedTarget != target || f.targetHead() != target {
		t.Fatalf("a replayed group delivery moved the target: %s", f.targetHead())
	}
	f.verifyGroupReceipts(ids, result)
	if filepath.Base(result.Request.TargetRef) != "v2" {
		t.Fatal(result.Request.TargetRef)
	}
}
