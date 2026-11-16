package server

import (
	"bytes"
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

func TestIssueResultServerSavesSatisfyTheRelations(t *testing.T) {
	f := newIssueResultFixture(t)
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
	revision := func() uint64 {
		state, err := f.a.Project.IssueResult(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		return state.Revision
	}
	// entrance runs one client entrance and checks that the revision rose by the number of saves.
	entrance := func(name string, run func() error) {
		t.Helper()
		before, savesBefore := revision(), saves.Load()
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, want := revision()-before, uint64(saves.Load()-savesBefore); got != want || want == 0 {
			t.Fatalf("%s: revision rose by %d for %d saves", name, got, want)
		}
	}
	var inspection issue.ResultInspection
	entrance("inspect", func() (err error) {
		inspection, err = issue.InspectResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID)
		return
	})
	// A definite provider rejection, then the same assessment published.
	f.provider.postRejected = true
	entrance("apply rejected", func() error {
		_, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection))
		if err == nil {
			return errors.New("rejection not reported")
		}
		return nil
	})
	f.provider.postRejected = false
	entrance("inspect", func() (err error) {
		inspection, err = issue.InspectResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID)
		return
	})
	var record issue.ResultRecord
	entrance("apply", func() (err error) {
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
		entrance("inspect", func() (err error) {
			inspection, err = issue.InspectResult(f.ctxB, f.provider, f.rootB, f.repo, 42, f.cardID)
			return
		})
		entrance("decide "+tc.decision, func() error {
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
