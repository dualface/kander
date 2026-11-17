package remote_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

const (
	aggregateBatch  = "aggregate-batch"
	aggregateBase   = "1111111111111111111111111111111111111111"
	aggregateTarget = "2222222222222222222222222222222222222222"
	aggregateFix    = "3333333333333333333333333333333333333333"
)

// aggregateFixture is a planned two-holder batch: every aggregation is a joint
// publication, holder a runs the commands and holder b confirms.
type aggregateFixture struct {
	t    *testing.T
	a, b *remote.Session
	ids  []string
}

func newAggregateFixture(t *testing.T) *aggregateFixture {
	t.Helper()
	t.Setenv(remote.EnvExecutionID, "")
	a, b, ids := publicationSessions(t)
	f := &aggregateFixture{t: t, a: a, b: b, ids: ids}
	plan := board.ReviewPlan{Schema: 1, PlanID: "aggregate-plan", Sealed: true, Author: "coordinator", Basis: "verify aggregate replay", CWD: "/host-a/clone", ReportLanguage: "en", TaskIDs: ids, Batches: []board.ReviewPlanBatch{{BatchID: aggregateBatch, TaskIDs: ids, Base: aggregateBase, TargetCommit: aggregateTarget, Requirements: map[string]string{"PMQA": "N/A: fixture", "Security": "N/A: repository policy"}}}}
	proof := f.joint("review-plan", plan)
	if _, err := a.Project.ReviewPlan(t.Context(), client.NewKey(), plan, proof); err != nil {
		t.Fatal(err)
	}
	return f
}

// joint runs a joint authority to completion through b's confirmation.
func (f *aggregateFixture) joint(operation string, payload any) *client.Claim {
	f.t.Helper()
	_, err := f.a.ReviewAuthority(f.t.Context(), operation, payload, f.ids)
	if _, err := f.b.ConfirmReviewPublication(f.t.Context(), pendingPublicationID(f.t, err), f.ids[1]); err != nil {
		f.t.Fatal(err)
	}
	proof, err := f.a.ReviewAuthority(f.t.Context(), operation, payload, f.ids)
	if err != nil {
		f.t.Fatal(err)
	}
	return proof
}

// aggregate runs AggregateReview to completion and returns its answer with the
// publication ID it asked b to confirm.
func (f *aggregateFixture) aggregate() (json.RawMessage, string) {
	f.t.Helper()
	_, err := f.a.AggregateReview(f.t.Context(), aggregateBatch)
	id := pendingPublicationID(f.t, err)
	if _, err := f.b.ConfirmReviewPublication(f.t.Context(), id, f.ids[1]); err != nil {
		f.t.Fatal(err)
	}
	out, err := f.a.AggregateReview(f.t.Context(), aggregateBatch)
	if err != nil {
		f.t.Fatal(err)
	}
	return out, id
}

func (f *aggregateFixture) view() board.ReviewBatchView {
	f.t.Helper()
	raw, err := f.a.Project.ReviewBatchView(f.t.Context(), aggregateBatch)
	if err != nil {
		f.t.Fatal(err)
	}
	var v board.ReviewBatchView
	if err := json.Unmarshal(raw, &v); err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *aggregateFixture) advance() {
	f.t.Helper()
	x := board.ReviewBatchAdvance{BatchID: aggregateBatch, ExpectedRevision: f.view().Batch.Revision, Advance: board.ReviewAdvance{PreviousTarget: aggregateTarget, Target: aggregateFix, Reason: "fixture fix", Deliveries: map[string]string{aggregateFix: f.ids[0]}}}
	proof := f.joint("review-advance", x)
	if _, err := f.a.Project.ReviewAction(f.t.Context(), client.NewKey(), "advance", x, proof); err != nil {
		f.t.Fatal(err)
	}
}

func (f *aggregateFixture) requests() int {
	f.t.Helper()
	requests, err := f.a.Project.Requests(f.t.Context(), f.ids[1])
	if err != nil {
		f.t.Fatal(err)
	}
	return len(requests)
}

// digest is the view hash a close request binds for the printed answer.
func digest(t *testing.T, out json.RawMessage) string {
	t.Helper()
	var v board.ReviewBatchView
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	return board.ReviewViewDigest(v)
}

