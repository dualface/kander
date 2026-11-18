//go:build unix

package review

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

// remoteWorld is a server project with one claimed card and a client checkout.
// The evidence commands of the tests run through the same entry as the CLI, so
// the Git checks run in h.repo and the server only revalidates the board side.
type remoteWorld struct {
	h       *reviewHarness
	session *remote.Session
	task    string
}

func newRemoteWorld(t *testing.T) *remoteWorld {
	t.Helper()
	h := newCodexHarness(t)
	t.Chdir(h.repo)
	useServer(t)
	t.Setenv(board.EnvBoardDir, filepath.Join(h.root, "forbidden-board"))
	t.Setenv(remote.EnvClientHome, filepath.Join(h.root, "client"))
	srv, err := server.Open(server.Options{DataDir: filepath.Join(h.root, "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	var cfg map[string]any
	configPath := os.Getenv(config.EnvConfig)
	if err := json.Unmarshal([]byte(readFile(t, configPath)), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["server"] = map[string]string{"url": httpServer.URL}
	cfg["project"] = map[string]string{"name": "no-delivery"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	effective, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), effective, h.repo)
	if err != nil {
		t.Fatal(err)
	}
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "no-delivery", Title: "No delivery", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if _, err := session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := session.Project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "no-delivery-execution", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: session.ServerURL, ClientID: session.Client.ClientID(), ProjectID: session.Info.ID, TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, RenewedAt: grant.Claim.RenewedAt, TTL: grant.TTL, RenewInterval: grant.RenewInterval}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	return &remoteWorld{h: h, session: session, task: task.TaskID}
}

func (w *remoteWorld) batch(id, previous, base, target string) board.ReviewPlanBatch {
	return board.ReviewPlanBatch{BatchID: id, PreviousBatchID: previous, TaskIDs: []string{w.task}, Base: base, TargetCommit: target, Requirements: noDeliveryNA}
}

func (w *remoteWorld) plan(t *testing.T, sealed bool, batches ...board.ReviewPlanBatch) {
	t.Helper()
	p := board.ReviewPlan{Schema: 1, Sealed: sealed, PlanID: "plan", Author: "owner", Basis: "the card has no delivery commit", CWD: w.h.repo, ReportLanguage: "en", TaskIDs: []string{w.task}, Batches: batches}
	commandOK(t, "plan", w.h.repo, dispositionJSON(t, w.h, "plan", p))
}

func (w *remoteWorld) view(t *testing.T, batch string) board.ReviewBatchView {
	t.Helper()
	v, err := remoteBatchView(t.Context(), w.session, batch)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// closeRequest aggregates the batch through the service and writes the close
// request that binds the aggregated view.
func (w *remoteWorld) closeRequest(t *testing.T, batch string) (board.ReviewCloseRequest, string) {
	t.Helper()
	commandOK(t, "aggregate", w.h.repo, batch)
	v := w.view(t, batch)
	r := board.ReviewCloseRequest{BatchID: batch, ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "owner", Roles: map[string]board.ReviewRoleConclusion{}}
	return r, dispositionJSON(t, w.h, "close-"+batch, r)
}

func (w *remoteWorld) progress(t *testing.T) board.ReviewProgress {
	t.Helper()
	raw, err := w.session.Project.ReviewProgress(t.Context(), w.task)
	if err != nil {
		t.Fatal(err)
	}
	var progress board.ReviewProgress
	if err := json.Unmarshal(raw, &progress); err != nil {
		t.Fatal(err)
	}
	return progress
}

func (w *remoteWorld) resource(t *testing.T, kind, id string, value any) {
	t.Helper()
	if err := readRemoteResource(t.Context(), w.session, kind, id, value); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteNoDeliveryBatchClosesAfterHeadMovedOnAndWorktreeIsDirty(t *testing.T) {
	w := newRemoteWorld(t)
	w.plan(t, true, w.batch("batch", "", w.h.head, w.h.head))
	later := commitFile(t, w.h.repo, "later.txt", "another card's delivery\n", "later delivery")
	if err := os.WriteFile(filepath.Join(w.h.repo, "uncommitted"), []byte("user change"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, file := w.closeRequest(t, "batch")
	c := closureOf(t, commandOK(t, "close", w.h.repo, file))
	if !c.Git.NoDelivery || c.Git.ObservedHead != later || c.Git.Head != w.h.head || c.TargetCommit != w.h.head {
		t.Fatalf("closure evidence: %+v", c.Git)
	}
	if !strings.HasPrefix(c.Git.CWD, "project:") {
		t.Fatalf("the server stored the client path: %q", c.Git.CWD)
	}
	if got := w.progress(t); got.Status != "closed" {
		t.Fatalf("progress: %+v", got)
	}
	// With --json the command prints the same object on one line, and nothing on stderr.
	code, out, stderr := captureRun(t, []string{"progress", w.h.repo, w.task, "--json"})
	var printed board.ReviewProgress
	if err := json.Unmarshal([]byte(out), &printed); code != 0 || stderr != "" || err != nil || printed.Status != "closed" || strings.Count(out, "\n") != 1 {
		t.Fatalf("progress --json: %d %q %q %v", code, out, stderr, err)
	}
	// A refused request is the error object, with the status of the text form.
	code, out, stderr = captureRun(t, []string{"progress", w.h.repo, "20260101-no-such-task", "--json"})
	if code == 0 || out != "" || !strings.Contains(stderr, `{"error":{"code":`) || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("missing card --json: %d %q %q", code, out, stderr)
	}
}

func TestRemoteNoDeliveryBatchRefusesATargetOutsideHeadHistory(t *testing.T) {
	w := newRemoteWorld(t)
	side := commitFile(t, w.h.repo, "side.txt", "side\n", "side")
	w.plan(t, true, w.batch("batch", "", side, side))
	_, file := w.closeRequest(t, "batch")
	gitRepo(t, w.h.repo, "reset", "-q", "--hard", w.h.base)
	code, _, stderr := captureRun(t, []string{"close", w.h.repo, file})
	if code == 0 || !strings.Contains(stderr, side) || !strings.Contains(stderr, w.h.base) {
		t.Fatalf("accepted: %d %s", code, stderr)
	}
	if got := w.progress(t); got.Status == "closed" {
		t.Fatalf("the refused close left the batch closed: %+v", got)
	}
	gitRepo(t, w.h.repo, "reset", "-q", "--hard", side)
	commandOK(t, "close", w.h.repo, file)
}

func TestRemoteBatchWithADeliveryKeepsTheStrictClosureChecks(t *testing.T) {
	w := newRemoteWorld(t)
	w.plan(t, true, w.batch("batch", "", w.h.base, w.h.head))
	_, file := w.closeRequest(t, "batch")
	commitFile(t, w.h.repo, "later.txt", "later\n", "later")
	code, _, stderr := captureRun(t, []string{"close", w.h.repo, file})
	if code == 0 || !strings.Contains(stderr, "clean worktree") {
		t.Fatalf("moved HEAD accepted: %d %s", code, stderr)
	}
	gitRepo(t, w.h.repo, "reset", "-q", "--hard", w.h.head)
	c := closureOf(t, commandOK(t, "close", w.h.repo, file))
	if c.Git.NoDelivery || c.Git.ObservedHead != "" {
		t.Fatalf("a batch with a delivery carries the marker: %+v", c.Git)
	}
}

// TestServerRefusesANoDeliveryMarkerOnABatchWithADelivery sends the closure
// without the client's own checks: the request a hand-built client could send.
func TestServerRefusesANoDeliveryMarkerOnABatchWithADelivery(t *testing.T) {
	w := newRemoteWorld(t)
	w.plan(t, true, w.batch("batch", "", w.h.base, w.h.head))
	r, _ := w.closeRequest(t, "batch")
	v := w.view(t, "batch")
	edges, _, err := board.ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: w.h.repo, Head: w.h.head, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges, NoDelivery: true, ObservedHead: w.h.head}
	for name, change := range map[string]func(*board.ReviewGitEvidence){
		"marker on a batch with a delivery": func(*board.ReviewGitEvidence) {},
		"observed head without marker":      func(e *board.ReviewGitEvidence) { e.NoDelivery = false },
	} {
		forged := evidence
		change(&forged)
		payload := map[string]any{"request": r, "evidence": forged}
		proof, err := w.session.ReviewAuthority(t.Context(), "review-close:"+r.BatchID, payload, v.Batch.TaskIDs)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.session.Project.ReviewClose(t.Context(), client.NewKey(), payload, proof); err == nil {
			t.Fatalf("%s: the server accepted the closure", name)
		}
		if got := w.progress(t); got.Status == "closed" {
			t.Fatalf("%s: the refused closure was written: %+v", name, got)
		}
	}
	// The same batch closes through the client, which sends no marker.
	commandOK(t, "close", w.h.repo, dispositionJSON(t, w.h, "close", r))
}

func (w *remoteWorld) retarget(t *testing.T, batch string, r board.ReviewBatchRetarget) string {
	t.Helper()
	var plan board.ReviewPlan
	w.resource(t, "plans", "plan", &plan)
	x := board.ReviewPlanExtension{PlanID: "plan", ExpectedRevision: plan.Revision, Author: "owner", Basis: "the target was wrong", RetargetBatches: map[string]board.ReviewBatchRetarget{batch: r}}
	return dispositionJSON(t, w.h, "retarget-"+batch, x)
}

func TestRemoteRetargetCorrectsAnUnrunBatchThroughTheServer(t *testing.T) {
	w := newRemoteWorld(t)
	next := commitFile(t, w.h.repo, "next.txt", "next\n", "next")
	w.plan(t, true, w.batch("batch", "", w.h.base, w.h.base))
	commandOK(t, "aggregate", w.h.repo, "batch")
	var before board.ReviewPlan
	var batchBefore board.ReviewBatch
	w.resource(t, "plans", "plan", &before)
	w.resource(t, "batches", "batch", &batchBefore)
	viewBefore := w.view(t, "batch")

	file := w.retarget(t, "batch", board.ReviewBatchRetarget{PreviousBase: w.h.base, PreviousTarget: w.h.base, Base: w.h.head, Target: next})
	commandOK(t, "extend-plan", w.h.repo, file)

	var after board.ReviewPlan
	var batchAfter board.ReviewBatch
	w.resource(t, "plans", "plan", &after)
	w.resource(t, "batches", "batch", &batchAfter)
	if after.Revision != before.Revision+1 || batchAfter.Revision != batchBefore.Revision+1 || !after.Sealed {
		t.Fatalf("revisions: plan %d to %d, batch %d to %d, sealed %v", before.Revision, after.Revision, batchBefore.Revision, batchAfter.Revision, after.Sealed)
	}
	if got := after.Batches[0]; got.Base != w.h.head || got.TargetCommit != next || batchAfter.Base != w.h.head || batchAfter.TargetCommit != next {
		t.Fatalf("plan %+v, batch %+v", got, batchAfter)
	}
	// The aggregate view changed with the batch revision: the old hash closes nothing.
	viewAfter := w.view(t, "batch")
	if board.ReviewViewDigest(viewAfter) == board.ReviewViewDigest(viewBefore) || viewAfter.Batch.TargetCommit != next {
		t.Fatalf("the aggregate view kept the old target: %+v", viewAfter.Batch)
	}
	stale := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: viewBefore.Batch.Revision, ViewHash: board.ReviewViewDigest(viewBefore), Author: "owner", Roles: map[string]board.ReviewRoleConclusion{}}
	code, _, _ := captureRun(t, []string{"close", w.h.repo, dispositionJSON(t, w.h, "stale-close", stale)})
	if code == 0 {
		t.Fatal("the old view hash closed the batch")
	}
	_, fresh := w.closeRequest(t, "batch")
	commandOK(t, "close", w.h.repo, fresh)
}

func TestRemoteRetargetRefusalsWriteNothing(t *testing.T) {
	w := newRemoteWorld(t)
	next := commitFile(t, w.h.repo, "next.txt", "next\n", "next")
	w.plan(t, false, w.batch("one", "", w.h.base, w.h.head), w.batch("two", "one", w.h.head, next))
	var before board.ReviewPlan
	w.resource(t, "plans", "plan", &before)
	cases := map[string]struct {
		batch string
		r     board.ReviewBatchRetarget
	}{
		"stale previous values":        {"one", board.ReviewBatchRetarget{PreviousBase: w.h.head, PreviousTarget: w.h.head, Base: w.h.base, Target: w.h.head}},
		"base is not the predecessor":  {"two", board.ReviewBatchRetarget{PreviousBase: w.h.head, PreviousTarget: next, Base: w.h.base, Target: next}},
		"target with a successor":      {"one", board.ReviewBatchRetarget{PreviousBase: w.h.base, PreviousTarget: w.h.head, Base: w.h.base, Target: next}},
		"git ancestry of the new edge": {"two", board.ReviewBatchRetarget{PreviousBase: w.h.head, PreviousTarget: next, Base: w.h.head, Target: w.h.base}},
	}
	for name, c := range cases {
		code, _, _ := captureRun(t, []string{"extend-plan", w.h.repo, w.retarget(t, c.batch, c.r)})
		if code == 0 {
			t.Fatalf("%s: accepted", name)
		}
		var after board.ReviewPlan
		w.resource(t, "plans", "plan", &after)
		if after.Revision != before.Revision {
			t.Fatalf("%s: the refused request wrote the plan", name)
		}
	}
	// A closed batch cannot be corrected, though the request is well formed.
	gitRepo(t, w.h.repo, "reset", "-q", "--hard", w.h.head)
	_, file := w.closeRequest(t, "one")
	commandOK(t, "close", w.h.repo, file)
	code, _, _ := captureRun(t, []string{"extend-plan", w.h.repo, w.retarget(t, "one", board.ReviewBatchRetarget{PreviousBase: w.h.base, PreviousTarget: w.h.head, Base: w.h.head, Target: w.h.head})})
	if code == 0 {
		t.Fatal("a closed batch was retargeted")
	}
}
