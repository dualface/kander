package remote_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	clock              *atomic.Int64
	url                string
	task               api.Task
	grant              api.ClaimGrant
	ids                []string
	planned            []string
}

// newSingleMember creates a claimed card in cardGroup and a coordinator
// process of coordGroup. The coordinator of a group other than the card's
// supervises a second card, and an empty cardGroup leaves the card out of
// every group.
func newSingleMember(t *testing.T, cardGroup, coordGroup string) *singleMember {
	t.Helper()
	return newSingleMemberOf(t, cardGroup, coordGroup, false)
}

// newSingleMemberOf with trashed also plans a second card of the group, held by
// the same holder, that leaves the batch before the first review write, so the
// batch keeps two cards and one of them live.
func newSingleMemberOf(t *testing.T, cardGroup, coordGroup string, trashed bool) *singleMember {
	t.Helper()
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	clock := &atomic.Int64{}
	clock.Store(time.Now().UnixNano())
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), TTL: 90 * time.Second, RenewInterval: 15 * time.Second, Now: func() time.Time { return time.Unix(0, clock.Load()) }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
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
	f := &singleMember{t: t, holder: open(a, "holder"), coordinate: open(b, "coordinator"), log: log, clock: clock, url: httpServer.URL}
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
	planned := f.ids
	var second remote.Credential
	if trashed {
		other := f.todo("left-card", cardGroup)
		grant, err := f.holder.Project.Claim(t.Context(), "", other.TaskID, api.ClaimRequest{ClientID: a.ClientID(), ExecutionID: "execution-left", Owner: "codex"})
		if err != nil {
			t.Fatal(err)
		}
		second = remote.Credential{ServerURL: httpServer.URL, ClientID: a.ClientID(), ProjectID: info.ID, TaskID: other.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
		if err := f.holder.SaveCredential(second); err != nil {
			t.Fatal(err)
		}
		// The server sorts a plan's members in place, so a plan written in
		// another order would not match its own receipt.
		planned = slices.Sorted(slices.Values([]string{card.TaskID, other.TaskID}))
	}
	f.planned = planned
	f.publish("review-plan", f.plan("single-plan"))
	if trashed {
		if _, err := f.holder.Project.Move(t.Context(), client.NewKey(), second.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "left the batch", Decision: "fixture decision"}, second.Claim()); err != nil {
			t.Fatal(err)
		}
	}
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
	return board.ReviewPlan{Schema: 1, PlanID: id, Sealed: true, Author: "coordinator", Basis: "single member", CWD: "/host-a/clone", ReportLanguage: "en", TaskIDs: f.planned, Batches: []board.ReviewPlanBatch{{BatchID: singleBatch, TaskIDs: f.planned, Base: "N/A", TargetCommit: "N/A", Requirements: map[string]string{"PMQA": "N/A: fixture", "Security": "N/A: repository policy"}}}}
}

