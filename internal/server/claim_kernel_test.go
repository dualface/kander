package server

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
)

// claimProbe projects the claim record of one task before and after an
// entrance and compares it with the kernel step.
type claimProbe struct {
	t *testing.T
	s *Server
	p *client.Project
}

func (c claimProbe) state(id string) serverclaim.State {
	c.t.Helper()
	st, _ := c.stateAt(id, nil)
	return st
}

// stateAt projects with the generation base of an operation, or with the base
// the record fixes when base is nil, and returns the base it used.
func (c claimProbe) stateAt(id string, base *uint64) (serverclaim.State, uint64) {
	c.t.Helper()
	proj, err := c.s.project(c.p.ID())
	if err != nil {
		c.t.Fatal(err)
	}
	r, found, err := proj.readClaim(id)
	if err != nil {
		c.t.Fatal(err)
	}
	view, err := board.LoadBoard(proj.board)
	if err != nil {
		c.t.Fatal(err)
	}
	entry, err := board.Locate(view, id)
	if err != nil {
		c.t.Fatal(err)
	}
	k, err := proj.projectClaimAt(r, found, claimedState(entry.State), base)
	if err != nil {
		c.t.Fatal(err)
	}
	return k.state, k.base
}

// refines runs fn and asserts projection(after) = Step(projection(before), e),
// both with the generation base fixed before fn, when accepted, or an error, a
// kernel refusal, and an unchanged projection.
func (c claimProbe) refines(id string, e serverclaim.Event, accepted bool, fn func() error) {
	c.t.Helper()
	before, base := c.stateAt(id, nil)
	err := fn()
	after, _ := c.stateAt(id, &base)
	want, ok := serverclaim.Step(before, e, serverclaim.Params{MaxGen: uint32(before.Gen + 1)})
	switch {
	case ok != accepted:
		c.t.Fatalf("kernel acceptance of %T on %+v: %v, want %v", e, before, ok, accepted)
	case accepted && err != nil:
		c.t.Fatalf("%T: %v", e, err)
	case accepted && after != want:
		c.t.Fatalf("%T: projection %+v, kernel %+v", e, after, want)
	case !accepted && err == nil:
		c.t.Fatalf("%T was accepted on %+v", e, before)
	case !accepted && after != before:
		c.t.Fatalf("refused %T changed the projection: %+v -> %+v", e, before, after)
	}
}

// Every claim entrance of the server refines the claim kernel: a claim, a
// renewal, holder writes, a lease expiry, a takeover, a release, and a
// termination. The refusals cover theorems 3 to 5: a second claim of a held
// card, a forged token, a write and a termination of an older generation while
// a valid claim exists, a takeover against a live lease and with a stale
// observed generation.
func TestClaimEntrancesRefineTheKernel(t *testing.T) {
	var at atomic.Int64
	at.Store(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC).UnixNano())
	now := func() time.Time { return time.Unix(0, at.Load()) }
	s, h := openTest(t, t.TempDir(), now)
	defer s.Close()
	defer h.Close()
	p, id := prepareTodo(t, h.URL)
	c := claimProbe{t: t, s: s, p: p}
	ctx := t.Context()

	var grant api.ClaimGrant
	c.refines(id, serverclaim.Claim{Fresh: true}, true, func() (err error) {
		grant, err = p.Claim(ctx, "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-1", Owner: "claude"})
		return err
	})
	own := client.Claim{Token: grant.Token, Generation: 1}
	c.refines(id, serverclaim.Claim{Fresh: true}, false, func() error {
		_, err := p.Claim(ctx, "", id, api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-b", Owner: "codex"})
		return err
	})
	forged := client.Claim{Token: "forged", Generation: 1}
	c.refines(id, serverclaim.Write{G: 1, Tg: 2}, false, func() error {
		_, err := p.PutDocument(ctx, "", id, api.Document{Path: "report.md", Text: "x", ExpectedRevision: grant.Task.Revision}, &forged)
		return err
	})
	at.Add(int64(30 * time.Second))
	c.refines(id, serverclaim.Renew{G: 1, Tg: 1}, true, func() error { _, err := p.Renew(ctx, id, own); return err })
	c.refines(id, serverclaim.Write{G: 1, Tg: 1}, true, func() error {
		_, err := p.PutDocument(ctx, "", id, api.Document{Path: "report.md", Text: "x", ExpectedRevision: grant.Task.Revision}, &own)
		return err
	})
	takeover := api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-2", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: 1}
	c.refines(id, serverclaim.Takeover{Expected: 1, Fresh: true, G: 2, Tg: 2}, false, func() error {
		_, err := p.Takeover(ctx, "", id, takeover)
		return err
	})

	// The environment: the lease expires with no request at all.
	before := c.state(id)
	at.Add(int64(DefaultTTL + time.Second))
	if want, _ := serverclaim.Step(before, serverclaim.Expire{}, serverclaim.Params{MaxGen: 2}); c.state(id) != want {
		t.Fatalf("lease expiry: %+v, kernel %+v", c.state(id), want)
	}
	c.refines(id, serverclaim.Renew{G: 1, Tg: 1}, false, func() error { _, err := p.Renew(ctx, id, own); return err })
	stale := takeover
	stale.ExpectedGeneration = 3
	c.refines(id, serverclaim.Takeover{Expected: 2, Fresh: true, G: 2, Tg: 2}, false, func() error {
		_, err := p.Takeover(ctx, "", id, stale)
		return err
	})
	var next api.ClaimGrant
	c.refines(id, serverclaim.Takeover{Expected: 1, Fresh: true, G: 2, Tg: 2}, true, func() (err error) {
		next, err = p.Takeover(ctx, "", id, takeover)
		return err
	})
	if next.Claim.Generation != 2 {
		t.Fatalf("takeover granted generation %d", next.Claim.Generation)
	}
	// The old execution's requests are refused while the new claim is valid.
	c.refines(id, serverclaim.Write{G: 3, Tg: 3}, false, func() error {
		_, err := p.PutDocument(ctx, "", id, api.Document{Path: "report.md", Text: "late", ExpectedRevision: next.Task.Revision}, &own)
		return err
	})
	c.refines(id, serverclaim.Leave{G: 3, Tg: 3, Terminate: true}, false, func() error {
		_, err := p.Move(ctx, "", id, api.Move{State: "archived", Result: "cancelled", Reason: "late", Decision: "old"}, &own)
		return err
	})
	// The current token with a header of another generation is the exemption
	// theorem 5 leaves to v2 (counterexample C): the termination passes.
	current := client.Claim{Token: next.Token, Generation: 1}
	c.refines(id, serverclaim.Leave{G: 3, Tg: 2, Terminate: true}, true, func() error {
		_, err := p.Move(ctx, "", id, api.Move{State: "archived", Result: "cancelled", Reason: "header", Decision: "user:end"}, &current)
		return err
	})
}

