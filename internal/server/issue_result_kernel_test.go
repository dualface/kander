package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/issue"
	sk "github.com/dualface/kander/internal/statemachine/issueresultsave"
)

// A save of the central result record runs the issue result kernels of the
// store (internal/issue) and of the resource (this package). These tests drive
// the production entrances against a real server: every save a client makes is
// checked against the update and work relations before the server sees it, and
// the resource revision rises by one per accepted save.

// observeSaves checks every save a client sends against the update and work
// relations before the server sees it, and counts the saves.
func (f *issueResultFixture) observeSaves(t *testing.T) *atomic.Int32 {
	query := api.IssueResultQuery{SourceKey: f.sourceKey, TaskID: f.cardID}
	var saves atomic.Int32
	f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/issues/results/save") {
			return false
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var input api.IssueResultWrite
		if err := json.Unmarshal(body, &input); err != nil {
			t.Error(err)
			return false
		}
		current, err := f.a.Project.IssueResult(t.Context(), query)
		if err != nil {
			t.Error(err)
			return false
		}
		var previous, proposed issue.ResultStore
		var work issue.ResultWork
		for raw, target := range map[*json.RawMessage]any{&current.Store: &previous, &input.Store: &proposed, &current.Work: &work} {
			if err := json.Unmarshal(*raw, target); err != nil {
				t.Error(err)
				return false
			}
		}
		if err := issue.ValidateResultStoreUpdate(previous, proposed); err != nil {
			t.Errorf("update relation: %v", err)
		}
		if err := issue.ValidateResultStoreWork(previous, proposed, work); err != nil {
			t.Errorf("work relation: %v", err)
		}
		if input.ExpectedRevision != current.Revision {
			t.Errorf("client expected revision %d, stored %d", input.ExpectedRevision, current.Revision)
		}
		saves.Add(1)
		return false
	}
	return &saves
}

// entrance runs one client entrance and checks that the central revision rose
// by the number of saves the entrance made.
func (f *issueResultFixture) entrance(t *testing.T, saves *atomic.Int32, name string, run func() error) {
	t.Helper()
	query := api.IssueResultQuery{SourceKey: f.sourceKey, TaskID: f.cardID}
	revision := func() uint64 {
		state, err := f.a.Project.IssueResult(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		return state.Revision
	}
	before, savesBefore := revision(), saves.Load()
	if err := run(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if got, want := revision()-before, uint64(saves.Load()-savesBefore); got != want || want == 0 {
		t.Fatalf("%s: revision rose by %d for %d saves", name, got, want)
	}
}

func TestIssueResultServerSavesSatisfyTheRelations(t *testing.T) {
	f := newIssueResultFixture(t)
	saves := f.observeSaves(t)
	var inspection issue.ResultInspection
	inspect := func(ctx context.Context, root string) func() error {
		return func() (err error) {
			inspection, err = issue.InspectResult(ctx, f.provider, root, f.repo, 42, f.cardID)
			return
		}
	}
	f.entrance(t, saves, "inspect", inspect(f.ctxA, f.rootA))
	// A definite provider rejection, then the same assessment published.
	f.provider.postRejected = true
	f.entrance(t, saves, "apply rejected", func() error {
		_, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection))
		if err == nil {
			return errors.New("rejection not reported")
		}
		return nil
	})
	f.provider.postRejected = false
	f.entrance(t, saves, "inspect", inspect(f.ctxA, f.rootA))
	var record issue.ResultRecord
	f.entrance(t, saves, "apply", func() (err error) {
		record, err = issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection))
		return
	})
	if record.Status != "published" || len(record.Failures) != 1 {
		t.Fatalf("record: %+v", record)
	}
	// A refusal, then a consent that closes the issue (the second client decides).
	for _, tc := range []struct {
		decision   string
		reconsider bool
	}{{"no", false}, {"yes", true}} {
		f.entrance(t, saves, "inspect", inspect(f.ctxB, f.rootB))
		f.entrance(t, saves, "decide "+tc.decision, func() error {
			_, err := issue.DecideResult(f.ctxB, f.provider, f.rootB, f.repo, 42, f.cardID, issue.ResultDecision{Token: inspection.Token,
				Version: record.Version, Decision: tc.decision, UserReference: "User: " + tc.decision, Reconsider: tc.reconsider})
			return err
		})
	}
	final := f.inspect(f.ctxA, f.rootA)
	statuses := []string{}
	for _, d := range final.Records[record.Version].Decisions {
		statuses = append(statuses, d.Status)
	}
	if strings.Join(statuses, ",") != "declined,closed" || f.provider.closes != 1 {
		t.Fatalf("decisions %v, closes %d", statuses, f.provider.closes)
	}
}

