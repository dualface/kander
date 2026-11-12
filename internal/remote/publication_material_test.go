package remote_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func publicationSessions(t *testing.T) (*remote.Session, *remote.Session, []string) {
	t.Helper()
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	a, _ := client.New(httpServer.URL, "host-a")
	b, _ := client.New(httpServer.URL, "host-b")
	info, err := a.Register(t.Context(), "joint-material", "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := []*remote.Session{{Client: a, Project: a.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: filepath.Join(t.TempDir(), "a")}, {Client: b, Project: b.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: filepath.Join(t.TempDir(), "b")}}
	var ids []string
	for i, s := range sessions {
		if err := os.MkdirAll(s.Home, 0o700); err != nil {
			t.Fatal(err)
		}
		task, err := s.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: []string{"material-one", "material-two"}[i], Title: "Verify joint material recovery", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: 20261112-material-group", 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nCARD_REVIEW: PASS\n", 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
		task, err = s.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
		if err != nil {
			t.Fatal(err)
		}
		task, err = s.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		grant, err := s.Project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: s.Client.ClientID(), ExecutionID: "execution-" + s.Client.ClientID(), Owner: "codex"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveCredential(remote.Credential{ServerURL: s.ServerURL, ClientID: s.Client.ClientID(), ProjectID: info.ID, TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.TaskID)
	}
	return sessions[0], sessions[1], ids
}

func pendingPublicationID(t *testing.T, err error) string {
	t.Helper()
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("want pending confirmation: %v", err)
	}
	id := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(err.Error())
	if id == "" {
		t.Fatalf("pending result omitted identity: %v", err)
	}
	return id
}

func TestJointReviewRetryFreezesVerificationTimeAndSendsConfirmedMaterial(t *testing.T) {
	t.Setenv(remote.EnvExecutionID, "")
	a, b, ids := publicationSessions(t)
	plan := board.ReviewPlan{Schema: 1, PlanID: "material-plan", Sealed: true, Author: "coordinator", Basis: "verify recovery", CWD: "/host-a/clone", ReportLanguage: "en", TaskIDs: ids, Batches: []board.ReviewPlanBatch{{BatchID: "material-batch", TaskIDs: ids, Base: "N/A", TargetCommit: "N/A", Requirements: map[string]string{"PMQA": "N/A: fixture", "Security": "N/A: repository policy"}}}}
	_, err := a.ReviewAuthority(t.Context(), "review-plan", plan, ids)
	id := pendingPublicationID(t, err)
	if _, err := b.ConfirmReviewPublication(t.Context(), id, ids[1]); err != nil {
		t.Fatal(err)
	}
	proof, err := a.ReviewAuthority(t.Context(), "review-plan", plan, ids)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Project.ReviewPlan(t.Context(), "publish-plan", plan, proof); err != nil {
		t.Fatal(err)
	}
	raw, err := a.Project.ReviewBatchView(t.Context(), "material-batch")
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
	evidence := board.ReviewGitEvidence{CWD: "/host-a/clone", Head: "N/A", NotApplicable: "review-plan: all roles explicitly N/A; no Git target", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	body := map[string]any{"request": request, "evidence": evidence}
	_, err = a.ReviewAuthority(t.Context(), "review-close:material-batch", body, ids)
	closeID := pendingPublicationID(t, err)
	if _, err := b.ConfirmReviewPublication(t.Context(), closeID, ids[1]); err != nil {
		t.Fatal(err)
	}
	// A new process rechecks the same Git facts at a different instant. The
	// payload sent to the producer must retain the one each holder confirmed.
	reopened := *a
	evidence.VerifiedAt = time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
	body["evidence"] = evidence
	proof, err = reopened.ReviewAuthority(t.Context(), "review-close:material-batch", body, ids)
	if err != nil || proof.PublicationID != closeID {
		t.Fatalf("retry created different material: %+v %v", proof, err)
	}
	if _, err := reopened.Project.ReviewClose(t.Context(), "close-confirmed-material", body, proof); err != nil {
		t.Fatal(err)
	}
	published, err := a.Project.Publication(t.Context(), closeID)
	if err != nil || published.Status != "published" {
		t.Fatalf("confirmed material was not committed: %+v %v", published, err)
	}
	// Publication itself advanced the card versions. A later member exclusion
	// changes the required holder set too. Neither fact can turn a read-only
	// replay of the already published operation into a fresh proposal.
	own, found, err := b.Credential(ids[1])
	if err != nil || !found {
		t.Fatalf("holder credential: %v", err)
	}
	if _, err := b.Project.Move(t.Context(), "later-exclusion", ids[1], api.Move{State: "trash", Result: "trashed", Reason: "later cancellation", Decision: "fixture decision"}, own.Claim()); err != nil {
		t.Fatal(err)
	}
	requestsBefore, err := a.Project.Requests(t.Context(), ids[1])
	if err != nil {
		t.Fatal(err)
	}
	replay, err := reopened.ReviewAuthority(t.Context(), "review-close:material-batch", body, ids)
	if err != nil || replay.PublicationID != closeID {
		t.Fatalf("published replay required fresh authority: %+v %v", replay, err)
	}
	if _, err := a.Project.ReviewClose(t.Context(), "published-material-replay", body, replay); err != nil {
		t.Fatal(err)
	}
	requestsAfter, err := a.Project.Requests(t.Context(), ids[1])
	if err != nil || len(requestsBefore) != len(requestsAfter) {
		t.Fatalf("published replay sent new approval requests: %+v %v", requestsAfter, err)
	}
}
