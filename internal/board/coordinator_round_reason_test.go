package board

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// roundGroup claims a group of one member bound to dispatch "round-one" at epoch 1
// and one plain member, so archiving the first never ends the group. The claim
// token is a recognizable string the reasons must never repeat.
func roundGroup(t *testing.T, root string) (id string, c CoordinatorCheckpoint, d Dispatch) {
	t.Helper()
	id = coordinatorCard(t, root, "round-bound").Entry.TaskID
	rest := coordinatorCard(t, root, "round-rest").Entry.TaskID
	c, err := ClaimCoordinator(context.Background(), root, CoordinatorClaim{GroupID: coordinatorGroup, Owner: "coordinator", Token: firstToken, Basis: "测试编排授权", Members: []string{id, rest}})
	if err != nil {
		t.Fatal(err)
	}
	d = prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "round-one"))
	if a := authFrom(transactionSnapshot(t, root, id).Text); a != (ExecutionAuthorization{DispatchID: "round-one", Epoch: 1}) {
		t.Fatalf("the card binds %+v", a)
	}
	return id, coordinatorReconcile(t, root, c), d
}

// reconcileBlocked reconciles with one member's observation replaced and returns
// the checkpoint and the reason that member was blocked with. The reason never
// holds the coordinator token.
func reconcileBlocked(t *testing.T, root string, c CoordinatorCheckpoint, id string, o CoordinatorObservation) (CoordinatorCheckpoint, string) {
	t.Helper()
	r := coordinatorRequest(t, root, c)
	o.Revision = r.Members[id].Revision
	r.Members[id] = o
	next, err := ReconcileCoordinator(context.Background(), root, r, nil)
	var blocked *CoordinatorBlockedError
	if err != nil && !errors.As(err, &blocked) {
		t.Fatal(err)
	}
	b := next.Members[id].Blocked
	if b == nil {
		t.Fatalf("%s was not blocked: %+v", id, next.Members[id])
	}
	if strings.Contains(b.Reason, firstToken) {
		t.Fatalf("the token is written in %q", b.Reason)
	}
	return next, b.Reason
}

// requireReason requires that the reason starts with the label that names the
// refusal, as the checkpoint keeps it.
func requireReason(tb testing.TB, reason, label string, want ...string) {
	tb.Helper()
	if !strings.HasPrefix(reason, label) {
		tb.Fatalf("%q does not start with %q", reason, label)
	}
	for _, part := range want {
		if !strings.Contains(reason, part) {
			tb.Fatalf("%q missing from %q", part, reason)
		}
	}
}

