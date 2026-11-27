package server_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

func branchPolicy(mode string) enforcedPolicy {
	return enforcedPolicy{mode: mode, smallPMQA: "auto", largePM: "auto", review: true}
}

func wantSnapshot(t *testing.T, what string, got *api.BranchSnapshot, mode string, revision uint64, scope string) {
	t.Helper()
	if got == nil || *got != (api.BranchSnapshot{BranchMode: mode, PolicyRevision: revision, Scope: scope}) {
		t.Fatalf("%s: want %s/%d/%s, got %+v", what, mode, revision, scope, got)
	}
}

// The first claim of a card outside a task group fixes its branch snapshot
// from the policy; a later policy change and a takeover keep it, and the
// takeover checks no policy revision.
func TestFirstClaimFixesTheBranchSnapshotOfACard(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	first := putPolicy(t, p, branchPolicy("main-only"))
	task := todoTask(t, p, "snapshot-card")
	grant, err := claimFor(t, p, "", task.TaskID, "exec-first", first, "main-develop")
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot(t, "grant", grant.Claim.BranchSnapshot, "main-only", first, api.BranchSnapshotTask)
	putPolicy(t, p, branchPolicy("main-develop"))
	shown, err := p.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot(t, "show after a policy change", shown.Claim.BranchSnapshot, "main-only", first, api.BranchSnapshotTask)

	f.clock.advance(91 * time.Second)
	next, err := p.Takeover(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-second", Owner: "claude", Decision: "takeover fixture", ExpectedGeneration: grant.Claim.Generation, ExpectedRevision: shown.Revision, PolicyRevision: first, BranchMode: "main-develop"})
	if err != nil {
		t.Fatalf("takeover with an old policy revision: %v", err)
	}
	wantSnapshot(t, "takeover", next.Claim.BranchSnapshot, "main-only", first, api.BranchSnapshotTask)
}

// Without a policy the snapshot is the branch mode the client reports, which
// a first claim must name.
func TestFirstClaimWithoutPolicyTakesTheReportedBranchMode(t *testing.T) {
	p := newFixture(t).project("host-a")
	task := todoTask(t, p, "reported-mode")
	for _, mode := range []string{"", "trunk"} {
		_, err := claimFor(t, p, "", task.TaskID, "exec-"+mode, 0, mode)
		refusal(t, err, api.CodeInvalidRequest, http.StatusBadRequest, "", "branch_mode")
	}
	grant, err := claimFor(t, p, "", task.TaskID, "exec-reported", 0, "main-only")
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot(t, "grant", grant.Claim.BranchSnapshot, "main-only", 0, api.BranchSnapshotTask)
}

// A first claim with a stale revision writes nothing, not even a receipt:
// the same key with the current revision then claims. A replay of that done
// receipt answers it again without checking the revision.
func TestStaleFirstClaimWritesNothing(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	old := putPolicy(t, p, branchPolicy("main-develop"))
	current := putPolicy(t, p, branchPolicy("main-only"))
	task := todoTask(t, p, "stale-claim")
	_, err := claimFor(t, p, "claim-key", task.TaskID, "exec-stale", old, "main-develop")
	refusal(t, err, api.CodePolicyRevisionStale, http.StatusConflict, "", "revision 2")
	shown, err := p.Show(t.Context(), task.TaskID)
	if err != nil || shown.State != "todo" || shown.Claim != nil {
		t.Fatalf("stale claim wrote: %+v %v", shown, err)
	}
	grant, err := claimFor(t, p, "claim-key", task.TaskID, "exec-stale", current, "main-develop")
	if err != nil {
		t.Fatalf("resend with the current revision: %v", err)
	}
	wantSnapshot(t, "grant", grant.Claim.BranchSnapshot, "main-only", current, api.BranchSnapshotTask)
	putPolicy(t, p, branchPolicy("main-develop"))
	replay, err := claimFor(t, p, "claim-key", task.TaskID, "exec-stale", current, "main-develop")
	if err != nil || replay.Claim.Generation != grant.Claim.Generation {
		t.Fatalf("replay of the done receipt: %+v %v", replay, err)
	}
}

// A group claim fixes the snapshot of the group. Its members get none of
// their own, claim without a policy check, and show the group's; a later
// group takes the policy in force when it is first claimed.
func TestGroupClaimFixesTheSnapshotOfTheGroupForItsMembers(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	first := putPolicy(t, p, branchPolicy("main-develop"))
	request := groupRequest(t, p)
	request.PolicyRevision, request.BranchMode = first, "main-only"
	grant, err := p.ClaimGroup(t.Context(), "", request.Request.GroupID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot(t, "group grant", grant.Claim.BranchSnapshot, "main-develop", first, api.BranchSnapshotGroup)

	second := putPolicy(t, p, branchPolicy("main-only"))
	view, err := p.Group(t.Context(), request.Request.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot(t, "group view after a policy change", view.Claim.BranchSnapshot, "main-develop", first, api.BranchSnapshotGroup)
	member := request.Request.Members[0]
	memberGrant, err := claimFor(t, p, "", member, "exec-member", first, "main-only")
	if err != nil {
		t.Fatalf("member claim with an old revision: %v", err)
	}
	wantSnapshot(t, "member grant", memberGrant.Claim.BranchSnapshot, "main-develop", first, api.BranchSnapshotGroup)
	shown, err := p.Show(t.Context(), member)
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot(t, "member show", shown.Claim.BranchSnapshot, "main-develop", first, api.BranchSnapshotGroup)

	other := todoTask(t, p, "later-group-member", "20261127-later-group")
	later := request
	later.ExecutionID = "later-coordinator"
	later.Request.GroupID, later.Request.Members = "20261127-later-group", []string{other.TaskID}
	_, err = p.ClaimGroup(t.Context(), "later-key", later.Request.GroupID, later, nil)
	refusal(t, err, api.CodePolicyRevisionStale, http.StatusConflict, "")
	later.PolicyRevision = second
	laterGrant, err := p.ClaimGroup(t.Context(), "later-key", later.Request.GroupID, later, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot(t, "later group", laterGrant.Claim.BranchSnapshot, "main-only", second, api.BranchSnapshotGroup)
}
