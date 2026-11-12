package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/remote"
)

type issueResultProvider struct {
	repository                issue.Repository
	mu                        sync.Mutex
	value                     issue.ResultRemote
	posts, closes             int
	postUnknown, closeUnknown bool
	postRejected              bool
}

func (p *issueResultProvider) ReadResult(context.Context, issue.Repository, int) (issue.ResultRemote, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value := p.value
	value.Comments = slices.Clone(value.Comments)
	return value, nil
}
func (p *issueResultProvider) PostResult(_ context.Context, _ issue.Repository, _ int, body string) (issue.ResultComment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.posts++
	if p.postRejected {
		return issue.ResultComment{}, &issue.ResultWriteRejection{Err: errors.New("definite provider refusal without an effect")}
	}
	comment := issue.ResultComment{ID: int64(100 + p.posts), AuthorID: p.value.ActorID, Body: body}
	p.value.Comments = append(p.value.Comments, comment)
	if p.postUnknown {
		return comment, errors.New("simulated lost provider response")
	}
	return comment, nil
}
func (p *issueResultProvider) CloseResult(context.Context, issue.Repository, int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	p.value.State, p.value.StateVersion = "closed", "42:closed"
	if p.closeUnknown {
		return errors.New("simulated lost close response")
	}
	return nil
}

type issueResultFixture struct {
	t                         *testing.T
	srv                       *Server
	http                      *httptest.Server
	infoID, cardID, sourceKey string
	repo                      issue.Repository
	provider                  *issueResultProvider
	a, b                      *remote.Session
	rootA, rootB              string
	ctxA, ctxB                context.Context
	intercept                 func(http.ResponseWriter, *http.Request, http.Handler) bool
}

func newIssueResultFixture(t *testing.T, legacy ...bool) *issueResultFixture {
	t.Helper()
	f := &issueResultFixture{t: t}
	f.repo = issue.Repository{Host: "github.com", Owner: "test", Name: "repo", URL: "https://github.com/test/repo"}
	source := filepath.Join(t.TempDir(), "old-board")
	if err := boardInitIssueResultSource(t, source, f); err != nil {
		t.Fatal(err)
	}
	f.provider = &issueResultProvider{repository: f.repo, value: issue.ResultRemote{SourceKey: f.sourceKey, IssueID: 42, ActorID: 7, State: "open", StateVersion: "42:initial", Revision: "first", Comments: []issue.ResultComment{}}}
	if len(legacy) > 0 && legacy[0] {
		inspection, err := issue.InspectResult(t.Context(), f.provider, source, f.repo, 42, f.cardID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := issue.ApplyResult(t.Context(), f.provider, source, f.repo, 42, f.cardID, resultFixtureProposal(inspection)); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := Open(Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = srv
	imported, err := srv.Import("issue-results", "", source)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	f.infoID = imported.Project.ID
	handler := srv.Handler()
	f.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.intercept != nil && f.intercept(w, r, handler) {
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { f.http.Close(); _ = f.srv.Close() })
	ca, _ := client.New(f.http.URL, "result-a")
	cb, _ := client.New(f.http.URL, "result-b")
	ca.Retries, cb.Retries = 0, 0
	f.a = &remote.Session{Client: ca, Project: ca.Project(f.infoID), Info: imported.Project, ServerURL: f.http.URL, Home: filepath.Join(t.TempDir(), "client-a")}
	f.b = &remote.Session{Client: cb, Project: cb.Project(f.infoID), Info: imported.Project, ServerURL: f.http.URL, Home: filepath.Join(t.TempDir(), "client-b")}
	for _, s := range []*remote.Session{f.a, f.b} {
		if err := os.MkdirAll(s.Home, 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.rootA, err = f.a.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	f.rootB, err = f.b.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	f.ctxA = issue.WithRemoteResult(t.Context(), f.a, t.TempDir())
	f.ctxB = issue.WithRemoteResult(t.Context(), f.b, t.TempDir())
	return f
}
func boardInitIssueResultSource(t *testing.T, root string, f *issueResultFixture) error {
	if err := board.InitStorage(root); err != nil {
		return err
	}
	snapshot, err := issue.BuildImportSnapshot(issue.IssueSnapshot{Repository: f.repo, Number: 42, Title: "Verify distributed result publication", State: "open", CreatedAt: time.Now(), UpdatedAt: time.Now(), FetchedAt: time.Now()})
	if err != nil {
		return err
	}
	imported, err := issue.PublishImport(root, snapshot, issue.ImportOptions{Language: "en"})
	if err != nil {
		return err
	}
	f.cardID, f.sourceKey = imported.TaskID, imported.SourceKey
	// A stopped historical v1 board supplies completed evidence. Migration and
	// all result operations themselves still use their real production entrances.
	destination := filepath.Join(root, "done", f.cardID)
	if err := os.Rename(imported.Path, destination); err != nil {
		return err
	}
	path := filepath.Join(destination, "spec.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(data), "<FILL_IN>", "filled")
	text = strings.Replace(text, "## SUMMARY", "## SUMMARY\n\nThe entire issue is resolved. Verified: go test ./...\nDelivery: "+strings.Repeat("a", 40)+"\n", 1)
	text = strings.Replace(text, "- RESULT:", "- RESULT: completed", 1)
	return os.WriteFile(path, []byte(text), 0600)
}
func (f *issueResultFixture) inspect(ctx context.Context, root string) issue.ResultInspection {
	f.t.Helper()
	value, err := issue.InspectResult(ctx, f.provider, root, f.repo, 42, f.cardID)
	if err != nil {
		f.t.Fatal(err)
	}
	return value
}
func resultFixtureProposal(inspection issue.ResultInspection) issue.ResultProposal {
	count := 0
	for _, card := range inspection.Cards {
		section, _ := board.SectionBody(card.Spec, "ACCEPTANCE_CRITERIA")
		for line := range strings.SplitSeq(section, "\n") {
			if strings.HasPrefix(line, "- [ ] ") || strings.HasPrefix(line, "- [x] ") {
				count++
			}
		}
	}
	outcomes := make([]string, count)
	for i := range outcomes {
		outcomes[i] = "met"
	}
	return issue.ResultProposal{Token: inspection.Token, Outcomes: outcomes, Checks: []issue.ResultCheck{{Command: "go test ./...", Status: "pass"}}, Body: "The issue is resolved. The recorded verification passed.", FullyResolved: true, ResolutionEvidence: "The entire issue is resolved."}
}

func (p *issueResultProvider) ResolveRepository(context.Context, string, string) (issue.Repository, error) {
	return p.repository, nil
}
func (p *issueResultProvider) ListIssues(context.Context, issue.Repository, issue.IssueQuery) (issue.IssuePage, error) {
	return issue.IssuePage{}, errors.New("unexpected list")
}
func (p *issueResultProvider) GetIssue(context.Context, issue.Repository, int, bool) (issue.IssueSnapshot, error) {
	return issue.IssueSnapshot{}, errors.New("unexpected issue read")
}