// A launcher adoption is a holder write that keeps the claim record.
func TestAdoptionRefinesTheKernel(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	p, id := prepareTodo(t, h.URL)
	c := claimProbe{t: t, s: s, p: p}
	ctx := t.Context()
	g, err := p.Claim(ctx, "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-start", Owner: "claude", Start: &api.StartInput{Session: "claude pending"}})
	if err != nil {
		t.Fatal(err)
	}
	own := client.Claim{Token: g.Token, Generation: 1}
	if _, err := p.Execution(ctx, "", id, api.ExecutionUpdate{Action: api.ExecutionUnresolve, Reason: "late step failed"}, own); err != nil {
		t.Fatal(err)
	}
	state, err := p.ExecutionState(ctx, id, own)
	if err != nil {
		t.Fatal(err)
	}
	adopt := api.ExecutionUpdate{Action: api.ExecutionAdopt, Decision: "user:adopt", ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Presence: &api.AgentObservation{Status: "alive", ObservedAt: time.Now()}}
	c.refines(id, serverclaim.Write{G: 1, Tg: 1}, true, func() error { _, err := p.Execution(ctx, "", id, adopt, own); return err })
	if state, err = p.ExecutionState(ctx, id, own); err != nil || state.Record.Status != board.ExecutionRunning {
		t.Fatalf("adoption: %+v %v", state.Record, err)
	}
}

