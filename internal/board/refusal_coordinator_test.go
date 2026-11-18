package board

import (
	"context"
	"strings"
	"testing"
)

const (
	firstToken  = "tok-first-9f3a41c2-secret"
	secondToken = "tok-second-77c10d5e-secret"
)

// secretFree requires that no token of the group appears in a refusal.
func secretFree(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("no refusal")
	}
	for _, secret := range []string{firstToken, secondToken, "someone-else-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("a token is written in %q", err)
		}
	}
}

func claimWith(token string, c CoordinatorCheckpoint, ids ...string) CoordinatorClaim {
	return CoordinatorClaim{GroupID: coordinatorGroup, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Owner: "successor", Token: token, Basis: "basis", Members: ids}
}

// R15: a claim names the revision or the epoch it got wrong, and the reused token
// only as reused. The values of the CAS appear only when the CAS fact fails.
func TestClaimRefusalNamesValues(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := coordinatorCard(t, root, "claim-values").Entry.TaskID
	c, err := ClaimCoordinator(context.Background(), root, CoordinatorClaim{GroupID: coordinatorGroup, Owner: "coordinator", Token: firstToken, Basis: "basis", Members: []string{id}})
	if err != nil {
		t.Fatal(err)
	}

	stale := claimWith(secondToken, c, id)
	stale.ExpectedRevision = 9
	_, err = ClaimCoordinator(context.Background(), root, stale)
	secretFree(t, err)
	requireRefusal(t, err, "board.coordinator_conflict", []string{"claim CAS or reused token", "expected_revision is 9", "group " + coordinatorGroup + " is at revision 1", "kander coordinator show " + coordinatorGroup}, "expected_epoch", "the token is the same")

	fenced := claimWith(secondToken, c, id)
	fenced.ExpectedEpoch = 4
	_, err = ClaimCoordinator(context.Background(), root, fenced)
	secretFree(t, err)
	requireRefusal(t, err, "board.coordinator_conflict", []string{"claim CAS or reused token", "expected_epoch is 4", "coordinator epoch 1"}, "expected_revision", "same as the token")

	reused := claimWith(firstToken, c, id)
	_, err = ClaimCoordinator(context.Background(), root, reused)
	secretFree(t, err)
	requireRefusal(t, err, "board.coordinator_conflict", []string{"claim CAS or reused token", "the token is the same as the token of the current authority"}, "expected_revision", "expected_epoch", "kander coordinator show")
}

// R16 and R17: a reconcile names the missing checkpoint, the field of the
// authority that differs, or the stale revision; no token is written, and the
// CAS values appear only when the CAS fact fails.
func TestReconcileRefusalNamesValues(t *testing.T) {
	english(t)
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "reconcile-a"), recoveryMember(t, root, "reconcile-b")}

	missing := CoordinatorReconcile{GroupID: coordinatorGroup, ExpectedRevision: 1, Authority: CoordinatorAuthority{Owner: "coordinator", Epoch: 1, Token: firstToken},
		Members: map[string]CoordinatorObservation{ids[0]: {Revision: 1}, ids[1]: {Revision: 1}}}
	_, err := ReconcileCoordinator(context.Background(), root, missing, nil)
	secretFree(t, err)
	requireRefusal(t, err, "board.coordinator_conflict", []string{"checkpoint missing", "group " + coordinatorGroup + " has no checkpoint", "kander coordinator claim"}, "stale coordinator authority")

	c0, err := ClaimCoordinator(context.Background(), root, CoordinatorClaim{GroupID: coordinatorGroup, Owner: "coordinator", Token: firstToken, Basis: "basis", Members: ids})
	if err != nil {
		t.Fatal(err)
	}
	c0 = coordinatorReconcile(t, root, c0)
	moveMember(t, root, ids[0], "review")
	r := coordinatorRequest(t, root, c0)
	c1, err := ReconcileCoordinator(context.Background(), root, r, nil)
	if err != nil {
		t.Fatal(err)
	}

	token := r
	token.Authority.Token = "someone-else-secret"
	owner := r
	owner.Authority.Owner = "someone-else"
	pasted := r
	pasted.Authority.Owner = firstToken
	epoch := r
	epoch.Authority.Epoch += 2
	for name, tc := range map[string]struct {
		request CoordinatorReconcile
		want    []string
		absent  []string
	}{
		"token":                 {token, []string{"stale coordinator authority", "the token differs"}, []string{"owner is", "epoch is", "expected_revision"}},
		"owner":                 {owner, []string{"stale coordinator authority", "owner is someone-else", "belongs to coordinator"}, []string{"the token differs", "epoch is", "expected_revision"}},
		"owner holding a token": {pasted, []string{"stale coordinator authority", "owner is <invalid>"}, []string{"the token differs", "epoch is", "expected_revision"}},
		"epoch":                 {epoch, []string{"stale coordinator authority", "epoch is 3", "authority is at epoch 1"}, []string{"the token differs", "owner is", "expected_revision"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReconcileCoordinator(context.Background(), root, tc.request, nil)
			secretFree(t, err)
			requireRefusal(t, err, "board.coordinator_conflict", tc.want, tc.absent...)
			requireCheckpointUnchanged(t, root, c1)
		})
	}

	// The authority holds and only the revision is stale: the request is no retry of the last one.
	stale := r
	o := stale.Members[ids[0]]
	o.Base = strings.Repeat("c", 40)
	stale.Members = map[string]CoordinatorObservation{ids[0]: o, ids[1]: r.Members[ids[1]]}
	_, err = ReconcileCoordinator(context.Background(), root, stale, nil)
	secretFree(t, err)
	requireRefusal(t, err, "board.coordinator_conflict", []string{"checkpoint CAS", "expected_revision is " + num(c0.Revision), "group " + coordinatorGroup + " is at revision " + num(c1.Revision), "last_request_hash", "kander coordinator show"}, "owner is", "epoch is", "the token differs")
	requireCheckpointUnchanged(t, root, c1)
}