// A block says which round the card is bound to and which one the observation
// gave, so the caller can tell a round it left out from a stale or wrong one. The
// label stays the prefix, a correct observation clears the block, and what is
// accepted or blocked does not change.
func TestRoundReasonNamesBothSides(t *testing.T) {
	english(t)
	base := strings.Repeat("a", 40)
	other := strings.Repeat("c", 40)
	bound := "the card binds dispatch round-one at epoch 1"
	same := "the observation gives dispatch_id round-one and epoch 1"
	for name, tc := range map[string]struct {
		obs    CoordinatorObservation
		prefix string
		want   []string
	}{
		"nothing given": {CoordinatorObservation{}, "wrong dispatch round: ",
			[]string{bound, "the observation gives no dispatch_id and no epoch"}},
		"stale dispatch": {CoordinatorObservation{DispatchID: "round-zero", Epoch: 1, Base: base}, "wrong dispatch round: ",
			[]string{bound, "the observation gives dispatch_id round-zero and epoch 1"}},
		"stale epoch": {CoordinatorObservation{DispatchID: "round-one", Epoch: 2, Base: base}, "wrong dispatch round: ",
			[]string{bound, "the observation gives dispatch_id round-one and epoch 2"}},
		"dispatch only": {CoordinatorObservation{DispatchID: "round-one", Base: base}, "wrong dispatch round: ",
			[]string{bound, "the observation gives dispatch_id round-one and epoch (not given)"}},
		"epoch only": {CoordinatorObservation{Epoch: 1, Base: base}, "wrong dispatch round: ",
			[]string{bound, "the observation gives dispatch_id (not given) and epoch 1"}},
		"base not given": {CoordinatorObservation{DispatchID: "round-one", Epoch: 1}, "dispatch base or epoch: ",
			[]string{bound, same, "the dispatch record holds epoch 1 and base " + base + ", but the observation gives base (not given)"}},
		"base differs": {CoordinatorObservation{DispatchID: "round-one", Epoch: 1, Base: other}, "dispatch base or epoch: ",
			[]string{bound, same, "the dispatch record holds epoch 1 and base " + base + ", but the observation gives base " + other}},
	} {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			id, c, _ := roundGroup(t, root)
			blocked, reason := reconcileBlocked(t, root, c, id, tc.obs)
			requireReason(t, reason, tc.prefix+id, append(tc.want, "kander show "+id, "kander dispatch show "+id, "kander coordinator show "+coordinatorGroup)...)
			if m := blocked.Members[id]; m.Dispatch == nil || m.Dispatch.ID != "round-one" || m.State != "review" {
				t.Fatalf("a block moved the mirror: %+v", m)
			}
			// The same card and dispatch accept the observation the card names.
			next := coordinatorReconcile(t, root, blocked)
			if m := next.Members[id]; m.Blocked != nil || m.Dispatch == nil || m.Dispatch.ID != "round-one" || m.Dispatch.Epoch != 1 {
				t.Fatalf("a correct observation kept the block: %+v", m)
			}
		})
	}
}

// A member the card does not bind to a dispatch is blocked by an observation that
// names one, and the reason shows what the observation gave.
func TestRoundReasonOfAnUnboundMember(t *testing.T) {
	english(t)
	for name, tc := range map[string]struct {
		obs  CoordinatorObservation
		want []string
	}{
		"round": {CoordinatorObservation{DispatchID: "stale-round", Epoch: 1},
			[]string{"the card binds no dispatch", "the observation gives dispatch_id stale-round and epoch 1"}},
		"base only": {CoordinatorObservation{Base: strings.Repeat("c", 40)},
			[]string{"the card binds no dispatch", "the observation gives no dispatch_id and no epoch", "the observation gives base " + strings.Repeat("c", 40)}},
	} {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			id, c, _ := roundGroup(t, root)
			rest := checkpointIDs(c)[0]
			if rest == id {
				rest = checkpointIDs(c)[1]
			}
			blocked, reason := reconcileBlocked(t, root, c, rest, tc.obs)
			requireReason(t, reason, "unbound member cannot acknowledge a dispatch: "+rest, tc.want...)
			if next := coordinatorReconcile(t, root, blocked); next.Members[rest].Blocked != nil {
				t.Fatalf("a correct observation kept the block: %+v", next.Members[rest])
			}
		})
	}
}

// A member that ended in archived or trash has released its binding, and the
// reason shows the round an observation still names there as well.
func TestRoundReasonOfATerminalMember(t *testing.T) {
	english(t)
	for name, obs := range map[string]CoordinatorObservation{
		"the released round": {DispatchID: "round-one", Epoch: 1},
		"another round":      {DispatchID: "round-zero", Epoch: 1},
	} {
		for _, target := range []string{"archived", "trash"} {
			t.Run(name+" "+target, func(t *testing.T) {
				root := tempBoard(t)
				id, c, d := roundGroup(t, root)
				for _, state := range []string{"working", "review"} {
					if _, err := dispatchMove(t, root, d, state); err != nil {
						t.Fatal(err)
					}
				}
				result := map[string]string{"archived": "cancelled", "trash": "trashed"}[target]
				if _, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, target, MoveOptions{Result: result, Reason: "stop", Decision: "user"}); err != nil {
					t.Fatal(err)
				}
				blocked, reason := reconcileBlocked(t, root, c, id, obs)
				requireReason(t, reason, "wrong dispatch round: "+id, "the card binds no dispatch", "the observation gives dispatch_id "+obs.DispatchID+" and epoch 1")
				if next := coordinatorReconcile(t, root, blocked); next.Members[id].Blocked != nil || next.Members[id].State != target {
					t.Fatalf("a correct observation kept the block: %+v", next.Members[id])
				}
			})
		}
	}
}

