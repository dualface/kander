package server_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/board/boardtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/server"
)

// reviewedHolder is a prepared holder whose card has real review evidence on
// the board of the server: one batch, one run, one finding assigned to the card.
type reviewedHolder struct {
	*preparedHolder
	evidence boardtest.Reviewed
}

func newReviewedHolder(t *testing.T, slug string) *reviewedHolder {
	t.Helper()
	t.Setenv(config.EnvLangCLI, "en")
	h := newPreparedHolder(t, slug)
	root, err := h.f.srv.BoardRoot(h.holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	evidence := boardtest.ReviewedFinding(t, root, h.task)
	// Publishing the run touched the card; a proposal binds the version it sees.
	if h.card, err = h.holder.Show(t.Context(), h.task); err != nil {
		t.Fatal(err)
	}
	return &reviewedHolder{preparedHolder: h, evidence: evidence}
}

// fixInput is a fix dispatch on the evidence; change adjusts the binding.
func (h *reviewedHolder) fixInput(id string, change ...func(*board.DispatchFixBinding)) board.DispatchInput {
	in := h.input(id)
	in.Kind, in.Base, in.Message = "fix", h.evidence.Commit, "Fix the reviewed finding"
	in.Evidence.Fix = &board.DispatchFixBinding{BatchID: h.evidence.BatchID, Findings: []board.DispatchFindingReference{{FindingRef: board.FindingRef{RunID: h.evidence.RunID, FindingID: h.evidence.FindingID}}}}
	for _, c := range change {
		c(in.Evidence.Fix)
	}
	return in
}

func (h *reviewedHolder) sendFix(key string, in board.DispatchInput) (api.CollabRequest, error) {
	return h.sender.CreateRequest(h.f.t.Context(), key, h.task, api.NewCollabRequest{Kind: api.RequestFix, Message: in.Message, Dispatch: &in, CardRevision: h.card.Revision, TargetGeneration: h.grant.Claim.Generation, AcceptWithin: api.DurationOf(120 * time.Second)})
}

func (h *reviewedHolder) requestCount(t *testing.T) int {
	t.Helper()
	requests, err := h.holder.Requests(t.Context(), h.task)
	if err != nil {
		t.Fatal(err)
	}
	return len(requests)
}

// apiError unwraps the refusal the client returned.
func apiError(t *testing.T, err error) *api.Error {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) {
		t.Fatalf("not an API error: %v", err)
	}
	return e
}

func TestFixProposalEvidenceIsCheckedWhenTheProposalIsCreated(t *testing.T) {
	h := newReviewedHolder(t, "fix-proposal-check")
	late := func(f *board.DispatchFixBinding) { f.Findings[0].PreviousRunID = "earlier" }

	_, err := h.sendFix("refused", h.fixInput("fix-first", late))
	refusal := apiError(t, err)
	if refusal.Code != api.CodeBoardRejected || refusal.BoardCode != "board.dispatch_evidence_invalid" || refusal.Status != http.StatusUnprocessableEntity {
		t.Fatalf("refusal %+v", refusal)
	}
	for _, want := range []string{"2 problem(s):", h.evidence.RunID + "/PM-01: previous_run_id: provided \"earlier\", the run record holds \"\"", "Next step:"} {
		if !strings.Contains(refusal.Message, want) {
			t.Fatalf("refusal lacks %q:\n%s", want, refusal.Message)
		}
	}
	if n := h.requestCount(t); n != 0 {
		t.Fatalf("a refused proposal left %d request(s)", n)
	}
	// The same key and the same content replay the stored refusal.
	_, err = h.sendFix("refused", h.fixInput("fix-first", late))
	if replay := apiError(t, err); replay.Message != refusal.Message || replay.BoardCode != refusal.BoardCode {
		t.Fatalf("replay %+v, want %+v", replay, refusal)
	}
	// The sender corrects the evidence and sends again under a new key. A
	// non-holder's dispatch prepare may keep the dispatch ID: nothing took it.
	created, err := h.sendFix("corrected", h.fixInput("fix-first"))
	if err != nil || created.Status != api.RequestOpen || created.Dispatch == nil || created.Dispatch.Kind != "fix" {
		t.Fatalf("corrected proposal: %+v %v", created, err)
	}
	if n := h.requestCount(t); n != 1 {
		t.Fatalf("%d request(s) after the corrected proposal", n)
	}
	// The holder prepares exactly what was proposed: the check at creation and
	// the check at preparation agree on valid evidence.
	if err := h.prepare("fix-prepare", *created.Dispatch); err != nil {
		t.Fatalf("prepare of the proposal: %v", err)
	}
}