// R18: a recovery names the revision, the epoch, or the room that fails, and is
// no CAS refusal when the CAS facts hold.
func TestRecoverRefusalNamesValues(t *testing.T) {
	english(t)
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	revision := transactionRevision(t, root, ids[0])
	stale, fenced := recoverRequest(c, ids[0]), recoverRequest(c, ids[0])
	stale.ExpectedRevision = 1
	fenced.ExpectedEpoch = 6
	_, err := RecoverCoordinator(context.Background(), root, stale, revision)
	secretFree(t, err)
	requireRefusal(t, err, "board.coordinator_conflict", []string{"recover CAS", "names revision 1 of group " + coordinatorGroup, "checkpoint is at revision " + num(c.Revision), "kander coordinator show"}, "coordinator epoch")
	_, err = RecoverCoordinator(context.Background(), root, fenced, revision)
	secretFree(t, err)
	requireRefusal(t, err, "board.coordinator_conflict", []string{"recover CAS", "names coordinator epoch 6", "is at epoch " + num(c.Authority.Epoch)}, "names revision")

	// The CAS facts hold and the member is not blocked: the refusal is another one.
	_, err = RecoverCoordinator(context.Background(), root, recoverRequest(c, ids[1]), transactionRevision(t, root, ids[1]))
	requireRefusal(t, err, "board.recover_not_blocked", []string{ids[1]}, "recover CAS")
	requireCheckpointUnchanged(t, root, c)
}

// R19: the card below its mirror names both revisions, and the same block keeps its
// text, so a second reconcile of the same state writes no new checkpoint revision.
func TestMirrorRevisionRefusalIsStable(t *testing.T) {
	english(t)
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "stable-a"), recoveryMember(t, root, "stable-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	err := WithTransaction(root, LockScope{Groups: []string{coordinatorGroup}, ExclusiveBoard: true}, func(tx *Transaction) error {
		current, _, e := readCheckpoint(tx, coordinatorGroup)
		if e != nil {
			return e
		}
		m := current.Members[ids[0]]
		m.Revision += 5
		current.Members[ids[0]] = m
		current.Revision++
		return putCheckpoint(tx, &current)
	})
	if err != nil {
		t.Fatal(err)
	}
	mirror := storedCheckpoint(t, root).Members[ids[0]].Revision
	card := transactionRevision(t, root, ids[0])
	if mirror <= card {
		t.Fatalf("mirror %d card %d", mirror, card)
	}
	err = WithTransaction(root, LockScope{Tasks: ids, Groups: []string{taskStartGroup, reviewControlGroup, dispatchRegistry}, ReadOnly: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(ids[0])
		if e != nil {
			return e
		}
		_, e = reconcileCoordinatorMember(context.Background(), tx, &coordinatorPass{}, ids[0], storedCheckpoint(t, root).Members[ids[0]], CoordinatorObservation{Revision: s.Revision})
		requireRefusal(t, e, "board.coordinator_conflict", []string{"member revision or execution cycle changed: " + ids[0], "below the revision " + num(mirror)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, storedCheckpoint(t, root)), nil)
	if err != nil {
		t.Fatal(err)
	}
	block := first.Members[ids[0]].Blocked
	if block == nil {
		t.Fatal("the member was not blocked")
	}
	for _, part := range []string{"member revision or execution cycle changed: " + ids[0], "card is at revision " + num(card), "below the revision " + num(mirror)} {
		if !strings.Contains(block.Reason, part) {
			t.Fatalf("%q missing from %q", part, block.Reason)
		}
	}
	if first.Revision <= c.Revision {
		t.Fatalf("the block was not published: %d", first.Revision)
	}
	second, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, first), nil)
	if err != nil || second.Revision != first.Revision || second.Members[ids[0]].Blocked.Reason != block.Reason {
		t.Fatalf("the same block changed the checkpoint: %v %+v", err, second)
	}
	// The card is written again and stays below the mirror: the block keeps its
	// first text, so the checkpoint is not rewritten for a block that did not change.
	text := transactionSnapshot(t, root, ids[0])
	updateSnapshot(t, root, text, text.Text+"\nA note that advances the card revision.\n")
	if now := transactionRevision(t, root, ids[0]); now <= card || now >= mirror {
		t.Fatalf("card %d mirror %d", now, mirror)
	}
	third, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, second), nil)
	if err != nil || third.Revision != second.Revision || third.Members[ids[0]].Blocked.Reason != block.Reason {
		t.Fatalf("a block that stays the same rewrote the checkpoint: %v %+v", err, third.Members[ids[0]].Blocked)
	}
}
