package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/server"
)

// lockedBuffer collects the server log of a test fixture.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// startLogged is start with a server log the test can read.
func startLogged(t *testing.T, c *clock) (*fixture, *lockedBuffer) {
	t.Helper()
	logs := &lockedBuffer{}
	s, err := server.Open(server.Options{
		DataDir: t.TempDir(), TTL: 90 * time.Second, RenewInterval: 15 * time.Second, Now: c.now,
		Logger: slog.New(slog.NewTextHandler(logs, nil)), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, dir: "", clock: c, srv: s, http: httptest.NewServer(s.Handler())}
	t.Cleanup(f.stop)
	return f, logs
}

const (
	coordGroup      = "20261120-coordinated-group"
	coordOtherGroup = "20261120-other-coordinated-group"
	coordBase       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	coordTarget     = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// worldSpec shapes the two members of a coordinated group.
type worldSpec struct {
	// groups is the TASK_GROUP of each member; empty means none.
	groups [2]string
	// working keeps a member in working instead of moving it to review.
	working [2]bool
}

// coordWorld is a group of two members, each claimed by its own host and moved
// to review, and a coordinator that holds the group lease and no card claim.
type coordWorld struct {
	t           *testing.T
	f           *fixture
	logs        *lockedBuffer
	coordinator *client.Project
	hosts       [2]*client.Project
	tasks       [2]string
	grants      [2]api.ClaimGrant
	lease       api.GroupGrant
	request     api.GroupClaim
}

func newCoordWorld(t *testing.T, mutate ...func(*worldSpec)) *coordWorld {
	t.Helper()
	spec := worldSpec{groups: [2]string{coordGroup, coordGroup}}
	for _, m := range mutate {
		m(&spec)
	}
	c := &clock{}
	c.at.Store(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC).UnixNano())
	f, logs := startLogged(t, c)
	w := &coordWorld{t: t, f: f, logs: logs, coordinator: f.project("coordinator"), hosts: [2]*client.Project{f.project("host-a"), f.project("host-b")}}
	slugs := [2]string{"coord-one", "coord-two"}
	var grouped []string
	for i := range w.tasks {
		var card api.Task
		if spec.groups[i] == "" {
			card = todoTask(t, w.hosts[i], slugs[i])
		} else {
			card = todoTask(t, w.hosts[i], slugs[i], spec.groups[i])
		}
		card = withTaskBranch(t, w.hosts[i], card)
		w.tasks[i] = card.TaskID
		if spec.groups[i] == coordGroup {
			grouped = append(grouped, card.TaskID)
		}
	}
	w.request = api.GroupClaim{ExecutionID: "coordinator-execution", Request: board.CoordinatorClaim{GroupID: coordGroup, Owner: "codex", Token: "public-session", Basis: "coordinated review fixture", Members: grouped}}
	lease, err := w.coordinator.ClaimGroup(t.Context(), "", coordGroup, w.request, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.lease = lease
	for i := range w.tasks {
		grant, err := claim(t, w.hosts[i], w.tasks[i], []string{"host-a", "host-b"}[i])
		if err != nil {
			t.Fatal(err)
		}
		w.grants[i] = grant
		if !spec.working[i] {
			w.toReview(i)
		}
	}
	return w
}

func (w *coordWorld) holder(i int) client.Claim {
	return client.Claim{Token: w.grants[i].Token, Generation: w.grants[i].Claim.Generation}
}

// toReview delivers member i and moves it to review.
func (w *coordWorld) toReview(i int) api.Task {
	w.t.Helper()
	commit := strings.Repeat([]string{"c", "d"}[i], 40)
	stageDelivery(w.t, w.hosts[i], w.grants[i], commit)
	holder := w.holder(i)
	card, err := w.hosts[i].Move(w.t.Context(), "", w.tasks[i], api.Move{State: "review", DeliveryCommit: commit}, &holder)
	if err != nil {
		w.t.Fatal(err)
	}
	return card
}

func (w *coordWorld) show(id string) api.Task {
	w.t.Helper()
	card, err := w.coordinator.Show(w.t.Context(), id)
	if err != nil {
		w.t.Fatal(err)
	}
	return card
}

// basis declares the current revision and claim generation of each task.
func (w *coordWorld) basis(ids ...string) api.CoordinatorBasis {
	w.t.Helper()
	if len(ids) == 0 {
		ids = w.tasks[:]
	}
	basis := api.CoordinatorBasis{}
	for _, id := range ids {
		card := w.show(id)
		basis = append(basis, api.PublicationMember{TaskID: id, Revision: card.Revision, Generation: card.Claim.Generation})
	}
	return basis
}

// proof is the group lease of the coordinator with a basis of the current
// version of ids, or of both members.
func (w *coordWorld) proof(ids ...string) *client.Claim {
	w.t.Helper()
	return &client.Claim{Token: w.lease.Token, Generation: w.lease.Claim.Generation, CoordinatorBasis: w.basis(ids...)}
}

func (w *coordWorld) revisions() map[string]uint64 {
	w.t.Helper()
	out := map[string]uint64{}
	for _, id := range w.tasks {
		out[id] = w.show(id).Revision
	}
	return out
}

// plan is a sealed review plan with one batch over ids, or over both members.
func (w *coordWorld) plan(id string, requirements map[string]string, base, target string, ids ...string) board.ReviewPlan {
	if len(ids) == 0 {
		ids = w.tasks[:]
	}
	return board.ReviewPlan{Schema: 1, Sealed: true, PlanID: id, Author: "coordinator", Basis: "coordinator write fixture", CWD: "project:" + w.coordinator.ID(), ReportLanguage: "en", TaskIDs: ids,
		Batches: []board.ReviewPlanBatch{{BatchID: id + "-batch", TaskIDs: ids, Base: base, TargetCommit: target, Requirements: requirements}}}
}

func notApplicable() map[string]string {
	return map[string]string{"PMQA": "N/A: coordinator fixture", "Security": "N/A: repository policy"}
}

func (w *coordWorld) writePlan(plan board.ReviewPlan, key string, proof *client.Claim) error {
	_, err := w.coordinator.ReviewPlan(w.t.Context(), key, plan, proof)
	return err
}

// requests counts the collaboration requests of both members.
func (w *coordWorld) requests() int {
	w.t.Helper()
	total := 0
	for _, id := range w.tasks {
		list, err := w.coordinator.Requests(w.t.Context(), id)
		if err != nil {
			w.t.Fatal(err)
		}
		total += len(list)
	}
	return total
}

// refused runs a plan write with proof and asserts that it is refused with code
// and leaves every member and the plan as they were.
func (w *coordWorld) refused(code string, proof *client.Claim) {
	w.t.Helper()
	before := w.revisions()
	plan := w.plan("refused-plan", notApplicable(), "N/A", "N/A")
	expectCode(w.t, w.writePlan(plan, "refused-"+code, proof), code)
	if after := w.revisions(); !reflect.DeepEqual(before, after) {
		w.t.Fatalf("a refused write changed the members: %v -> %v", before, after)
	}
	if _, err := w.coordinator.ReviewResource(w.t.Context(), "plans", plan.PlanID); err == nil {
		w.t.Fatal("a refused write left a plan")
	}
	if w.requests() != 0 {
		w.t.Fatal("a refused write created collaboration requests")
	}
}

func TestCoordinatorReviewWritesPlanWithoutConfirmation(t *testing.T) {
	w := newCoordWorld(t)
	before := w.revisions()
	plan := w.plan("coordinator-plan", notApplicable(), "N/A", "N/A")
	if err := w.writePlan(plan, "coordinator-plan", w.proof()); err != nil {
		t.Fatalf("the coordinator write was refused: %v", err)
	}
	for _, id := range w.tasks {
		current := w.show(id)
		if current.Revision != before[id]+1 {
			t.Fatalf("%s was not written once: %d -> %d", id, before[id], current.Revision)
		}
		if copied, err := w.coordinator.Document(t.Context(), id, "reviews/plan.json"); err != nil || !strings.Contains(copied.Text, plan.PlanID) {
			t.Fatalf("%s has no review copy of the plan: %v", id, err)
		}
	}
	if stored := readPlan(t, w.coordinator, plan.PlanID); stored.PlanID != plan.PlanID || len(stored.TaskIDs) != 2 {
		t.Fatalf("plan: %+v", stored)
	}
	if w.requests() != 0 {
		t.Fatal("the coordinator write created collaboration requests")
	}
}

// dispatchTo drives the dispatch of member i to state: prepared, unknown (the
// delivery attempt was made), or completed (accepted, then handed back to
// review). The member stays in review in each case, because a card is never in
// review with an accepted dispatch: accepting moves it to working.
func (w *coordWorld) dispatchTo(i int, state board.DispatchState) {
	w.t.Helper()
	ctx, holder := w.t.Context(), w.holder(i)
	card := w.show(w.tasks[i])
	input := board.DispatchInput{ID: "coord-round", TaskID: card.TaskID, Kind: "sync", Message: "synchronize", Base: coordBase}
	d, err := w.hosts[i].DispatchAction(ctx, "prepare", card.TaskID, "prepare", api.DispatchCommand{Input: &input, CardRevision: card.Revision}, holder)
	if err != nil {
		w.t.Fatal(err)
	}
	if state == board.DispatchPrepared {
		return
	}
	card = w.show(card.TaskID)
	if d, err = w.hosts[i].DispatchAction(ctx, "attempt", card.TaskID, "attempt", api.DispatchCommand{DispatchID: input.ID, Revision: d.Revision, CardRevision: card.Revision}, holder); err != nil {
		w.t.Fatal(err)
	}
	if state == board.DispatchUnknown {
		return
	}
	if _, err = w.hosts[i].Move(ctx, "accept", card.TaskID, api.Move{State: "working", Authorization: d.Authorization}, &holder); err != nil {
		w.t.Fatal(err)
	}
	commit := strings.Repeat("e", 40)
	stageDelivery(w.t, w.hosts[i], w.grants[i], commit)
	if _, err = w.hosts[i].Move(ctx, "complete", card.TaskID, api.Move{State: "review", DeliveryCommit: commit, Authorization: d.Authorization}, &holder); err != nil {
		w.t.Fatal(err)
	}
}

// otherLease is the group lease of another group, held by another client.
func (w *coordWorld) otherLease() api.GroupGrant {
	w.t.Helper()
	one := todoTask(w.t, w.hosts[0], "coord-other-one", coordOtherGroup)
	two := todoTask(w.t, w.hosts[0], "coord-other-two", coordOtherGroup)
	request := api.GroupClaim{ExecutionID: "other-coordinator", Request: board.CoordinatorClaim{GroupID: coordOtherGroup, Owner: "codex", Token: "other-session", Basis: "another group", Members: []string{one.TaskID, two.TaskID}}}
	grant, err := w.f.project("other-coordinator").ClaimGroup(w.t.Context(), "", coordOtherGroup, request, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return grant
}

// takeOverLease lets another client take the group lease over after it lapsed
// and returns the new grant.
func (w *coordWorld) takeOverLease() api.GroupGrant {
	w.t.Helper()
	view, err := w.coordinator.Group(w.t.Context(), coordGroup)
	if err != nil {
		w.t.Fatal(err)
	}
	w.f.clock.advance(91 * time.Second)
	take := w.request
	take.ExecutionID, take.ExpectedGeneration, take.Decision = "successor-coordinator", w.lease.Claim.Generation, "explicit coordinator takeover fixture"
	take.Request.ExpectedRevision, take.Request.ExpectedEpoch = view.Checkpoint.Revision, view.Checkpoint.Authority.Epoch
	grant, err := w.f.project("successor-coordinator").ClaimGroup(w.t.Context(), "", coordGroup, take, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return grant
}

func TestCoordinatorReviewRefusesWithoutAuthority(t *testing.T) {
	t.Run("no group lease proof", func(t *testing.T) {
		w := newCoordWorld(t)
		w.refused(api.CodeLeaseInvalid, &client.Claim{CoordinatorBasis: w.basis()})
	})
	t.Run("a forged group token", func(t *testing.T) {
		w := newCoordWorld(t)
		w.refused(api.CodeHeldByOther, &client.Claim{Token: strings.Repeat("f", 64), Generation: w.lease.Claim.Generation, CoordinatorBasis: w.basis()})
	})
	t.Run("a member claim token is not a group lease", func(t *testing.T) {
		w := newCoordWorld(t)
		w.refused(api.CodeHeldByOther, &client.Claim{Token: w.grants[0].Token, Generation: w.grants[0].Claim.Generation, CoordinatorBasis: w.basis()})
	})
	t.Run("the lease of another group", func(t *testing.T) {
		w := newCoordWorld(t)
		other := w.otherLease()
		w.refused(api.CodeHeldByOther, &client.Claim{Token: other.Token, Generation: other.Claim.Generation, CoordinatorBasis: w.basis()})
	})
	t.Run("the coordinator was taken over", func(t *testing.T) {
		w := newCoordWorld(t)
		old := w.proof()
		successor := w.takeOverLease()
		w.refused(api.CodeTakenOver, old)
		// The successor holds the lease and writes.
		fresh := &client.Claim{Token: successor.Token, Generation: successor.Claim.Generation, CoordinatorBasis: w.basis()}
		if err := w.writePlan(w.plan("successor-plan", notApplicable(), "N/A", "N/A"), "successor", fresh); err != nil {
			t.Fatalf("the successor was refused: %v", err)
		}
	})
	t.Run("the lease was released", func(t *testing.T) {
		w := newCoordWorld(t)
		proof := w.proof()
		release := api.GroupRelease{GroupID: coordGroup, Reason: "done", Decision: "user-decision-release"}
		if _, err := w.coordinator.ReleaseGroup(t.Context(), "", coordGroup, release, client.Claim{Token: w.lease.Token, Generation: w.lease.Claim.Generation}); err != nil {
			t.Fatal(err)
		}
		w.refused(api.CodeLeaseInvalid, proof)
	})
	t.Run("the lease was lost", func(t *testing.T) {
		w := newCoordWorld(t)
		proof := w.proof()
		if _, err := w.coordinator.ObserveGroup(t.Context(), "", coordGroup, api.AgentObservation{Status: "stopped"}, client.Claim{Token: w.lease.Token, Generation: w.lease.Claim.Generation}); err != nil {
			t.Fatal(err)
		}
		w.refused(api.CodeLeaseInvalid, proof)
	})
}

func TestCoordinatorReviewRefusesMembersThatDoNotQualify(t *testing.T) {
	t.Run("a member without a task group", func(t *testing.T) {
		w := newCoordWorld(t, func(s *worldSpec) { s.groups[1] = "" })
		w.refused(api.CodeHeldByOther, w.proof())
	})
	t.Run("a member of another group", func(t *testing.T) {
		w := newCoordWorld(t, func(s *worldSpec) { s.groups[1] = coordOtherGroup })
		w.refused(api.CodeHeldByOther, w.proof())
	})
	t.Run("a member in working", func(t *testing.T) {
		w := newCoordWorld(t, func(s *worldSpec) { s.working[1] = true })
		w.refused(api.CodeHeldByOther, w.proof())
	})
	for _, state := range []board.DispatchState{board.DispatchPrepared, board.DispatchUnknown} {
		t.Run("a dispatch in state "+string(state), func(t *testing.T) {
			w := newCoordWorld(t)
			w.dispatchTo(1, state)
			if got := w.show(w.tasks[1]); got.State != "review" {
				t.Fatalf("the member is in %s", got.State)
			}
			w.refused(api.CodeHeldByOther, w.proof())
		})
	}
	// An accepted dispatch keeps its card in working, so the state condition
	// refuses it; the dispatch condition is checked on its own below.
	t.Run("a member with an accepted dispatch", func(t *testing.T) {
		w := newCoordWorld(t)
		w.dispatchTo(1, board.DispatchUnknown)
		card := w.show(w.tasks[1])
		holder := w.holder(1)
		d, err := w.hosts[1].Dispatch(t.Context(), card.TaskID, "coord-round")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.hosts[1].Move(t.Context(), "accept", card.TaskID, api.Move{State: "working", Authorization: d.Authorization}, &holder); err != nil {
			t.Fatal(err)
		}
		if got, err := w.hosts[1].Dispatch(t.Context(), card.TaskID, "coord-round"); err != nil || got.State != board.DispatchAccepted {
			t.Fatalf("dispatch: %+v %v", got.State, err)
		}
		w.refused(api.CodeHeldByOther, w.proof())
	})
	t.Run("the dispatch condition on its own", func(t *testing.T) {
		text := "- TASK_GROUP: " + coordGroup + "\n- DISPATCH_ID: bound-round\n"
		reads := func(state board.DispatchState, err error) func(string, string) (board.Dispatch, error) {
			return func(string, string) (board.Dispatch, error) { return board.Dispatch{State: state}, err }
		}
		for _, state := range []board.DispatchState{board.DispatchPrepared, board.DispatchUnknown, board.DispatchAccepted} {
			expectCode(t, server.OpenDispatchRefusal("task", text, reads(state, nil)), api.CodeHeldByOther)
		}
		expectCode(t, server.OpenDispatchRefusal("task", text, reads("", io.ErrUnexpectedEOF)), api.CodeHeldByOther)
		for _, state := range []board.DispatchState{board.DispatchCompleted, board.DispatchFailed, board.DispatchCancelled} {
			if err := server.OpenDispatchRefusal("task", text, reads(state, nil)); err != nil {
				t.Fatalf("%s counts as in progress: %v", state, err)
			}
		}
		if err := server.OpenDispatchRefusal("task", "- TASK_GROUP: "+coordGroup+"\n", reads(board.DispatchAccepted, nil)); err != nil {
			t.Fatalf("a card without a binding counts as in progress: %v", err)
		}
	})
}

func TestCoordinatorReviewRefusesAStaleBasis(t *testing.T) {
	t.Run("a stale revision", func(t *testing.T) {
		w := newCoordWorld(t)
		proof := w.proof()
		proof.CoordinatorBasis[0].Revision--
		w.refused(api.CodeRevisionConflict, proof)
	})
	t.Run("a holder wrote after the basis was read", func(t *testing.T) {
		w := newCoordWorld(t)
		proof := w.proof()
		holder := w.holder(0)
		card := w.show(w.tasks[0])
		if _, err := w.hosts[0].PutDocument(t.Context(), "later", card.TaskID, api.Document{Path: "progress.md", Text: "later content", ExpectedRevision: card.Revision}, &holder); err != nil {
			t.Fatal(err)
		}
		w.refused(api.CodeRevisionConflict, proof)
	})
	t.Run("a member was taken over", func(t *testing.T) {
		w := newCoordWorld(t)
		old := w.proof(w.tasks[0])
		w.f.clock.advance(91 * time.Second)
		if _, err := w.f.project("host-c").Takeover(t.Context(), "", w.tasks[0], api.ClaimRequest{ClientID: "host-c", ExecutionID: "successor", Owner: "codex", Decision: "explicit takeover fixture", ExpectedGeneration: w.grants[0].Claim.Generation}); err != nil {
			t.Fatal(err)
		}
		// The coordinator learned the new revision but not the new generation.
		basis := w.basis()
		basis[0].Generation = old.CoordinatorBasis[0].Generation
		w.refused(api.CodeLeaseInvalid, &client.Claim{Token: w.lease.Token, Generation: w.lease.Claim.Generation, CoordinatorBasis: basis})
	})
	t.Run("the basis misses a live member", func(t *testing.T) {
		w := newCoordWorld(t)
		w.refused(api.CodeEvidenceIncomplete, w.proof(w.tasks[0]))
	})
	t.Run("the basis names an extra member", func(t *testing.T) {
		w := newCoordWorld(t)
		before := w.revisions()
		plan := w.plan("narrow-plan", notApplicable(), "N/A", "N/A", w.tasks[0])
		expectCode(t, w.writePlan(plan, "narrow", w.proof()), api.CodeEvidenceIncomplete)
		if !reflect.DeepEqual(before, w.revisions()) {
			t.Fatal("a refused write changed the members")
		}
	})
}

// mixedProofs builds the coordinator proofs that must be refused as invalid
// requests: another proof next to the declaration, or a malformed declaration.
func mixedProofs() map[string]func(w *coordWorld) *client.Claim {
	return map[string]func(w *coordWorld) *client.Claim{
		"a joint publication": func(w *coordWorld) *client.Claim {
			proof := w.proof()
			proof.PublicationID = "review-" + strings.Repeat("a", 64)
			return proof
		},
		"multi-task claim proofs": func(w *coordWorld) *client.Claim {
			proof := w.proof()
			proof.Members = map[string]api.ClaimProof{w.tasks[0]: {Token: strings.Repeat("a", 64), Generation: 1}}
			return proof
		},
		"an empty basis": func(w *coordWorld) *client.Claim {
			proof := w.proof()
			proof.CoordinatorBasis = api.CoordinatorBasis{}
			return proof
		},
		"a duplicate member": func(w *coordWorld) *client.Claim {
			proof := w.proof()
			proof.CoordinatorBasis = append(proof.CoordinatorBasis, proof.CoordinatorBasis[0])
			return proof
		},
		"a zero revision": func(w *coordWorld) *client.Claim {
			proof := w.proof()
			proof.CoordinatorBasis[0].Revision = 0
			return proof
		},
		"an invalid task ID": func(w *coordWorld) *client.Claim {
			proof := w.proof()
			proof.CoordinatorBasis[0].TaskID = "../escape"
			return proof
		},
	}
}

func TestCoordinatorReviewRefusesMixedOrMalformedProofs(t *testing.T) {
	for name, build := range mixedProofs() {
		t.Run(name, func(t *testing.T) {
			w := newCoordWorld(t)
			w.refused(api.CodeInvalidRequest, build(w))
		})
	}
}

func TestCoordinatorReviewLeavesTheDispositionToTheHolder(t *testing.T) {
	w := newCoordWorld(t)
	before := w.revisions()
	proof := w.proof(w.tasks[0])
	submission := server.ReviewDispositionSubmission{Disposition: board.ReviewDisposition{TaskID: w.tasks[0], RunID: "any-run", RecordID: "any-record"}, ExpectedRevision: before[w.tasks[0]]}
	_, err := w.coordinator.ReviewAction(t.Context(), "coordinator-disposition", "disposition", submission, proof)
	expectCode(t, err, api.CodeHeldByOther)
	if !reflect.DeepEqual(before, w.revisions()) {
		t.Fatal("a refused disposition changed a member")
	}
}

func TestCoordinatorReviewAllowsACompletedDispatchAndAnExpiredLease(t *testing.T) {
	t.Run("a dispatch that completed and was not settled", func(t *testing.T) {
		w := newCoordWorld(t)
		w.dispatchTo(1, board.DispatchCompleted)
		card := w.show(w.tasks[1])
		if d, err := w.hosts[1].Dispatch(t.Context(), card.TaskID, "coord-round"); err != nil || d.State != board.DispatchCompleted || card.State != "review" {
			t.Fatalf("fixture: %s %+v %v", card.State, d.State, err)
		}
		if err := w.writePlan(w.plan("completed-plan", notApplicable(), "N/A", "N/A"), "completed-dispatch", w.proof()); err != nil {
			t.Fatalf("a completed dispatch blocked the write: %v", err)
		}
	})
	t.Run("a lease that expired and was not taken over", func(t *testing.T) {
		w := newCoordWorld(t)
		proof := w.proof()
		w.f.clock.advance(10 * time.Minute)
		if err := w.writePlan(w.plan("lapsed-plan", notApplicable(), "N/A", "N/A"), "lapsed-lease", proof); err != nil {
			t.Fatalf("an expired lease that nobody took over was refused: %v", err)
		}
	})
}

func TestCoordinatorReviewWriteReplaysItsReceipt(t *testing.T) {
	w := newCoordWorld(t)
	plan := w.plan("replay-plan", notApplicable(), "N/A", "N/A")
	if err := w.writePlan(plan, "replayed", w.proof()); err != nil {
		t.Fatal(err)
	}
	after := w.revisions()
	// The same key answers from the receipt, whatever the declaration says.
	if err := w.writePlan(plan, "replayed", w.proof()); err != nil {
		t.Fatalf("the retry was refused: %v", err)
	}
	if !reflect.DeepEqual(after, w.revisions()) {
		t.Fatal("the retry wrote again")
	}
}

func TestCoordinatorReviewWriteEmitsCardEventsWithoutAGeneration(t *testing.T) {
	w := newCoordWorld(t)
	page, err := w.coordinator.Events(t.Context(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.writePlan(w.plan("event-plan", notApplicable(), "N/A", "N/A"), "events", w.proof()); err != nil {
		t.Fatal(err)
	}
	next, err := w.coordinator.Events(t.Context(), page.Cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range next.Events {
		if e.Kind == api.EventTaskChanged {
			seen[e.TaskID] = true
			if e.Generation != 0 {
				t.Fatalf("a coordinator write published generation %d for %s", e.Generation, e.TaskID)
			}
		}
	}
	for _, id := range w.tasks {
		if !seen[id] {
			t.Fatalf("no task event for %s: %+v", id, next.Events)
		}
	}
}

func TestCoordinatorReviewWriteLogsOneRecordWithoutTokens(t *testing.T) {
	w := newCoordWorld(t)
	proof := w.proof()
	before := strings.Count(w.logs.String(), "review write by group coordinator")
	if err := w.writePlan(w.plan("log-plan", notApplicable(), "N/A", "N/A"), "logged", proof); err != nil {
		t.Fatal(err)
	}
	text := w.logs.String()
	if got := strings.Count(text, "review write by group coordinator") - before; got != 1 {
		t.Fatalf("%d records:\n%s", got, text)
	}
	var line string
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "review write by group coordinator") {
			line = l
		}
	}
	for _, want := range []string{"operation=review-plan", "group=" + coordGroup, "generation=" + "1", w.tasks[0], w.tasks[1]} {
		if !strings.Contains(line, want) {
			t.Fatalf("the record lacks %q: %s", want, line)
		}
	}
	for _, secret := range []string{w.lease.Token, w.grants[0].Token, w.grants[1].Token} {
		if strings.Contains(text, secret) {
			t.Fatal("the server log holds a token")
		}
	}
	// A refused write leaves no such record.
	count := strings.Count(w.logs.String(), "review write by group coordinator")
	w.refused(api.CodeRevisionConflict, func() *client.Claim { p := w.proof(); p.CoordinatorBasis[0].Revision--; return p }())
	if strings.Count(w.logs.String(), "review write by group coordinator") != count {
		t.Fatal("a refused write was logged as a coordinator write")
	}
}

const coordWriter = "coordinator-review-writer-secret-0123456789abcdef0123456789abcdef0123"

// writerProof is the coordinator proof plus the private writer secret of a
// review run. The writer secret stays with the client that started the run.
func (w *coordWorld) writerProof() *client.Claim {
	proof := w.proof()
	proof.ReviewWriter = coordWriter[:64]
	return proof
}

func (w *coordWorld) prepareRequest(runID, batchID string, schema int, requirements map[string]string) api.ReviewPrepare {
	return api.ReviewPrepare{
		Input:        board.ReviewInput{RunID: runID, BatchID: batchID, TaskIDs: w.tasks[:], Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "/independent/clone", Base: coordBase, Commit: coordTarget, ReportLanguage: "en", FindingsSchema: schema},
		Requirements: requirements,
		Originals:    map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")},
		Version:      "test",
		WriterHash:   board.ReviewDigest([]byte(coordWriter[:64])),
	}
}

// prepareRun prepares a review run of both members; the batch is planned when
// a plan holds batchID.
func (w *coordWorld) prepareRun(runID, batchID string, schema int, requirements map[string]string) board.ReviewRun {
	w.t.Helper()
	request := w.prepareRequest(runID, batchID, schema, requirements)
	raw, err := w.coordinator.ReviewRuntime(w.t.Context(), "prepare-"+runID, runID, "prepare", request, w.writerProof())
	if err != nil {
		w.t.Fatalf("the coordinator prepare was refused: %v", err)
	}
	var prepared board.ReviewPreparation
	if err := json.Unmarshal(raw, &prepared); err != nil || !prepared.Fresh {
		w.t.Fatalf("prepare: %+v %v", prepared, err)
	}
	return prepared.Run
}

// finalize uploads the collected output of a prepared run with the given report.
func (w *coordWorld) finalize(run board.ReviewRun, report string) board.ReviewRun {
	w.t.Helper()
	finalization := runtimeFinalization(run)
	finalization.Report = []byte(report)
	raw, err := w.coordinator.ReviewRuntime(w.t.Context(), "finalize-"+run.RunID, run.RunID, "finalize", finalization, &client.Claim{ReviewWriter: coordWriter[:64]})
	if err != nil {
		w.t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &run); err != nil {
		w.t.Fatal(err)
	}
	return run
}

func (w *coordWorld) publish(run board.ReviewRun) error {
	_, err := w.coordinator.ReviewRuntime(w.t.Context(), "publish-"+run.RunID, run.RunID, "publish", map[string]string{}, w.writerProof())
	return err
}

func (w *coordWorld) batchView(batch string) board.ReviewBatchView {
	w.t.Helper()
	raw, err := w.coordinator.ReviewBatchView(w.t.Context(), batch)
	if err != nil {
		w.t.Fatal(err)
	}
	var view board.ReviewBatchView
	if err := json.Unmarshal(raw, &view); err != nil {
		w.t.Fatal(err)
	}
	return view
}

func required() map[string]string {
	return map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
}

// A coordinator writes every kind of review evidence in one command, without a
// joint publication and without a collaboration request.
func TestCoordinatorReviewWritesEveryOperation(t *testing.T) {
	// ok fails the test when the coordinator write was refused, and checks that
	// no joint publication or request came with it.
	ok := func(t *testing.T, w *coordWorld, what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: the coordinator write was refused: %v", what, err)
		}
		if w.requests() != 0 {
			t.Fatalf("%s created collaboration requests", what)
		}
	}
	ctx := func(t *testing.T) context.Context { return t.Context() }

	t.Run("review-extend-plan add_members", func(t *testing.T) {
		w := newCoordWorld(t)
		plan := w.plan("members-plan", notApplicable(), "N/A", "N/A", w.tasks[0])
		plan.Sealed = false
		ok(t, w, "plan", w.writePlan(plan, "members-plan", w.proof(w.tasks[0])))
		stored := readPlan(t, w.coordinator, plan.PlanID)
		request := board.ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: stored.Revision, AddMembers: []string{w.tasks[1]}, Author: "coordinator", Basis: "the second member started"}
		_, err := w.coordinator.ReviewAction(ctx(t), "members-extend", "extend-plan", request, w.proof())
		ok(t, w, "add_members", err)
		if got := readPlan(t, w.coordinator, plan.PlanID); len(got.TaskIDs) != 2 || got.Revision != stored.Revision+1 {
			t.Fatalf("plan: %+v", got)
		}
	})
	t.Run("review-aggregate", func(t *testing.T) {
		w := newCoordWorld(t)
		plan := w.plan("aggregate-plan", notApplicable(), "N/A", "N/A")
		ok(t, w, "plan", w.writePlan(plan, "aggregate-plan", w.proof()))
		_, err := w.coordinator.ReviewAggregate(ctx(t), "aggregate", plan.PlanID+"-batch", w.proof())
		ok(t, w, "aggregate", err)
	})
	t.Run("review-close", func(t *testing.T) {
		w := newCoordWorld(t)
		plan := w.plan("close-plan", notApplicable(), "N/A", "N/A")
		batch := plan.PlanID + "-batch"
		ok(t, w, "plan", w.writePlan(plan, "close-plan", w.proof()))
		_, err := w.coordinator.ReviewAggregate(ctx(t), "close-aggregate", batch, w.proof())
		ok(t, w, "aggregate", err)
		view := w.batchView(batch)
		request := board.ReviewCloseRequest{BatchID: batch, ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "coordinator",
			Roles: map[string]board.ReviewRoleConclusion{}, ResolvedFailures: map[string]string{}, Opinions: []board.ReviewOpinion{}}
		edges, _, err := board.ReviewClosureEdges(view, request)
		if err != nil {
			t.Fatal(err)
		}
		evidence := board.ReviewGitEvidence{CWD: "project:" + w.coordinator.ID(), Head: "N/A", NotApplicable: "coordinator fixture", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
		_, err = w.coordinator.ReviewClose(ctx(t), "close", map[string]any{"request": request, "evidence": evidence}, w.proof())
		ok(t, w, "close", err)
	})
	t.Run("review-advance", func(t *testing.T) {
		w := newCoordWorld(t)
		plan := w.plan("advance-plan", notApplicable(), coordBase, coordTarget)
		batch := plan.PlanID + "-batch"
		ok(t, w, "plan", w.writePlan(plan, "advance-plan", w.proof()))
		request := board.ReviewBatchAdvance{BatchID: batch, ExpectedRevision: w.batchView(batch).Batch.Revision, Advance: board.ReviewAdvance{PreviousTarget: coordTarget, Target: strings.Repeat("3", 40), Reason: "coordinator fixture", Deliveries: map[string]string{strings.Repeat("3", 40): w.tasks[0]}}}
		_, err := w.coordinator.ReviewAction(ctx(t), "advance", "advance", request, w.proof())
		ok(t, w, "advance", err)
	})
	t.Run("review-prepare and review-publish", func(t *testing.T) {
		w := newCoordWorld(t)
		run := w.finalize(w.prepareRun("prepare-run", "prepare-batch", 0, required()), "PASS\n")
		ok(t, w, "prepare", nil)
		ok(t, w, "publish", w.publish(run))
		for _, id := range w.tasks {
			indexes, err := board.ParseReviewIndexes(w.show(id).Text)
			if err != nil || len(indexes) != 1 {
				t.Fatalf("%s: run was not published once: %+v %v", id, indexes, err)
			}
		}
	})
	t.Run("review-abandon", func(t *testing.T) {
		w := newCoordWorld(t)
		run := w.finalize(w.prepareRun("abandon-run", "abandon-batch", 0, required()), "PASS\n")
		request := board.ReviewAbandonRequest{RunID: run.RunID, Decision: "user decision 2026-11-20", Reason: "the writer can no longer publish"}
		_, err := w.coordinator.ReviewAction(ctx(t), "abandon", "abandon", request, w.proof())
		ok(t, w, "abandon", err)
	})
	t.Run("review-assign", func(t *testing.T) {
		w := newCoordWorld(t)
		plan := w.plan("assign-plan", required(), coordBase, coordTarget)
		batch := plan.PlanID + "-batch"
		ok(t, w, "plan", w.writePlan(plan, "assign-plan", w.proof()))
		run := w.finalize(w.prepareRun("assign-run", batch, 0, required()), "PASS\n```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```\n")
		ok(t, w, "publish", w.publish(run))
		request := board.ReviewAssignment{RunID: run.RunID, BatchID: batch, Author: "coordinator", Basis: "no findings", Items: map[string][]string{}}
		_, err := w.coordinator.ReviewAction(ctx(t), "assign", "assign", request, w.proof())
		ok(t, w, "assign", err)
	})
	t.Run("review-interpret", func(t *testing.T) {
		w := newCoordWorld(t)
		report := "PASS\n"
		run := w.finalize(w.prepareRun("interpret-run", "interpret-batch", board.FindingsSchemaReceiver, required()), report)
		ok(t, w, "publish", w.publish(run))
		request := board.ReviewInterpretation{RunID: run.RunID, ReportHash: board.ReviewDigest([]byte(report)), Author: "coordinator", Basis: "the report states no findings", Complete: true,
			Findings: board.ReviewFindings{Findings: []board.ReviewFinding{}, NonBlocking: []board.ReviewFinding{}}, Locations: []board.LegacyFindingLocation{}, NoFindings: &board.ReviewReportQuote{StartLine: 1, EndLine: 1, Quote: "PASS"}}
		_, err := w.coordinator.ReviewAction(ctx(t), "interpret", "interpret", request, w.proof())
		ok(t, w, "interpret", err)
	})
	t.Run("review-map-legacy", func(t *testing.T) {
		w := newCoordWorld(t)
		report := "FAIL\nthe first finding\n"
		run := w.finalize(w.prepareRun("legacy-run", "legacy-batch", 0, required()), report)
		ok(t, w, "publish", w.publish(run))
		request := board.LegacyFindingMap{RunID: run.RunID, ReportHash: board.ReviewDigest([]byte(report)), Author: "coordinator", Basis: "mapped by hand", Complete: true,
			Findings:  board.ReviewFindings{Findings: []board.ReviewFinding{{ID: "F1", Tier: "high", Text: "the first finding", Evidence: "report line 2"}}, NonBlocking: []board.ReviewFinding{}},
			Locations: []board.LegacyFindingLocation{{FindingID: "F1", StartLine: 2, EndLine: 2, Quote: "the first finding"}}}
		_, err := w.coordinator.ReviewAction(ctx(t), "map-legacy", "map-legacy", request, w.proof())
		ok(t, w, "map-legacy", err)
	})
}

// closeBatch aggregates and closes a not-applicable batch with the coordinator
// write.
func (w *coordWorld) closeBatch(batch string, roles map[string]board.ReviewRoleConclusion) {
	w.t.Helper()
	if _, err := w.coordinator.ReviewAggregate(w.t.Context(), "aggregate-"+batch, batch, w.proof()); err != nil {
		w.t.Fatal(err)
	}
	view := w.batchView(batch)
	request := board.ReviewCloseRequest{BatchID: batch, ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "coordinator",
		Roles: roles, ResolvedFailures: map[string]string{}, Opinions: []board.ReviewOpinion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		w.t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: "project:" + w.coordinator.ID(), Head: coordTarget, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	if _, err := w.coordinator.ReviewClose(w.t.Context(), "close-"+batch, map[string]any{"request": request, "evidence": evidence}, w.proof()); err != nil {
		w.t.Fatal(err)
	}
}

// hidePlanPointers moves the plan pointers of both members aside while fn
// runs. A v1 board once let a run outside the plan exist; no public entrance
// writes one now, so the fixture builds it the way the board package tests do.
func (w *coordWorld) hidePlanPointers(fn func()) {
	w.t.Helper()
	root, err := w.f.srv.BoardRoot(w.coordinator.ID())
	if err != nil {
		w.t.Fatal(err)
	}
	var moved []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(filepath.Dir(path)) == "task-plans" && slices.Contains(w.tasks[:], strings.TrimSuffix(d.Name(), ".json")) {
			moved = append(moved, path)
		}
		return err
	})
	if err != nil || len(moved) != 2 {
		w.t.Fatalf("plan pointers: %v %v", moved, err)
	}
	for _, path := range moved {
		if err := os.Rename(path, path+".moved"); err != nil {
			w.t.Fatal(err)
		}
	}
	defer func() {
		for _, path := range moved {
			if err := os.Rename(path+".moved", path); err != nil {
				w.t.Fatal(err)
			}
		}
	}()
	fn()
}

// A coordinator accepts an unplanned batch through the separate resolve-batch
// operation, which the extend-plan command sends when it carries resolve_batch.
func TestCoordinatorReviewResolvesAnUnplannedBatch(t *testing.T) {
	w := newCoordWorld(t)
	clean := "PASS\n```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```\n"
	plan := w.plan("resolve-plan", required(), coordBase, coordTarget)
	cover := plan.PlanID + "-batch"
	if err := w.writePlan(plan, "resolve-plan", w.proof()); err != nil {
		t.Fatal(err)
	}
	run := w.finalize(w.prepareRun("cover-run", cover, 0, required()), clean)
	if err := w.publish(run); err != nil {
		t.Fatal(err)
	}
	assignment := board.ReviewAssignment{RunID: run.RunID, BatchID: cover, Author: "coordinator", Basis: "no findings", Items: map[string][]string{}}
	if _, err := w.coordinator.ReviewAction(t.Context(), "assign-cover", "assign", assignment, w.proof()); err != nil {
		t.Fatal(err)
	}
	w.closeBatch(cover, map[string]board.ReviewRoleConclusion{"PMQA": {RunID: run.RunID, PassedAt: run.Commit, Basis: "independently checked"}})
	w.hidePlanPointers(func() {
		stray := w.finalize(w.prepareRun("stray-run", "stray-batch", 0, required()), clean)
		if err := w.publish(stray); err != nil {
			t.Fatal(err)
		}
	})
	stored := readPlan(t, w.coordinator, plan.PlanID)
	extension := board.ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: stored.Revision, Author: "coordinator", Basis: "the closed batch covers the stray one",
		ResolveBatch: &board.ReviewBatchResolutionRequest{BatchID: "stray-batch", CoveringBatchID: cover, Findings: []board.ReviewResolvedFinding{}}}
	raw, err := w.coordinator.ReviewAction(t.Context(), "resolution-facts", "resolution-facts", extension, nil)
	if err != nil {
		t.Fatal(err)
	}
	var facts board.ReviewResolutionFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	resolution := api.ReviewResolution{Extension: extension, Facts: facts, Git: board.ReviewResolutionGit{CWD: facts.CWD, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: facts.Edges}}
	if _, err := w.coordinator.ReviewAction(t.Context(), "resolve", "resolve-batch", resolution, w.proof()); err != nil {
		t.Fatalf("the coordinator resolution was refused: %v", err)
	}
	if got := readPlan(t, w.coordinator, plan.PlanID); len(got.ResolvedBatches) != 1 || got.Revision != stored.Revision+1 {
		t.Fatalf("plan: %+v", got)
	}
	if w.requests() != 0 {
		t.Fatal("the resolution created collaboration requests")
	}
}

// The recovery of a prepared run returns the existing run before any write, and
// still answers a malformed or mixed coordinator declaration as an invalid
// request: the same four refusals as a write.
func TestCoordinatorReviewRefusesMixedProofsWhenRecoveringAPreparedRun(t *testing.T) {
	for name, build := range mixedProofs() {
		t.Run(name, func(t *testing.T) {
			w := newCoordWorld(t)
			w.prepareRun("recover-run", "recover-batch", 0, required())
			before := w.revisions()
			proof := build(w)
			proof.ReviewWriter = coordWriter[:64]
			request := w.prepareRequest("recover-run", "recover-batch", 0, required())
			_, err := w.coordinator.ReviewRuntime(t.Context(), client.NewKey(), "recover-run", "prepare", request, proof)
			expectCode(t, err, api.CodeInvalidRequest)
			if !reflect.DeepEqual(before, w.revisions()) {
				t.Fatal("a refused recovery changed the members")
			}
		})
	}
}
