package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/issue"
)

func TestIssueResultCASAcrossClientsPublishesOnlyOnce(t *testing.T) {
	f := newIssueResultFixture(t)
	inspection := f.inspect(f.ctxA, f.rootA)
	proposal := resultFixtureProposal(inspection)
	var arrivals atomic.Int32
	ready := make(chan struct{})
	f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/results/save") {
			n := arrivals.Add(1)
			if n == 2 {
				close(ready)
			}
			if n <= 2 {
				select {
				case <-ready:
				case <-r.Context().Done():
					w.WriteHeader(http.StatusRequestTimeout)
					return true
				}
			}
		}
		return false
	}
	results := make([]issue.ResultRecord, 2)
	failures := make([]error, 2)
	var group sync.WaitGroup
	for i, ctx := range []context.Context{f.ctxA, f.ctxB} {
		group.Go(func() {
			results[i], failures[i] = issue.ApplyResult(ctx, f.provider, []string{f.rootA, f.rootB}[i], f.repo, 42, f.cardID, proposal)
		})
	}
	group.Wait()
	successes := 0
	for i, err := range failures {
		if err == nil {
			successes++
			if results[i].Status != "published" {
				t.Fatal("successful CAS did not publish")
			}
		} else if !api.IsCode(err, api.CodeRevisionConflict) {
			t.Fatalf("unexpected competing result: %v", err)
		}
	}
	if successes != 1 || f.provider.posts != 1 {
		t.Fatalf("concurrent publications: success=%d posts=%d errors=%v", successes, f.provider.posts, failures)
	}
	recovered := f.inspect(f.ctxB, f.rootB)
	if len(recovered.Records) != 1 {
		t.Fatalf("central records diverged: %+v", recovered.Records)
	}
	for _, record := range recovered.Records {
		if record.CommentID != 101 || record.Status != "published" {
			t.Fatalf("wrong formal result: %+v", record)
		}
	}
}
func TestIssueResultUnknownExternalEffectsRecoverOnAnotherClient(t *testing.T) {
	f := newIssueResultFixture(t)
	inspection := f.inspect(f.ctxA, f.rootA)
	f.provider.postUnknown = true
	if _, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection)); err == nil {
		t.Fatal("provider response was not lost")
	}
	inspection = f.inspect(f.ctxB, f.rootB)
	var published issue.ResultRecord
	for _, record := range inspection.Records {
		published = *record
	}
	if published.Status != "published" || published.CommentID != 101 || f.provider.posts != 1 {
		t.Fatalf("cross-client publication recovery: %+v", published)
	}
	f.provider.closeUnknown = true
	decision := issue.ResultDecision{Token: inspection.Token, Version: published.Version, Decision: "yes", UserReference: "explicit separate user consent to close this exact issue state"}
	if _, err := issue.DecideResult(f.ctxB, f.provider, f.rootB, f.repo, 42, f.cardID, decision); err == nil {
		t.Fatal("close response was not lost")
	}
	after := f.inspect(f.ctxA, f.rootA)
	record := after.Records[published.Version]
	if record == nil || len(record.Decisions) != 1 || record.Decisions[0].Status != "observed-closed" || f.provider.closes != 1 {
		t.Fatalf("close recovery: %+v", record)
	}
}
func TestIssueResultRejectsStaleResourceAndHistoryRegression(t *testing.T) {
	f := newIssueResultFixture(t)
	query := api.IssueResultQuery{SourceKey: f.sourceKey, TaskID: f.cardID}
	first, err := f.a.Project.IssueResult(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	var work issue.ResultWork
	if err := json.Unmarshal(first.Work, &work); err != nil {
		t.Fatal(err)
	}
	inspection := f.inspect(f.ctxA, f.rootA)
	input := api.IssueResultWrite{Query: query, ExpectedRevision: first.Revision, WorkDigest: work.Digest, Store: first.Store}
	if _, err := f.b.Project.SaveIssueResult(t.Context(), "stale-result-state", input); !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("stale resource write: %v", err)
	}
	published, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection))
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.b.Project.IssueResult(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	var store issue.ResultStore
	if err := json.Unmarshal(current.Store, &store); err != nil {
		t.Fatal(err)
	}
	delete(store.Records, published.Version)
	raw, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision, input.Store = current.Revision, raw
	if _, err := f.b.Project.SaveIssueResult(t.Context(), "delete-result-history", input); err == nil {
		t.Fatal("published result history removed")
	}
	input.Store = current.Store
	input.WorkDigest = "changed-work"
	if _, err := f.b.Project.SaveIssueResult(t.Context(), "stale-result-evidence", input); !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("stale evidence write: %v", err)
	}
}

func TestIssueResultNewPendingSiblingFencesOldAssessment(t *testing.T) {
	f := newIssueResultFixture(t)
	inspection := f.inspect(f.ctxA, f.rootA)
	sibling, err := f.b.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "pending-issue-sibling", Title: "Complete remaining issue work", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(sibling.Text, "## DISCUSSION\n", "## DISCUSSION\n\nSOURCE_ISSUE: "+f.sourceKey+" (anchor: "+f.cardID+")\n", 1)
	if _, err := f.b.Project.PutDocument(t.Context(), "bind-pending-sibling", sibling.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: sibling.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection)); err == nil {
		t.Fatal("old assessment ignored new pending issue work")
	}
	if f.provider.posts != 0 {
		t.Fatal("incomplete work set published a result")
	}
}

func TestIssueResultAdoptsMigratedHistoryWithoutRepublishing(t *testing.T) {
	f := newIssueResultFixture(t, true)
	query := api.IssueResultQuery{SourceKey: f.sourceKey, TaskID: f.cardID}
	state, err := f.a.Project.IssueResult(t.Context(), query)
	if err != nil || state.Revision != 0 {
		t.Fatalf("legacy source was not read before central adoption: %+v %v", state, err)
	}
	var original issue.ResultStore
	if err := json.Unmarshal(state.Store, &original); err != nil {
		t.Fatal(err)
	}
	inspection := f.inspect(f.ctxB, f.rootB)
	record, err := issue.ApplyResult(f.ctxB, f.provider, f.rootB, f.repo, 42, f.cardID, resultFixtureProposal(inspection))
	if err != nil {
		t.Fatal(err)
	}
	if f.provider.posts != 1 || original.Records[record.Version] == nil || record.CommentID != 101 {
		t.Fatalf("legacy result was republished or lost: %+v", record)
	}
	state, err = f.a.Project.IssueResult(t.Context(), query)
	if err != nil || state.Revision == 0 {
		t.Fatal("migrated history was not centrally adopted")
	}
	var adopted issue.ResultStore
	if err := json.Unmarshal(state.Store, &adopted); err != nil {
		t.Fatal(err)
	}
	if adopted.Records[record.Version].Body != original.Records[record.Version].Body {
		t.Fatal("migration changed immutable comment body")
	}
}
