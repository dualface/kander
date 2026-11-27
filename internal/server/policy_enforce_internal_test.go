package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

// todo creates a ready card through the client and returns its ID.
func (f *policyFixture) todo(slug string) string {
	f.t.Helper()
	ctx := f.t.Context()
	task, err := f.project.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: slug, Title: "Demo " + slug, Language: "en"})
	if err != nil {
		f.t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if task, err = f.project.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		f.t.Fatal(err)
	}
	if _, err = f.project.Move(ctx, "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		f.t.Fatal(err)
	}
	return task.TaskID
}

// A claim that crashed after its pending receipt is resent with the same key
// and a newer policy revision: the revision is not part of the receipt
// digest, so the resend is the same claim, not an idempotency mismatch. A
// stale resend is refused first and leaves the receipt pending.
func TestPendingClaimReceiptAcceptsAResendWithANewerPolicyRevision(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	if _, err := f.put("policy-1", 0, policyValues("main-only")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.put("policy-2", 1, policyValues("main-only")); err != nil {
		t.Fatal(err)
	}
	id := f.todo("pending-claim")
	p := f.policyProject()
	key := "crashed-claim"
	request := api.ClaimRequest{ClientID: "policy-client", ExecutionID: "exec-crashed", Owner: "claude"}
	operation := "claim:" + id
	at := receiptScope{key: key}.receipt(operation)
	pending := receipt{Schema: receiptSchema, Key: key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptPending, CreatedAt: time.Now().UTC()}
	if err := writeJSON(p.root, at.rel(), pending); err != nil {
		t.Fatal(err)
	}

	stale := request
	stale.PolicyRevision, stale.BranchMode = 1, "main-develop"
	out := p.claim(receiptScope{key: key}, id, stale)
	requireAPICode(t, out.err, api.CodePolicyRevisionStale, http.StatusConflict)
	var still receipt
	if found, err := readJSON(p.root, at.rel(), &still); err != nil || !found || still.Status != receiptPending {
		t.Fatalf("stale resend touched the receipt: %+v %v", still, err)
	}

	current := request
	current.PolicyRevision, current.BranchMode = 2, "main-develop"
	out = p.claim(receiptScope{key: key}, id, current)
	if out.err != nil || out.status != http.StatusOK {
		t.Fatalf("resend with the current revision: %d %v", out.status, out.err)
	}
	record, found, err := p.readClaim(id)
	if err != nil || !found || record.BranchSnapshot == nil || record.BranchSnapshot.BranchMode != "main-only" || record.BranchSnapshot.PolicyRevision != 2 {
		t.Fatalf("claim record: %+v %v", record, err)
	}
}

// A card that has a claim record from before snapshots, and a group with a
// group claim record from before them, keep having none: their claims check
// no policy revision and create no snapshot.
func TestClaimsOfCardsAndGroupsClaimedBeforeSnapshotsCreateNone(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	if _, err := f.put("policy-1", 0, policyValues("main-only")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.put("policy-2", 1, policyValues("main-only")); err != nil {
		t.Fatal(err)
	}
	id := f.todo("legacy-claim")
	p := f.policyProject()
	now := p.server.now()
	if err := p.writeClaim(claimRecord{TaskID: id, ClientID: "old-client", ExecutionID: "exec-old", Owner: "claude", Generation: 1, Status: api.ClaimReleased, RenewedAt: now, ExpiresAt: now}); err != nil {
		t.Fatal(err)
	}
	if p.taskClaimCreatesSnapshot(id) {
		t.Fatal("a card with a claim record would get a snapshot")
	}
	if err := p.checkSnapshotClaim(p.taskClaimCreatesSnapshot(id), 1, "main-develop"); err != nil {
		t.Fatalf("an old card is checked: %v", err)
	}
	out := p.claim(receiptScope{key: "legacy-claim"}, id, api.ClaimRequest{ClientID: "policy-client", ExecutionID: "exec-new", Owner: "claude", PolicyRevision: 1, BranchMode: "main-develop"})
	if out.err != nil {
		t.Fatalf("claim of an old card with an old revision: %v", out.err)
	}
	record, _, err := p.readClaim(id)
	if err != nil || record.Generation != 2 || record.BranchSnapshot != nil {
		t.Fatalf("old card record: %+v %v", record, err)
	}

	group := "20261127-legacy-group"
	if err := stageGroupClaimFile(p, claimRecord{TaskID: group, ClientID: "old-client", ExecutionID: "old-coordinator", Owner: "claude", Generation: 1, Status: api.ClaimReleased, RenewedAt: now, ExpiresAt: now}); err != nil {
		t.Fatal(err)
	}
	if p.groupClaimCreatesSnapshot(group) {
		t.Fatal("a group with a group claim record would get a snapshot")
	}
	if snapshot := p.effectiveSnapshot(nil, "# Member\n\n- TYPE: Feature\n- TASK_GROUP: "+group+"\n"); snapshot != nil {
		t.Fatalf("member of an old group: %+v", snapshot)
	}
}

func stageGroupClaimFile(p *project, r claimRecord) error {
	return writeJSON(p.root, servicePath(groupClaimName(r.TaskID)), r)
}

// A batch whose members differ in SIZE takes the large stages: the policy
// requires PMQA for large tasks only, so a batch of two small cards may mark
// it N/A, and the same batch with a large member may not.
func TestMixedSizeBatchUsesTheLargeStages(t *testing.T) {
	f := newPolicyFixture(t, Hooks{})
	written, err := f.put("policy-1", 0, policyValues("main-develop"))
	if err != nil {
		t.Fatal(err)
	}
	small, other := f.todo("mixed-small"), f.todo("mixed-other")
	large, err := f.project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "mixed-large", Title: "Demo mixed-large", Language: "en", Large: true})
	if err != nil {
		t.Fatal(err)
	}
	p := f.policyProject()
	requirements := map[string]string{"PMQA": "N/A: small cards only", "Security": "required"}
	if _, err := p.checkNewReviewBatches(written.Revision, []newReviewBatch{{id: "small-batch", taskIDs: []string{small, other}, requirements: requirements}}); err != nil {
		t.Fatalf("a batch of small cards under the small stages: %v", err)
	}
	_, err = p.checkNewReviewBatches(written.Revision, []newReviewBatch{{id: "mixed-batch", taskIDs: []string{small, large.TaskID}, requirements: requirements}})
	if e := requireAPICode(t, err, api.CodePolicyRequirementUnmet, http.StatusBadRequest); !strings.Contains(e.Message, "requires PMQA for large tasks") {
		t.Fatalf("mixed batch refusal: %v", e)
	}
}
