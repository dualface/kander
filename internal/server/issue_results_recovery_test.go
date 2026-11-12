package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/remote"
)

func TestIssueResultPublishedReceiptSurvivesResponseLossAndServerRestart(t *testing.T) {
	f := newIssueResultFixture(t)
	inspection := f.inspect(f.ctxA, f.rootA)
	var hide atomic.Bool
	hide.Store(true)
	f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		if !hide.Load() || !strings.HasSuffix(r.URL.Path, "/issues/results/save") {
			return false
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		var input api.IssueResultWrite
		var store issue.ResultStore
		if json.Unmarshal(data, &input) != nil || json.Unmarshal(input.Store, &store) != nil {
			return false
		}
		published := false
		for _, record := range store.Records {
			published = published || record.Status == "published"
		}
		if !published {
			return false
		}
		response := httptest.NewRecorder()
		next.ServeHTTP(response, r)
		if response.Code != http.StatusOK {
			t.Errorf("result before dropped response: %d %s", response.Code, response.Body.String())
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	}
	if _, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection)); err == nil {
		t.Fatal("published result response was not lost")
	}
	hide.Store(false)
	data := f.srv.data
	f.http.Close()
	if err := f.srv.Close(); err != nil {
		t.Fatal(err)
	}
	srv, err := Open(Options{DataDir: data})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = srv
	f.http = httptest.NewServer(srv.Handler())
	cl, err := client.New(f.http.URL, "result-b")
	if err != nil {
		t.Fatal(err)
	}
	reopened := &remote.Session{Client: cl, Project: cl.Project(f.infoID), Info: f.b.Info, ServerURL: f.http.URL, Home: f.b.Home}
	ctx := issue.WithRemoteResult(t.Context(), reopened, t.TempDir())
	after := f.inspect(ctx, f.rootB)
	if len(after.Records) != 1 || f.provider.posts != 1 {
		t.Fatal("restart duplicated result publication")
	}
	for _, record := range after.Records {
		if record.Status != "published" || record.CommentID != 101 {
			t.Fatalf("lost result receipt: %+v", record)
		}
	}
	raw, err := reopened.Project.IssueIndex(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var index issue.Index
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	if index[f.sourceKey].ResultPending {
		t.Fatal("central result was ignored by issue index")
	}
}
func TestIssueResultRefusalAndReopenRequireNewConsentAcrossClients(t *testing.T) {
	f := newIssueResultFixture(t)
	inspection := f.inspect(f.ctxA, f.rootA)
	record, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection))
	if err != nil {
		t.Fatal(err)
	}
	inspection = f.inspect(f.ctxB, f.rootB)
	decision := issue.ResultDecision{Token: inspection.Token, Version: record.Version, Decision: "no", UserReference: "explicit refusal"}
	if _, err := issue.DecideResult(f.ctxB, f.provider, f.rootB, f.repo, 42, f.cardID, decision); err != nil {
		t.Fatal(err)
	}
	decision.Decision = "yes"
	if _, err := issue.DecideResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, decision); err == nil {
		t.Fatal("another client reused refusal as consent")
	}
	decision.Reconsider = true
	decision.UserReference = "explicit user reconsideration"
	if _, err := issue.DecideResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, decision); err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	f.provider.value.State = "open"
	f.provider.value.StateVersion = "42:reopened"
	f.provider.mu.Unlock()
	if _, err := issue.DecideResult(f.ctxB, f.provider, f.rootB, f.repo, 42, f.cardID, decision); err == nil {
		t.Fatal("old consent survived reopen")
	}
	if f.provider.closes != 1 {
		t.Fatal("reopened issue was closed using stale consent")
	}
}

func TestIssueResultDefiniteRejectionAllowsFreshAssessmentAfterInspection(t *testing.T) {
	f := newIssueResultFixture(t)
	inspection := f.inspect(f.ctxA, f.rootA)
	f.provider.postRejected = true
	if _, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection)); err == nil {
		t.Fatal("definite provider refusal disappeared")
	}
	f.provider.postRejected = false
	inspection = f.inspect(f.ctxB, f.rootB)
	for _, record := range inspection.Records {
		if record.Status != "rejected" || len(record.Failures) != 1 {
			t.Fatalf("definite refusal history: %+v", record)
		}
	}
	published, err := issue.ApplyResult(f.ctxB, f.provider, f.rootB, f.repo, 42, f.cardID, resultFixtureProposal(inspection))
	if err != nil || published.Status != "published" || len(published.Failures) != 1 || f.provider.posts != 2 || len(f.provider.value.Comments) != 1 {
		t.Fatalf("fresh assessed retry: %+v %v posts=%d", published, err, f.provider.posts)
	}
}

func TestIssueResultServerRejectsUnboundAssessmentBeforeProviderWrite(t *testing.T) {
	for _, field := range []string{"digest", "evidence", "unreserved-publication"} {
		t.Run(field, func(t *testing.T) {
			f := newIssueResultFixture(t)
			inspection := f.inspect(f.ctxA, f.rootA)
			f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
				if !strings.HasSuffix(r.URL.Path, "/issues/results/save") {
					return false
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return false
				}
				var input api.IssueResultWrite
				var store issue.ResultStore
				if json.Unmarshal(data, &input) != nil || json.Unmarshal(input.Store, &store) != nil {
					t.Fatal("invalid intercepted result body")
				}
				for _, record := range store.Records {
					if record.Status == "uncertain" {
						switch field {
						case "digest":
							record.CardDigest = "other-work"
						case "evidence":
							record.EvidenceVersion = "other-evidence"
						case "unreserved-publication":
							record.Status = "published"
							record.CommentID = 999
						}
					}
				}
				input.Store, err = json.Marshal(store)
				if err != nil {
					t.Fatal(err)
				}
				data, err = json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				r.Body = io.NopCloser(bytes.NewReader(data))
				r.ContentLength = int64(len(data))
				return false
			}
			if _, err := issue.ApplyResult(f.ctxA, f.provider, f.rootA, f.repo, 42, f.cardID, resultFixtureProposal(inspection)); !api.IsCode(err, api.CodeInvalidRequest) {
				t.Fatalf("unbound assessment: %v", err)
			}
			if f.provider.posts != 0 {
				t.Fatal("invalid assessment reached provider mutation")
			}
		})
	}
}