// TestIssueResultServerEquivalentAndRejectedCloseSequences drives the sequences
// the first test does not: an equivalent comment, a close request the provider
// definitely rejects, a refusal after that rejected consent, and the consent
// again with reconsideration. Every save is checked against both relations.
func TestIssueResultServerEquivalentAndRejectedCloseSequences(t *testing.T) {
	f := newIssueResultFixture(t)
	saves := f.observeSaves(t)
	f.provider.value.Comments = []issue.ResultComment{{ID: 99, AuthorID: 80, Body: "Human verification: fixed, tests pass."}}
	var inspection issue.ResultInspection
	inspect := func() error {
		var err error
		inspection, err = issue.InspectResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID)
		return err
	}
	f.entrance(t, saves, "inspect", inspect)
	var record issue.ResultRecord
	f.entrance(t, saves, "equivalent", func() (err error) {
		p := resultFixtureProposal(inspection)
		p.EquivalentCommentID, p.Body = 99, ""
		record, err = issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, p)
		return
	})
	if record.Status != "equivalent" || record.CommentID != 99 || f.provider.posts != 0 {
		t.Fatalf("record: %+v posts %d", record, f.provider.posts)
	}
	decide := func(decision string, reconsider bool) error {
		if err := inspect(); err != nil {
			return err
		}
		_, err := issue.DecideResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, issue.ResultDecision{Token: inspection.Token,
			Version: record.Version, Decision: decision, UserReference: "User: " + decision, Reconsider: reconsider})
		return err
	}
	decisions := func() string {
		var out []string
		for _, d := range f.inspect(f.ctxA, f.rootA).Records[record.Version].Decisions {
			out = append(out, d.Status)
		}
		return strings.Join(out, ",")
	}
	// The provider definitely rejects the close request: the consent is rejected and a failure is recorded.
	f.provider.closeRejected = true
	f.entrance(t, saves, "close rejected", func() error {
		if err := decide("yes", false); err == nil {
			return errors.New("close rejection not reported")
		}
		return nil
	})
	if got := decisions(); got != "rejected" {
		t.Fatalf("decisions %q", got)
	}
	// The rejected consent is the first decision of its state version, so it blocks a refusal that does
	// not reconsider; nothing is saved.
	if err := inspect(); err != nil {
		t.Fatal(err)
	}
	before := saves.Load()
	_, err := issue.DecideResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, issue.ResultDecision{Token: inspection.Token,
		Version: record.Version, Decision: "no", UserReference: "User: no"})
	if err == nil || !strings.Contains(err.Error(), "old close consent cannot be reused") {
		t.Fatalf("refusal after a rejected consent: %v", err)
	}
	if saves.Load() != before {
		t.Fatal("a refused decision saved")
	}
	// With reconsideration the refusal is recorded, and then the consent closes the issue.
	f.provider.closeRejected = false
	f.entrance(t, saves, "refusal", func() error { return decide("no", true) })
	f.entrance(t, saves, "close", func() error { return decide("yes", true) })
	if got := decisions(); got != "rejected,declined,closed" || f.provider.closes != 2 {
		t.Fatalf("decisions %q, closes %d", got, f.provider.closes)
	}
}

// TestIssueResultRevisionKernelFrames drives the resource kernel with revisions
// near the frame shifts: an accepted save rises by one, a request that expects
// another revision is refused, and the last revision is never saved over.
func TestIssueResultRevisionKernelFrames(t *testing.T) {
	for _, actual := range []uint64{0, 1, 1<<31 - 1, 1 << 31, 1<<31 + 1, 1 << 40, ^uint64(0) - 1, ^uint64(0)} {
		r := newIssueRevision(actual)
		next, ok := r.save(actual, true, true, true)
		if want := actual != ^uint64(0); ok != want || ok && next != actual+1 || !ok && next != actual {
			t.Errorf("revision %d: save %d %v", actual, next, ok)
		}
		for _, wrong := range []uint64{actual + 1, actual - 1, 0, ^uint64(0)} {
			if wrong == actual {
				continue
			}
			if next, ok := r.save(wrong, true, true, true); ok || next != actual {
				t.Errorf("revision %d: a request that expects %d saved %d", actual, wrong, next)
			}
		}
		// Each fact the shell computes refuses the save, and the revision stays.
		for i, facts := range [][3]bool{{false, true, true}, {true, false, true}, {true, true, false}} {
			if next, ok := r.save(actual, facts[0], facts[1], facts[2]); ok || next != actual {
				t.Errorf("revision %d: fact %d failed and the save was accepted", actual, i)
			}
		}
		// A stored record that exists has a revision.
		if got, want := r.read(true), actual != 0; got != want {
			t.Errorf("revision %d: read of a stored record %v", actual, got)
		}
		if !r.read(false) {
			t.Errorf("revision %d: read without a stored record refused", actual)
		}
	}
	// The kernel is the one the proofs are about: the state after a save is the
	// state before it with the revision raised by one.
	before := newIssueRevision(7).state
	after, ok := sk.Step(before, sk.Save{Work: true, Expected: before.Rev, Room: true, Upd: true, Tx: true}, issueSaveParams)
	if !ok || after.Rev != before.Rev+1 {
		t.Fatalf("kernel save: %+v %v", after, ok)
	}
}