// publish runs a write the holder proves directly.
func (f *singleMember) publish(operation string, plan board.ReviewPlan) {
	f.t.Helper()
	proof, err := f.holder.ReviewAuthority(f.t.Context(), operation, plan, f.planned)
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

// A coordinator that still holds the credential of a replaced generation asks
// the current holder instead of sending that proof.
func TestCoordinatorWithStaleMemberCredentialAsksCurrentHolder(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	stale := remote.Credential{ServerURL: f.url, ClientID: f.coordinate.Client.ClientID(), ProjectID: f.coordinate.Info.ID, TaskID: f.ids[0], ExecutionID: "stale-execution", Generation: f.grant.Claim.Generation, Token: strings.Repeat("a", 64)}
	if err := f.coordinate.SaveCredential(stale); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(91 * time.Second))
	successor, _ := client.New(f.url, "host-c")
	grant, err := successor.Project(f.coordinate.Info.ID).Takeover(t.Context(), "", f.ids[0], api.ClaimRequest{ClientID: "host-c", ExecutionID: "successor", Owner: "codex", ExpectedGeneration: f.grant.Claim.Generation, Decision: "explicit test takeover"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.coordinate.AggregateReview(t.Context(), singleBatch)
	pendingPublicationID(t, err)
	requests := f.requests()
	if len(requests) != 1 || requests[0].TargetGeneration != grant.Claim.Generation {
		t.Fatalf("current holder was not asked: %+v", requests)
	}
}

// Executions on one client home share the replay records of published
// operations, and each still takes its own route.
func TestSharedClientHomeKeepsEachExecutionsRoute(t *testing.T) {
	sharedClientHomeRoutes(t, newSingleMember(t, "20261118-single-group", ""))
}

// The same holds when a second card of the batch left before the first write,
// so the batch keeps two cards and the publication has one member.
func TestSharedClientHomeRoutesAfterAMemberLeftTheBatch(t *testing.T) {
	sharedClientHomeRoutes(t, newSingleMemberOf(t, "20261118-single-group", "", true))
}

func sharedClientHomeRoutes(t *testing.T, f *singleMember) {
	t.Helper()
	const group = "20261118-single-group"
	coordinator := &remote.Session{Client: f.coordinate.Client, Project: f.coordinate.Project, Info: f.coordinate.Info, ServerURL: f.url, Home: f.holder.Home}
	as := func(execution, coordinating string) {
		t.Setenv(remote.EnvExecutionID, execution)
		t.Setenv(remote.EnvCoordinatorExecutionID, coordinating)
	}
	as("coordinator-execution", "coordinator-execution")
	request := board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "label", Basis: "shared home fixture", Members: f.planned}
	if _, err := coordinator.AcquireCoordinator(t.Context(), request, ""); err != nil {
		t.Fatal(err)
	}
	_, err := coordinator.AggregateReview(t.Context(), singleBatch)
	id := pendingPublicationID(t, err)
	as(f.grant.Claim.ExecutionID, "")
	if _, err := f.holder.ConfirmReviewPublication(t.Context(), id, f.ids[0]); err != nil {
		t.Fatal(err)
	}
	as("coordinator-execution", "coordinator-execution")
	if _, err := coordinator.AggregateReview(t.Context(), singleBatch); err != nil {
		t.Fatal(err)
	}

	f.log.take()
	as(f.grant.Claim.ExecutionID, "")
	if _, err := f.holder.AggregateReview(t.Context(), singleBatch); err != nil {
		t.Fatalf("holder did not take its direct route: %v", err)
	}
	for _, record := range f.log.take() {
		if strings.HasSuffix(record.Path, "/aggregate") && (record.Claims == "" || record.Publication != "") {
			t.Fatalf("holder replayed the coordinator's publication: %+v", record)
		}
	}
	as("stranger-execution", "")
	_, err = coordinator.AggregateReview(t.Context(), singleBatch)
	if !api.IsCode(err, api.CodeHeldByOther) {
		t.Fatalf("a process that is neither holder nor coordinator got: %v", err)
	}
	if writes := f.log.take(); len(writes) != 0 {
		t.Fatalf("refused command wrote: %+v", writes)
	}
}

// A retry of a published one-card operation still returns the saved result
// after the holder stopped and released its claim.
func TestPublishedSingleMemberOperationReplaysAfterHolderReleased(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	_, err := f.coordinate.AggregateReview(t.Context(), singleBatch)
	f.awaitHolder(err)
	first, err := f.coordinate.AggregateReview(t.Context(), singleBatch)
	if err != nil {
		t.Fatal(err)
	}
	own, found, err := f.holder.Credential(f.ids[0])
	if err != nil || !found {
		t.Fatalf("holder credential: %v", err)
	}
	state, err := f.holder.Project.ExecutionState(t.Context(), f.ids[0], *own.Claim())
	if err != nil {
		t.Fatal(err)
	}
	stopped := api.AgentObservation{Status: "stopped", Reason: "fixture agent exited"}
	update := api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Presence: &stopped, Decision: "fixture release"}
	if _, err := f.holder.Project.Execution(t.Context(), client.NewKey(), f.ids[0], update, *own.Claim()); err != nil {
		t.Fatal(err)
	}
	if task, err := f.holder.Project.Show(t.Context(), f.ids[0]); err != nil || task.State != "working" || api.ClaimStands(task.Claim.Status) {
		t.Fatalf("fixture did not leave a working card with a lost claim: %+v %v", task, err)
	}
	before := f.revision()
	again, err := f.coordinate.AggregateReview(t.Context(), singleBatch)
	if err != nil || digest(t, again) != digest(t, first) {
		t.Fatalf("saved result was not replayed: %v", err)
	}
	if f.revision() != before {
		t.Fatal("replay changed the card")
	}
}
