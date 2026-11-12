package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/server"
)

// clock is a test clock the server consults for every lease decision.
type clock struct{ at atomic.Int64 }

func (c *clock) now() time.Time          { return time.Unix(0, c.at.Load()) }
func (c *clock) advance(d time.Duration) { c.at.Add(int64(d)) }

type fixture struct {
	t     *testing.T
	dir   string
	clock *clock
	srv   *server.Server
	http  *httptest.Server
}

func start(t *testing.T, dir string, c *clock) *fixture {
	t.Helper()
	s, err := server.Open(server.Options{
		DataDir: dir, TTL: 90 * time.Second, RenewInterval: 15 * time.Second, Now: c.now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	f := &fixture{t: t, dir: dir, clock: c, srv: s, http: h}
	t.Cleanup(f.stop)
	return f
}

func (f *fixture) stop() {
	if f.http != nil {
		f.http.Close()
		f.http = nil
	}
	if f.srv != nil {
		f.srv.Close()
		f.srv = nil
	}
}

func newFixture(t *testing.T) *fixture {
	c := &clock{}
	c.at.Store(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC).UnixNano())
	return start(t, t.TempDir(), c)
}

func (f *fixture) project(clientID string) *client.Project {
	f.t.Helper()
	c, err := client.New(f.http.URL, clientID)
	if err != nil {
		f.t.Fatal(err)
	}
	c.Backoff = time.Millisecond
	p, err := c.Register(f.t.Context(), "demo", "git@example.invalid:demo.git")
	if err != nil {
		f.t.Fatal(err)
	}
	return c.Project(p.ID)
}

// todoTask creates a task with a complete contract and moves it to todo.
func todoTask(t *testing.T, p *client.Project, slug string, group ...string) api.Task {
	t.Helper()
	ctx := t.Context()
	task, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: slug, Title: "Demo " + slug, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	if len(group) != 0 {
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group[0], 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nCARD_REVIEW: PASS\n", 1)
	}
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if task, err = p.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if task, err = p.Move(ctx, "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	return task
}

func claim(t *testing.T, p *client.Project, id, client string) (api.ClaimGrant, error) {
	return p.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: client, ExecutionID: "exec-" + client, Owner: "claude"})
}

func expectCode(t *testing.T, err error, code string) {
	t.Helper()
	if !api.IsCode(err, code) {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestProjectIdentityIsByNameAndRemote(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	if a.ID() != b.ID() {
		t.Fatalf("same project name resolved to %s and %s", a.ID(), b.ID())
	}
	c, _ := client.New(f.http.URL, "host-c")
	_, err := c.Register(t.Context(), "demo", "git@example.invalid:other.git")
	expectCode(t, err, api.CodeProjectMismatch)
}

func TestConcurrentClaimHasOneWinner(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "race")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, p := range []*client.Project{a, b} {
		wg.Go(func() {
			_, errs[i] = claim(t, p, task.TaskID, []string{"host-a", "host-b"}[i])
		})
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			expectCode(t, err, api.CodeHeldByOther)
		}
	}
	if won != 1 {
		t.Fatalf("want exactly one claim, got %d (%v)", won, errs)
	}
	for _, p := range []*client.Project{a, b} {
		list, err := p.List(t.Context(), "")
		if err != nil || len(list.Tasks) != 1 || list.Tasks[0].Claim == nil {
			t.Fatalf("both clients must see the claimed task: %+v %v", list, err)
		}
	}
}

