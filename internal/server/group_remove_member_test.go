package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// removalFixture is a claimed group of three unstarted members.
type removalFixture struct {
	fixture *fixture
	host    *client.Project
	grant   api.GroupGrant
	proof   client.Claim
	members []string
}

func newRemovalFixture(t *testing.T) *removalFixture {
	t.Helper()
	f := newFixture(t)
	a := f.project("host-a")
	group := "20261118-removal-group"
	var ids []string
	for _, slug := range []string{"removal-one", "removal-two", "removal-three"} {
		ids = append(ids, todoTask(t, a, slug, group).TaskID)
	}
	grant, err := a.ClaimGroup(t.Context(), "", group, api.GroupClaim{ExecutionID: "removal-coordinator", Request: board.CoordinatorClaim{GroupID: group, Owner: "codex", Token: "public-session", Basis: "confirmed group fixture", Members: ids}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &removalFixture{fixture: f, host: a, grant: grant, proof: client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}, members: ids}
}

func (f *removalFixture) request(member string) board.CoordinatorRemoveMember {
	c := f.grant.Checkpoint
	return board.CoordinatorRemoveMember{GroupID: c.GroupID, Member: member, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Decision: "user-decision-2", Reason: "the card depends on another group"}
}

func (f *removalFixture) remove(t *testing.T, key string, r board.CoordinatorRemoveMember, proof client.Claim) (board.CoordinatorCheckpoint, error) {
	t.Helper()
	return f.host.CoordinatorAction(t.Context(), key, r.GroupID, "remove-member", r, proof)
}

func (f *removalFixture) requireUnchanged(t *testing.T) {
	t.Helper()
	view, err := f.host.Group(t.Context(), f.grant.Checkpoint.GroupID)
	if err != nil || view.Checkpoint.Revision != f.grant.Checkpoint.Revision || len(view.Checkpoint.Members) != 3 || len(view.Checkpoint.Removals) != 0 {
		t.Fatalf("a refused removal changed the group: %+v %v", view.Checkpoint, err)
	}
	for _, id := range f.members {
		task, err := f.host.Show(t.Context(), id)
		if err != nil || board.TaskGroupFrom(task.Text) != f.grant.Checkpoint.GroupID {
			t.Fatalf("a refused removal changed %s: %v", id, err)
		}
	}
}

func TestRemoveMemberRouteTakesAnUnstartedMemberOutAndReplays(t *testing.T) {
	f := newRemovalFixture(t)
	r := f.request(f.members[0])
	before, err := f.host.Events(t.Context(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.remove(t, "remove-first", r, f.proof)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Members) != 2 || next.Revision != f.grant.Checkpoint.Revision+1 || next.Authority != f.grant.Checkpoint.Authority || len(next.Removals) != 1 || next.Removals[0].TaskID != r.Member {
		t.Fatalf("checkpoint: %+v", next)
	}
	task, err := f.host.Show(t.Context(), r.Member)
	if err != nil || board.TaskGroupFrom(task.Text) != "" || task.State != "todo" {
		t.Fatalf("card: %+v %v", task, err)
	}
	if decisions, _ := board.SectionBody(task.Text, "CONTRACT_DECISIONS"); !strings.Contains(decisions, "user-decision-2") {
		t.Fatalf("decisions: %s", decisions)
	}
	view, err := f.host.Group(t.Context(), r.GroupID)
	if err != nil || view.Checkpoint.Revision != next.Revision || len(view.Checkpoint.Removals) != 1 {
		t.Fatalf("view: %+v %v", view, err)
	}
	// The same key replays the first answer; a new key on the old revision is refused.
	replay, err := f.remove(t, "remove-first", r, f.proof)
	if err != nil || replay.Revision != next.Revision || replay.ContentHash != next.ContentHash {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if _, err = f.remove(t, "remove-again", r, f.proof); err == nil {
		t.Fatal("a second request on the old revision was accepted")
	}
	// Subscribers see the group and the card.
	page, err := f.host.Events(t.Context(), before.Cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	var group, card bool
	for _, e := range page.Events {
		group = group || (e.Kind == api.EventGroupChanged && e.GroupID == r.GroupID)
		card = card || (e.Kind == api.EventTaskChanged && e.TaskID == r.Member)
	}
	if !group || !card {
		t.Fatalf("events: %+v", page.Events)
	}
}

func TestRemoveMemberRouteNeedsTheGroupLease(t *testing.T) {
	f := newRemovalFixture(t)
	r := f.request(f.members[0])
	other := f.fixture.project("host-b")
	t.Run("no lease", func(t *testing.T) {
		_, err := other.CoordinatorAction(t.Context(), "no-lease", r.GroupID, "remove-member", r, client.Claim{Token: f.grant.Checkpoint.Authority.Token, Generation: f.grant.Claim.Generation})
		expectCode(t, err, api.CodeHeldByOther)
		f.requireUnchanged(t)
	})
	t.Run("route group differs", func(t *testing.T) {
		wrong := r
		wrong.GroupID = "20261118-another-group"
		_, err := f.host.CoordinatorAction(t.Context(), "wrong-group", r.GroupID, "remove-member", wrong, f.proof)
		expectCode(t, err, api.CodeInvalidRequest)
		f.requireUnchanged(t)
	})
	t.Run("lost", func(t *testing.T) {
		g := newRemovalFixture(t)
		if _, err := g.host.ObserveGroup(t.Context(), "", g.grant.Checkpoint.GroupID, api.AgentObservation{Status: "stopped"}, g.proof); err != nil {
			t.Fatal(err)
		}
		_, err := g.remove(t, "lost", g.request(g.members[0]), g.proof)
		if err == nil {
			t.Fatal("a lost lease removed a member")
		}
		g.requireUnchanged(t)
	})
	t.Run("released", func(t *testing.T) {
		g := newRemovalFixture(t)
		if _, err := g.host.ReleaseGroup(t.Context(), "", g.grant.Checkpoint.GroupID, api.GroupRelease{GroupID: g.grant.Checkpoint.GroupID, Reason: "done", Decision: "user-decision-5"}, g.proof); err != nil {
			t.Fatal(err)
		}
		if _, err := g.remove(t, "released", g.request(g.members[0]), g.proof); err == nil {
			t.Fatal("a released lease removed a member")
		}
		g.requireUnchanged(t)
	})
	t.Run("taken over", func(t *testing.T) {
		g := newRemovalFixture(t)
		g.fixture.clock.advance(91 * time.Second)
		b := g.fixture.project("host-b")
		c := g.grant.Checkpoint
		claim := api.GroupClaim{ExecutionID: "successor-coordinator", ExpectedGeneration: g.grant.Claim.Generation, Decision: "explicit coordinator handoff after lease loss", Request: board.CoordinatorClaim{GroupID: c.GroupID, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Owner: "codex", Token: "successor-session", Basis: "confirmed group fixture", Members: g.members}}
		if _, err := b.ClaimGroup(t.Context(), "takeover", c.GroupID, claim, nil); err != nil {
			t.Fatal(err)
		}
		_, err := g.remove(t, "late", g.request(g.members[0]), g.proof)
		expectCode(t, err, api.CodeTakenOver)
	})
}

func TestRemoveMemberRouteRefusesAMemberTheServerClaimed(t *testing.T) {
	f := newRemovalFixture(t)
	_, own := startClaim(t, f.host, f.members[0])
	// Working: the board refuses it, and so does the claim record.
	if _, err := f.remove(t, "claimed-working", f.request(f.members[0]), f.proof); err == nil {
		t.Fatal("a claimed member left the group")
	}
	// A rolled-back start leaves the card in todo and its claim record behind.
	task, err := f.host.Execution(t.Context(), "", f.members[0], api.ExecutionUpdate{Action: api.ExecutionRollback}, own)
	if err != nil || task.State != "todo" || task.Claim == nil {
		t.Fatalf("%+v %v", task, err)
	}
	_, err = f.remove(t, "claimed-todo", f.request(f.members[0]), f.proof)
	if err == nil || !strings.Contains(err.Error(), "server claim record") {
		t.Fatalf("a member with a claim record: %v", err)
	}
	// A member that never had a claim leaves the same group.
	if _, err = f.remove(t, "unclaimed", f.request(f.members[1]), f.proof); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveMemberRouteRefusesAStaleRequest(t *testing.T) {
	f := newRemovalFixture(t)
	before, err := f.host.Events(t.Context(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	stale := f.request(f.members[0])
	stale.ExpectedRevision++
	if _, err := f.remove(t, "stale", stale, f.proof); err == nil {
		t.Fatal("a request on another revision was accepted")
	}
	blank := f.request(f.members[0])
	blank.Reason = " "
	if _, err := f.remove(t, "blank", blank, f.proof); err == nil {
		t.Fatal("a blank reason was accepted")
	}
	f.requireUnchanged(t)
	// A refusal tells subscribers of the card nothing.
	page, err := f.host.Events(t.Context(), before.Cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Events {
		if e.Kind == api.EventTaskChanged {
			t.Fatalf("a refused removal published %+v", e)
		}
	}
}

// A lease whose clock ran out and that nobody took over still stands.
func TestRemoveMemberRouteWorksUnderALapsedLease(t *testing.T) {
	f := newRemovalFixture(t)
	f.fixture.clock.advance(91 * time.Second)
	view, err := f.host.Group(t.Context(), f.grant.Checkpoint.GroupID)
	if err != nil || view.Claim == nil || view.Claim.Status != api.ClaimLapsed {
		t.Fatalf("the fixture lease did not lapse: %+v %v", view.Claim, err)
	}
	next, err := f.remove(t, "lapsed", f.request(f.members[0]), f.proof)
	if err != nil || len(next.Members) != 2 || len(next.Removals) != 1 {
		t.Fatalf("%+v %v", next, err)
	}
}