// A generation beyond the kernel's uint32 parameters keeps every decision and
// refines the kernel: startup reconciliation reads the record, the holder
// renews, an older header is refused, and after the lease expires a takeover
// that observed the generation gets the next one.
func TestLargeGenerationKeepsDecisions(t *testing.T) {
	for _, gen := range []uint64{1 << 31, 1 << 40, ^uint64(0) - 2} {
		dir := t.TempDir()
		s, h := openTest(t, dir, nil)
		p, id := prepareTodo(t, h.URL)
		proj, _ := s.project(p.ID())
		view, _ := board.LoadBoard(proj.board)
		entry, _ := board.Locate(view, id)
		if _, err := board.MoveWithOptions(entry, proj.board, "working", board.MoveOptions{Owner: "claude"}); err != nil {
			t.Fatal(err)
		}
		var at atomic.Int64
		at.Store(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC).UnixNano())
		now := func() time.Time { return time.Unix(0, at.Load()) }
		r := claimRecord{TaskID: id, ClientID: "host-a", ExecutionID: "exec-big", Owner: "claude", Generation: gen, Status: api.ClaimActive, ExpiresAt: now().Add(time.Hour)}
		token := s.token(proj.info.ID, id, r.ExecutionID, gen)
		r.TokenHash = tokenHash(token)
		if err := proj.writeClaim(r); err != nil {
			t.Fatal(err)
		}
		h.Close()
		s.Close()
		s, h = openTest(t, dir, now)
		cl, _ := client.New(h.URL, "host-a")
		p = cl.Project(p.ID())
		c := claimProbe{t: t, s: s, p: p}
		own := client.Claim{Token: token, Generation: gen}
		c.refines(id, serverclaim.Renew{G: 1, Tg: 1}, true, func() error { _, err := p.Renew(t.Context(), id, own); return err })
		c.refines(id, serverclaim.Renew{G: 2, Tg: 1}, false, func() error {
			_, err := p.Renew(t.Context(), id, client.Claim{Token: token, Generation: gen - 1})
			if !api.IsCode(err, api.CodeLeaseInvalid) {
				t.Fatalf("generation %d: an older header: %v", gen, err)
			}
			return err
		})
		at.Add(int64(2 * time.Hour))
		takeover := api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-next", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: gen}
		var next api.ClaimGrant
		c.refines(id, serverclaim.Takeover{Expected: 1, Fresh: true, G: 2, Tg: 2}, true, func() (err error) {
			next, err = p.Takeover(t.Context(), "", id, takeover)
			return err
		})
		if next.Claim.Generation != gen+1 {
			t.Fatalf("generation %d: takeover granted %d", gen, next.Claim.Generation)
		}
		h.Close()
		s.Close()
	}
}

// A lease release and a launch rollback refine the kernel, and so does the
// termination of a task whose lease the server answers CodeLeaseInvalid for.
func TestReleaseRollbackAndTerminationRefineTheKernel(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	p, id := prepareTodo(t, h.URL)
	c := claimProbe{t: t, s: s, p: p}
	ctx := t.Context()
	g, err := p.Claim(ctx, "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-start", Owner: "claude", Start: &api.StartInput{Session: "claude pending"}})
	if err != nil {
		t.Fatal(err)
	}
	own := client.Claim{Token: g.Token, Generation: 1}
	c.refines(id, serverclaim.Rollback{G: 1, Tg: 1}, true, func() error {
		_, err := p.Execution(ctx, "", id, api.ExecutionUpdate{Action: api.ExecutionRollback}, own)
		return err
	})
	c.refines(id, serverclaim.Rollback{G: 1, Tg: 1}, false, func() error {
		_, err := p.Execution(ctx, "", id, api.ExecutionUpdate{Action: api.ExecutionRollback}, own)
		return err
	})

	var second api.ClaimGrant
	c.refines(id, serverclaim.Claim{Fresh: true}, true, func() (err error) {
		second, err = p.Claim(ctx, "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-again", Owner: "claude"})
		return err
	})
	proof := client.Claim{Token: second.Token, Generation: 2}
	state, err := p.ExecutionState(ctx, id, proof)
	if err != nil {
		t.Fatal(err)
	}
	release := api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Decision: "user:release", Presence: &api.AgentObservation{Status: "stopped"}}
	c.refines(id, serverclaim.Release{G: 2, Tg: 2}, true, func() error {
		_, err := p.Execution(ctx, "", id, release, proof)
		return err
	})
	// No claim is valid any more: a termination needs no holder proof.
	c.refines(id, serverclaim.Leave{G: 3, Tg: 3, Terminate: true}, true, func() error {
		_, err := p.Move(ctx, "", id, api.Move{State: "archived", Result: "cancelled", Reason: "stopped", Decision: "user:stop"}, nil)
		return err
	})
}

// Startup reconciliation refines the kernel: a pending record whose move
// committed for its owner becomes active.
func TestReconcileRefinesTheKernel(t *testing.T) {
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	r := claimRecord{TaskID: id, ClientID: "host-a", ExecutionID: "exec-1", Owner: "claude", Generation: 1, Status: claimPending}
	if err := proj.writeClaim(r); err != nil {
		t.Fatal(err)
	}
	view, _ := board.LoadBoard(proj.board)
	entry, _ := board.Locate(view, id)
	if _, err := board.MoveWithOptions(entry, proj.board, "working", board.MoveOptions{Owner: "claude"}); err != nil {
		t.Fatal(err)
	}
	before := claimProbe{t: t, s: s, p: p}.state(id)
	h.Close()
	s.Close()

	s, h = openTest(t, dir, nil)
	defer s.Close()
	defer h.Close()
	after := claimProbe{t: t, s: s, p: p}.state(id)
	want, ok := serverclaim.Step(before, serverclaim.Reconcile{Owner: true}, serverclaim.Params{MaxGen: 2})
	if !ok || after != want || after.Status != serverclaim.StatusActive {
		t.Fatalf("reconcile: %+v, kernel %+v (%v)", after, want, ok)
	}
}
