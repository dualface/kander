package remote_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

const singleBatch = "single-batch"

// writeRecord is the proof a write request carried to the server.
type writeRecord struct{ Method, Path, Claims, Publication string }

// writeLog records every request that is not a read, so a test sees which
// proof a command sent and whether a refused command sent anything at all.
type writeLog struct {
	mu      sync.Mutex
	records []writeRecord
}

func (l *writeLog) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			l.mu.Lock()
			l.records = append(l.records, writeRecord{r.Method, r.URL.Path, r.Header.Get(api.HeaderClaims), r.Header.Get(api.HeaderPublication)})
			l.mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

// take returns the writes since the previous call.
func (l *writeLog) take() []writeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	records := l.records
	l.records = nil
	return records
}

// singleMember is a one-card review batch whose holder runs on host a, and a
// second process on another host that coordinates the card's task group.
type singleMember struct {
	t                  *testing.T
	holder, coordinate *remote.Session
	log                *writeLog
	task               api.Task
	grant              api.ClaimGrant
	ids                []string
}

// newSingleMember creates a claimed card in cardGroup and a coordinator
// process of coordGroup. The coordinator of a group other than the card's
// supervises a second card, and an empty cardGroup leaves the card out of
// every group.
func newSingleMember(t *testing.T, cardGroup, coordGroup string) *singleMember {
	t.Helper()
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	log := &writeLog{}
	httpServer := httptest.NewServer(log.wrap(srv.Handler()))
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	a, _ := client.New(httpServer.URL, "host-a")
	b, _ := client.New(httpServer.URL, "host-b")
	info, err := a.Register(t.Context(), "single-member", "")
	if err != nil {
		t.Fatal(err)
	}
	open := func(c *client.Client, name string) *remote.Session {
		s := &remote.Session{Client: c, Project: c.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: filepath.Join(t.TempDir(), name)}
		if err := os.MkdirAll(s.Home, 0o700); err != nil {
			t.Fatal(err)
		}
		return s
	}
	f := &singleMember{t: t, holder: open(a, "holder"), coordinate: open(b, "coordinator"), log: log}
	card := f.todo("single-card", cardGroup)
	members := []string{card.TaskID}
	if coordGroup != "" {
		if coordGroup != cardGroup {
			members = []string{f.todo("other-card", coordGroup).TaskID}
		}
		request := board.CoordinatorClaim{GroupID: coordGroup, Owner: "coordinator", Token: "label", Basis: "single member fixture", Members: members}
		if _, err := f.coordinate.AcquireCoordinator(t.Context(), request, ""); err != nil {
			t.Fatal(err)
		}
	}
	f.grant, err = f.holder.Project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: a.ClientID(), ExecutionID: "execution-host-a", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	f.task, f.ids = f.grant.Task, []string{card.TaskID}
	credential := remote.Credential{ServerURL: httpServer.URL, ClientID: a.ClientID(), ProjectID: info.ID, TaskID: card.TaskID, ExecutionID: f.grant.Claim.ExecutionID, Generation: f.grant.Claim.Generation, Token: f.grant.Token}
	if err := f.holder.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	f.publish("review-plan", f.plan("single-plan"))
	f.log.take()
	return f
}