func TestFixProposalCheckRunsOnlyForANewFixProposal(t *testing.T) {
	h := newReviewedHolder(t, "fix-proposal-once")
	checked := 0
	t.Cleanup(server.SetFixProposalCheck(func(tx *board.Transaction, in board.DispatchInput) error {
		checked++
		return board.CheckDispatchFixProposal(tx, in)
	}))
	first, err := h.sendFix("fix-key", h.fixInput("fix-once"))
	if err != nil || checked != 1 {
		t.Fatalf("first proposal: %+v %v after %d check(s)", first, err, checked)
	}
	// The stored receipt answers a replay, and the slot of the dispatch ID
	// answers another key with the same material; neither checks again.
	if again, err := h.sendFix("fix-key", h.fixInput("fix-once")); err != nil || again.ID != first.ID || checked != 1 {
		t.Fatalf("replay: %+v %v after %d check(s)", again, err, checked)
	}
	if again, err := h.sendFix("fix-other-key", h.fixInput("fix-once")); err != nil || again.ID != first.ID || checked != 1 {
		t.Fatalf("second creation: %+v %v after %d check(s)", again, err, checked)
	}
	// Other material under the taken ID is an idempotency mismatch, whatever the
	// evidence says.
	_, err = h.sendFix("fix-mismatch", h.fixInput("fix-once", func(f *board.DispatchFixBinding) { f.BatchID = "no-such-batch" }))
	expectCode(t, err, api.CodeIdempotencyMismatch)
	if checked != 1 || h.requestCount(t) != 1 {
		t.Fatalf("%d check(s), %d request(s)", checked, h.requestCount(t))
	}
	// Sync proposals and plain requests carry no fix evidence.
	h.propose(t, "sync-key", h.input("sync-proposal"), 120*time.Second)
	h.create(t, "plain-key", api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: h.grant.Claim.Generation})
	if checked != 1 {
		t.Fatalf("a request without fix evidence was checked: %d", checked)
	}
}

// A failure that says nothing about the evidence is an internal error: the
// receipt stays pending, so the same key retries instead of replaying it.
func TestFixProposalCheckFailureIsRetried(t *testing.T) {
	h := newReviewedHolder(t, "fix-proposal-retry")
	restore := server.SetFixProposalCheck(func(*board.Transaction, board.DispatchInput) error { return errors.New("read: input/output error") })
	t.Cleanup(restore)
	_, err := h.sendFix("retried", h.fixInput("fix-retried"))
	expectCode(t, err, api.CodeInternal)
	if n := h.requestCount(t); n != 0 {
		t.Fatalf("a failed check left %d request(s)", n)
	}
	restore()
	if created, err := h.sendFix("retried", h.fixInput("fix-retried")); err != nil || created.Status != api.RequestOpen {
		t.Fatalf("retry of the pending key: %+v %v", created, err)
	}
}

func TestFixProposalWithoutABatchNamesTheBatchID(t *testing.T) {
	h := newReviewedHolder(t, "fix-proposal-no-batch")
	_, err := h.sendFix("no-batch", h.fixInput("fix-no-batch", func(f *board.DispatchFixBinding) { f.BatchID = "no-such-batch" }))
	refusal := apiError(t, err)
	if refusal.BoardCode != "board.dispatch_evidence_invalid" || !strings.Contains(refusal.Message, `batch_id "no-such-batch": no such review batch`) {
		t.Fatalf("refusal %+v", refusal)
	}
}

// A later review round makes the first round obsolete. The holder that prepares
// its own fix dispatch is told which run came after; a proposal made before the
// round names the card version the publication of that round changed, so its
// preparation is refused as stale and its sender proposes again.
func TestFixOnASupersededRoundNamesTheLaterRun(t *testing.T) {
	h := newReviewedHolder(t, "fix-proposal-superseded")
	proposed, err := h.sendFix("before-successor", h.fixInput("fix-before"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := h.f.srv.BoardRoot(h.holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	next := boardtest.Successor(t, root, h.task, h.evidence)
	card, err := h.holder.Show(t.Context(), h.task)
	if err != nil {
		t.Fatal(err)
	}
	stale := h.prepare("stale-prepare", *proposed.Dispatch)
	expectCode(t, stale, api.CodeRevisionConflict)

	h.card = card
	direct := h.fixInput("fix-direct")
	err = h.prepare("direct-prepare", direct)
	if e := apiError(t, err); e.BoardCode != "board.dispatch_evidence_invalid" || !strings.Contains(e.Message, h.evidence.RunID+"/"+h.evidence.FindingID+": superseded review round; later run(s): "+next.RunID) || strings.Contains(e.Message, "Next step") {
		t.Fatalf("prepare of a superseded round: %+v", e)
	}
	// The sender proposes again on the new version and learns it at once.
	_, err = h.sendFix("after-successor", h.fixInput("fix-after"))
	if e := apiError(t, err); !strings.Contains(e.Message, "later run(s): "+next.RunID) || !strings.Contains(e.Message, "Next step") {
		t.Fatalf("proposal of a superseded round: %+v", e)
	}
}