func TestOnlyTheHolderWritesAndRenewalKeepsRevision(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "holder")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	own := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	edit := api.Document{Path: "spec.md", Text: grant.Task.Text + "\n", ExpectedRevision: grant.Task.Revision}
	_, err = b.PutDocument(t.Context(), "", task.TaskID, edit, nil)
	expectCode(t, err, api.CodeHeldByOther)
	_, err = b.PutDocument(t.Context(), "", task.TaskID, edit, &client.Claim{Token: "forged", Generation: 1})
	expectCode(t, err, api.CodeHeldByOther)
	_, err = b.Move(t.Context(), "", task.TaskID, api.Move{State: "done", Result: "completed"}, nil)
	expectCode(t, err, api.CodeHeldByOther)

	f.clock.advance(60 * time.Second)
	if _, err := a.Renew(t.Context(), task.TaskID, own); err != nil {
		t.Fatal(err)
	}
	after, err := a.Show(t.Context(), task.TaskID)
	if err != nil || after.Revision != grant.Task.Revision {
		t.Fatalf("renewal changed the revision: %d -> %d (%v)", grant.Task.Revision, after.Revision, err)
	}
	if _, err := a.PutDocument(t.Context(), "", task.TaskID, edit, &own); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredLeaseNeedsExplicitTakeoverAndFencesTheOldExecution(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "fence")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	old := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	request, err := b.CreateRequest(t.Context(), "", task.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix it", TargetGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}

	takeover := api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-b2", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: 1}
	_, err = b.Takeover(t.Context(), "", task.TaskID, takeover)
	expectCode(t, err, api.CodeLeaseActive)

	f.clock.advance(91 * time.Second)
	shown, _ := b.Show(t.Context(), task.TaskID)
	if shown.Claim == nil || shown.Claim.Status != api.ClaimLost || shown.State != "working" {
		t.Fatalf("an expired lease must read as lost and keep the task: %+v", shown.Claim)
	}
	_, err = a.Renew(t.Context(), task.TaskID, old)
	expectCode(t, err, api.CodeLeaseInvalid)

	next, err := b.Takeover(t.Context(), "", task.TaskID, takeover)
	if err != nil {
		t.Fatal(err)
	}
	if next.Claim.Generation != 2 || next.Token == "" {
		t.Fatalf("takeover must grant generation 2: %+v", next.Claim)
	}
	if !strings.Contains(next.Task.Text, "- OWNER: codex") {
		t.Fatalf("the takeover must hand the card's execution to the new owner:\n%s", next.Task.Text)
	}
	// Every late write of the old execution is refused.
	late := api.Document{Path: "report.md", Text: "late", ExpectedRevision: next.Task.Revision}
	_, err = a.PutDocument(t.Context(), "", task.TaskID, late, &old)
	expectCode(t, err, api.CodeHeldByOther)
	_, err = a.Move(t.Context(), "", task.TaskID, api.Move{State: "done", Result: "completed"}, &old)
	expectCode(t, err, api.CodeHeldByOther)
	_, err = a.Renew(t.Context(), task.TaskID, old)
	expectCode(t, err, api.CodeHeldByOther)
	// A token from the old generation with the new generation number is still refused.
	_, err = a.Renew(t.Context(), task.TaskID, client.Claim{Token: old.Token, Generation: 2})
	expectCode(t, err, api.CodeHeldByOther)

	requests, err := b.Requests(t.Context(), task.TaskID)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID || requests[0].Status != api.RequestSuperseded {
		t.Fatalf("the old generation's request must be superseded: %+v %v", requests, err)
	}
	newClaim := client.Claim{Token: next.Token, Generation: 2}
	_, err = b.AnswerRequest(t.Context(), "", task.TaskID, request.ID, api.CollabAnswer{Status: api.RequestDone}, newClaim)
	expectCode(t, err, api.CodeLeaseInvalid)
	if _, err := b.PutDocument(t.Context(), "", task.TaskID, late, &newClaim); err != nil {
		t.Fatal(err)
	}
}