func TestAggregateAfterViewChangePublishesCurrentViewAndCloses(t *testing.T) {
	f := newAggregateFixture(t)
	first, firstID := f.aggregate()
	if digest(t, first) != board.ReviewViewDigest(f.view()) {
		t.Fatal("first aggregation is not the current view")
	}

	before := f.requests()
	again, err := f.a.AggregateReview(t.Context(), aggregateBatch)
	// The server answers a fresh publication indented and a replay compact;
	// the command prints both through the view type, so the hash decides.
	if err != nil || digest(t, again) != digest(t, first) || f.requests() != before {
		t.Fatalf("unchanged view did not replay the publication: %v", err)
	}

	f.advance()
	before = f.requests()
	_, err = f.a.AggregateReview(t.Context(), aggregateBatch)
	if secondID := pendingPublicationID(t, err); secondID == firstID {
		t.Fatalf("changed view reused publication %s", firstID)
	}
	if f.requests() <= before {
		t.Fatal("changed view sent no confirmation request")
	}
	second, _ := f.aggregate()
	current := f.view()
	if digest(t, second) != board.ReviewViewDigest(current) || current.Batch.TargetCommit != aggregateFix {
		t.Fatalf("aggregation after the advance is stale: %s", second)
	}

	request := board.ReviewCloseRequest{BatchID: aggregateBatch, ExpectedRevision: current.Batch.Revision, ViewHash: digest(t, second), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(current, request)
	if err != nil {
		t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: "/host-a/clone", Head: aggregateFix, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	body := map[string]any{"request": request, "evidence": evidence}
	if _, err := f.a.Project.ReviewClose(t.Context(), client.NewKey(), body, f.joint("review-close:"+aggregateBatch, body)); err != nil {
		t.Fatalf("close bound to the new aggregation failed: %v", err)
	}
}

func TestAggregateIgnoresReplayRecordOfOlderClient(t *testing.T) {
	f := newAggregateFixture(t)
	// An older client keyed the aggregation by the batch ID alone.
	operation := "review-aggregate:" + aggregateBatch
	proof := f.joint(operation, aggregateBatch)
	if _, err := f.a.Project.ReviewAggregate(t.Context(), client.NewKey(), aggregateBatch, proof); err != nil {
		t.Fatal(err)
	}
	f.advance()
	out, id := f.aggregate()
	if id == proof.PublicationID || digest(t, out) != board.ReviewViewDigest(f.view()) {
		t.Fatalf("older replay record returned a stale view: %s", out)
	}
}

func TestAggregateRefusesReplayedResultOfAnotherView(t *testing.T) {
	f := newAggregateFixture(t)
	_, firstID := f.aggregate()
	f.advance()
	// Point the record of the current view at the earlier publication.
	root, err := f.a.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	key, err := json.Marshal(struct {
		Operation string
		Material  json.RawMessage
	}{"review-aggregate:" + aggregateBatch + "@view:" + board.ReviewViewDigest(f.view()), json.RawMessage(`"` + aggregateBatch + `"`)})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(key)
	record := filepath.Join(root, "review-operations", hex.EncodeToString(sum[:])+".json")
	if err := os.WriteFile(record, []byte(`["`+firstID+`"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := f.a.AggregateReview(t.Context(), aggregateBatch); err == nil || out != nil || !strings.Contains(err.Error(), aggregateBatch) {
		t.Fatalf("stale replay was accepted: %s %v", out, err)
	}
}

// publishedPrepare answers every preparation as already published and returns
// a saved aggregation, as a server would for a reused publication ID.
type publishedPrepare struct {
	remote.ProjectService
	saved json.RawMessage
}

func (p publishedPrepare) PreparePublication(_ context.Context, request api.PublicationPrepare) (api.Publication, error) {
	return api.Publication{PublicationPrepare: request, Status: "published", Result: p.saved}, nil
}

func (p publishedPrepare) ReviewAggregate(context.Context, string, string, *client.Claim) (json.RawMessage, error) {
	return p.saved, nil
}

func TestAggregateRefusesPublishedPreparationOfAnotherView(t *testing.T) {
	f := newAggregateFixture(t)
	first, _ := f.aggregate()
	f.advance()
	stubbed := *f.a
	stubbed.Project = publishedPrepare{ProjectService: f.a.Project, saved: first}
	if out, err := stubbed.AggregateReview(t.Context(), aggregateBatch); err == nil || out != nil || !strings.Contains(err.Error(), aggregateBatch) {
		t.Fatalf("stale published preparation was accepted: %s %v", out, err)
	}
}