func (f *singleMember) todo(slug, group string) api.Task {
	f.t.Helper()
	task, err := f.holder.Project.NewTask(f.t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Review a single member", Language: "en"})
	if err != nil {
		f.t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if group != "" {
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nCARD_REVIEW: PASS\n", 1)
	}
	if task, err = f.holder.Project.PutDocument(f.t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		f.t.Fatal(err)
	}
	if task, err = f.holder.Project.Move(f.t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		f.t.Fatal(err)
	}
	return task
}

func (f *singleMember) plan(id string) board.ReviewPlan {
	return board.ReviewPlan{Schema: 1, PlanID: id, Sealed: true, Author: "coordinator", Basis: "single member", CWD: "/host-a/clone", ReportLanguage: "en", TaskIDs: f.ids, Batches: []board.ReviewPlanBatch{{BatchID: singleBatch, TaskIDs: f.ids, Base: "N/A", TargetCommit: "N/A", Requirements: map[string]string{"PMQA": "N/A: fixture", "Security": "N/A: repository policy"}}}}
}

// publish runs a write the holder proves directly.
func (f *singleMember) publish(operation string, plan board.ReviewPlan) {
	f.t.Helper()
	proof, err := f.holder.ReviewAuthority(f.t.Context(), operation, plan, f.ids)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.holder.Project.ReviewPlan(f.t.Context(), client.NewKey(), plan, proof); err != nil {
		f.t.Fatal(err)
	}
}

func (f *singleMember) revision() uint64 {
	f.t.Helper()
	task, err := f.holder.Project.Show(f.t.Context(), f.ids[0])
	if err != nil {
		f.t.Fatal(err)
	}
	return task.Revision
}

func (f *singleMember) requests() []api.CollabRequest {
	f.t.Helper()
	requests, err := f.holder.Project.Requests(f.t.Context(), f.ids[0])
	if err != nil {
		f.t.Fatal(err)
	}
	return requests
}

// awaitHolder checks the wait a coordinator gets, and has the holder confirm
// the publication it names.
func (f *singleMember) awaitHolder(err error) {
	f.t.Helper()
	id := pendingPublicationID(f.t, err)
	for _, want := range []string{f.ids[0], "kander review confirm <absolute-CWD> " + id + " <task-id>"} {
		if !strings.Contains(err.Error(), want) {
			f.t.Fatalf("wait omits %q: %v", want, err)
		}
	}
	if _, err := f.holder.ConfirmReviewPublication(f.t.Context(), id, f.ids[0]); err != nil {
		f.t.Fatal(err)
	}
}

func (f *singleMember) closeBody() map[string]any {
	f.t.Helper()
	raw, err := f.holder.Project.ReviewBatchView(f.t.Context(), singleBatch)
	if err != nil {
		f.t.Fatal(err)
	}
	var view board.ReviewBatchView
	if err := json.Unmarshal(raw, &view); err != nil {
		f.t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: singleBatch, ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		f.t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: "/host-a/clone", Head: "N/A", NotApplicable: "review-plan: all roles explicitly N/A; no Git target", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	return map[string]any{"request": request, "evidence": evidence}
}

func TestCoordinatorSingleMemberAggregateAwaitsHolderConfirmation(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	before := f.revision()
	_, err := f.coordinate.AggregateReview(t.Context(), singleBatch)
	f.awaitHolder(err)
	requests := f.requests()
	if len(requests) != 1 || requests[0].Kind != api.RequestReview || requests[0].TargetGeneration != f.grant.Claim.Generation {
		t.Fatalf("holder was not asked to confirm: %+v", requests)
	}
	if f.revision() != before {
		t.Fatal("waiting for the holder changed the card")
	}
	out, err := f.coordinate.AggregateReview(t.Context(), singleBatch)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.holder.Project.ReviewBatchView(t.Context(), singleBatch)
	if err != nil {
		t.Fatal(err)
	}
	if digest(t, out) != digest(t, raw) {
		t.Fatal("printed view is not the published view")
	}
}

func TestCoordinatorSingleMemberOperationsAwaitHolderConfirmation(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	closure := f.closeBody()
	advance := board.ReviewBatchAdvance{BatchID: singleBatch, ExpectedRevision: 1, Advance: board.ReviewAdvance{PreviousTarget: "N/A", Target: "N/A", Reason: "fixture", Deliveries: map[string]string{}}}
	for _, step := range []struct {
		operation string
		payload   any
	}{
		{"review-plan", f.plan("second-plan")},
		{"review-advance", advance},
		{"review-assign", map[string]string{"run_id": "single-run"}},
		{"review-close:" + singleBatch, closure},
	} {
		t.Run(step.operation, func(t *testing.T) {
			_, err := f.coordinate.ReviewAuthority(t.Context(), step.operation, step.payload, f.ids)
			f.awaitHolder(err)
			proof, err := f.coordinate.ReviewAuthority(t.Context(), step.operation, step.payload, f.ids)
			if err != nil || !strings.HasPrefix(proof.PublicationID, "review-") {
				t.Fatalf("confirmed operation: %+v %v", proof, err)
			}
			if step.operation == "review-close:"+singleBatch {
				if _, err := f.coordinate.Project.ReviewClose(t.Context(), client.NewKey(), closure, proof); err != nil {
					t.Fatalf("confirmed close: %v", err)
				}
			}
		})
	}
}

func TestHolderRunsSingleMemberAggregateWithDirectProof(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	if _, err := f.holder.AggregateReview(t.Context(), singleBatch); err != nil {
		t.Fatalf("holder needed more than its own proof: %v", err)
	}
	var writes []writeRecord
	for _, record := range f.log.take() {
		if strings.HasSuffix(record.Path, "/aggregate") {
			writes = append(writes, record)
		}
	}
	if len(writes) != 1 || writes[0].Claims == "" || writes[0].Publication != "" {
		t.Fatalf("aggregate did not carry only the direct proof: %+v", writes)
	}
	if len(f.requests()) != 0 {
		t.Fatal("holder created a confirmation request for itself")
	}
}

func TestSingleMemberRefusesProcessesThatAreNotHolderOrCoordinator(t *testing.T) {
	for _, step := range []struct {
		name, cardGroup, coordGroup string
		run                         func(*singleMember) error
	}{
		{"disposition", "20261118-single-group", "20261118-single-group", func(f *singleMember) error {
			_, err := f.coordinate.ReviewAuthority(t.Context(), "review-disposition", map[string]string{"run_id": "single-run"}, f.ids)
			return err
		}},
		{"no group", "", "20261118-single-group", func(f *singleMember) error {
			_, err := f.coordinate.AggregateReview(t.Context(), singleBatch)
			return err
		}},
		{"another group", "20261118-single-group", "20261118-other-group", func(f *singleMember) error {
			_, err := f.coordinate.AggregateReview(t.Context(), singleBatch)
			return err
		}},
	} {
		t.Run(step.name, func(t *testing.T) {
			f := newSingleMember(t, step.cardGroup, step.coordGroup)
			err := step.run(f)
			if !api.IsCode(err, api.CodeHeldByOther) {
				t.Fatalf("want held_by_other: %v", err)
			}
			for _, want := range []string{f.grant.Claim.ExecutionID, f.grant.Claim.Owner, "generation " + strconv.FormatUint(f.grant.Claim.Generation, 10)} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal omits %q: %v", want, err)
				}
			}
			if writes := f.log.take(); len(writes) != 0 || len(f.requests()) != 0 {
				t.Fatalf("refused command wrote: %+v", writes)
			}
		})
	}
}