func TestRequestsAreAnsweredByTheHolder(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "collab")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.CreateRequest(t.Context(), "", task.TaskID, api.NewCollabRequest{Kind: api.RequestSync, Message: "sync", TargetGeneration: 7})
	expectCode(t, err, api.CodeLeaseInvalid)
	r, err := b.CreateRequest(t.Context(), "", task.TaskID, api.NewCollabRequest{Kind: api.RequestSync, Message: "sync", TargetGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.AnswerRequest(t.Context(), "", task.TaskID, r.ID, api.CollabAnswer{Status: api.RequestDone}, client.Claim{})
	expectCode(t, err, api.CodeHeldByOther)
	answered, err := a.AnswerRequest(t.Context(), "", task.TaskID, r.ID, api.CollabAnswer{Status: api.RequestDone, Note: "synced"}, client.Claim{Token: grant.Token, Generation: 1})
	if err != nil || answered.Status != api.RequestDone {
		t.Fatalf("holder answer: %+v %v", answered, err)
	}
}

func TestIdempotentRetriesReturnTheFirstResult(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	ctx := t.Context()
	req := api.NewTask{Type: "feature", Slug: "once", Title: "Once", Language: "en"}
	first, err := a.NewTask(ctx, "key-new-1", req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.NewTask(ctx, "key-new-1", req)
	if err != nil || second.TaskID != first.TaskID || second.Revision != first.Revision {
		t.Fatalf("retry must return the first task: %+v %v", second, err)
	}
	req.Title = "Other"
	_, err = a.NewTask(ctx, "key-new-1", req)
	expectCode(t, err, api.CodeIdempotencyMismatch)
	list, _ := a.List(ctx, "")
	if len(list.Tasks) != 1 {
		t.Fatalf("retries created %d tasks", len(list.Tasks))
	}

	task := todoTask(t, a, "grant")
	g1, err := a.Claim(ctx, "key-claim", task.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-1", Owner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := a.Claim(ctx, "key-claim", task.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-1", Owner: "claude"})
	if err != nil || g2.Token != g1.Token || g2.Claim.Generation != g1.Claim.Generation {
		t.Fatalf("a replayed claim must return the same grant: %+v %v", g2.Claim, err)
	}
}

func TestEventsResumeFromTheListCursor(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	list, err := b.List(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan api.EventPage, 1)
	go func() {
		page, _ := b.Events(context.Background(), list.Cursor, 5*time.Second)
		done <- page
	}()
	time.Sleep(50 * time.Millisecond)
	task, err := a.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "evt", Title: "Evt", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	page := <-done
	if page.Reset || len(page.Events) == 0 || page.Events[0].TaskID != task.TaskID {
		t.Fatalf("long poll must deliver the new task: %+v", page)
	}
	stale, err := b.Events(t.Context(), "nope:1", 0)
	if err != nil || !stale.Reset {
		t.Fatalf("an unknown cursor must reset: %+v %v", stale, err)
	}
}

func TestRestartKeepsStateAndRefusesASecondInstance(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	task := todoTask(t, a, "restart")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Open(server.Options{DataDir: f.dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}); err == nil {
		t.Fatal("a second instance on one data directory must be refused")
	}
	f.stop()
	g := start(t, f.dir, f.clock)
	c, _ := client.New(g.http.URL, "host-a")
	p := c.Project(a.ID())
	shown, err := p.Show(t.Context(), task.TaskID)
	if err != nil || shown.State != "working" || shown.Claim == nil || shown.Claim.Generation != 1 {
		t.Fatalf("claim lost across restart: %+v %v", shown, err)
	}
	if _, err := p.Renew(t.Context(), task.TaskID, client.Claim{Token: grant.Token, Generation: 1}); err != nil {
		t.Fatalf("the holder keeps its lease across restart: %v", err)
	}
}

func TestProtocolMismatchIsRefused(t *testing.T) {
	f := newFixture(t)
	req, _ := http.NewRequest(http.MethodGet, f.http.URL+"/api/v1/projects?name=demo", nil)
	req.Header.Set(api.HeaderProtocol, "99")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("want 412, got %d", resp.StatusCode)
	}
}

func TestPlainMoveIntoWorkingIsAClaim(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	task := todoTask(t, a, "plain")
	_, err := a.Move(t.Context(), "", task.TaskID, api.Move{State: "working"}, nil)
	expectCode(t, err, api.CodeClaimRequired)
}