// The reason carries nothing that changes between two identical requests, so the
// second reconcile of the same wrong observation writes no checkpoint revision,
// and a different wrong observation does.
func TestRoundReasonIsStableForTheSameObservation(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id, c, _ := roundGroup(t, root)
	wrong := CoordinatorObservation{DispatchID: "round-zero", Epoch: 1}
	first, reason := reconcileBlocked(t, root, c, id, wrong)
	if first.Revision != c.Revision+1 {
		t.Fatalf("the block was not published: %d after %d", first.Revision, c.Revision)
	}
	second, again := reconcileBlocked(t, root, first, id, wrong)
	if again != reason || second.Revision != first.Revision {
		t.Fatalf("the same observation changed the checkpoint: %q at %d, then %q at %d", reason, first.Revision, again, second.Revision)
	}
	if stored := storedCheckpoint(t, root); stored.Revision != first.Revision {
		t.Fatalf("a repeat wrote revision %d", stored.Revision)
	}
	third, other := reconcileBlocked(t, root, second, id, CoordinatorObservation{DispatchID: "round-two", Epoch: 1})
	if other == reason || third.Revision != second.Revision+1 {
		t.Fatalf("another observation kept the text: %q at %d", other, third.Revision)
	}
}

// The token of the coordinator is never written, also when an observation holds it
// by mistake in a field the reason echoes.
func TestRoundReasonNeverWritesTheToken(t *testing.T) {
	english(t)
	base := strings.Repeat("a", 40)
	for name, o := range map[string]CoordinatorObservation{
		"dispatch": {DispatchID: firstToken, Epoch: 1, Base: base},
		"base":     {DispatchID: "round-one", Epoch: 1, Base: firstToken},
	} {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			id, c, _ := roundGroup(t, root)
			_, reason := reconcileBlocked(t, root, c, id, o)
			if !strings.Contains(reason, "<invalid>") {
				t.Fatalf("the token was not marked in %q", reason)
			}
		})
	}
	// An unbound member echoes the base of the observation as well.
	root := tempBoard(t)
	id, c, _ := roundGroup(t, root)
	rest := checkpointIDs(c)[0]
	if rest == id {
		rest = checkpointIDs(c)[1]
	}
	_, reason := reconcileBlocked(t, root, c, rest, CoordinatorObservation{Base: firstToken})
	if !strings.Contains(reason, "<invalid>") {
		t.Fatalf("the token was not marked in %q", reason)
	}
}

// Two valid dispatch IDs that share a long prefix are written in full, so the
// reason shows the difference that blocked the member.
func TestRoundReasonKeepsLongDispatchIDsApart(t *testing.T) {
	english(t)
	root := tempBoard(t)
	prefix := strings.Repeat("long-round-", 10)
	bound, stale := prefix+"one", prefix+"two"
	id := coordinatorCard(t, root, "round-long").Entry.TaskID
	rest := coordinatorCard(t, root, "round-long-rest").Entry.TaskID
	c, err := ClaimCoordinator(context.Background(), root, CoordinatorClaim{GroupID: coordinatorGroup, Owner: "coordinator", Token: firstToken, Basis: "测试编排授权", Members: []string{id, rest}})
	if err != nil {
		t.Fatal(err)
	}
	prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), bound))
	c = coordinatorReconcile(t, root, c)
	_, reason := reconcileBlocked(t, root, c, id, CoordinatorObservation{DispatchID: stale, Epoch: 1})
	requireReason(t, reason, "wrong dispatch round: "+id, "the card binds dispatch "+bound+" at epoch 1", "the observation gives dispatch_id "+stale+" and epoch 1")
}
