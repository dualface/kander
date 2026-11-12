package remote_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

type integrationFixture struct {
	t                                       *testing.T
	root, bare, first, second, base, commit string
	a, b                                    *remote.Session
	task                                    api.Task
	credential                              remote.Credential
	intercept                               func(http.ResponseWriter, *http.Request, http.Handler) bool
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	f := newIntegrationGitFixture(t)
	f.reviewedTask()
	return f
}
func newIntegrationGitFixture(t *testing.T) *integrationFixture {
	t.Helper()
	t.Setenv(remote.EnvExecutionID, "")
	f := &integrationFixture{t: t, root: t.TempDir()}
	f.bare, f.first, f.second = filepath.Join(f.root, "common.git"), filepath.Join(f.root, "first"), filepath.Join(f.root, "second")
	deliveryGit(t, f.root, "init", "--bare", f.bare)
	deliveryGit(t, f.root, "clone", f.bare, f.first)
	if err := os.WriteFile(filepath.Join(f.first, "work.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.first, "add", "work.txt")
	deliveryGit(t, f.first, "commit", "-m", "Initial work")
	f.base = deliveryGit(t, f.first, "rev-parse", "HEAD")
	deliveryGit(t, f.first, "push", "origin", "HEAD:refs/heads/v2")
	deliveryGit(t, f.root, "clone", "--branch", "v2", f.bare, f.second)
	if err := os.WriteFile(filepath.Join(f.first, "work.txt"), []byte("reviewed delivery\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.first, "commit", "-am", "Deliver reviewed work")
	f.commit = deliveryGit(t, f.first, "rev-parse", "HEAD")
	srv, err := server.Open(server.Options{DataDir: filepath.Join(f.root, "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.intercept != nil && f.intercept(w, r, handler) {
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { hs.Close(); _ = srv.Close() })
	ca, _ := client.New(hs.URL, "host-a")
	cb, _ := client.New(hs.URL, "host-b")
	ca.Retries, cb.Retries = 0, 0
	info, err := ca.Register(t.Context(), "integration-clones", f.bare)
	if err != nil {
		t.Fatal(err)
	}
	f.a = &remote.Session{Client: ca, Project: ca.Project(info.ID), Info: info, ServerURL: hs.URL, Home: filepath.Join(f.root, "home-a"), WorkerStarter: func(string) error { return nil }}
	f.b = &remote.Session{Client: cb, Project: cb.Project(info.ID), Info: info, ServerURL: hs.URL, Home: filepath.Join(f.root, "home-b"), WorkerStarter: func(string) error { return nil }}
	for _, s := range []*remote.Session{f.a, f.b} {
		if err := os.MkdirAll(s.Home, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func (f *integrationFixture) reviewedTask() {
	t, s := f.t, f.a
	task, err := s.Project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "integration-delivery", Title: "Integrate a reviewed delivery", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.ReplaceAll(text, "- [ ]", "- [x]")
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: task/integration-delivery", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	task, err = s.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.Project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "task-integration-owner", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	f.credential = remote.Credential{ServerURL: s.ServerURL, ClientID: "host-a", ProjectID: s.Info.ID, TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
	if err := s.SaveCredential(f.credential); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishCommit(t.Context(), f.first, task.TaskID, f.commit, f.credential); err != nil {
		t.Fatal(err)
	}
	own := f.credential.Claim()
	task, err = s.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "review", DeliveryCommit: f.commit}, own)
	if err != nil {
		t.Fatal(err)
	}
	plan := board.ReviewPlan{Schema: 1, PlanID: "integration-plan", Sealed: true, Author: "coordinator", Basis: "Git integration fixture", CWD: "project:" + s.Info.ID, ReportLanguage: "en", TaskIDs: []string{task.TaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "integration-batch", TaskIDs: []string{task.TaskID}, Base: f.base, TargetCommit: f.commit, Requirements: map[string]string{"PMQA": "N/A: Git integration fixture", "Security": "N/A: repository policy"}}}}
	if _, err := s.Project.ReviewPlan(t.Context(), "", plan, own); err != nil {
		t.Fatal(err)
	}
	raw, err := s.Project.ReviewBatchView(t.Context(), "integration-batch")
	if err != nil {
		t.Fatal(err)
	}
	var view board.ReviewBatchView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: view.Batch.BatchID, ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"request": request, "evidence": board.ReviewGitEvidence{CWD: plan.CWD, Head: f.commit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}}
	if _, err := s.Project.ReviewClose(t.Context(), "", body, own); err != nil {
		t.Fatal(err)
	}
	f.task, err = s.Project.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
}
func (f *integrationFixture) options(id string) remote.IntegrationOptions {
	return remote.IntegrationOptions{OperationID: id, Purpose: "deliver", TargetRef: "refs/heads/v2", Commit: f.commit, TaskID: f.task.TaskID, Decision: "explicitly integrate the closed reviewed delivery into v2"}
}