func TestDocumentDigestMismatchPublishesNothing(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	task, err := a.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "digest", Title: "Digest", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"path":"report.md","text":"partial","sha256":"00","expected_revision":` + strconv.FormatUint(task.Revision, 10) + `}`
	req, _ := http.NewRequest(http.MethodPut, f.http.URL+"/api/v1/projects/"+a.ID()+"/tasks/"+task.TaskID+"/documents/report.md", strings.NewReader(body))
	req.Header.Set(api.HeaderProtocol, "1")
	req.Header.Set(api.HeaderIdempotencyKey, "key-digest")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", resp.StatusCode)
	}
	_, err = a.Document(t.Context(), task.TaskID, "report.md")
	expectCode(t, err, api.CodeNotFound)
}

func startClaim(t *testing.T, p *client.Project, id string) (api.ClaimGrant, client.Claim) {
	t.Helper()
	g, err := p.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-start", Owner: "claude", Start: &api.StartInput{Session: "claude pending", Window: ""}})
	if err != nil {
		t.Fatal(err)
	}
	return g, client.Claim{Token: g.Token, Generation: g.Claim.Generation}
}

func TestLauncherStartRecordsWindowAndConfirms(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	task := todoTask(t, a, "launch")
	g, own := startClaim(t, a, task.TaskID)
	if !strings.Contains(g.Task.Text, "- SESSION: claude pending") {
		t.Fatalf("the start claim records the session:\n%s", g.Task.Text)
	}
	shown, err := a.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionWindow, Window: "tmux:s1:w1"}, own)
	if err != nil || !strings.Contains(shown.Text, "- WINDOW: tmux:s1:w1") {
		t.Fatalf("window: %v", err)
	}
	if _, err := a.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, own); err != nil {
		t.Fatal(err)
	}
	_, err = a.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, own)
	expectCode(t, err, api.CodeInvalidRequest)
	b := f.project("host-b")
	_, err = b.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionWindow, Window: "x"}, client.Claim{})
	expectCode(t, err, api.CodeHeldByOther)
}

func TestFailedLauncherStartRollsBackToTodo(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	task := todoTask(t, a, "rollback")
	_, own := startClaim(t, a, task.TaskID)
	shown, err := a.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, own)
	if err != nil || shown.State != "todo" || shown.Claim == nil || shown.Claim.Status != api.ClaimReleased {
		t.Fatalf("rollback must return the card to todo and release the claim: %+v %v", shown.Claim, err)
	}
	if strings.Contains(shown.Text, "claude pending") {
		t.Fatal("rollback restores the card text")
	}
	_, err = a.Renew(t.Context(), task.TaskID, own)
	expectCode(t, err, api.CodeLeaseInvalid)
	if g, err := claim(t, a, task.TaskID, "host-b"); err != nil || g.Claim.Generation != 2 {
		t.Fatalf("the task is claimable again with the next generation: %v", err)
	}
}

// The N/A review gate runs over the server: only the holder publishes the
// plan and the closure, and done releases the claim.
func TestReviewGateAndCompletionOverTheServer(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "gate")
	g, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	own := &client.Claim{Token: g.Token, Generation: g.Claim.Generation}
	cwd := t.TempDir()
	plan := map[string]any{
		"schema": 1, "sealed": true, "plan_id": "plan-1", "author": "claude", "basis": "test", "cwd": cwd, "report_language": "en",
		"task_ids": []string{task.TaskID},
		"batches": []map[string]any{{"batch_id": "batch-1", "task_ids": []string{task.TaskID}, "base": "N/A", "target_commit": "N/A",
			"requirements": map[string]string{"PMQA": "N/A: test", "Security": "N/A: test"}}},
	}
	_, err = b.ReviewPlan(t.Context(), "", plan, nil)
	expectCode(t, err, api.CodeHeldByOther)
	if _, err := a.ReviewPlan(t.Context(), "", plan, own); err != nil {
		t.Fatal(err)
	}
	view, err := a.ReviewAggregate(t.Context(), "", "batch-1", own)
	if err != nil {
		t.Fatal(err)
	}
	var v board.ReviewBatchView
	if err := json.Unmarshal(view, &v); err != nil {
		t.Fatal(err)
	}
	report := api.Document{Path: "report.md", Text: "done", ExpectedRevision: g.Task.Revision}
	if shown, err := a.Show(t.Context(), task.TaskID); err == nil {
		report.ExpectedRevision = shown.Revision
	}
	if _, err := a.PutDocument(t.Context(), "", task.TaskID, report, own); err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: "batch-1", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "claude",
		Roles: map[string]board.ReviewRoleConclusion{}, ResolvedFailures: map[string]string{}, Opinions: []board.ReviewOpinion{}}
	edges, _, err := board.ReviewClosureEdges(v, request)
	if err != nil {
		t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: cwd, Head: "N/A", NotApplicable: "test", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	if _, err := a.ReviewClose(t.Context(), "", map[string]any{"request": request, "evidence": evidence}, own); err != nil {
		t.Fatal(err)
	}
	done, err := a.Move(t.Context(), "", task.TaskID, api.Move{State: "done", Result: "completed"}, own)
	if err != nil || done.State != "done" || done.Claim.Status != api.ClaimReleased {
		t.Fatalf("done must release the claim: %+v %v", done.Claim, err)
	}
}
